package parser

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// Helper parsing methods used across directive parsers.
// These implement the common patterns in Beancount syntax.

// parseDate parses a DATE token and converts it to *ast.Date.
func (p *Parser) parseDate() (*ast.Date, error) {
	tok := p.expect(DATE, "expected date")
	if tok.Type == ILLEGAL {
		return nil, p.errorAtToken(tok, "expected date")
	}

	var date ast.Date
	if err := date.Capture([]string{tok.String(p.source)}); err != nil {
		return nil, p.errorAtToken(tok, "invalid date: %v", err)
	}

	return &date, nil
}

// parseAccount parses an ACCOUNT token and converts it to ast.Account.
// The account name is interned to save memory.
func (p *Parser) parseAccount() (ast.Account, error) {
	tok := p.expect(ACCOUNT, "expected account")
	if tok.Type == ILLEGAL {
		actualTok := p.peek()
		return "", p.errorAtToken(actualTok, "expected account but got %s %q", actualTok.Type, actualTok.String(p.source))
	}

	// Intern account name for memory efficiency
	accountStr := p.interner.InternBytes(tok.Bytes(p.source))

	var account ast.Account
	if err := account.Capture([]string{accountStr}); err != nil {
		return "", p.errorAtToken(tok, "invalid account: %v", err)
	}

	return account, nil
}

// parseAmount parses an amount: NUMBER CURRENCY
func (p *Parser) parseAmount() (*ast.Amount, error) {
	numTok := p.expect(NUMBER, "expected number")
	if numTok.Type == ILLEGAL {
		return nil, p.errorAtToken(numTok, "expected number")
	}

	currTok := p.expect(IDENT, "expected currency")
	if currTok.Type == ILLEGAL {
		return nil, p.errorAtToken(currTok, "expected currency")
	}

	// Intern currency code (USD, EUR, etc.)
	currency := p.interner.InternBytes(currTok.Bytes(p.source))

	return &ast.Amount{
		Value:    numTok.String(p.source),
		Currency: currency,
	}, nil
}

// parseAmountOptional parses an amount with optional currency: NUMBER [CURRENCY]
// If no currency is provided, Currency will be an empty string.
func (p *Parser) parseAmountOptional() (*ast.Amount, error) {
	numTok := p.expect(NUMBER, "expected number")
	if numTok.Type == ILLEGAL {
		return nil, p.errorAtToken(numTok, "expected number")
	}

	currency := ""
	// Currency is optional - only parse if present
	if p.check(IDENT) {
		currTok := p.expect(IDENT, "expected currency")
		currency = p.interner.InternBytes(currTok.Bytes(p.source))
	}

	return &ast.Amount{
		Value:    numTok.String(p.source),
		Currency: currency,
	}, nil
}

// parseCost parses a cost specification: { [*] [AMOUNT] [, DATE] [, LABEL] }
func (p *Parser) parseCost() (*ast.Cost, error) {
	p.consume(LBRACE, "expected '{'")

	cost := &ast.Cost{}

	// Check for merge cost {*}
	if p.match(ASTERISK) {
		cost.IsMerge = true
		p.consume(RBRACE, "expected '}'")
		return cost, nil
	}

	// Check for empty cost {}
	if p.check(RBRACE) {
		p.advance()
		return cost, nil
	}

	// Parse amount if present
	if p.check(NUMBER) {
		amt, err := p.parseAmount()
		if err != nil {
			return nil, err
		}
		cost.Amount = amt
	}

	// Parse optional date
	if p.match(COMMA) {
		if p.check(DATE) {
			date, err := p.parseDate()
			if err != nil {
				return nil, err
			}
			cost.Date = date
		}
	}

	// Parse optional label
	if p.match(COMMA) {
		if p.check(STRING) {
			labelTok := p.advance()
			cost.Label = p.unquoteString(labelTok.String(p.source))
		}
	}

	p.consume(RBRACE, "expected '}'")
	return cost, nil
}

// parseString parses a STRING token and unquotes it.
func (p *Parser) parseString() (string, error) {
	tok := p.expect(STRING, "expected string")
	if tok.Type == ILLEGAL {
		return "", p.errorAtToken(tok, "expected string")
	}

	return p.unquoteString(tok.String(p.source)), nil
}

// parseIdent parses an IDENT token.
func (p *Parser) parseIdent() (string, error) {
	tok := p.expect(IDENT, "expected identifier")
	if tok.Type == ILLEGAL {
		return "", p.errorAtToken(tok, "expected identifier")
	}

	return tok.String(p.source), nil
}

