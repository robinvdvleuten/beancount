package parser

import (
	"fmt"
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
		return "", p.errorAtEndOfPrevious("expected account but got %s %q", actualTok.Type, actualTok.String(p.source))
	}

	// Intern account name for memory efficiency
	accountStr := p.interner.InternBytes(tok.Bytes(p.source))

	var account ast.Account
	if err := account.Capture([]string{accountStr}); err != nil {
		return "", p.errorAtToken(tok, "invalid account: %v", err)
	}

	return account, nil
}

// parseAmount parses an amount: NUMBER CURRENCY or (EXPRESSION) CURRENCY
// Expressions are captured as-is (not evaluated) and stored in Amount.Value.
// The ledger phase evaluates expressions when computing balances.
func (p *Parser) parseAmount() (*ast.Amount, error) {
	var value string

	// Check if next character in source is '(' (start of expression)
	// We need to look at the raw source since '(' is not tokenized
	tok := p.peek()
	if tok.Start < len(p.source) && p.source[tok.Start] == '(' {
		// Capture expression text without evaluating
		value = p.parseExpression()
	} else {
		// Plain number
		numTok := p.peek()
		if numTok.Type == NUMBER {
			p.advance()
		} else {
			numTok = p.expect(NUMBER, "expected number")
			if numTok.Type == ILLEGAL {
				return nil, p.errorAtToken(numTok, "expected number or expression")
			}
		}
		value = numTok.String(p.source)
	}

	// Parse currency (same for both plain and expression)
	currTok := p.expect(IDENT, "expected currency")
	if currTok.Type == ILLEGAL {
		return nil, p.errorAtEndOfPrevious("expected currency")
	}

	// Intern currency code (USD, EUR, etc.)
	currency := p.interner.InternBytes(currTok.Bytes(p.source))

	return &ast.Amount{
		Value:    value,
		Currency: currency,
	}, nil
}

// parseExpression captures an expression's text from source without evaluating it.
// Scans the source starting from current position to find matching parentheses.
// Returns the full expression text including parentheses: "(5 + 3)"
func (p *Parser) parseExpression() string {
	startPos := p.peek().Start

	// Scan through source to find matching closing paren
	pos := startPos
	if pos >= len(p.source) || p.source[pos] != '(' {
		// Should not happen, but return empty string
		return ""
	}

	depth := 0
	for pos < len(p.source) {
		ch := p.source[pos]
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				// Found matching closing paren
				endPos := pos + 1
				exprText := string(p.source[startPos:endPos])

				// Now consume all the tokens that were part of this expression
				// We need to advance past the expression in the token stream
				for !p.isAtEnd() && p.peek().Start < endPos {
					p.advance()
				}

				return exprText
			}
		}
		pos++
	}

	// Unmatched parentheses - return what we have
	return string(p.source[startPos:pos])
}

// parseCost parses a cost specification: { [*] [AMOUNT] [, DATE] [, LABEL] } or {{ AMOUNT [, DATE] [, LABEL] }}
func (p *Parser) parseCost() (*ast.Cost, error) {
	// Check for {{ or {
	isTotal := false
	if p.check(LDBRACE) {
		p.advance() // consume {{
		isTotal = true
	} else {
		p.consume(LBRACE, "expected '{' or '{{'")
	}

	cost := &ast.Cost{IsTotal: isTotal}

	// Check for merge cost {*} (only valid with single braces)
	if p.match(ASTERISK) {
		if isTotal {
			return nil, p.error("merge cost {*} cannot use total cost syntax {{}}")
		}
		cost.IsMerge = true
		p.consume(RBRACE, "expected '}'")
		return cost, nil
	}

	// Determine closing token
	closingToken := RBRACE
	if isTotal {
		closingToken = RDBRACE
	}

	// Check for empty cost (only valid with single braces)
	if p.check(closingToken) {
		if isTotal {
			return nil, p.error("empty total cost {{}} is not allowed")
		}
		p.advance()
		return cost, nil
	}

	// Parse amount (required for total cost)
	if p.check(NUMBER) {
		amt, err := p.parseAmount()
		if err != nil {
			return nil, err
		}
		cost.Amount = amt
	} else if isTotal {
		return nil, p.error("total cost {{}} requires an amount")
	}

	// Parse optional date and/or label
	if p.match(COMMA) {
		if p.check(DATE) {
			// Parse date
			date, err := p.parseDate()
			if err != nil {
				return nil, err
			}
			cost.Date = date

			// Check for another comma and label
			if p.match(COMMA) {
				if p.check(STRING) {
					labelTok := p.advance()
					label, err := p.unquoteString(labelTok.String(p.source))
					if err != nil {
						return nil, p.errorAtToken(labelTok, "invalid string literal: %v", err)
					}
					cost.Label = label
				}
			}
		} else if p.check(STRING) {
			// Parse label directly (no date)
			labelTok := p.advance()
			label, err := p.unquoteString(labelTok.String(p.source))
			if err != nil {
				return nil, p.errorAtToken(labelTok, "invalid string literal: %v", err)
			}
			cost.Label = label
		}
	}

	// Consume closing brace(s)
	if isTotal {
		p.consume(RDBRACE, "expected '}}'")
	} else {
		p.consume(RBRACE, "expected '}'")
	}

	return cost, nil
}

