package parser

// Lexer implements a zero-copy lexer for Beancount files.
//
// The zero-copy approach:
// - Tokens store byte offsets, not string values
// - No intermediate token format conversions
// - String interning for repeated values
// - Pre-allocated token buffer

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// InvalidUTF8Error is returned when the lexer encounters invalid UTF-8 sequences or
// non-ASCII control characters in the input.
type InvalidUTF8Error struct {
	Filename string // Filename for error reporting
	Line     int    // Line number (1-indexed)
	Column   int    // Column number (1-indexed)
	Byte     byte   // The invalid byte
}

func (e *InvalidUTF8Error) Error() string {
	return fmt.Sprintf("%s:%d: Invalid token: '\\x%02x'", e.Filename, e.Line, e.Byte)
}

// Lexer tokenizes Beancount source code.
type Lexer struct {
	source   []byte    // Source buffer (potentially mmap'd)
	filename string    // Filename for error reporting
	pos      int       // Current byte position
	line     int       // Current line (1-indexed)
	column   int       // Current column (1-indexed)
	tokens   []Token   // Token buffer (pre-allocated)
	interner *Interner // String interning pool
}

// NewLexer creates a new lexer for the given source.
func NewLexer(source []byte, filename string) *Lexer {
	// Estimate token count: empirically ~1 token per 20 bytes
	// This pre-allocation eliminates many slice growth operations
	estimatedTokens := len(source)/20 + 1000

	// Scale interner capacity with source size
	internerCap := len(source) / 40
	if internerCap < 2000 {
		internerCap = 2000
	}

	return &Lexer{
		source:   source,
		filename: filename,
		line:     1,
		column:   1,
		tokens:   make([]Token, 0, estimatedTokens),
		interner: NewInterner(internerCap),
	}
}

// Interner returns the string interner, useful for parser.
func (l *Lexer) Interner() *Interner {
	return l.interner
}

// ScanAll lexes the entire source file and returns all tokens.
// This is a single-pass scanner with no backtracking.
// Returns nil and an InvalidUTF8Error if the source contains invalid UTF-8.
func (l *Lexer) ScanAll() ([]Token, error) {
	// Validate UTF-8 upfront
	if err := l.validateUTF8(); err != nil {
		return nil, err
	}

	for l.pos < len(l.source) {
		tok := l.scanNextToken()
		// scanNextToken returns EOF when it hits the end, but we may still be in the loop
		// if there are trailing newlines being tracked
		if tok.Type == EOF {
			break
		}
		l.tokens = append(l.tokens, tok)
	}

	// Add EOF token
	l.tokens = append(l.tokens, Token{
		Type:   EOF,
		Start:  l.pos,
		End:    l.pos,
		Line:   l.line,
		Column: l.column,
	})

	return l.tokens, nil
}

// validateUTF8 validates that the source contains valid UTF-8 and no invalid control characters.
// Invalid control characters are bytes < 0x20 (except tab \t, newline \n, and carriage return \r)
// and bytes >= 0x80 that are not part of valid multi-byte UTF-8 sequences.
func (l *Lexer) validateUTF8() error {
	line := 1
	col := 1

	for i := 0; i < len(l.source); i++ {
		ch := l.source[i]

		// Allow: tab (0x09), newline (0x0a), carriage return (0x0d)
		// Reject: other control characters (0x00-0x08, 0x0b-0x0c, 0x0e-0x1f)
		if ch < 0x20 && ch != '\t' && ch != '\n' && ch != '\r' {
			return &InvalidUTF8Error{
				Filename: l.filename,
				Line:     line,
				Column:   col,
				Byte:     ch,
			}
		}

		// Check for invalid UTF-8 at 0x80 and above
		if ch >= 0x80 {
			r, size := utf8.DecodeRune(l.source[i:])
			if r == utf8.RuneError {
				return &InvalidUTF8Error{
					Filename: l.filename,
					Line:     line,
					Column:   col,
					Byte:     ch,
				}
			}
			// Skip the remaining bytes of this rune
			for j := 1; j < size; j++ {
				i++
				col++
			}
		}

		// Update line/column tracking
		if ch == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}

	return nil
}

