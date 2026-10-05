package parser

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// Parser implements a recursive descent parser for Beancount files.
//
// The recursive descent approach:
// - No backtracking (LL(1) lookahead)
// - Direct AST construction (no reflection)
// - Deterministic parsing

// Parser parses Beancount source into an AST.
type Parser struct {
	source   []byte    // Source buffer
	tokens   []Token   // Token stream from lexer
	pos      int       // Current token position
	filename string    // Filename for error reporting
	interner *Interner // String interning pool
	errs     ParseErrors

	// recovered is set once a syntax error has been recovered from, and
	// recoveredEnd is the offset the recovery skipped to: the end of the
	// last token it dropped. resumedAt is the offset of the token the last
	// recovery resumed at (-1 before any), and resumedMidLine says it was
	// not the first token of a line in column 1 but one that can start a
	// declaration, where Bison resumes wherever it stands.
	recovered      bool
	recoveredEnd   int
	resumedAt      int
	resumedMidLine bool

	lineStarts []int // Byte offset of each line's start, built on the first error

	// options are the options in effect, which, as in beancount's parser,
	// only the file's own options read so far set: the account names every
	// account is checked against and allow_pipe_separator.
	options *config.Config

	// read holds the amounts read since the current declaration started,
	// and dropped those of the declarations a syntax error dropped, which
	// like beancount's parser, which feeds its display context as it reads
	// each amount, still count towards display precision.
	read, dropped []*ast.Amount
	// readAt holds, for each amount in read, the offset of the token after
	// it, so that finishHeader can take back the amounts read past a
	// header's line.
	readAt []int

	// reduceErr is the error the last directive's rule raised as it
	// reduced (an open's invalid booking method), or nil.
	reduceErr *ParseError
}

// NewParser creates a new parser with the given source and tokens.
func NewParser(source []byte, tokens []Token, filename string, interner *Interner) *Parser {
	return &Parser{
		source:    source,
		tokens:    tokens,
		filename:  filename,
		interner:  interner,
		resumedAt: -1,
		options:   config.New(),
	}
}

