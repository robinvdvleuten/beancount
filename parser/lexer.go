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
)

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
func (l *Lexer) ScanAll() []Token {
	for l.pos < len(l.source) {
		l.skipWhitespace()

		if l.pos >= len(l.source) {
			break
		}

		// Skip comments
		if l.peek() == ';' {
			l.skipComment()
			continue
		}

		// Scan next token
		tok := l.scanToken()
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

	return l.tokens
}

// scanToken scans the next token from the current position.
func (l *Lexer) scanToken() Token {
	start := l.pos
	startLine := l.line
	startCol := l.column

	ch := l.advance()

	switch {
	// Check for dates first: YYYY-MM-DD (starts with digit)
	// This must come before number scanning
	case ch >= '0' && ch <= '9':
		// Peek ahead to check if this looks like a date
		if l.isDatePattern(start) {
			return l.scanDate(start, startLine, startCol)
		}
		return l.scanNumber(start, startLine, startCol)
	case ch == '-' && l.peekIsDigit():
		return l.scanNumber(start, startLine, startCol)

	// Strings: "..."
	case ch == '"':
		return l.scanString(start, startLine, startCol)

	// Tags: #tag
	case ch == '#':
		return l.scanTag(start, startLine, startCol)

	// Links: ^link
	case ch == '^':
		return l.scanLink(start, startLine, startCol)

	// Accounts (start with capital) or identifiers
	// Also check for non-ASCII bytes that might be Unicode uppercase or other letters
	case ch >= 'A' && ch <= 'Z' || ch >= 0x80:
		return l.scanAccountOrIdent(start, startLine, startCol)

	// Keywords or identifiers (start with lowercase)
	case ch >= 'a' && ch <= 'z':
		return l.scanKeywordOrIdent(start, startLine, startCol)

	// Single-character tokens
	case ch == '*':
		return Token{ASTERISK, start, l.pos, startLine, startCol}
	case ch == '!':
		return Token{EXCLAIM, start, l.pos, startLine, startCol}
	case ch == ':':
		return Token{COLON, start, l.pos, startLine, startCol}
	case ch == ',':
		return Token{COMMA, start, l.pos, startLine, startCol}

	// { or {{
	case ch == '{':
		if l.peek() == '{' {
			l.advance()
			return Token{LDBRACE, start, l.pos, startLine, startCol}
		}
		return Token{LBRACE, start, l.pos, startLine, startCol}

	// } or }}
	case ch == '}':
		if l.peek() == '}' {
			l.advance()
			return Token{RDBRACE, start, l.pos, startLine, startCol}
		}
		return Token{RBRACE, start, l.pos, startLine, startCol}

	// @ or @@
	case ch == '@':
		if l.peek() == '@' {
			l.advance()
			return Token{ATAT, start, l.pos, startLine, startCol}
		}
		return Token{AT, start, l.pos, startLine, startCol}

	default:
		return Token{ILLEGAL, start, l.pos, startLine, startCol}
	}
}

// isDatePattern checks if the position starts a date pattern YYYY-MM-DD
func (l *Lexer) isDatePattern(start int) bool {
	// Need at least 10 characters: YYYY-MM-DD
	if start+10 > len(l.source) {
		return false
	}

	// Check pattern: digit{4}-digit{2}-digit{2}
	src := l.source[start:]
	return src[0] >= '0' && src[0] <= '9' &&
		src[1] >= '0' && src[1] <= '9' &&
		src[2] >= '0' && src[2] <= '9' &&
		src[3] >= '0' && src[3] <= '9' &&
		src[4] == '-' &&
		src[5] >= '0' && src[5] <= '9' &&
		src[6] >= '0' && src[6] <= '9' &&
		src[7] == '-' &&
		src[8] >= '0' && src[8] <= '9' &&
		src[9] >= '0' && src[9] <= '9'
}

// scanDate scans a date: YYYY-MM-DD
func (l *Lexer) scanDate(start, line, col int) Token {
	// Date pattern is exactly 10 characters
	// First digit already consumed, consume remaining 9
	for i := 0; i < 9; i++ {
		l.advance()
	}
	return Token{DATE, start, l.pos, line, col}
}

// scanNumber scans a number: [-+]?[0-9]+(\.[0-9]+)?
func (l *Lexer) scanNumber(start, line, col int) Token {
	// Optional sign already consumed if present

	// Scan integer part
	for l.pos < len(l.source) && l.source[l.pos] >= '0' && l.source[l.pos] <= '9' {
		l.advance()
	}

	// Scan optional decimal part
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		// Look ahead to ensure next char is digit
		if l.pos+1 < len(l.source) && l.source[l.pos+1] >= '0' && l.source[l.pos+1] <= '9' {
			l.advance() // consume '.'
			for l.pos < len(l.source) && l.source[l.pos] >= '0' && l.source[l.pos] <= '9' {
				l.advance()
			}
		}
	}

	return Token{NUMBER, start, l.pos, line, col}
}

// scanString scans a quoted string: "..."
func (l *Lexer) scanString(start, line, col int) Token {
	// Opening quote already consumed

	// Scan until closing quote or end of line
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if ch == '"' {
			l.advance() // consume closing quote
			break
		}
		if ch == '\n' {
			// String shouldn't span lines in Beancount
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

	return Token{STRING, start, l.pos, line, col}
}

// scanTag scans a tag: #[A-Za-z0-9_-]+
func (l *Lexer) scanTag(start, line, col int) Token {
	// # already consumed

	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') &&
			(ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			break
		}
		l.advance()
	}

	return Token{TAG, start, l.pos, line, col}
}

// scanLink scans a link: ^[A-Za-z0-9_-]+
func (l *Lexer) scanLink(start, line, col int) Token {
	// ^ already consumed

	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') &&
			(ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			break
		}
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

	for l.pos < len(l.source) {
		ch := l.source[l.pos]

		// Accept: letters (ASCII or UTF-8), digits, colons, hyphens
		// UTF-8 continuation bytes (0x80-0xBF) and start bytes (0xC0-0xFF) are accepted
		isASCIILetter := (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
		isDigit := ch >= '0' && ch <= '9'
		isUTF8 := ch >= 0x80 // UTF-8 multi-byte character
		isSpecial := ch == ':' || ch == '-'

		if !isASCIILetter && !isDigit && !isUTF8 && !isSpecial {
			break
		}

		if ch == ':' {
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

	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') &&
			(ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			break
		}
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

// skipWhitespace skips whitespace and updates line/column tracking.
func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			break
		}
		if ch == '\n' {
			l.line++
			l.column = 1
			l.pos++
		} else {
			l.column++
			l.pos++
		}
	}
}

// skipComment skips a comment line (;...)
func (l *Lexer) skipComment() {
	// Skip to end of line
	for l.pos < len(l.source) && l.source[l.pos] != '\n' {
		l.pos++
	}
	// Skip the newline itself
	if l.pos < len(l.source) && l.source[l.pos] == '\n' {
		l.pos++
		l.line++
		l.column = 1
	}
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
	ch := l.source[l.pos]
	return ch >= '0' && ch <= '9'
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