// scanNextToken scans the next token including comments and blank lines.
// A blank line is one that contains only whitespace (spaces, tabs, carriage returns).
// Each blank line should generate exactly one NEWLINE token.
func (l *Lexer) scanNextToken() Token {
	// Track whether current line has non-whitespace content
	lineHasContent := false
	blankLineStartPos := -1
	blankLineStartLine := 0
	blankLineStartCol := 0

	for l.pos < len(l.source) {
		ch := l.source[l.pos]

		if ch == '\n' {
			// Newline ends the current line
			if !lineHasContent {
				// Current line was blank (only whitespace or empty)
				// Record this newline as marking a blank line
				if blankLineStartPos < 0 {
					blankLineStartPos = l.pos
					blankLineStartLine = l.line
					blankLineStartCol = l.column
				}
				// Emit a NEWLINE token for this blank line
				tok := Token{NEWLINE, blankLineStartPos, l.pos, blankLineStartLine, blankLineStartCol}
				l.pos++
				l.line++
				l.column = 1
				return tok
			}
			// Line had content - just advance past the newline
			l.pos++
			l.line++
			l.column = 1
			lineHasContent = false
			continue
		}

		if ch == ' ' || ch == '\t' || ch == '\r' {
			// Whitespace doesn't count as content
			l.pos++
			l.column++
			continue
		}

		// Non-whitespace character - emit token (comment or regular)
		if ch == ';' {
			return l.scanComment()
		}

		// Regular token (returns and consumes trailing newline)
		return l.scanToken()
	}

	// End of file - return EOF placeholder (will be replaced by actual EOF in ScanAll)
	return Token{EOF, l.pos, l.pos, l.line, l.column}
}

// scanToken scans the next token from the current position.
// All content-bearing tokens consume their trailing newline (if present),
// ensuring NEWLINE tokens represent only blank lines. This maintains
// consistent semantics: content tokens own their line, COMMENT tokens own
// their line, and only NEWLINE represents a blank line.
func (l *Lexer) scanToken() Token {
	start := l.pos
	startLine := l.line
	startCol := l.column

	ch := l.advance()

	var tok Token

	switch {
	// Check for dates first: YYYY-MM-DD (starts with digit)
	// This must come before number scanning
	case isDigit(ch):
		// Peek ahead to check if this looks like a date
		if l.isDatePattern(start) {
			tok = l.scanDate(start, startLine, startCol)
		} else {
			tok = l.scanNumber(start, startLine, startCol)
		}
	case ch == '-' && l.peekIsDigit():
		tok = l.scanNumber(start, startLine, startCol)

	// Strings: "..."
	case ch == '"':
		tok = l.scanString(start, startLine, startCol)

	// Tags: #tag
	case ch == '#':
		tok = l.scanTag(start, startLine, startCol)

	// Links: ^link
	case ch == '^':
		tok = l.scanLink(start, startLine, startCol)

	// Accounts (start with capital) or identifiers
	// Also check for non-ASCII bytes that might be Unicode uppercase or other letters
	case isUppercaseLetter(ch) || isUTF8Byte(ch):
		tok = l.scanAccountOrIdent(start, startLine, startCol)

	// Keywords or identifiers (start with lowercase)
	case isLowercaseLetter(ch):
		tok = l.scanKeywordOrIdent(start, startLine, startCol)

	// Single-character tokens
	case ch == '*':
		tok = Token{ASTERISK, start, l.pos, startLine, startCol}
	case ch == '!':
		tok = Token{EXCLAIM, start, l.pos, startLine, startCol}
	case ch == ':':
		tok = Token{COLON, start, l.pos, startLine, startCol}
	case ch == ',':
		tok = Token{COMMA, start, l.pos, startLine, startCol}

	// { or {{
	case ch == '{':
		if l.peek() == '{' {
			l.advance()
			tok = Token{LDBRACE, start, l.pos, startLine, startCol}
		} else {
			tok = Token{LBRACE, start, l.pos, startLine, startCol}
		}

	// } or }}
	case ch == '}':
		if l.peek() == '}' {
			l.advance()
			tok = Token{RDBRACE, start, l.pos, startLine, startCol}
		} else {
			tok = Token{RBRACE, start, l.pos, startLine, startCol}
		}

	// @ or @@
	case ch == '@':
		if l.peek() == '@' {
			l.advance()
			tok = Token{ATAT, start, l.pos, startLine, startCol}
		} else {
			tok = Token{AT, start, l.pos, startLine, startCol}
		}

	default:
		tok = Token{ILLEGAL, start, l.pos, startLine, startCol}
	}

	// Consume trailing newline if present, as this content token owns its line
	if l.pos < len(l.source) && l.source[l.pos] == '\n' {
		l.advance()
	}

	return tok
}

// isDatePattern checks if the position starts a date pattern YYYY-MM-DD
func (l *Lexer) isDatePattern(start int) bool {
	// Need at least 10 characters: YYYY-MM-DD
	if start+10 > len(l.source) {
		return false
	}

	// Check pattern: digit{4}-digit{2}-digit{2}
	src := l.source[start:]
	return isDigit(src[0]) && isDigit(src[1]) && isDigit(src[2]) && isDigit(src[3]) &&
		src[4] == '-' &&
		isDigit(src[5]) && isDigit(src[6]) &&
		src[7] == '-' &&
		isDigit(src[8]) && isDigit(src[9])
}