// Parse parses the token stream into an AST. Like beancount, a syntax error
// drops the directive it is in and parsing goes on; the error is a
// ParseErrors listing each one, returned with the AST of everything else.
func (p *Parser) Parse() (*ast.AST, error) {
	tree := &ast.AST{}

	// Like beancount's grammar, an indented line may only continue the dated
	// directive right above it (as indented comments do); anywhere else at
	// top level it is a syntax error. continuationLine is the line such a
	// comment would have to be on, or 0 when none may follow.
	continuationLine := 0
	// turnedDown is the error of the transaction right above, which
	// beancount's grammar read to its end and then rejected. A line that
	// breaks the transaction off before that end takes the error back.
	var turnedDown *ParseError
	withdraw := func() {
		p.errs = slices.DeleteFunc(p.errs, func(e *ParseError) bool { return e == turnedDown })
	}

	for !p.isAtEnd() {
		tok := p.peek()
		tokType := tok.Type
		continuesDirective := tokType == COMMENT && p.indented(tok) && tok.Line == continuationLine
		// A token recovery resumed at past column 1 has had its
		// indentation reported, as Bison reports the INDENT before it.
		resumed := p.resumedMidLine && tok.Start == p.resumedAt
		if p.indented(tok) && tokType != NEWLINE && tokType != EOF && !continuesDirective && !resumed {
			// A line continuing a dated directive that is none of its
			// lines is a syntax error inside it: like beancount, drop it.
			switch {
			case tok.Line != continuationLine:
			case turnedDown != nil:
				withdraw()
			case len(tree.Directives) > 0:
				tree.Directives = tree.Directives[:len(tree.Directives)-1]
				p.dropRead()
			}
			p.read, p.readAt, p.reduceErr, turnedDown = nil, nil, nil, nil
			if tok.Line == continuationLine {
				// Inside a directive the token is what fails: a string
				// spanning lines where it ends.
				p.recover(p.errorAtToken(tok, "unexpected indentation"))
			} else {
				// At top level the indentation fails, where the line
				// starts.
				pos := tokenPosition(tok, p.filename)
				p.recover(newErrorfWithSource(pos, p.calculateSourceRange(pos), "unexpected indentation"))
			}
			continuationLine = 0
			continue
		}
		// Like an indented line, an invalid token starting the line after a
		// dated directive drops it: beancount's lexer returns its error
		// while the grammar still waits to see whether the directive goes
		// on, and Bison's recovery discards the directive.
		if tok.Line == continuationLine && p.lexerErrorAt(tok.Start) {
			if turnedDown != nil {
				withdraw()
			} else if len(tree.Directives) > 0 {
				tree.Directives = tree.Directives[:len(tree.Directives)-1]
				p.dropRead()
			}
		}
		p.read, p.readAt, p.reduceErr = nil, nil, nil
		if !continuesDirective {
			turnedDown = nil
		}
		if tokType != DATE && !continuesDirective {
			continuationLine = 0
		}

		// Dispatch by token type
		switch tokType {
		case COMMENT:
			comment := p.parseComment()
			tree.Comments = append(tree.Comments, comment)
			if continuationLine != 0 {
				continuationLine = tok.Line + 1
			}

		case NEWLINE:
			blankLine := p.parseBlankLine()
			tree.BlankLines = append(tree.BlankLines, blankLine)

		case OPTION:
			opt, err := p.parseOption()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Options = append(tree.Options, opt)
			// Its errors are the ledger's and the loader's to report.
			p.options.ApplyOption(opt)

		case INCLUDE:
			inc, err := p.parseInclude()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Includes = append(tree.Includes, inc)

		case PLUGIN:
			plugin, err := p.parsePlugin()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Plugins = append(tree.Plugins, plugin)

		case PUSHTAG:
			pushtag, err := p.parsePushtag()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Pushtags = append(tree.Pushtags, pushtag)

		case POPTAG:
			poptag, err := p.parsePoptag()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Poptags = append(tree.Poptags, poptag)

		case PUSHMETA:
			pushmeta, err := p.parsePushmeta()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Pushmetas = append(tree.Pushmetas, pushmeta)

		case POPMETA:
			popmeta, err := p.parsePopmeta()
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Popmetas = append(tree.Popmetas, popmeta)

		case DATE:
			directive, err := p.parseDirective()
			var rejection rejected
			if errors.As(err, &rejection) {
				errors.As(rejection.error, &turnedDown)
				p.errs = append(p.errs, turnedDown)
				p.dropRead()
				continuationLine = p.lineAfterPrevious()
				continue
			}
			if err != nil {
				p.recover(err)
				continuationLine = 0
				continue
			}
			tree.Directives = append(tree.Directives, directive)
			continuationLine = p.lineAfterPrevious()

		case EOF:
			// Done - loop will exit via !p.isAtEnd()

		default:
			tok := p.peek()
			if tok.Type == ACCOUNT && lexerRejectsAccount(tok.Bytes(p.source)) {
				p.recover(p.errorAtToken(tok, "invalid token %q", tok.String(p.source)))
			} else {
				p.recover(p.errorAtToken(tok, "unexpected token %s %q", tok.Type, tok.String(p.source)))
			}
			continuationLine = 0
		}
	}

	tree.DroppedAmounts = p.dropped
	if len(p.errs) > 0 {
		return tree, p.errs
	}
	return tree, nil
}