// parseString parses a STRING token and unquotes it.
func (p *Parser) parseString() (string, error) {
	tok := p.expect(STRING, "expected string")
	if tok.Type == ILLEGAL {
		return "", p.errorAtEndOfPrevious("expected string")
	}

	unquoted, err := p.unquoteString(tok.String(p.source))
	if err != nil {
		return "", p.errorAtToken(tok, "invalid string literal: %v", err)
	}

	return p.interner.Intern(unquoted), nil
}

// parseIdent parses an IDENT token.
func (p *Parser) parseIdent() (string, error) {
	tok := p.expect(IDENT, "expected identifier")
	if tok.Type == ILLEGAL {
		return "", p.errorAtEndOfPrevious("expected identifier")
	}

	return tok.String(p.source), nil
}

// parseTag parses a TAG token and returns the tag without the # prefix.
func (p *Parser) parseTag() (ast.Tag, error) {
	tok := p.expect(TAG, "expected tag")
	if tok.Type == ILLEGAL {
		return "", p.errorAtEndOfPrevious("expected tag")
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
		return "", p.errorAtEndOfPrevious("expected link")
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
		// Must be IDENT or a keyword, followed by COLON immediately (no whitespace)
		isMetadataKey := (keyTok.Type == IDENT || p.isKeyword(keyTok.Type)) &&
			p.peekAhead(1).Type == COLON &&
			keyTok.Column+keyTok.Len() == p.peekAhead(1).Column

		if !isMetadataKey {
			break
		}

		p.advance() // consume key
		p.consume(COLON, "expected ':'")

		// Parse the metadata value based on token type
		value := p.parseMetadataValue()

		metadata = append(metadata, &ast.Metadata{
			Key:   keyTok.String(p.source),
			Value: value,
		})
	}

	return metadata
}

// parseMetadataValue parses a typed metadata value. Beancount supports 8 value types:
// strings, dates, accounts, currencies, tags, links, numbers, amounts, and booleans.
func (p *Parser) parseMetadataValue() *ast.MetadataValue {
	tok := p.peek()

	// Parse based on token type with specific-to-general order
	switch tok.Type {
	case STRING:
		// String (quoted) - most specific
		str, err := p.parseString()
		if err == nil {
			return &ast.MetadataValue{StringValue: &str}
		}

	case DATE:
		// Date (ISO format)
		date, err := p.parseDate()
		if err == nil {
			return &ast.MetadataValue{Date: date}
		}

	case TAG:
		// Tag (with # prefix)
		tag, err := p.parseTag()
		if err == nil {
			return &ast.MetadataValue{Tag: &tag}
		}

	case LINK:
		// Link (with ^ prefix)
		link, err := p.parseLink()
		if err == nil {
			return &ast.MetadataValue{Link: &link}
		}

	case ACCOUNT:
		// Account (colon-separated)
		account, err := p.parseAccount()
		if err == nil {
			return &ast.MetadataValue{Account: &account}
		}

	case NUMBER:
		// Could be Number or Amount - need LL(2) lookahead
		if p.peekAhead(1).Type == IDENT {
			// Amount (number + currency)
			amount, err := p.parseAmount()
			if err == nil {
				return &ast.MetadataValue{Amount: amount}
			}
		} else {
			// Just a number
			numStr := tok.String(p.source)
			p.advance()
			return &ast.MetadataValue{Number: &numStr}
		}

	case IDENT:
		// Could be Account, Currency, or Boolean
		identStr := tok.String(p.source)

		// Check for Boolean (TRUE/FALSE)
		if identStr == "TRUE" {
			p.advance()
			trueVal := true
			return &ast.MetadataValue{Boolean: &trueVal}
		}
		if identStr == "FALSE" {
			p.advance()
			falseVal := false
			return &ast.MetadataValue{Boolean: &falseVal}
		}

		// Check for Account (contains colon)
		if strings.Contains(identStr, ":") {
			account, err := p.parseAccount()
			if err == nil {
				return &ast.MetadataValue{Account: &account}
			}
		}

		// Otherwise treat as Currency
		p.advance()
		return &ast.MetadataValue{Currency: &identStr}
	}

	// Fallback: read rest of line as string
	value := p.parseRestOfLine()
	unquoted, err := p.unquoteString(value)
	if err != nil {
		// For fallback metadata, if unquoting fails, keep original value
		// This maintains compatibility with existing behavior
		return &ast.MetadataValue{StringValue: &value}
	}
	return &ast.MetadataValue{StringValue: &unquoted}
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

// unquoteString removes surrounding quotes from a string and processes escape sequences.
// It handles \", \\, \n, \t, \r escape sequences according to Beancount string rules.
// Returns an error for invalid escape sequences or malformed strings.
func (p *Parser) unquoteString(s string) (string, error) {
	// Fast path: check if string has quotes and potential escapes
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s, &StringLiteralError{
			Message: "string must be enclosed in double quotes",
		}
	}

	inner := s[1 : len(s)-1]

	// Fast path: no escape sequences, return as-is
	if !strings.Contains(inner, "\\") {
		return inner, nil
	}

	var buf strings.Builder
	buf.Grow(len(inner))

	i := 0
	for i < len(inner) {
		if inner[i] == '\\' {
			if i+1 >= len(inner) {
				return "", &StringLiteralError{
					Message: "escape sequence at end of string",
				}
			}

			// Process escape sequence
			switch inner[i+1] {
			case '"':
				buf.WriteByte('"')
				i += 2
			case '\\':
				buf.WriteByte('\\')
				i += 2
			case 'n':
				buf.WriteByte('\n')
				i += 2
			case 't':
				buf.WriteByte('\t')
				i += 2
			case 'r':
				buf.WriteByte('\r')
				i += 2
			default:
				return "", &StringLiteralError{
					Message: fmt.Sprintf("invalid escape sequence '\\%c'", inner[i+1]),
				}
			}
		} else {
			buf.WriteByte(inner[i])
			i++
		}
	}

	return buf.String(), nil
}

