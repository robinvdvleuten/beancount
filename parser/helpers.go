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
	if !p.check(DATE) {
		return nil, p.errorAtToken(p.peek(), "expected date")
	}
	tok := p.advance()

	var date ast.Date
	if err := date.Capture([]string{tok.String(p.source)}); err != nil {
		return nil, p.errorAtToken(tok, "invalid date: %v", err)
	}

	return &date, nil
}

// parseAccount parses an ACCOUNT token and converts it to ast.Account.
// The account name is interned to save memory.
func (p *Parser) parseAccount() (ast.Account, error) {
	if !p.check(ACCOUNT) {
		actualTok := p.peek()
		return "", p.errorAtEndOfPrevious("expected account but got %s %q", actualTok.Type, actualTok.String(p.source))
	}
	tok := p.advance()

	// Intern account name for memory efficiency
	accountStr := p.internIdent(tok)

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
	tok := p.peek()
	if tok.Type == ILLEGAL && p.isExpressionStartToken(tok) {
		return nil, p.errorAtToken(tok, "unmatched parentheses in expression")
	}
	if !p.check(NUMBER) && !p.check(EXPRESSION) {
		return nil, p.errorAtToken(p.peek(), "expected number or expression")
	}
	valueTok := p.advance()
	isExpression := valueTok.Type == EXPRESSION
	value := valueTok.String(p.source)
	if valueTok.Type == NUMBER {
		// Remove commas from number (e.g., "1,000" -> "1000")
		value = strings.ReplaceAll(value, ",", "")
	}

	if !p.check(IDENT) {
		return nil, p.errorAtEndOfPrevious("expected currency")
	}
	currTok := p.advance()

	// Intern currency code (USD, EUR, etc.)
	currency := p.internCurrency(currTok)

	// Get the raw number token for perfect round-trip formatting
	// Only available for plain numbers, not expressions
	var raw string
	if !isExpression && valueTok.Type == NUMBER {
		raw = valueTok.String(p.source)
	}

	return ast.NewAmountWithRaw(raw, value, currency), nil
}

// parseCost parses a cost specification: { [*] [AMOUNT] [, DATE] [, LABEL] } or {{ AMOUNT [, DATE] [, LABEL] }}
func (p *Parser) parseCost() (*ast.Cost, error) {
	// Check for {{ or {
	isTotal := false
	if p.check(LDBRACE) {
		p.advance() // consume {{
		isTotal = true
	} else {
		if err := p.consume(LBRACE, "expected '{' or '{{'"); err != nil {
			return nil, err
		}
	}

	cost := &ast.Cost{IsTotal: isTotal}

	// Check for merge cost {*} (only valid with single braces)
	if p.match(ASTERISK) {
		if isTotal {
			return nil, p.error("merge cost {*} cannot use total cost syntax {{}}")
		}
		cost.IsMerge = true
		if err := p.consume(RBRACE, "expected '}'"); err != nil {
			return nil, err
		}
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
	if p.check(NUMBER) || p.check(EXPRESSION) {
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
		if err := p.consume(RDBRACE, "expected '}}'"); err != nil {
			return nil, err
		}
	} else {
		if err := p.consume(RBRACE, "expected '}'"); err != nil {
			return nil, err
		}
	}

	return cost, nil
}

// parseString parses a STRING token and returns a RawString with both
// the raw token (for round-trip formatting) and the unquoted value.
func (p *Parser) parseString() (ast.RawString, error) {
	if !p.check(STRING) {
		return ast.RawString{}, p.errorAtEndOfPrevious("expected string")
	}
	tok := p.advance()

	rawValue := tok.String(p.source)
	unquoted, err := p.unquoteString(rawValue)
	if err != nil {
		return ast.RawString{}, p.errorAtToken(tok, "invalid string literal: %v", err)
	}

	return ast.NewRawStringWithRaw(rawValue, p.internString(unquoted)), nil
}

// unquoteString unquotes a string by removing surrounding quotes and processing escapes.
func (p *Parser) unquoteString(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s, &StringLiteralError{
			Message: "string must be enclosed in double quotes",
		}
	}

	inner := s[1 : len(s)-1]

	// Fast path: no escape sequences, return as-is
	if !containsEscapeSequences(inner) {
		return inner, nil
	}

	// Slow path: process escape sequences
	return p.processEscapeSequences(inner)
}

// containsEscapeSequences checks if a string contains any backslash that needs processing.
// This includes both valid escape sequences and invalid ones (which will error during processing).
func containsEscapeSequences(s string) bool {
	return strings.IndexByte(s, '\\') >= 0
}

