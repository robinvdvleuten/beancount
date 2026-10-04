package parser

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

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

	// Like beancount, an account its lexer reads but its account pattern
	// rejects is reported and still read, so the directive stays.
	var account ast.Account
	if err := account.Capture([]string{accountStr}); err != nil {
		pos := tokenPosition(tok, p.filename)
		p.errs = append(p.errs, newErrorfWithSource(pos, p.calculateSourceRange(pos), "invalid account name %s: %v", accountStr, err))
		account = ast.Account(accountStr)
	}

	return account, nil
}

// parseAmount parses an amount: NUMBER CURRENCY or (EXPRESSION) CURRENCY
// Expressions are captured as-is (not evaluated) and stored in Amount.Value.
// The ledger phase evaluates expressions when computing balances.
func (p *Parser) parseAmount() (*ast.Amount, error) {
	valueTok, isExpression, value, err := p.parseAmountValueToken()
	if err != nil {
		return nil, err
	}

	if !p.check(IDENT) {
		return nil, p.errorAtEndOfPrevious("expected currency")
	}
	currTok := p.advance()

	return p.amountFromValueToken(valueTok, currTok, isExpression, value), nil
}

func (p *Parser) amountFromValueToken(valueTok, currTok Token, isExpression bool, value string) *ast.Amount {
	currency := p.internCurrency(currTok)

	raw := valueTok.String(p.source)

	return ast.NewAmountWithRaw(raw, value, currency)
}

// indented reports whether tok is indented, as beancount's lexer decides
// INDENT: a line's leading whitespace is an indent only when it is spaces
// and tabs alone, so a lone \r in it makes the line read as if it started
// in column 1. A token after another on its line is past column 1.
func (p *Parser) indented(tok Token) bool {
	if tok.Column <= 1 {
		return false
	}
	carriageReturn := false
	for i := tok.Start - 1; i >= 0; i-- {
		switch p.source[i] {
		case ' ', '\t':
		case '\r':
			carriageReturn = true
		case '\n':
			return !carriageReturn
		default:
			return true
		}
	}
	return !carriageReturn
}

// continuesPreviousLine reports whether the next token continues the previous
// token's line, i.e. there is no line break between them. Used to keep
// line-scoped elements (tags and links) from absorbing tokens of following
// lines, matching the official grammar where tags_links end at EOL.
func (p *Parser) continuesPreviousLine() bool {
	prev := p.previous()
	next := p.peek()
	// COMMENT tokens include their trailing newline in their bounds.
	if prev.End > prev.Start && prev.End <= len(p.source) {
		if p.source[prev.End-1] == '\n' {
			return false
		}
	}
	for i := prev.End; i < next.Start && i < len(p.source); i++ {
		if p.source[i] == '\n' {
			return false
		}
	}
	return true
}

// parseIncompleteAmount parses posting amounts where the number, the currency,
// or both may be absent (official grammar: maybe_number maybe_currency).
// Interpolation completes the missing part during validation. Returns nil
// when neither part is present on the given line.
func (p *Parser) parseIncompleteAmount(line int) (*ast.Amount, error) {
	// Like its currency, a posting's number must be on the posting's line.
	if p.peek().Line == line && (p.check(NUMBER) || p.check(EXPRESSION) || p.isExpressionStartToken(p.peek())) {
		valueTok, isExpression, value, err := p.parseAmountValueToken()
		if err != nil {
			return nil, err
		}
		if p.check(IDENT) && p.peek().Line == line {
			return p.amountFromValueToken(valueTok, p.advance(), isExpression, value), nil
		}
		return ast.NewAmountWithRaw(valueTok.String(p.source), value, ""), nil
	}

	if p.check(IDENT) && p.peek().Line == line {
		return ast.NewAmount("", p.internCurrency(p.advance())), nil
	}

	return nil, nil
}