// scanDate scans a date: YYYY-MM-DD
// Returns ILLEGAL token if the date is invalid (year 0, invalid month/day).
// This matches Beancount's lexer behavior which validates dates at lex time.
func (l *Lexer) scanDate(start, line, col int) Token {
	// Date pattern is exactly 10 characters
	// First digit already consumed, consume remaining 9
	for i := 0; i < 9; i++ {
		l.advance()
	}

	// Validate the date - extract YYYY-MM-DD components
	src := l.source[start:l.pos]
	if !isValidDate(src) {
		return Token{ILLEGAL, start, l.pos, line, col}
	}

	return Token{DATE, start, l.pos, line, col}
}

// isValidDate validates a date string in YYYY-MM-DD format.
// Returns false for:
// - Year 0 (Beancount requires year 1-9999)
// - Invalid month (must be 01-12)
// - Invalid day for the given month (accounts for leap years)
func isValidDate(date []byte) bool {
	if len(date) != 10 {
		return false
	}

	// Parse year (YYYY)
	year := int(date[0]-'0')*1000 + int(date[1]-'0')*100 + int(date[2]-'0')*10 + int(date[3]-'0')
	if year == 0 {
		return false // Year 0 is invalid in Beancount
	}

	// Parse month (MM)
	month := int(date[5]-'0')*10 + int(date[6]-'0')
	if month < 1 || month > 12 {
		return false
	}

	// Parse day (DD)
	day := int(date[8]-'0')*10 + int(date[9]-'0')
	if day < 1 {
		return false
	}

	// Check day against month (with leap year handling)
	maxDay := daysInMonth(year, month)
	return day <= maxDay
}

// daysInMonth returns the number of days in the given month.
func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if isLeapYear(year) {
			return 29
		}
		return 28
	}
	return 0
}

// isLeapYear returns true if the year is a leap year.
func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// scanNumber scans a number: [-+]?[0-9]+(,[0-9]{3})*(\.[0-9]+)?
// Commas are allowed as thousands separators within the integer part.
func (l *Lexer) scanNumber(start, line, col int) Token {
	// Optional sign already consumed if present

	// Scan integer part (including optional comma thousands separators)
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if isDigit(ch) {
			l.advance()
		} else if ch == ',' {
			// Look ahead: comma must be followed by exactly 3 digits
			// Valid: 1,000  1,000,000
			// Invalid: 1,00 1,0000
			if l.pos+4 <= len(l.source) &&
				isDigit(l.source[l.pos+1]) &&
				isDigit(l.source[l.pos+2]) &&
				isDigit(l.source[l.pos+3]) {
				l.advance() // consume comma
			} else {
				break
			}
		} else {
			break
		}
	}

	// Scan optional decimal part
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		// Look ahead to ensure next char is digit
		if l.pos+1 < len(l.source) && isDigit(l.source[l.pos+1]) {
			l.advance() // consume '.'
			for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
				l.advance()
			}
		}
	}

	return Token{NUMBER, start, l.pos, line, col}
}

// scanString scans a quoted string: "..."
// Strings must not contain literal newlines. Use escape sequences like \n instead.
func (l *Lexer) scanString(start, line, col int) Token {
	// Opening quote already consumed

	// Scan until closing quote or end of source
	closed := false
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if ch == '"' {
			l.advance() // consume closing quote
			closed = true
			break
		}
		// Reject literal newlines in strings (must use escape sequences)
		// IMPORTANT: Do NOT consume the newline. Leave it for scanToken/scanNextToken
		// to handle, so blank lines are properly tracked and NEWLINE tokens are emitted.
		// This maintains the invariant that content tokens own their line, but the
		// line-ending newline belongs to scanNextToken for blank line detection.
		if ch == '\n' {
			break
		}
		// Handle escape sequences
		if ch == '\\' && l.pos+1 < len(l.source) {
			l.advance() // skip backslash
			l.advance() // skip escaped char
		} else {
			l.advance()
		}
	}

	if !closed {
		return Token{ILLEGAL, start, l.pos, line, col}
	}

	return Token{STRING, start, l.pos, line, col}
}

// scanTag scans a tag: #[A-Za-z0-9_-]+
func (l *Lexer) scanTag(start, line, col int) Token {
	// # already consumed

	for l.pos < len(l.source) && isValidInTag(l.source[l.pos]) {
		l.advance()
	}

	return Token{TAG, start, l.pos, line, col}
}

// scanLink scans a link: ^[A-Za-z0-9_-]+
func (l *Lexer) scanLink(start, line, col int) Token {
	// ^ already consumed

	for l.pos < len(l.source) && isValidInTag(l.source[l.pos]) {
		l.advance()
	}

	return Token{LINK, start, l.pos, line, col}
}