// skipLine skips all tokens on the current line.
func (p *Parser) skipLine() {
	line := p.peek().Line
	for !p.isAtEnd() && p.peek().Line == line {
		p.advance()
	}
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
	// Return the peeked token but marked as illegal, preserving position info
	return Token{
		Type:   ILLEGAL,
		Start:  tok.Start,
		End:    tok.End,
		Line:   tok.Line,
		Column: tok.Column,
	}
}

func (p *Parser) expect(typ TokenType, message string) Token {
	return p.consume(typ, message)
}

// Error helpers

func (p *Parser) errorAtToken(tok Token, format string, args ...interface{}) error {
	pos := tokenPosition(tok, p.filename)
	sourceRange := p.calculateSourceRange(pos)
	return newErrorfWithSource(pos, sourceRange, format, args...)
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

// errorAtEndOfPrevious creates an error positioned at the end of the previous token.
// This is used when a required token is missing in a sequence, so the error points
// to where the token was expected (after the last valid token) rather than at the
// next token's position (which might be on a different line).
func (p *Parser) errorAtEndOfPrevious(format string, args ...interface{}) error {
	if p.pos == 0 {
		// Fallback: use current token position if no previous token
		return p.errorAtToken(p.peek(), format, args...)
	}
	prev := p.previous()
	pos := ast.Position{
		Filename: p.filename,
		Offset:   prev.End,
		Line:     prev.Line,
		Column:   prev.Column + (prev.End - prev.Start),
	}
	sourceRange := p.calculateSourceRange(pos)
	return newErrorfWithSource(pos, sourceRange, format, args...)
}

// calculateSourceRange determines the byte range in source that contains context lines around the error position.
// This includes 2 lines before and 2 lines after the error line for context display.
func (p *Parser) calculateSourceRange(pos ast.Position) SourceRange {
	// Split source into lines to find line boundaries
	sourceStr := string(p.source)
	lines := strings.Split(sourceStr, "\n")

	// Determine line range to include (2 lines before and after error line)
	startLine := pos.Line - 3 // 0-based, show 2 lines before
	endLine := pos.Line + 1   // show 1 line after (inclusive)

	// Ensure bounds
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	// Calculate byte offsets for the line range
	startOffset := 0
	if startLine > 0 {
		// Sum lengths of all lines before startLine, plus newline characters
		for i := 0; i < startLine; i++ {
			startOffset += len(lines[i]) + 1 // +1 for newline
		}
	}

	endOffset := startOffset
	for i := startLine; i <= endLine; i++ {
		if i < len(lines) {
			endOffset += len(lines[i])
			if i < endLine { // Don't add newline after last line
				endOffset += 1
			}
		}
	}

	// Ensure we don't exceed source bounds
	if endOffset > len(p.source) {
		endOffset = len(p.source)
	}

	return SourceRange{
		StartOffset: startOffset,
		EndOffset:   endOffset,
		Source:      p.source[startOffset:endOffset],
	}
}