// processEscapeSequences processes escape sequences in a string's inner content.
// This is the core of the unquoting logic, extracted for reuse.
// Note: This implementation matches the original behavior where we check if
// a backslash is preceded by another backslash to determine escape behavior.
func (p *Parser) processEscapeSequences(inner string) (string, error) {
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

// parseIdent parses an IDENT token.
func (p *Parser) parseIdent() (string, error) {
	if !p.check(IDENT) {
		return "", p.errorAtEndOfPrevious("expected identifier")
	}
	tok := p.advance()

	return tok.String(p.source), nil
}

// parseTag parses a TAG token and returns the tag without the # prefix.
func (p *Parser) parseTag() (ast.Tag, error) {
	if !p.check(TAG) {
		return "", p.errorAtEndOfPrevious("expected tag")
	}
	tok := p.advance()

	var tag ast.Tag
	if err := tag.Capture([]string{tok.String(p.source)}); err != nil {
		return "", p.errorAtToken(tok, "invalid tag: %v", err)
	}

	return tag, nil
}

// parseLink parses a LINK token and returns the link without the ^ prefix.
func (p *Parser) parseLink() (ast.Link, error) {
	if !p.check(LINK) {
		return "", p.errorAtEndOfPrevious("expected link")
	}
	tok := p.advance()

	var link ast.Link
	if err := link.Capture([]string{tok.String(p.source)}); err != nil {
		return "", p.errorAtToken(tok, "invalid link: %v", err)
	}

	return link, nil
}

// parseMetadataFromLine parses metadata entries with tracking of whether they are inline.
// If ownerLine > 0, metadata on that same line will be marked as Inline=true.
func (p *Parser) parseMetadataFromLine(ownerLine int) ([]*ast.Metadata, error) {
	var metadata []*ast.Metadata

	// Metadata lines are key: value where key can be IDENT or any keyword
	for {
		keyTok := p.peek()
		if !p.isMetadataKeyStart(keyTok) {
			break
		}

		p.advance() // consume key
		if err := p.consume(COLON, "expected ':'"); err != nil {
			return nil, err
		}

		// Parse the metadata value based on token type
		value, err := p.parseMetadataValue(keyTok.Line)
		if err != nil {
			return nil, err
		}

		// Determine if this metadata is inline (on the same line as owner)
		inline := ownerLine > 0 && keyTok.Line == ownerLine

		metadata = append(metadata, &ast.Metadata{
			Key:    keyTok.String(p.source),
			Value:  value,
			Inline: inline,
		})
	}

	return metadata, nil
}

func (p *Parser) isMetadataKeyStart(tok Token) bool {
	return (tok.Type == IDENT || p.isKeyword(tok.Type)) &&
		p.peekAhead(1).Type == COLON &&
		tok.Column+tok.Len() == p.peekAhead(1).Column
}

// parseMetadataValue parses a typed metadata value. Beancount supports 8 value types:
// strings, dates, accounts, currencies, tags, links, numbers, amounts, and booleans.
func (p *Parser) parseMetadataValue(line int) (*ast.MetadataValue, error) {
	tok := p.peek()
	if tok.Type == EOF || tok.Line != line || tok.Type == COMMENT {
		return nil, nil
	}

	// Parse based on token type with specific-to-general order
	switch tok.Type {
	case STRING:
		// String (quoted) - most specific
		str, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return &ast.MetadataValue{StringValue: &str}, nil

	case DATE:
		// Date (ISO format)
		date, err := p.parseDate()
		if err != nil {
			return nil, err
		}
		return &ast.MetadataValue{Date: date}, nil

	case TAG:
		// Tag (with # prefix)
		tag, err := p.parseTag()
		if err != nil {
			return nil, err
		}
		return &ast.MetadataValue{Tag: &tag}, nil

	case LINK:
		// Link (with ^ prefix)
		link, err := p.parseLink()
		if err != nil {
			return nil, err
		}
		return &ast.MetadataValue{Link: &link}, nil

	case ACCOUNT:
		// Account (colon-separated)
		account, err := p.parseAccount()
		if err != nil {
			return nil, err
		}
		return &ast.MetadataValue{Account: &account}, nil

	case NUMBER:
		// Could be Number or Amount - need LL(2) lookahead
		nextTok := p.peekAhead(1)
		if nextTok.Type == IDENT && nextTok.Line == tok.Line {
			// Amount (number + currency) - both must be on same line
			amount, err := p.parseAmount()
			if err != nil {
				return nil, err
			}
			return &ast.MetadataValue{Amount: amount}, nil
		}
		// Just a number - remove commas from thousands separators
		numStr := strings.ReplaceAll(tok.String(p.source), ",", "")
		p.advance()
		return &ast.MetadataValue{Number: &numStr}, nil

	case IDENT:
		// Could be Account, Currency, or Boolean
		identStr := tok.String(p.source)

		// Check for Boolean (TRUE/FALSE)
		if identStr == "TRUE" {
			p.advance()
			trueVal := true
			return &ast.MetadataValue{Boolean: &trueVal}, nil
		}
		if identStr == "FALSE" {
			p.advance()
			falseVal := false
			return &ast.MetadataValue{Boolean: &falseVal}, nil
		}

		// Check for Account (contains colon)
		if strings.Contains(identStr, ":") {
			account, err := p.parseAccount()
			if err != nil {
				return nil, err
			}
			return &ast.MetadataValue{Account: &account}, nil
		}

		// Otherwise treat as Currency
		p.advance()
		return &ast.MetadataValue{Currency: &identStr}, nil
	}

	// Fallback: read rest of line as string
	value := p.parseRestOfLine()
	unquoted, err := p.unquoteString(value)
	if err != nil {
		// For fallback metadata, if unquoting fails, keep original value
		// This maintains compatibility with existing behavior
		rawStr := ast.NewRawString(value)
		return &ast.MetadataValue{StringValue: &rawStr}, nil
	}
	rawStr := ast.NewRawString(unquoted)
	return &ast.MetadataValue{StringValue: &rawStr}, nil
}

func (p *Parser) parseCustomValue(line int) (*ast.CustomValue, error) {
	tok := p.peek()
	if tok.Type == EOF || tok.Line != line || tok.Type == COMMENT {
		return nil, nil
	}

	switch tok.Type {
	case STRING:
		str, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return &ast.CustomValue{String: &str.Value}, nil

	case DATE:
		date, err := p.parseDate()
		if err != nil {
			return nil, err
		}
		return &ast.CustomValue{Date: date}, nil

	case IDENT:
		ident := p.internIdent(tok)
		p.advance()
		switch ident {
		case "TRUE", "FALSE":
			return &ast.CustomValue{BooleanValue: &ident}, nil
		default:
			return &ast.CustomValue{String: &ident}, nil
		}

	case ACCOUNT:
		account := p.internIdent(tok)
		p.advance()
		return &ast.CustomValue{String: &account}, nil

	case NUMBER:
		if nextTok := p.peekAhead(1); nextTok.Type == IDENT && nextTok.Line == line {
			amount, err := p.parseAmount()
			if err != nil {
				return nil, err
			}
			return &ast.CustomValue{Amount: amount}, nil
		}
		number := strings.ReplaceAll(tok.String(p.source), ",", "")
		p.advance()
		return &ast.CustomValue{Number: &number}, nil
	}

	return nil, nil
}

// isKeyword returns true if the token type is a keyword.
func (p *Parser) isKeyword(typ TokenType) bool {
	switch typ {
	case TXN, BALANCE, OPEN, CLOSE, COMMODITY, PAD, NOTE, DOCUMENT,
		PRICE, EVENT, QUERY, CUSTOM, OPTION, INCLUDE, PLUGIN,
		PUSHTAG, POPTAG, PUSHMETA, POPMETA:
		return true
	default:
		return false
	}
}

// parseRestOfLine reads all tokens until end of line and returns as string.
func (p *Parser) parseRestOfLine() string {
	currentLine := p.peek().Line

	var buf strings.Builder
	for !p.isAtEnd() && p.peek().Line == currentLine {
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		tok := p.advance()
		buf.WriteString(tok.String(p.source))
	}

	return strings.TrimSpace(buf.String())
}

// parseRestOfLineUntilComment reads tokens until end of line or an inline comment.
func (p *Parser) parseRestOfLineUntilComment() string {
	currentLine := p.peek().Line

	var buf strings.Builder
	for !p.isAtEnd() && p.peek().Line == currentLine {
		if p.peek().Type == COMMENT {
			break
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		tok := p.advance()
		buf.WriteString(tok.String(p.source))
	}

	return strings.TrimSpace(buf.String())
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

func (p *Parser) consume(typ TokenType, message string) error {
	if p.check(typ) {
		p.advance()
		return nil
	}
	return p.errorAtToken(p.peek(), "%s", message)
}

// String interning helpers - deduplicate repeated strings for memory efficiency

// internCurrency interns a currency identifier from a token.
// Currency codes like USD, EUR, GBP are frequently repeated in large files,
// so interning saves memory by maintaining a single copy per unique value.
func (p *Parser) internCurrency(tok Token) string {
	return p.interner.InternBytes(tok.Bytes(p.source))
}

// internString interns a string value.
// Used for interning strings that appear multiple times in the file
// (e.g., payees, narrations, account names).
func (p *Parser) internString(s string) string {
	return p.interner.Intern(s)
}

// internIdent interns an identifier from a token.
// Used for identifiers that may be repeated (e.g., options, metadata keys).
func (p *Parser) internIdent(tok Token) string {
	return p.interner.InternBytes(tok.Bytes(p.source))
}

// finishDirective captures trailing inline comment and metadata for any directive.
// This consolidates the common end-of-directive logic used by all directive parsers.
func (p *Parser) finishDirective(d ast.Directive) error {
	metadata, err := p.finishMetadataLine(d, d.Position().Line)
	if err != nil {
		return err
	}
	d.AddMetadata(metadata...)
	return nil
}

func (p *Parser) consumeInlineComment(line int) *ast.Comment {
	if p.isAtEnd() || p.peek().Line != line || p.peek().Type != COMMENT {
		return nil
	}
	return p.parseComment()
}

func (p *Parser) attachInlineComment(target ast.WithComment, line int) {
	if comment := p.consumeInlineComment(line); comment != nil {
		target.SetComment(comment)
	}
}

func (p *Parser) finishLine(target ast.WithComment, line int) error {
	p.attachInlineComment(target, line)
	return p.expectLineEnd(line)
}

func (p *Parser) finishMetadataLine(target ast.WithComment, line int) ([]*ast.Metadata, error) {
	p.attachInlineComment(target, line)

	metadata, err := p.parseMetadataFromLine(line)
	if err != nil {
		return nil, err
	}

	p.attachInlineComment(target, line)
	if err := p.expectLineEnd(line); err != nil {
		return nil, err
	}

	return metadata, nil
}

func (p *Parser) expectLineEnd(line int) error {
	if !p.isAtEnd() && p.peek().Line == line {
		tok := p.peek()
		return p.errorAtToken(tok, "unexpected token %s %q", tok.Type, tok.String(p.source))
	}
	return nil
}

func (p *Parser) isExpressionStartToken(tok Token) bool {
	if tok.Start >= len(p.source) {
		return false
	}
	if p.source[tok.Start] == '(' {
		return true
	}
	if (p.source[tok.Start] == '+' || p.source[tok.Start] == '-') &&
		tok.Start+1 < len(p.source) &&
		p.source[tok.Start+1] == '(' {
		return true
	}
	return false
}

// Error helpers

func (p *Parser) errorAtToken(tok Token, format string, args ...any) error {
	pos := tokenPosition(tok, p.filename)
	sourceRange := p.calculateSourceRange(pos)
	return newErrorfWithSource(pos, sourceRange, format, args...)
}

func (p *Parser) error(format string, args ...any) error {
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

// tokenPositionFromPeek extracts position from the current token.
func (p *Parser) tokenPositionFromPeek() ast.Position {
	return tokenPosition(p.peek(), p.filename)
}

// tokenPositionFromPrevious extracts position from the previous token.
// Used internally for position handling in error reporting.
// nolint: unused
func (p *Parser) tokenPositionFromPrevious() ast.Position {
	return tokenPosition(p.previous(), p.filename)
}

// positionAtEndOfPrevious returns a position at the end of the previous token.
// This is used to point at where a missing token was expected.
func (p *Parser) positionAtEndOfPrevious() ast.Position {
	if p.pos == 0 {
		// Fallback to current token if no previous
		return p.tokenPositionFromPeek()
	}
	prev := p.previous()
	return ast.Position{
		Filename: p.filename,
		Offset:   prev.End,
		Line:     prev.Line,
		Column:   prev.Column + (prev.End - prev.Start),
	}
}

// errorAtEndOfPrevious creates an error positioned at the end of the previous token.
// This is used when a required token is missing in a sequence, so the error points
// to where the token was expected (after the last valid token) rather than at the
// next token's position (which might be on a different line).
func (p *Parser) errorAtEndOfPrevious(format string, args ...any) error {
	pos := p.positionAtEndOfPrevious()
	sourceRange := p.calculateSourceRange(pos)
	return newErrorfWithSource(pos, sourceRange, format, args...)
}

// calculateSourceRange determines the byte range in source that contains context lines around the error position.
// This includes 2 lines before and 2 lines after the error line for context display.
func (p *Parser) calculateSourceRange(pos ast.Position) SourceRange {
	// Determine line range to include (2 lines before and after error line)
	// pos.Line is 1-based
	wantStart := pos.Line - 2 // show 2 lines before (1-based)
	if wantStart < 1 {
		wantStart = 1
	}
	wantEnd := pos.Line + 1 // show 1 line after (1-based, inclusive)

	// Single pass through source bytes to find line boundaries
	// This avoids string(p.source) conversion and strings.Split allocation
	currentLine := 1
	startOffset := 0
	endOffset := len(p.source)
	foundStart := wantStart == 1
	foundEnd := false

	for i, b := range p.source {
		if b == '\n' {
			currentLine++
			if !foundStart && currentLine == wantStart {
				startOffset = i + 1
				foundStart = true
			}
			if currentLine > wantEnd {
				endOffset = i // exclude this newline
				foundEnd = true
				break
			}
		}
	}
	_ = foundEnd

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