// scanAccountOrIdent scans an account name or identifier starting with capital letter or Unicode character.
// Accounts contain colons (Assets:Bank:Checking), identifiers don't (USD).
// Supports Unicode letters (French, German, Chinese, Japanese, Korean, Arabic, etc.)
func (l *Lexer) scanAccountOrIdent(start, line, col int) Token {
	// First character (capital letter or Unicode) already consumed
	hasColon := false

	for l.pos < len(l.source) && isValidInAccountOrIdent(l.source[l.pos]) {
		if l.source[l.pos] == ':' {
			hasColon = true
		}
		l.advance()
	}

	if hasColon {
		return Token{ACCOUNT, start, l.pos, line, col}
	}

	return Token{IDENT, start, l.pos, line, col}
}

// scanKeywordOrIdent scans a keyword or identifier starting with lowercase letter.
func (l *Lexer) scanKeywordOrIdent(start, line, col int) Token {
	// First character already consumed

	for l.pos < len(l.source) && isValidInIdentifier(l.source[l.pos]) {
		l.advance()
	}

	// Check if it's a keyword
	word := l.source[start:l.pos]
	tokType := l.keywordType(word)

	return Token{tokType, start, l.pos, line, col}
}

// keywordType returns the token type for a keyword, or IDENT if not a keyword.
func (l *Lexer) keywordType(word []byte) TokenType {
	// Use byte comparison to avoid allocating strings
	switch {
	case bytes.Equal(word, []byte("txn")):
		return TXN
	case bytes.Equal(word, []byte("balance")):
		return BALANCE
	case bytes.Equal(word, []byte("open")):
		return OPEN
	case bytes.Equal(word, []byte("close")):
		return CLOSE
	case bytes.Equal(word, []byte("commodity")):
		return COMMODITY
	case bytes.Equal(word, []byte("pad")):
		return PAD
	case bytes.Equal(word, []byte("note")):
		return NOTE
	case bytes.Equal(word, []byte("document")):
		return DOCUMENT
	case bytes.Equal(word, []byte("price")):
		return PRICE
	case bytes.Equal(word, []byte("event")):
		return EVENT
	case bytes.Equal(word, []byte("custom")):
		return CUSTOM
	case bytes.Equal(word, []byte("option")):
		return OPTION
	case bytes.Equal(word, []byte("include")):
		return INCLUDE
	case bytes.Equal(word, []byte("plugin")):
		return PLUGIN
	case bytes.Equal(word, []byte("pushtag")):
		return PUSHTAG
	case bytes.Equal(word, []byte("poptag")):
		return POPTAG
	case bytes.Equal(word, []byte("pushmeta")):
		return PUSHMETA
	case bytes.Equal(word, []byte("popmeta")):
		return POPMETA
	default:
		return IDENT
	}
}

// scanComment scans a comment line (;...) and returns a COMMENT token
func (l *Lexer) scanComment() Token {
	start := l.pos
	startLine := l.line
	startCol := l.column

	// Advance past the semicolon
	l.advance()

	// Scan to end of line
	for l.pos < len(l.source) && l.source[l.pos] != '\n' {
		l.advance()
	}

	// Consume the newline if present, as it's part of the comment's line
	if l.pos < len(l.source) && l.source[l.pos] == '\n' {
		l.advance()
	}

	return Token{COMMENT, start, l.pos, startLine, startCol}
}

// Helper methods

func (l *Lexer) peek() byte {
	if l.pos >= len(l.source) {
		return 0
	}
	return l.source[l.pos]
}

func (l *Lexer) peekIsDigit() bool {
	if l.pos >= len(l.source) {
		return false
	}
	return isDigit(l.source[l.pos])
}

func (l *Lexer) advance() byte {
	if l.pos >= len(l.source) {
		return 0
	}
	ch := l.source[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
	return ch
}

// Character classification helpers

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isLetter(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

func isUppercaseLetter(ch byte) bool {
	return ch >= 'A' && ch <= 'Z'
}

func isLowercaseLetter(ch byte) bool {
	return ch >= 'a' && ch <= 'z'
}

func isUTF8Byte(ch byte) bool {
	return ch >= 0x80
}

func isValidInTag(ch byte) bool {
	return isLetter(ch) || isDigit(ch) || ch == '_' || ch == '-'
}

func isValidInIdentifier(ch byte) bool {
	return isLetter(ch) || isDigit(ch) || ch == '_' || ch == '-'
}

func isValidInAccountOrIdent(ch byte) bool {
	return isLetter(ch) || isDigit(ch) || isUTF8Byte(ch) || ch == ':' || ch == '-'
}