// parseTag parses a TAG token and returns the tag without the # prefix.
func (p *Parser) parseTag() (ast.Tag, error) {
	tok := p.expect(TAG, "expected tag")
	if tok.Type == ILLEGAL {
		return "", p.errorAtToken(tok, "expected tag")
	}

	var tag ast.Tag
	if err := tag.Capture([]string{tok.String(p.source)}); err != nil {
		return "", p.errorAtToken(tok, "invalid tag: %v", err)
	}

	return tag, nil
}

// parseLink parses a LINK token and returns the link without the ^ prefix.
func (p *Parser) parseLink() (ast.Link, error) {
	tok := p.expect(LINK, "expected link")
	if tok.Type == ILLEGAL {
		return "", p.errorAtToken(tok, "expected link")
	}

	var link ast.Link
	if err := link.Capture([]string{tok.String(p.source)}); err != nil {
		return "", p.errorAtToken(tok, "invalid link: %v", err)
	}

	return link, nil
}

// parseMetadata parses metadata entries (key: value pairs).
// Metadata is indented on lines following a directive or posting.
// Metadata keys can be identifiers OR keywords (e.g., "price:", "export:", etc.)
func (p *Parser) parseMetadata() []*ast.Metadata {
	var metadata []*ast.Metadata

	// Metadata lines are key: value where key can be IDENT or any keyword
	for {
		keyTok := p.peek()

		// Check if this could be a metadata key
		// Must be IDENT or a keyword, followed by COLON
		isMetadataKey := (keyTok.Type == IDENT || p.isKeyword(keyTok.Type)) &&
			p.peekAhead(1).Type == COLON

		if !isMetadataKey {
			break
		}

		p.advance() // consume key
		p.consume(COLON, "expected ':'")

		// Read rest of line as value
		value := p.parseRestOfLine()

		// Unquote the value if it's a quoted string
		// The formatter will re-add quotes when formatting
		value = p.unquoteString(value)

		metadata = append(metadata, &ast.Metadata{
			Key:   keyTok.String(p.source),
			Value: value,
		})
	}

	return metadata
}

// isKeyword returns true if the token type is a keyword.
func (p *Parser) isKeyword(typ TokenType) bool {
	switch typ {
	case TXN, BALANCE, OPEN, CLOSE, COMMODITY, PAD, NOTE, DOCUMENT,
		PRICE, EVENT, CUSTOM, OPTION, INCLUDE, PLUGIN,
		PUSHTAG, POPTAG, PUSHMETA, POPMETA:
		return true
	default:
		return false
	}
}

// parseRestOfLine reads all tokens until end of line and returns as string.
func (p *Parser) parseRestOfLine() string {
	currentLine := p.peek().Line

	var parts []string
	for !p.isAtEnd() && p.peek().Line == currentLine {
		tok := p.advance()
		parts = append(parts, tok.String(p.source))
	}

	return strings.TrimSpace(strings.Join(parts, " "))
}

// unquoteString removes surrounding quotes from a string.
func (p *Parser) unquoteString(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// Helper methods for token navigation

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: EOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekAhead(n int) Token {
	pos := p.pos + n
	if pos >= len(p.tokens) {
		return Token{Type: EOF}
	}
	return p.tokens[pos]
}

func (p *Parser) previous() Token {
	if p.pos == 0 {
		return Token{Type: ILLEGAL}
	}
	return p.tokens[p.pos-1]
}

func (p *Parser) isAtEnd() bool {
	return p.peek().Type == EOF
}

func (p *Parser) check(typ TokenType) bool {
	return p.peek().Type == typ
}

func (p *Parser) match(types ...TokenType) bool {
	for _, typ := range types {
		if p.check(typ) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *Parser) advance() Token {
	if !p.isAtEnd() {
		p.pos++
	}
	return p.previous()
}

func (p *Parser) consume(typ TokenType, message string) Token {
	if p.check(typ) {
		return p.advance()
	}

	// Return illegal token and record error
	tok := p.peek()
	_ = p.errorAtToken(tok, "%s", message) // Error recorded, return handled by ILLEGAL token
	return Token{Type: ILLEGAL}
}

func (p *Parser) expect(typ TokenType, message string) Token {
	return p.consume(typ, message)
}

// Error helpers

func (p *Parser) errorAtToken(tok Token, format string, args ...interface{}) error {
	pos := tokenPosition(tok, p.filename)
	return newErrorf(pos, format, args...)
}

func (p *Parser) error(format string, args ...interface{}) error {
	tok := p.peek()
	return p.errorAtToken(tok, format, args...)
}

// tokenPosition extracts position information from a token.
func tokenPosition(tok Token, filename string) ast.Position {
	return ast.Position{
		Filename: filename,
		Offset:   tok.Start,
		Line:     tok.Line,
		Column:   tok.Column,
	}
}