// recover records a syntax error and skips to the next line that starts in
// column 1, dropping the rest of the directive the error is in, like
// beancount's grammar. Parsing resumes there, or, like Bison, which retries
// each token after the error, at an earlier token that can start a
// declaration: the token that raised the error, one later on its line, or
// one on an indented line. Like beancount's lexer, which reports each
// invalid token whatever the grammar is doing, every invalid token it skips
// past the error is reported too.
//
// Like the Bison parser beancount generates, a grammar error is reported
// only once three tokens have been shifted since the last error, reported
// or not (see shiftedSince); an invalid token is its lexer's error and is
// always reported.
func (p *Parser) recover(err error) {
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		parseErr = NewParseError(p.filename, err)
	}
	raisedAt := parseErr.Pos.Offset
	if parseErr.raisedAt != 0 {
		raisedAt = parseErr.raisedAt
	}
	if !p.recovered || p.lexerErrorAt(parseErr.Pos.Offset) || p.shiftedSince(raisedAt) >= 3 {
		p.errs = append(p.errs, parseErr)
	}
	p.recovered = true
	p.recoveredEnd = max(p.recoveredEnd, parseErr.Pos.Offset)
	// A header that goes on over several lines was read past its error:
	// to beancount each of those lines is an error of its own, so the
	// recovery reaches to the last of them.
	if p.pos > 0 && p.tokens[p.pos-1].Start > parseErr.Pos.Offset {
		p.recoveredEnd = max(p.recoveredEnd, p.contentEnd(p.tokens[p.pos-1]))
	}
	p.dropRead()
	for !p.isAtEnd() {
		tok := p.peek()
		if tok.Line > parseErr.Pos.Line && !p.indented(tok) {
			p.resumedAt, p.resumedMidLine = tok.Start, false
			return
		}
		// A declaration that fails at its own first token is skipped, so
		// recovery always moves on.
		if tok.Start >= parseErr.Pos.Offset && startsDeclaration(tok.Type) && tok.Start != p.resumedAt {
			p.resumedAt, p.resumedMidLine = tok.Start, true
			return
		}
		if tok.Start > parseErr.Pos.Offset {
			var skipped *ParseError
			switch {
			case tok.Type == ILLEGAL && p.lexerRejects(tok):
				errors.As(p.errorAtToken(tok, ""), &skipped)
			case tok.Type == ACCOUNT && lexerRejectsAccount(tok.Bytes(p.source)):
				errors.As(p.errorAtToken(tok, "invalid token %q", tok.String(p.source)), &skipped)
			case tok.Type == STRING && !utf8.Valid(tok.Bytes(p.source)):
				errors.As(p.invalidStringError(tok), &skipped)
			}
			if skipped != nil {
				p.errs = append(p.errs, skipped)
			}
		}
		if tok.Type != NEWLINE {
			p.recoveredEnd = max(p.recoveredEnd, p.contentEnd(tok))
		}
		p.advance()
	}
}

// contentEnd returns where a token's text ends, before the line break a
// comment owns: shiftedSince counts from that line break.
func (p *Parser) contentEnd(tok Token) int {
	end := tok.End
	for end > tok.Start && (p.source[end-1] == '\n' || p.source[end-1] == '\r') {
		end--
	}
	return end
}

// dropRead records the amounts read in a declaration being dropped, and
// withdraws the error its rule raised on reducing: beancount's grammar never
// reduces a directive whose body holds a syntax error.
func (p *Parser) dropRead() {
	p.dropped = append(p.dropped, p.read...)
	p.read, p.readAt = nil, nil
	if p.reduceErr != nil {
		p.errs = slices.DeleteFunc(p.errs, func(e *ParseError) bool { return e == p.reduceErr })
		p.reduceErr = nil
	}
}

// lexerErrorAt reports whether the token at offset is one beancount's lexer
// rejects as an invalid token, rather than one its grammar rejects.
func (p *Parser) lexerErrorAt(offset int) bool {
	i := sort.Search(len(p.tokens), func(i int) bool { return p.tokens[i].Start >= offset })
	if i == len(p.tokens) || p.tokens[i].Start != offset {
		return false
	}
	tok := p.tokens[i]
	return (tok.Type == ILLEGAL && p.lexerRejects(tok)) ||
		(tok.Type == ACCOUNT && lexerRejectsAccount(tok.Bytes(p.source)))
}

// shiftedSince counts the tokens beancount's grammar shifts after the last
// recovery, up to the error at offset end. Resumed at a token that can
// start a declaration, Bison shifts from that token on; otherwise it drops
// the rest of the erroneous line and shifts its line break (an indented
// line after it is an error of its own, so recovery skips it too). From
// there each line break counts, as does an indented line's INDENT and
// every token but a comment, a signed number counting its sign.
func (p *Parser) shiftedSince(end int) int {
	start, line := p.resumedAt, 0
	if p.resumedMidLine {
		// The resumed token's INDENT, if any, was dropped with the error.
		line = p.tokens[p.tokenIndexAt(start)].Line
	} else if lineEnd := bytes.IndexByte(p.source[p.recoveredEnd:], '\n'); lineEnd >= 0 {
		start = p.recoveredEnd + lineEnd
	} else {
		start = len(p.source)
	}
	if end <= start {
		return 0
	}
	n := bytes.Count(p.source[start:end], []byte("\n"))
	for i := p.tokenIndexAt(start); i < len(p.tokens) && p.tokens[i].Start < end; i++ {
		tok := p.tokens[i]
		if tok.Type == NEWLINE || tok.Type == EOF {
			continue
		}
		if tok.Line != line {
			line = tok.Line
			if p.indented(tok) {
				n++ // INDENT
			}
		}
		if tok.Type == COMMENT {
			continue
		}
		n++
		if tok.Type == NUMBER && (p.source[tok.Start] == '-' || p.source[tok.Start] == '+') {
			n++
		}
	}
	return n
}

