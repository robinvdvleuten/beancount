package parser

import (
	"context"
	"io"

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

// Parse parses the token stream into an AST.
func (p *Parser) Parse() (*ast.AST, error) {
	tree := &ast.AST{}

	for !p.isAtEnd() {
		// Dispatch based on token type
		switch p.peek().Type {
		case OPTION:
			opt, err := p.parseOption()
			if err != nil {
				return nil, err
			}
			tree.Options = append(tree.Options, opt)

		case INCLUDE:
			inc, err := p.parseInclude()
			if err != nil {
				return nil, err
			}
			tree.Includes = append(tree.Includes, inc)

		case PLUGIN:
			plugin, err := p.parsePlugin()
			if err != nil {
				return nil, err
			}
			tree.Plugins = append(tree.Plugins, plugin)

		case PUSHTAG:
			pushtag, err := p.parsePushtag()
			if err != nil {
				return nil, err
			}
			tree.Pushtags = append(tree.Pushtags, pushtag)

		case POPTAG:
			poptag, err := p.parsePoptag()
			if err != nil {
				return nil, err
			}
			tree.Poptags = append(tree.Poptags, poptag)

		case PUSHMETA:
			pushmeta, err := p.parsePushmeta()
			if err != nil {
				return nil, err
			}
			tree.Pushmetas = append(tree.Pushmetas, pushmeta)

		case POPMETA:
			popmeta, err := p.parsePopmeta()
			if err != nil {
				return nil, err
			}
			tree.Popmetas = append(tree.Popmetas, popmeta)

		case DATE:
			// Date-prefixed directives
			directive, err := p.parseDirective()
			if err != nil {
				return nil, err
			}
			tree.Directives = append(tree.Directives, directive)

		case EOF:
			// Done - loop will exit via !p.isAtEnd()

		default:
			// Skip unknown tokens/lines
			// This handles org-mode headers (* Options) and other non-directive lines
			currentLine := p.peek().Line
			for !p.isAtEnd() && p.peek().Line == currentLine {
				p.advance()
			}
		}
	}

	return tree, nil
}

// parseOption parses: option "key" "value"
func (p *Parser) parseOption() (*ast.Option, error) {
	tok := p.peek()
	p.consume(OPTION, "expected 'option'")

	name, err := p.parseString()
	if err != nil {
		return nil, err
	}

	value, err := p.parseString()
	if err != nil {
		return nil, err
	}

	return &ast.Option{
		Pos:   tokenPosition(tok, p.filename),
		Name:  name,
		Value: value,
	}, nil
}

// parseInclude parses: include "filename"
func (p *Parser) parseInclude() (*ast.Include, error) {
	tok := p.peek()
	p.consume(INCLUDE, "expected 'include'")

	filename, err := p.parseString()
	if err != nil {
		return nil, err
	}

	return &ast.Include{
		Pos:      tokenPosition(tok, p.filename),
		Filename: filename,
	}, nil
}

// parsePlugin parses: plugin "name" ["config"]
func (p *Parser) parsePlugin() (*ast.Plugin, error) {
	tok := p.peek()
	p.consume(PLUGIN, "expected 'plugin'")

	name, err := p.parseString()
	if err != nil {
		return nil, err
	}

	plugin := &ast.Plugin{
		Pos:  tokenPosition(tok, p.filename),
		Name: name,
	}

	// Optional config string
	if p.check(STRING) {
		config, err := p.parseString()
		if err != nil {
			return nil, err
		}
		plugin.Config = config
	}

	return plugin, nil
}

// parsePushtag parses: pushtag #tag
func (p *Parser) parsePushtag() (*ast.Pushtag, error) {
	tok := p.peek()
	p.consume(PUSHTAG, "expected 'pushtag'")

	tag, err := p.parseTag()
	if err != nil {
		return nil, err
	}

	return &ast.Pushtag{
		Pos: tokenPosition(tok, p.filename),
		Tag: tag,
	}, nil
}

// parsePoptag parses: poptag #tag
func (p *Parser) parsePoptag() (*ast.Poptag, error) {
	tok := p.peek()
	p.consume(POPTAG, "expected 'poptag'")

	tag, err := p.parseTag()
	if err != nil {
		return nil, err
	}

	return &ast.Poptag{
		Pos: tokenPosition(tok, p.filename),
		Tag: tag,
	}, nil
}

// parsePushmeta parses: pushmeta key: value
func (p *Parser) parsePushmeta() (*ast.Pushmeta, error) {
	tok := p.peek()
	p.consume(PUSHMETA, "expected 'pushmeta'")

	key, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	p.consume(COLON, "expected ':'")

	value := p.parseRestOfLine()

	return &ast.Pushmeta{
		Pos:   tokenPosition(tok, p.filename),
		Key:   key,
		Value: value,
	}, nil
}

// parsePopmeta parses: popmeta key:
func (p *Parser) parsePopmeta() (*ast.Popmeta, error) {
	tok := p.peek()
	p.consume(POPMETA, "expected 'popmeta'")

	key, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	p.consume(COLON, "expected ':'")

	return &ast.Popmeta{
		Pos: tokenPosition(tok, p.filename),
		Key: key,
	}, nil
}

// parseDirective dispatches to specific directive parsers based on the keyword.
// All directives start with a DATE token.
func (p *Parser) parseDirective() (ast.Directive, error) {
	// Capture position before parsing date
	dateTok := p.peek()
	pos := tokenPosition(dateTok, p.filename)

	date, err := p.parseDate()
	if err != nil {
		return nil, err
	}

	// LL(1) lookahead - deterministic dispatch
	switch p.peek().Type {
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
	case CUSTOM:
		return p.parseCustom(pos, date)
	default:
		return nil, p.error("unknown directive after date")
	}
}

// Public API functions that match the old parser interface

// Parse parses AST from an io.Reader.
// This is a convenience wrapper around ParseBytesWithFilename.
func Parse(ctx context.Context, r io.Reader) (*ast.AST, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseBytesWithFilename(ctx, "", data)
}

// ParseString parses AST from a string.
// This is a convenience wrapper around ParseBytesWithFilename.
func ParseString(ctx context.Context, str string) (*ast.AST, error) {
	return ParseBytesWithFilename(ctx, "", []byte(str))
}

// ParseBytes parses AST from bytes.
// This is a convenience wrapper around ParseBytesWithFilename.
func ParseBytes(ctx context.Context, data []byte) (*ast.AST, error) {
	return ParseBytesWithFilename(ctx, "", data)
}

// ParseBytesWithFilename parses AST from bytes with a filename for position tracking.
func ParseBytesWithFilename(ctx context.Context, filename string, data []byte) (*ast.AST, error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Lex
	lexTimer := telemetry.StartTimer(ctx, "parser.lexing")
	lexer := NewLexer(data, filename)
	tokens := lexer.ScanAll()
	lexTimer.End()

	// Parse
	parseTimer := telemetry.StartTimer(ctx, "parser.parsing")
	parser := NewParser(data, tokens, filename, lexer.Interner())
	tree, err := parser.Parse()
	parseTimer.End()

	if err != nil {
		return nil, err
	}

	// Apply push/pop directives
	pushPopTimer := telemetry.StartTimer(ctx, "parser.push_pop")
	if err := ast.ApplyPushPopDirectives(tree); err != nil {
		pushPopTimer.End()
		return nil, err
	}
	pushPopTimer.End()

	// Sort directives
	sortTimer := telemetry.StartTimer(ctx, "parser.sorting")
	err = ast.SortDirectives(tree)
	sortTimer.End()

	return tree, err
}