func (p *Parser) parseAmountValueToken() (Token, bool, string, error) {
	tok := p.peek()
	if tok.Type == ILLEGAL && p.isExpressionStartToken(tok) {
		return Token{}, false, "", p.errorAtToken(tok, "unmatched parentheses in expression")
	}
	if !p.check(NUMBER) && !p.check(EXPRESSION) {
		return Token{}, false, "", p.errorAtToken(p.peek(), "expected number or expression")
	}
	result, end, err := evaluateNumberExpression(p.source, tok.Start)
	if err != nil {
		return Token{}, false, "", p.errorAtToken(tok, "invalid number expression: %v", err)
	}
	isExpression := tok.Type == EXPRESSION || end > tok.End
	valueTok := tok
	valueTok.End = end
	valueTok.Type = NUMBER
	// A trailing dot states no fraction digits: "5." is 5.
	value := strings.TrimSuffix(strings.ReplaceAll(tok.String(p.source), ",", ""), ".")
	if isExpression {
		valueTok.Type = EXPRESSION
		value = canonicalExpressionValue(result)
	}
	for !p.isAtEnd() && p.peek().Start < end {
		p.advance()
	}
	return valueTok, isExpression, value, nil
}

// parseCost parses a cost specification: {} or a comma-separated list of
// components (AMOUNT, DATE, LABEL, the merge marker *) in any order, wrapped
// in {} (per-unit) or {{}} (total). Like beancount, total braces need no
// amount: {{}} and {{2020-01-01}} book as {} and {2020-01-01} do.
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

	// Determine closing token
	closingToken := RBRACE
	if isTotal {
		closingToken = RDBRACE
	}

	// Check for empty cost: {} or {{}}
	if p.check(closingToken) {
		p.advance()
		return cost, nil
	}

	// Parse comma-separated components (amount, date, label, merge marker)
	// in any order, matching the official beancount grammar. Like
	// beancount, the first of each kind counts; a repeated one goes to
	// Duplicates for Booking to report, and the transaction is kept.
	hasLabel := false
	for {
		// target is where the next component goes: the cost itself, or a
		// duplicate when the cost already has one of its kind.
		target := cost
		duplicate := func(has bool) {
			if has {
				target = &ast.Cost{}
				cost.Duplicates = append(cost.Duplicates, target)
			}
		}
		switch {
		case p.check(NUMBER) || p.check(EXPRESSION) || p.checkHash():
			duplicate(cost.Amount != nil || cost.Total != nil)
			if err := p.parseCostAmount(target); err != nil {
				return nil, err
			}

		case p.check(IDENT):
			// A currency without a number leaves the number to Booking
			// (official grammar: maybe_number CURRENCY).
			duplicate(cost.Amount != nil || cost.Total != nil)
			target.Amount = ast.NewAmount("", p.internCurrency(p.advance()))

		case p.check(DATE):
			duplicate(cost.Date != nil)
			date, err := p.parseDate()
			if err != nil {
				return nil, err
			}
			target.Date = date

		case p.check(STRING):
			duplicate(hasLabel)
			label, err := p.stringValue(p.advance())
			if err != nil {
				return nil, err
			}
			target.Label = label
			hasLabel = true

		case p.check(ASTERISK):
			// A merge marker, among the other components as beancount's
			// grammar takes it, in either braces; Booking reports it.
			duplicate(cost.IsMerge)
			p.advance()
			target.IsMerge = true

		default:
			return nil, p.error("expected cost amount, currency, date, label, or '*'")
		}

		if !p.match(COMMA) {
			break
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

// parseCostAmount parses a cost's amount like beancount's compound_amount:
// a number with an optional currency, or a compound of a per-unit number
// and a total around '#', either of which may be left out, before a
// currency. Booking fills in what is left out; a compound inside total
// braces is Booking's to report too.
func (p *Parser) parseCostAmount(cost *ast.Cost) error {
	number := func() (*ast.Amount, error) {
		if !p.check(NUMBER) && !p.check(EXPRESSION) {
			return &ast.Amount{}, nil
		}
		valueTok, _, value, err := p.parseAmountValueToken()
		if err != nil {
			return nil, err
		}
		return ast.NewAmountWithRaw(valueTok.String(p.source), value, ""), nil
	}

	perUnit, err := number()
	if err != nil {
		return err
	}
	if !p.checkHash() {
		if p.check(IDENT) {
			perUnit.Currency = p.internCurrency(p.advance())
		}
		cost.Amount = perUnit
		return nil
	}

	p.advance() // '#'
	total, err := number()
	if err != nil {
		return err
	}
	if !p.check(IDENT) {
		return p.errorAtEndOfPrevious("expected currency")
	}
	currency := p.internCurrency(p.advance())
	perUnit.Currency, total.Currency = currency, currency
	cost.Amount, cost.Total = perUnit, total
	return nil
}

// checkHash reports whether the next token is the '#' of a compound cost,
// which the lexer reads as a flag.
func (p *Parser) checkHash() bool {
	return p.check(FLAG) && p.peek().String(p.source) == "#"
}

// parseString parses a STRING token and returns a RawString with both
// the raw token (for round-trip formatting) and the unquoted value.
func (p *Parser) parseString() (ast.RawString, error) {
	if !p.check(STRING) {
		return ast.RawString{}, p.errorAtEndOfPrevious("expected string")
	}
	tok := p.advance()
	unquoted, err := p.stringValue(tok)
	if err != nil {
		return ast.RawString{}, err
	}
	return ast.NewRawStringWithRaw(tok.String(p.source), p.internString(unquoted)), nil
}

// stringValue returns a STRING token's value, unquoted.
func (p *Parser) stringValue(tok Token) (string, error) {
	raw := tok.String(p.source)
	if !utf8.ValidString(raw) {
		return "", p.invalidStringError(tok)
	}
	unquoted, err := p.unquoteString(raw)
	if err != nil {
		return "", p.errorAtToken(tok, "invalid string literal: %v", err)
	}
	return unquoted, nil
}

// invalidStringError reports a string that is not valid UTF-8. Like
// beancount's lexer, which decodes a string once it has read it, the
// error is on the line the string ends on.
func (p *Parser) invalidStringError(tok Token) error {
	text := strings.TrimRight(tok.String(p.source), "\r\n")
	pos := tokenPosition(tok, p.filename)
	if last := strings.LastIndexAny(text, "\r\n"); last >= 0 {
		pos.Line = p.endLine(tok)
		pos.Column = len(text) - last
		pos.Offset = tok.Start + last + 1
	}
	return newErrorfWithSource(pos, p.calculateSourceRange(pos), "string is not valid UTF-8")
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

// parseMetadata parses the metadata lines below a directive or posting,
// one key: value entry per line.
func (p *Parser) parseMetadata() ([]*ast.Metadata, error) {
	var metadata []*ast.Metadata

	// Metadata lines are key: value where key can be IDENT or any keyword
	for {
		// Like beancount's lexer, which skips them, indented comment lines
		// may come before a metadata line; they lead it.
		leading := p.indentedCommentsBeforeMetadata()
		if !p.isMetadataKeyAt(leading) {
			break
		}
		var comments []*ast.Comment
		for range leading {
			comments = append(comments, p.parseComment())
		}

		keyTok := p.advance() // consume key
		if err := p.consume(COLON, "expected ':'"); err != nil {
			return nil, err
		}

		// Parse the metadata value based on token type
		value, err := p.parseMetadataValue(keyTok.Line)
		if err != nil {
			return nil, err
		}

		md := &ast.Metadata{
			Key:      keyTok.String(p.source),
			Value:    value,
			Comments: comments,
		}
		md.SetPosition(tokenPosition(keyTok, p.filename))
		metadata = append(metadata, md)

		// A trailing comment belongs to the metadata line; consume it so the
		// block can continue with further metadata lines.
		if p.check(COMMENT) && p.continuesPreviousLine() {
			p.advance()
		}

		// A metadata entry owns the rest of its line: official beancount
		// requires an EOL after the value, so any further content on the
		// same line (another key, an account, ...) is a syntax error.
		if next := p.peek(); next.Type != EOF && next.Type != NEWLINE && p.continuesPreviousLine() {
			return nil, p.errorAtToken(next, "unexpected content after metadata value")
		}
	}

	return metadata, nil
}

// indentedCommentsBeforeMetadata counts the indented comment lines at the
// parser's position that an indented metadata line follows, with no blank
// line between; it is 0 when none do.
func (p *Parser) indentedCommentsBeforeMetadata() int {
	n := 0
	for {
		tok := p.peekAhead(n)
		if tok.Type != COMMENT || !p.indented(tok) || (n == 0 && p.continuesPreviousLine()) {
			break
		}
		n++
	}
	if n == 0 || !p.indented(p.peekAhead(n)) || !p.isMetadataKeyAt(n) {
		return 0
	}
	return n
}

// parseMetadataKey parses the key of a pushmeta or popmeta and its colon.
func (p *Parser) parseMetadataKey() (string, error) {
	if !p.isMetadataKeyStart() {
		return "", p.errorAtEndOfPrevious("expected metadata key")
	}
	key := p.advance().String(p.source)
	p.advance() // the colon
	return key, nil
}

// isMetadataKeyStart reports whether the next token is a metadata key, as
// beancount's lexer matches one, directly followed by its colon.
func (p *Parser) isMetadataKeyStart() bool {
	return p.isMetadataKeyAt(0)
}

// isMetadataKeyAt is isMetadataKeyStart for the token n ahead.
func (p *Parser) isMetadataKeyAt(n int) bool {
	tok, colon := p.peekAhead(n), p.peekAhead(n+1)
	// The official lexer requires keys of at least two characters
	// ([a-z][a-zA-Z0-9-_]+), a keyword included; a single-letter key is an
	// invalid token, and a currency before a colon (/ESZ24:) is no key.
	return (tok.Type == IDENT || p.isKeyword(tok.Type)) &&
		tok.Len() >= 2 &&
		isLowercaseLetter(p.source[tok.Start]) &&
		colon.Type == COLON &&
		tok.Column+tok.Len() == colon.Column
}

// parseMetadataValue parses a typed metadata value. Beancount supports 8 value types:
// strings, dates, accounts, currencies, tags, links, numbers, amounts, and booleans.
// An empty value and NULL are beancount's None: a nil value.
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

	case NUMBER, EXPRESSION:
		valueTok, isExpression, value, err := p.parseAmountValueToken()
		if err != nil {
			return nil, err
		}
		if p.check(IDENT) && p.peek().Line == tok.Line {
			currTok := p.advance()
			return &ast.MetadataValue{Amount: p.amountFromValueToken(valueTok, currTok, isExpression, value)}, nil
		}
		return &ast.MetadataValue{Number: &value}, nil

	case NONE:
		p.advance()
		return nil, nil

	case BOOL:
		p.advance()
		boolVal := tok.String(p.source) == "TRUE"
		return &ast.MetadataValue{Boolean: &boolVal}, nil

	case IDENT:
		// Could be Account or Currency
		identStr := tok.String(p.source)

		// Check for Account (contains colon)
		if strings.Contains(identStr, ":") {
			account, err := p.parseAccount()
			if err != nil {
				return nil, err
			}
			return &ast.MetadataValue{Account: &account}, nil
		}

		if !isUppercaseMetadataIdentifier(identStr) {
			return nil, p.errorAtToken(tok, "unsupported metadata value %q", identStr)
		}

		p.advance()
		return &ast.MetadataValue{Currency: &identStr}, nil
	}

	return nil, p.errorAtToken(tok, "unsupported metadata value %q", tok.String(p.source))
}

func isUppercaseMetadataIdentifier(value string) bool {
	if value == "" {
		return false
	}
	return strings.ToUpper(value) == value
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

	case BOOL:
		ident := p.internIdent(tok)
		p.advance()
		return &ast.CustomValue{BooleanValue: &ident}, nil

	case IDENT, NONE:
		// beancount's grammar takes no NULL and no bare currency among a
		// custom's values, only its booleans.
		return nil, p.errorAtToken(tok, "unexpected %s in custom values", tok.String(p.source))

	case ACCOUNT:
		account := p.internIdent(tok)
		p.advance()
		return &ast.CustomValue{String: &account}, nil

	case NUMBER, EXPRESSION:
		valueTok, isExpression, value, err := p.parseAmountValueToken()
		if err != nil {
			return nil, err
		}
		if p.check(IDENT) && p.peek().Line == line {
			currTok := p.advance()
			return &ast.CustomValue{Amount: p.amountFromValueToken(valueTok, currTok, isExpression, value)}, nil
		}
		return &ast.CustomValue{Number: &value}, nil
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

// parseRestOfLineUntilComment reads the tokens left on line, up to an inline
// comment; none when the next token is on a later line.
func (p *Parser) parseRestOfLineUntilComment(currentLine int) string {
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

// endLine returns the line tok ends on: its own, plus the line breaks
// inside it (a string spanning lines), not counting the trailing line break
// content tokens own.
func (p *Parser) endLine(tok Token) int {
	return tok.Line + ast.CountLineBreaks(strings.TrimSuffix(tok.String(p.source), "\n"))
}

// lineAfterPrevious returns the line following the one the last consumed
// token ends on.
func (p *Parser) lineAfterPrevious() int {
	return p.endLine(p.previous()) + 1
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

// finishDirective ends a dated directive's header, capturing its trailing
// inline comment, and parses its metadata lines. It is the common
// end-of-directive logic of every directive parser but the transaction's.
func (p *Parser) finishDirective(d ast.Directive) error {
	if err := p.finishHeader(d, d.Position().Offset); err != nil {
		return err
	}
	metadata, err := p.parseMetadata()
	if err != nil {
		return err
	}
	d.AddMetadata(metadata...)
	return nil
}

// headerContinuation returns the first token of the header parsed since the
// token at offset that starts on a later line than the token before it ends
// on, and false when the header stays on its line (a string spanning lines
// included).
func (p *Parser) headerContinuation(offset int) (Token, bool) {
	start := p.pos - 1
	for start > 0 && p.tokens[start].Start > offset {
		start--
	}
	for i := start + 1; i < p.pos; i++ {
		if p.tokens[i].Line != p.endLine(p.tokens[i-1]) {
			return p.tokens[i], true
		}
	}
	return Token{}, false
}

// finishHeader ends the line of a directive, posting or undated line
// whose first token is at offset. Like beancount's grammar, the line ends
// at its line's end: a token continued on the next line is a syntax error
// there. A string spanning lines ends it on the line the string closes
// on, where a comment is the target's inline comment and anything else a
// syntax error.
func (p *Parser) finishHeader(target ast.WithComment, offset int) error {
	if tok, ok := p.headerContinuation(offset); ok {
		return p.errorAtToken(tok, "unexpected token %s %q", tok.Type, tok.String(p.source))
	}
	line := p.lineAfterPrevious() - 1
	p.attachInlineComment(target, line)
	return p.expectLineEnd(line)
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
	if tok.Type == ILLEGAL {
		return newErrorfWithSource(pos, sourceRange, "%s", p.illegalTokenMessage(tok))
	}
	return newErrorfWithSource(pos, sourceRange, format, args...)
}

func (p *Parser) illegalTokenMessage(tok Token) string {
	text := tok.String(p.source)
	bytes := tok.Bytes(p.source)
	switch {
	case ast.IsDateLiteralShape(bytes):
		return fmt.Sprintf("invalid date %q", text)
	case strings.HasPrefix(text, `"`):
		return "unterminated string"
	case p.isExpressionStartToken(tok):
		return "unmatched parentheses in expression"
	default:
		return fmt.Sprintf("invalid token %q", text)
	}
}

// lexerRejects reports whether beancount's lexer, too, rejects an invalid
// token: a word, a number or date it cannot convert, an unterminated string
// or a stray character. The rest are characters it lexes as tokens of their
// own (an unmatched parenthesis in an expression, a lone sign, a slash or a
// pipe), which only its grammar rejects.
func (p *Parser) lexerRejects(tok Token) bool {
	switch p.source[tok.Start] {
	case '(', ')', '+', '-', '/', '|':
		return false
	}
	return true
}

// lexerRejectsAccount reports whether beancount's lexer rejects an ACCOUNT
// token as an invalid token: one with an empty component, or one whose
// component starts with an ASCII character other than a capital letter (or a
// digit, past the first) or goes on with one other than a letter, a digit or
// a dash. Non-ASCII characters pass its lexer, and only the account pattern
// rejects them; a trailing colon is a token of its own. A word with no
// colon but a trailing one is no account at all: unless it is a currency,
// which its lexer reads before the colon, it is an invalid token (`T:`).
func lexerRejectsAccount(name []byte) bool {
	name = bytes.TrimSuffix(name, []byte(":"))
	if !bytes.Contains(name, []byte(":")) {
		return !isCurrencyWord(name)
	}
	for i, component := range bytes.Split(name, []byte(":")) {
		if len(component) == 0 {
			return true
		}
		if first := component[0]; first < utf8.RuneSelf && !isUppercaseLetter(first) && (i == 0 || !isDigit(first)) {
			return true
		}
		for _, ch := range component[1:] {
			if ch < utf8.RuneSelf && !isLetter(ch) && !isDigit(ch) && ch != '-' {
				return true
			}
		}
	}
	return false
}

// isCurrencyWord reports whether name matches beancount's currency pattern
// starting with a letter, [A-Z][A-Z0-9'._-]*[A-Z0-9].
func isCurrencyWord(name []byte) bool {
	if len(name) < 2 || !isUppercaseLetter(name[0]) {
		return false
	}
	if last := name[len(name)-1]; !isUppercaseLetter(last) && !isDigit(last) {
		return false
	}
	for _, ch := range name[1 : len(name)-1] {
		if !isUppercaseLetter(ch) && !isDigit(ch) && !bytes.ContainsRune([]byte("'._-"), rune(ch)) {
			return false
		}
	}
	return true
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
	// An invalid token where something else was expected is the cause.
	if next := p.peek(); next.Type == ILLEGAL && next.Line == p.previous().Line {
		return p.errorAtToken(next, format, args...)
	}
	pos := p.positionAtEndOfPrevious()
	sourceRange := p.calculateSourceRange(pos)
	return newErrorfWithSource(pos, sourceRange, format, args...)
}

// calculateSourceRange determines the byte range in source that contains context lines around the error position.
// This includes 2 lines before and 1 line after the error line for context display.
func (p *Parser) calculateSourceRange(pos ast.Position) SourceRange {
	if p.lineStarts == nil {
		p.lineStarts = lineStarts(p.source)
	}

	// pos.Line is 1-based
	wantStart := min(max(pos.Line-2, 1), len(p.lineStarts)) // show 2 lines before
	wantEnd := pos.Line + 1                                 // show 1 line after (inclusive)

	startOffset := p.lineStarts[wantStart-1]
	endOffset := len(p.source)
	if wantEnd < len(p.lineStarts) {
		// The last byte of the line break ending line wantEnd.
		endOffset = p.lineStarts[wantEnd] - 1
	}

	return SourceRange{
		StartOffset: startOffset,
		EndOffset:   endOffset,
		Source:      p.source[startOffset:endOffset],
	}
}

// lineStarts returns the byte offset where each line of source starts,
// breaking lines after each \n like the lexer.
func lineStarts(source []byte) []int {
	starts := []int{0}
	for i, ch := range source {
		if ch == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}