// tokenIndexAt returns the index of the first token starting at or after
// offset.
func (p *Parser) tokenIndexAt(offset int) int {
	return sort.Search(len(p.tokens), func(i int) bool { return p.tokens[i].Start >= offset })
}

// startsDeclaration reports whether a token of type t can start one of
// beancount's declarations: a dated directive or an undated line.
func startsDeclaration(t TokenType) bool {
	switch t {
	case DATE, OPTION, INCLUDE, PLUGIN, PUSHTAG, POPTAG, PUSHMETA, POPMETA:
		return true
	}
	return false
}

// parseComment parses a comment token into a Comment AST node.
func (p *Parser) parseComment() *ast.Comment {
	tok := p.advance()
	content := tok.String(p.source)

	// Lexer includes the trailing newline in comment tokens.
	// Strip it to keep Comment.Content semantic (comment text without line terminator).
	content = strings.TrimSuffix(content, "\r\n")
	content = strings.TrimSuffix(content, "\n")

	// Determine comment type by checking if next token is a NEWLINE
	commentType := ast.StandaloneComment
	if !p.isAtEnd() && p.peek().Type == NEWLINE {
		commentType = ast.SectionComment
	}

	comment := &ast.Comment{
		Content: content,
		Type:    commentType,
	}
	comment.SetPosition(ast.Position{
		Filename: p.filename,
		Offset:   tok.Start,
		Line:     tok.Line,
		Column:   tok.Column,
	})
	return comment
}

// parseBlankLine parses a newline token into a BlankLine AST node.
func (p *Parser) parseBlankLine() *ast.BlankLine {
	tok := p.advance()
	blankLine := &ast.BlankLine{}
	blankLine.SetPosition(ast.Position{
		Filename: p.filename,
		Offset:   tok.Start,
		Line:     tok.Line,
		Column:   tok.Column,
	})
	return blankLine
}

// parseOption parses: option "key" "value"
func (p *Parser) parseOption() (*ast.Option, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(OPTION, "expected 'option'"); err != nil {
		return nil, err
	}

	name, err := p.parseString()
	if err != nil {
		return nil, err
	}

	value, err := p.parseString()
	if err != nil {
		return nil, err
	}

	opt := &ast.Option{
		Name:  name,
		Value: value,
	}
	opt.SetPosition(pos)
	if err := p.finishHeader(opt, pos.Offset); err != nil {
		return nil, err
	}
	return opt, nil
}

// parseInclude parses: include "filename"
func (p *Parser) parseInclude() (*ast.Include, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(INCLUDE, "expected 'include'"); err != nil {
		return nil, err
	}

	filename, err := p.parseString()
	if err != nil {
		return nil, err
	}

	inc := &ast.Include{
		Filename: filename,
	}
	inc.SetPosition(pos)
	if err := p.finishHeader(inc, pos.Offset); err != nil {
		return nil, err
	}
	return inc, nil
}

// parsePlugin parses: plugin "name" ["config"]
func (p *Parser) parsePlugin() (*ast.Plugin, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(PLUGIN, "expected 'plugin'"); err != nil {
		return nil, err
	}

	name, err := p.parseString()
	if err != nil {
		return nil, err
	}

	plugin := &ast.Plugin{
		Name: name,
	}
	plugin.SetPosition(pos)

	// Optional config string, on the plugin's own line: a string on the
	// next line is no part of it, and the plugin is read without one.
	if p.check(STRING) && p.continuesPreviousLine() {
		config, err := p.parseString()
		if err != nil {
			return nil, err
		}
		plugin.Config = config
	}

	if err := p.finishHeader(plugin, pos.Offset); err != nil {
		return nil, err
	}
	return plugin, nil
}

