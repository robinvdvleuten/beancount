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
	case isIdentStart(c):
		return l.scanIdent(start, line, col)
	case isDigit(c):
		return l.scanNumberOrDate(start, line, col)
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

func (l *Lexer) scanIdent(start, line, col int) Token {
	for l.pos < len(l.source) && isIdentPart(l.source[l.pos]) {
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

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
