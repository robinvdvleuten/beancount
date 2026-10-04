package parser

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/ast"
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
	// last token it dropped.
	recovered    bool
	recoveredEnd int

	lineStarts []int // Byte offset of each line's start, built on the first error
}

// NewParser creates a new parser with the given source and tokens.
func NewParser(source []byte, tokens []Token, filename string, interner *Interner) *Parser {
	return &Parser{
		source:   source,
		tokens:   tokens,
		filename: filename,
		interner: interner,
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

	for !p.isAtEnd() {
		tok := p.peek()
		tokType := tok.Type
		continuesDirective := tokType == COMMENT && p.indented(tok) && tok.Line == continuationLine
		if p.indented(tok) && tokType != NEWLINE && tokType != EOF && !continuesDirective {
			// A line continuing a dated directive that is none of its
			// lines is a syntax error inside it: like beancount, drop it.
			if tok.Line == continuationLine && len(tree.Directives) > 0 {
				tree.Directives = tree.Directives[:len(tree.Directives)-1]
			}
			p.recover(p.errorAtToken(tok, "unexpected indentation"))
			continuationLine = 0
			continue
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

	if len(p.errs) > 0 {
		return tree, p.errs
	}
	return tree, nil
}

// recover records a syntax error and skips to the next line that starts in
// column 1, dropping the rest of the directive the error is in, like
// beancount's grammar. Parsing resumes there. Like beancount's lexer, which
// reports each invalid token whatever the grammar is doing, every invalid
// token it skips past the error is reported too.
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
	if !p.recovered || p.lexerErrorAt(parseErr.Pos.Offset) || p.shiftedSince(p.recoveredEnd, parseErr.Pos.Offset) >= 3 {
		p.errs = append(p.errs, parseErr)
	}
	p.recovered = true
	p.recoveredEnd = max(p.recoveredEnd, parseErr.Pos.Offset)
	for !p.isAtEnd() {
		tok := p.peek()
		if tok.Line > parseErr.Pos.Line && !p.indented(tok) {
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
			p.recoveredEnd = max(p.recoveredEnd, tok.End)
		}
		p.advance()
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

// shiftedSince counts the tokens beancount's grammar shifts after recovering
// from a syntax error that ends at offset start, up to the error at offset
// end. Bison drops the rest of the erroneous line and shifts its line break
// (an indented line after it is an error of its own, so recovery skips it
// too); from there each line break counts, as does an indented line's
// INDENT and every token but a comment, a signed number counting its sign.
func (p *Parser) shiftedSince(start, end int) int {
	if lineEnd := bytes.IndexByte(p.source[start:], '\n'); lineEnd >= 0 {
		start += lineEnd
	} else {
		start = len(p.source)
	}
	if end <= start {
		return 0
	}
	n := bytes.Count(p.source[start:end], []byte("\n"))
	line := 0
	for i := sort.Search(len(p.tokens), func(i int) bool { return p.tokens[i].Start >= start }); i < len(p.tokens) && p.tokens[i].Start < end; i++ {
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

	// Optional config string
	if p.check(STRING) {
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
	start := p.pos
	value, err := p.parseMetadataValue(pos.Line)
	single := err == nil
	if next := p.peek(); next.Type != EOF && next.Type != COMMENT && next.Line == pos.Line {
		single = false
	}
	if !single {
		value = nil // Not a single value; applied as its source text
	}
	p.pos = start

	pm := &ast.Pushmeta{
		Key:       key,
		Value:     p.parseRestOfLineUntilComment(pos.Line),
		MetaValue: value,
		// NULL, or no value at all, which beancount pushes as None too.
		Null: single && value == nil,
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
		return nil, p.errorAtToken(dateTok, "unexpected end of file after date")
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

// MustParseString parses AST from a string, panicking on error.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ast := parser.MustParseString(context.Background(), "2024-01-01 open Assets:Checking")
func MustParseString(ctx context.Context, str string) *ast.AST {
	result, err := ParseString(ctx, str)
	if err != nil {
		panic(err)
	}
	return result
}

// ParseBytes parses a raw source AST from bytes.
// This is a convenience wrapper around ParseBytesWithFilename.
func ParseBytes(ctx context.Context, data []byte) (*ast.AST, error) {
	return ParseBytesWithFilename(ctx, "", data)
}

// MustParseBytes parses AST from bytes, panicking on error.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ast := parser.MustParseBytes(context.Background(), []byte("2024-01-01 open Assets:Checking"))
func MustParseBytes(ctx context.Context, data []byte) *ast.AST {
	result, err := ParseBytes(ctx, data)
	if err != nil {
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

// MustParseBytesWithFilename parses AST from bytes with a filename, panicking on error.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ast := parser.MustParseBytesWithFilename(context.Background(), "main.beancount", data)
func MustParseBytesWithFilename(ctx context.Context, filename string, data []byte) *ast.AST {
	result, err := ParseBytesWithFilename(ctx, filename, data)
	if err != nil {
		panic(err)
	}
	return result
}