// parsePushtag parses: pushtag #tag
func (p *Parser) parsePushtag() (*ast.Pushtag, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(PUSHTAG, "expected 'pushtag'"); err != nil {
		return nil, err
	}

	tag, err := p.parseTag()
	if err != nil {
		return nil, err
	}

	pt := &ast.Pushtag{
		Tag: tag,
	}
	pt.SetPosition(pos)
	if err := p.finishHeader(pt, pos.Offset); err != nil {
		return nil, err
	}
	return pt, nil
}

// parsePoptag parses: poptag #tag
func (p *Parser) parsePoptag() (*ast.Poptag, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(POPTAG, "expected 'poptag'"); err != nil {
		return nil, err
	}

	tag, err := p.parseTag()
	if err != nil {
		return nil, err
	}

	pt := &ast.Poptag{
		Tag: tag,
	}
	pt.SetPosition(pos)
	if err := p.finishHeader(pt, pos.Offset); err != nil {
		return nil, err
	}
	return pt, nil
}

// parsePushmeta parses: pushmeta key: value
func (p *Parser) parsePushmeta() (*ast.Pushmeta, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(PUSHMETA, "expected 'pushmeta'"); err != nil {
		return nil, err
	}

	key, err := p.parseMetadataKey()
	if err != nil {
		return nil, err
	}

	// Parse the value like any metadata value, then rewind: the source text
	// is kept for the formatter.
	// Like beancount's grammar, the value is one metadata value and the
	// line ends after it: anything more is a syntax error, which drops
	// the pushmeta.
	start := p.pos
	value, err := p.parseMetadataValue(pos.Line)
	if err != nil {
		return nil, err
	}
	if next := p.peek(); next.Type != EOF && next.Type != COMMENT && next.Line == pos.Line {
		return nil, p.errorAtToken(next, "unexpected token %s %q", next.Type, next.String(p.source))
	}
	p.pos = start

	pm := &ast.Pushmeta{
		Key:       key,
		Value:     p.parseRestOfLineUntilComment(pos.Line),
		MetaValue: value,
		// NULL, or no value at all, which beancount pushes as None too.
		Null: value == nil,
	}
	pm.SetPosition(pos)
	if err := p.finishHeader(pm, pos.Offset); err != nil {
		return nil, err
	}
	return pm, nil
}

// parsePopmeta parses: popmeta key:
func (p *Parser) parsePopmeta() (*ast.Popmeta, error) {
	pos := p.tokenPositionFromPeek()
	if err := p.consume(POPMETA, "expected 'popmeta'"); err != nil {
		return nil, err
	}

	key, err := p.parseMetadataKey()
	if err != nil {
		return nil, err
	}

	pm := &ast.Popmeta{
		Key: key,
	}
	pm.SetPosition(pos)
	if err := p.finishHeader(pm, pos.Offset); err != nil {
		return nil, err
	}
	return pm, nil
}

// parseDirective dispatches to specific directive parsers based on the keyword.
// All directives start with a DATE token.
func (p *Parser) parseDirective() (ast.Directive, error) {
	// Parse date first (no position capture yet)
	dateTok := p.peek()
	date, err := p.parseDate()
	if err != nil {
		return nil, err
	}

	if p.isAtEnd() {
		err := p.errorAtToken(dateTok, "unexpected end of file after date")
		// beancount's grammar reads the date and fails after it.
		var parseErr *ParseError
		if errors.As(err, &parseErr) {
			parseErr.raisedAt = dateTok.End
		}
		return nil, err
	}
	if p.peek().Line != dateTok.Line {
		return nil, p.errorAtEndOfPrevious("unexpected end of line after date")
	}

	// Check that next token is properly separated from date (whitespace required)
	nextTok := p.peek()
	if nextTok.Line == dateTok.Line && nextTok.Column == dateTok.Column+dateTok.Len() {
		return nil, p.errorAtToken(nextTok, "whitespace required between date and directive")
	}

	// Capture position from directive keyword token
	directiveTok := p.peek()
	pos := p.tokenPositionFromPeek()

	// LL(1) lookahead - dispatch via registry
	if isFlagToken(directiveTok) {
		return p.parseTransaction(pos, date)
	}
	switch directiveTok.Type {
	case TXN, ASTERISK, EXCLAIM:
		return p.parseTransaction(pos, date)
	case BALANCE:
		return p.parseBalance(pos, date)
	case OPEN:
		return p.parseOpen(pos, date)
	case CLOSE:
		return p.parseClose(pos, date)
	case COMMODITY:
		return p.parseCommodity(pos, date)
	case PAD:
		return p.parsePad(pos, date)
	case NOTE:
		return p.parseNote(pos, date)
	case DOCUMENT:
		return p.parseDocument(pos, date)
	case PRICE:
		return p.parsePrice(pos, date)
	case EVENT:
		return p.parseEvent(pos, date)
	case QUERY:
		return p.parseQuery(pos, date)
	case CUSTOM:
		return p.parseCustom(pos, date)
	default:
		return nil, p.error("unknown directive after date")
	}
}

