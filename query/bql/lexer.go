package bql

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// Lexer scans BQL query text into tokens. Whitespace (including newlines) is
// insignificant; queries may span multiple lines in the interactive shell.
type Lexer struct {
	source []byte
	pos    int
	line   int
	col    int
}

// NewLexer creates a lexer over the given query source.
func NewLexer(source []byte) *Lexer {
	return &Lexer{source: source, line: 1, col: 1}
}

// Next scans and returns the next token, or an EOF token at end of input.
func (l *Lexer) Next() Token {
	l.skipWhitespace()

	if l.pos >= len(l.source) {
		return Token{Type: EOF, Start: l.pos, End: l.pos, Line: l.line, Column: l.col}
	}

	start, line, col := l.pos, l.line, l.col
	c := l.source[l.pos]

	switch {
	case isLetter(c):
		return l.scanIdent(start, line, col)
	case isDigit(c):
		return l.scanNumberOrDate(start, line, col)
	case l.numberAhead(l.pos):
		// A leading dot or sign belongs to the number, as in bean-query's
		// [-+]?[0-9]*\.[0-9]+ and [-+]?[0-9]+ rules: -2 is one token, so
		// 1 -2 is two numbers, not a subtraction.
		if c == '-' || c == '+' {
			l.advance()
		}
		return l.scanNumber(start, line, col)
	case c == '"' || c == '\'':
		return l.scanString(c, start, line, col)
	}

	l.advance()
	tok := func(t TokenType) Token {
		return Token{Type: t, Start: start, End: l.pos, Line: line, Column: col}
	}

	switch c {
	case '(':
		return tok(LPAREN)
	case ')':
		return tok(RPAREN)
	case ',':
		return tok(COMMA)
	case ';':
		return tok(SEMICOLON)
	case '*':
		return tok(ASTERISK)
	case '/':
		return tok(SLASH)
	case '+':
		return tok(PLUS)
	case '-':
		return tok(MINUS)
	case '~':
		return tok(TILDE)
	case '=':
		return tok(EQ)
	case '!':
		if l.pos < len(l.source) && l.source[l.pos] == '=' {
			l.advance()
			return tok(NE)
		}
		return tok(ILLEGAL)
	case '<':
		if l.pos < len(l.source) && l.source[l.pos] == '=' {
			l.advance()
			return tok(LTE)
		}
		return tok(LT)
	case '>':
		if l.pos < len(l.source) && l.source[l.pos] == '=' {
			l.advance()
			return tok(GTE)
		}
		return tok(GT)
	}

	return tok(ILLEGAL)
}

// scanIdent scans an identifier or keyword. Like bean-query's
// [a-zA-Z][a-zA-Z_]* rule, identifiers hold no digits: account2 is the
// identifier account followed by the number 2.
func (l *Lexer) scanIdent(start, line, col int) Token {
	for l.pos < len(l.source) && (isLetter(l.source[l.pos]) || l.source[l.pos] == '_') {
		l.advance()
	}
	text := string(l.source[start:l.pos])
	typ := IDENT
	if kw, ok := keywords[strings.ToUpper(text)]; ok {
		typ = kw
	}
	return Token{Type: typ, Start: start, End: l.pos, Line: line, Column: col}
}

// scanNumberOrDate scans an INTEGER, DECIMAL, or DATE token. Date literals
// are detected by shape (YYYY-MM-DD); value validation happens in the parser
// so invalid dates report a positioned parse error, not a lexer error.
func (l *Lexer) scanNumberOrDate(start, line, col int) Token {
	if start+10 <= len(l.source) &&
		ast.IsDateLiteralShape(l.source[start:start+10]) &&
		l.source[start+4] == '-' {
		for l.pos < start+10 {
			l.advance()
		}
		return Token{Type: DATE, Start: start, End: l.pos, Line: line, Column: col}
	}
	return l.scanNumber(start, line, col)
}

// scanNumber scans the digits of an INTEGER, or a DECIMAL when a dot
// follows; either side of the dot may be empty (2. and .5).
func (l *Lexer) scanNumber(start, line, col int) Token {
	for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
		l.advance()
	}
	typ := INTEGER
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		typ = DECIMAL
		l.advance()
		for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
			l.advance()
		}
	}
	return Token{Type: typ, Start: start, End: l.pos, Line: line, Column: col}
}

// scanString scans a quoted string literal. Both single and double quotes are
// accepted, without escape sequences, matching the official BQL lexer.
func (l *Lexer) scanString(quote byte, start, line, col int) Token {
	l.advance() // opening quote
	for l.pos < len(l.source) && l.source[l.pos] != quote {
		l.advance()
	}
	if l.pos >= len(l.source) {
		// Unterminated string; report the whole remainder as illegal.
		return Token{Type: ILLEGAL, Start: start, End: l.pos, Line: line, Column: col}
	}
	l.advance() // closing quote
	return Token{Type: STRING, Start: start, End: l.pos, Line: line, Column: col}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.source) {
		switch l.source[l.pos] {
		case ' ', '\t', '\r', '\n':
			l.advance()
		default:
			return
		}
	}
}

func (l *Lexer) advance() {
	if l.source[l.pos] == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	l.pos++
}

// numberAhead reports whether a number without leading digits starts at
// pos: an optional sign followed by a digit, or by a dot and a digit.
func (l *Lexer) numberAhead(pos int) bool {
	if pos < len(l.source) && (l.source[pos] == '-' || l.source[pos] == '+') {
		pos++
	}
	if pos < len(l.source) && l.source[pos] == '.' {
		pos++
	}
	return pos < len(l.source) && isDigit(l.source[pos])
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
