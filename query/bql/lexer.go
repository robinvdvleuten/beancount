package bql

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

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
	case isLetter(c) || c == '_':
		return l.scanIdent(start, line, col)
	case isDigit(c):
		return l.scanNumberOrDate(start, line, col)
	case c == '.' && l.pos+1 < len(l.source) && isDigit(l.source[l.pos+1]):
		// A leading dot belongs to the number, as in beanquery's
		// [0-9]*\.[0-9]+ rule. Numbers carry no sign: - and + are
		// operators, so 1 -2 is a subtraction.
		return l.scanNumber(start, line, col)
	case c == '"' || c == '\'':
		return l.scanString(c, start, line, col)
	case c == '#':
		return l.scanTable(start, line, col)
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
	case '.':
		// A dot before a digit starts a number, above.
		return tok(DOT)
	case '[':
		return tok(LBRACKET)
	case ']':
		return tok(RBRACKET)
	case '=':
		return tok(EQ)
	case '%':
		return tok(PERCENT)
	case '!':
		if l.pos < len(l.source) && l.source[l.pos] == '=' {
			l.advance()
			return tok(NE)
		}
		if l.pos < len(l.source) && l.source[l.pos] == '~' {
			l.advance()
			return tok(NOTTILDE)
		}
		return tok(ILLEGAL)
	case '?':
		if l.pos < len(l.source) && l.source[l.pos] == '~' {
			l.advance()
			return tok(QTILDE)
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

// scanIdent scans an identifier or keyword, beanquery's
// [a-zA-Z_][a-zA-Z0-9_]* rule.
func (l *Lexer) scanIdent(start, line, col int) Token {
	for l.pos < len(l.source) && (isLetter(l.source[l.pos]) || isDigit(l.source[l.pos]) || l.source[l.pos] == '_') {
		l.advance()
	}
	text := string(l.source[start:l.pos])
	typ := IDENT
	if kw, ok := keywords[strings.ToUpper(text)]; ok {
		typ = kw
	}
	return Token{Type: typ, Start: start, End: l.pos, Line: line, Column: col}
}

// scanTable scans beanquery's #([a-zA-Z_][a-zA-Z0-9_]*)? rule: a # and the
// name of a Table, if one follows it at once.
func (l *Lexer) scanTable(start, line, col int) Token {
	l.advance() // #
	if l.pos < len(l.source) && (isLetter(l.source[l.pos]) || l.source[l.pos] == '_') {
		for l.pos < len(l.source) && (isLetter(l.source[l.pos]) || isDigit(l.source[l.pos]) || l.source[l.pos] == '_') {
			l.advance()
		}
	}
	return Token{Type: TABLE, Start: start, End: l.pos, Line: line, Column: col}
}

// scanNumberOrDate scans an INTEGER, DECIMAL, or DATE token. Date literals
// are detected by shape (YYYY-MM-DD); the parser checks the date exists.
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

// scanString scans a quoted string, beanquery's string rule:
//
//	"[^"]*"|'(?:[^']|'')*'
//
// A single-quoted string runs on over a doubled quote, a double-quoted one
// ends at its first closing quote. There are no escape sequences, and the
// token keeps the raw text, doubled quotes included, which is the string's
// value. A doubled quote in a quoted identifier ("a""b") lexes as adjacent
// strings, which the parser joins (parser.quotedIdent).
func (l *Lexer) scanString(quote byte, start, line, col int) Token {
	l.advance() // opening quote
	for l.pos < len(l.source) {
		if l.source[l.pos] != quote {
			l.advance()
			continue
		}
		if quote == '\'' && l.pos+1 < len(l.source) && l.source[l.pos+1] == '\'' {
			l.advance()
			l.advance()
			continue
		}
		l.advance() // closing quote
		return Token{Type: STRING, Start: start, End: l.pos, Line: line, Column: col}
	}
	// Unterminated string; report the whole remainder as illegal.
	return Token{Type: ILLEGAL, Start: start, End: l.pos, Line: line, Column: col}
}

// skipWhitespace skips white space, what beanquery's \s matches (IsSpace),
// an end-of-line comment and a block comment, in any order.
func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.source) {
		if end := l.blockCommentEnd(); end > 0 {
			for l.pos < end {
				l.advance()
			}
			continue
		}
		if l.source[l.pos] == ';' {
			if !l.atComment() {
				return
			}
			for l.pos < len(l.source) && l.source[l.pos] != '\n' {
				l.advance()
			}
			continue
		}
		r, size := utf8.DecodeRune(l.source[l.pos:])
		if !IsSpace(r) {
			return
		}
		for range size {
			l.advance()
		}
	}
}

// IsSpace reports whether r is white space to Python: what str.isspace
// and a str pattern's \s match, which adds \x1c to \x1f to Go's white
// space.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || r >= '\x1c' && r <= '\x1f'
}

// blockCommentEnd returns the offset just past the block comment at the
// current position, or 0 when none starts there. beanquery's comments
// pattern, /\*([^*]|[\r\n]|(\*+([^*\/]|[\r\n])))*\*+/, ends a comment at
// the first */ after its /*, across lines; an unterminated /* is no
// comment, so its / is a token and the parser reports the syntax error.
func (l *Lexer) blockCommentEnd() int {
	if !bytes.HasPrefix(l.source[l.pos:], []byte("/*")) {
		return 0
	}
	end := bytes.Index(l.source[l.pos+2:], []byte("*/"))
	if end < 0 {
		return 0
	}
	return l.pos + 2 + end + 2
}

// atComment reports whether the ; at the current position starts
// beanquery's end-of-line comment, ;[^\n]*?$ without re.MULTILINE: one that
// runs to the end of the text, or to a newline that ends it. Any other ;
// is a token, which may end the statement.
func (l *Lexer) atComment() bool {
	newline := bytes.IndexByte(l.source[l.pos:], '\n')
	return newline < 0 || l.pos+newline == len(l.source)-1
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

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