// Public API functions parse source syntax into raw ASTs.

// Parse parses a raw source AST from an io.Reader.
// This is a convenience wrapper around ParseBytesWithFilename.
func Parse(ctx context.Context, r io.Reader) (*ast.AST, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytesWithFilename(ctx, "", data)
}

// ParseString parses a raw source AST from a string.
// This is a convenience wrapper around ParseBytesWithFilename.
func ParseString(ctx context.Context, str string) (*ast.AST, error) {
	return ParseBytesWithFilename(ctx, "", []byte(str))
}

// MustParseString parses AST from a string, panicking on an error that
// drops part of it (an error that keeps its directive does not).
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ast := parser.MustParseString(context.Background(), "2024-01-01 open Assets:Checking")
func MustParseString(ctx context.Context, str string) *ast.AST {
	result, err := ParseString(ctx, str)
	if err := dropping(err); err != nil {
		panic(err)
	}
	return result
}

// ParseBytes parses a raw source AST from bytes.
// This is a convenience wrapper around ParseBytesWithFilename.
func ParseBytes(ctx context.Context, data []byte) (*ast.AST, error) {
	return ParseBytesWithFilename(ctx, "", data)
}

// MustParseBytes parses AST from bytes, panicking on an error that drops
// part of it.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ast := parser.MustParseBytes(context.Background(), []byte("2024-01-01 open Assets:Checking"))
func MustParseBytes(ctx context.Context, data []byte) *ast.AST {
	result, err := ParseBytes(ctx, data)
	if err := dropping(err); err != nil {
		panic(err)
	}
	return result
}

// ParseBytesWithFilename parses a raw source AST from bytes with a filename for position tracking.
// Parsing preserves directive source order and does not apply semantic push/pop directives.
// On syntax errors it returns a ParseErrors together with the AST of the
// directives that parsed.
func ParseBytesWithFilename(ctx context.Context, filename string, data []byte) (*ast.AST, error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Extract telemetry collector
	collector := telemetry.FromContext(ctx)

	// Lex
	lexTimer := collector.Start("parser.lexing")
	lexer := NewLexer(data, filename)
	tokens, err := lexer.ScanAll()
	lexTimer.End()

	if err != nil {
		return nil, err
	}

	// Parse
	parseTimer := collector.Start("parser.parsing")
	parser := NewParser(data, tokens, filename, lexer.Interner())
	tree, err := parser.Parse()
	parseTimer.End()

	// A ParseErrors comes with the AST of everything that parsed.
	return tree, err
}

// MustParseBytesWithFilename parses AST from bytes with a filename,
// panicking on an error that drops part of it.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ast := parser.MustParseBytesWithFilename(context.Background(), "main.beancount", data)
func MustParseBytesWithFilename(ctx context.Context, filename string, data []byte) *ast.AST {
	result, err := ParseBytesWithFilename(ctx, filename, data)
	if err := dropping(err); err != nil {
		panic(err)
	}
	return result
}

// dropping returns err unless it lists only errors that keep their
// directive, which leave the AST whole.
func dropping(err error) error {
	var syntaxErrs ParseErrors
	if errors.As(err, &syntaxErrs) && syntaxErrs.FirstDropping() == nil {
		return nil
	}
	return err
}
