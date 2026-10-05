package parser

// Lexer implements a zero-copy lexer for Beancount files.
//
// The zero-copy approach:
// - Tokens store byte offsets, not string values
// - No intermediate token format conversions
// - String interning for repeated values
// - Pre-allocated token buffer

import (
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/ast"
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

	// signRun caches signedValueAhead's last scan, a run of signs and
	// spaces from signRunStart up to signRunEnd, so a long run is scanned
	// once rather than again from each of its signs.
	signRunStart, signRunEnd int
	signRunValue             bool // a digit or '(' ends the run
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
// This is a single-pass scanner with no backtracking. Like beancount's
// lexer, it fails on no input: a comment may hold any bytes, and invalid
// UTF-8 or a control character elsewhere is left to the parser, which
// reports it and recovers (a string's bytes are checked as it is
// unquoted, a word's here, and a control character is an ILLEGAL token).
// The error is always nil.
func (l *Lexer) ScanAll() ([]Token, error) {
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
		if breakLen := l.lineBreakLenAt(l.pos); breakLen > 0 {
			// Line break ends the current line.
			if !lineHasContent {
				if blankLineStartPos < 0 {
					blankLineStartPos = l.pos
					blankLineStartLine = l.line
					blankLineStartCol = l.column
				}
				tok := Token{NEWLINE, blankLineStartPos, l.pos + breakLen, blankLineStartLine, blankLineStartCol}
				l.consumeLineBreak()
				return tok
			}

			l.consumeLineBreak()
			lineHasContent = false
			continue
		}

		ch := l.source[l.pos]
		// Org-mode and other non-directive lines are ignored by the grammar
		// like comments, but must survive formatting, so they become COMMENT
		// tokens and end any directive body just like a column-1 comment.
		if l.column == 1 && l.isNonDirectiveLine() {
			return l.scanComment()
		}

		if isBlank(ch) {
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
	case ch == '(':
		tok = l.scanExpression(start, startLine, startCol, ch)
	case ch == '+' && l.peekIsDigit():
		tok = l.scanNumber(start, startLine, startCol)
	case ch == '+' && l.peek() == '(':
		tok = l.scanExpression(start, startLine, startCol, ch)
	case ch == '-' && l.peekIsDigit():
		tok = l.scanNumber(start, startLine, startCol)
	case ch == '-' && l.peek() == '(':
		tok = l.scanExpression(start, startLine, startCol, ch)
	case (ch == '+' || ch == '-') && l.signedValueAhead():
		// A sign followed by more signs or spaces (--1, +-1, - 1): the
		// parser evaluates the whole expression from here.
		tok = Token{EXPRESSION, start, l.pos, startLine, startCol}

	// Strings: "..."
	case ch == '"':
		tok = l.scanString(start, startLine, startCol)

	// Tags: #tag
	case ch == '#':
		if l.pos < len(l.source) && isValidInTag(l.source[l.pos]) {
			tok = l.scanTag(start, startLine, startCol)
		} else {
			tok = Token{FLAG, start, l.pos, startLine, startCol}
		}

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
	case ch == '&' || ch == '?' || ch == '%':
		tok = Token{FLAG, start, l.pos, startLine, startCol}
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
	case ch == '~':
		tok = Token{TILDE, start, l.pos, startLine, startCol}

	// A currency starting with a slash (/ESZ24), or a division
	case ch == '/':
		if n := slashCurrencyLen(l.source[start:]); n > 0 {
			l.pos = start + n
			l.column = startCol + n
			tok = Token{IDENT, start, l.pos, startLine, startCol}
		} else {
			tok = Token{ILLEGAL, start, l.pos, startLine, startCol}
		}

	case ch == '|':
		tok = Token{PIPE, start, l.pos, startLine, startCol}

	// A sign and a closing parenthesis are tokens of beancount's own,
	// which only its grammar rejects.
	case ch == '+' || ch == '-' || ch == ')':
		tok = Token{ILLEGAL, start, l.pos, startLine, startCol}

	default:
		tok = l.invalidToken(start, startLine, startCol)
	}

	// Consume trailing spaces/tabs only when they are followed by a line break.
	// This preserves spaces before comments while ensuring content tokens still own
	// their physical line when users leave trailing whitespace at end-of-line.
	savedPos := l.pos
	savedCol := l.column
	for l.pos < len(l.source) && isBlank(l.source[l.pos]) {
		l.pos++
		l.column++
	}
	if l.lineBreakLenAt(l.pos) == 0 {
		l.pos = savedPos
		l.column = savedCol
	}

	// Consume trailing line break if present, as this content token owns its line.
	if l.lineBreakLenAt(l.pos) > 0 {
		l.consumeLineBreak()
	}

	return tok
}

// isDatePattern checks if the position starts a date as beancount's lexer
// reads one (ast.DateLiteralLen).
func (l *Lexer) isDatePattern(start int) bool {
	return ast.DateLiteralLen(l.source[start:]) > 0
}

// scanDate scans a date: four or more digits, then twice a '-' or a '/'
// and digits, like beancount's lexer. It returns an ILLEGAL token if the
// date is invalid (year 0, invalid month/day): beancount's lexer validates
// dates at lex time.
func (l *Lexer) scanDate(start, line, col int) Token {
	// The first digit is already consumed.
	for range ast.DateLiteralLen(l.source[start:]) - 1 {
		l.advance()
	}

	src := l.source[start:l.pos]
	if !ast.IsValidDateLiteral(src) {
		return Token{ILLEGAL, start, l.pos, line, col}
	}

	return Token{DATE, start, l.pos, line, col}
}

// scanNumber scans a number: [-+]?[0-9]+(,[0-9]{3})*(\.[0-9]*)?
// Commas are allowed as thousands separators within the integer part, and,
// like beancount, a trailing dot without fraction digits ("5.").
func (l *Lexer) scanNumber(start, line, col int) Token {
	digitStart := start
	if l.source[start] == '+' || l.source[start] == '-' {
		digitStart = start + 1
	}

	for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
		l.advance()
	}

	if l.groupSeparatorAhead() {
		if l.pos-digitStart > 3 {
			l.consumeNumberRemainder()
			return Token{ILLEGAL, start, l.pos, line, col}
		}

		for l.groupSeparatorAhead() {
			l.advance()
			for i := 0; i < 3; i++ {
				if l.pos >= len(l.source) || !isDigit(l.source[l.pos]) {
					l.consumeNumberRemainder()
					return Token{ILLEGAL, start, l.pos, line, col}
				}
				l.advance()
			}
			if l.pos < len(l.source) && isDigit(l.source[l.pos]) {
				l.consumeNumberRemainder()
				return Token{ILLEGAL, start, l.pos, line, col}
			}
		}
	}

	// Scan optional decimal part
	if l.pos < len(l.source) && l.source[l.pos] == '.' {
		l.advance() // consume '.'
		for l.pos < len(l.source) && isDigit(l.source[l.pos]) {
			l.advance()
		}
	}

	return Token{NUMBER, start, l.pos, line, col}
}

// groupSeparatorAhead reports whether the next byte is a comma inside the
// number: one a digit (or, for the malformed 1,,000, another comma) follows.
// Like v2's number pattern, a number never ends on a comma, so any other
// comma ends it, as in a cost spec's {10, 2020-01-01}.
func (l *Lexer) groupSeparatorAhead() bool {
	if l.pos+1 >= len(l.source) || l.source[l.pos] != ',' {
		return false
	}
	next := l.source[l.pos+1]
	return isDigit(next) || next == ','
}

// signedValueAhead reports whether the sign just consumed starts a number
// expression through further signs or spaces: after them comes a digit or an
// opening parenthesis on the same line.
func (l *Lexer) signedValueAhead() bool {
	if l.pos < l.signRunStart || l.pos > l.signRunEnd {
		end := l.pos
		for end < len(l.source) && isSignOrBlank(l.source[end]) {
			end++
		}
		l.signRunStart, l.signRunEnd = l.pos, end
		l.signRunValue = end < len(l.source) && (isDigit(l.source[end]) || l.source[end] == '(')
	}
	// A sign or a space must come between the sign just consumed and
	// what ends the run.
	return l.signRunValue && l.signRunEnd > l.pos
}

func isSignOrBlank(ch byte) bool {
	return ch == '+' || ch == '-' || isBlank(ch)
}

func (l *Lexer) consumeNumberRemainder() {
	for l.pos < len(l.source) {
		ch := l.source[l.pos]
		if isDigit(ch) || ch == ',' || ch == '.' {
			l.advance()
			continue
		}
		break
	}
}

// scanExpression scans a signed or unsigned parenthesized amount expression.
func (l *Lexer) scanExpression(start, line, col int, first byte) Token {
	depth := 0
	if first == '(' {
		depth = 1
	} else {
		if l.pos >= len(l.source) || l.source[l.pos] != '(' {
			return Token{ILLEGAL, start, l.pos, line, col}
		}
		l.advance() // consume opening '(' after leading sign
		depth = 1
	}

	for l.pos < len(l.source) {
		if l.lineBreakLenAt(l.pos) > 0 {
			return Token{ILLEGAL, start, l.pos, line, col}
		}

		ch := l.advance()
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return Token{EXPRESSION, start, l.pos, line, col}
			}
		}
	}

	return Token{ILLEGAL, start, l.pos, line, col}
}

// scanString scans a quoted string: "..."
// Strings may span multiple physical lines. Like beancount's string
// pattern, a quote with no closing quote after it, or none before a
// backslash ending a line, opens no string: it starts an invalid token.
func (l *Lexer) scanString(start, line, col int) Token {
	// Opening quote already consumed
	if !l.closingQuoteAhead() {
		return l.invalidToken(start, line, col)
	}

	// Scan to the closing quote, counting the lines on the way.
	for {
		if l.lineBreakLenAt(l.pos) > 0 {
			l.consumeLineBreak()
			continue
		}
		ch := l.advance()
		if ch == '"' {
			return Token{STRING, start, l.pos, line, col}
		}
		// An escaped character, a \r of a \r\n line break included.
		if ch == '\\' {
			if l.lineBreakLenAt(l.pos) > 0 {
				l.consumeLineBreak()
			} else {
				l.advance()
			}
		}
	}
}

// closingQuoteAhead reports whether beancount's string pattern,
// "([^\\"]|\\.)*", finds the closing quote of a string opened just
// before the current position: one not escaped, with no backslash before
// a \n on the way, since its escape matches any character but \n.
func (l *Lexer) closingQuoteAhead() bool {
	for i := l.pos; i < len(l.source); i++ {
		switch l.source[i] {
		case '"':
			return true
		case '\\':
			if i+1 == len(l.source) || l.source[i+1] == '\n' {
				return false
			}
			i++
		}
	}
	return false
}

// invalidToken returns an ILLEGAL token from start up to the next
// whitespace, as beancount's lexer skips an invalid token when no rule
// matches its first character: a quote or any other character in it
// included.
func (l *Lexer) invalidToken(start, line, col int) Token {
	for l.pos < len(l.source) && !isWhitespaceOrLineBreak(l.source[l.pos]) {
		l.advance()
	}
	return Token{ILLEGAL, start, l.pos, line, col}
}

// scanTag scans a tag: #[A-Za-z0-9_-]+
func (l *Lexer) scanTag(start, line, col int) Token {
	// # already consumed

	for l.pos < len(l.source) && isValidInTag(l.source[l.pos]) {
		l.advance()
	}

	// The official lexer requires at least one name character (#[A-Za-z0-9-_/.]+).
	if l.pos == start+1 {
		return Token{ILLEGAL, start, l.pos, line, col}
	}

	return Token{TAG, start, l.pos, line, col}
}

// scanLink scans a link: ^[A-Za-z0-9_-]+
func (l *Lexer) scanLink(start, line, col int) Token {
	// ^ already consumed

	for l.pos < len(l.source) && isValidInTag(l.source[l.pos]) {
		l.advance()
	}

	// The official lexer requires at least one name character (^[A-Za-z0-9-_/.]+).
	if l.pos == start+1 {
		return Token{ILLEGAL, start, l.pos, line, col}
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

	value := l.source[start:l.pos]
	if !utf8.Valid(value) || (l.pos < len(l.source) && isControlChar(l.source[l.pos])) {
		// Like beancount's lexer, which cannot decode the word, or match
		// it with the control character it runs into, and skips it as an
		// invalid token up to the next whitespace.
		for l.pos < len(l.source) && !isWhitespaceOrLineBreak(l.source[l.pos]) {
			l.advance()
		}
		return Token{ILLEGAL, start, l.pos, line, col}
	}

	if hasColon {
		return Token{ACCOUNT, start, l.pos, line, col}
	}

	if len(value) == 1 && isUppercaseLetter(value[0]) && l.whitespaceAhead() {
		// Like beancount v3's CAPITAL token, a capital letter before
		// whitespace, which its grammar reads as a currency or a flag:
		// an IDENT, which the parser takes as a flag where one goes.
		return Token{IDENT, start, l.pos, line, col}
	}
	if isASCII(value) && !isValidCurrencyLiteral(value) {
		// Like flex's longest match, a currency starting the word is a
		// token of its own (BA- is BA and a minus), and the rest is
		// lexed after it.
		if n := currencyPrefixLen(value); n > 0 {
			l.pos = start + n
			l.column = col + n
			return Token{identType(value[:n]), start, l.pos, line, col}
		}
		return l.invalidToken(start, line, col)
	}

	return Token{identType(value), start, l.pos, line, col}
}

// identType returns BOOL for TRUE and FALSE and NONE for NULL, which
// beancount v3's lexer matches as keywords ahead of a currency, so none of
// them is ever a currency; any other word is an IDENT.
func identType(word []byte) TokenType {
	switch string(word) {
	case "TRUE", "FALSE":
		return BOOL
	case "NULL":
		return NONE
	}
	return IDENT
}

// scanKeywordOrIdent scans a word starting with a lowercase letter. Like
// beancount's lexer, which has no lowercase identifier, it is a keyword or a
// metadata key (a word of two characters at least followed directly by a
// colon, [a-z][a-zA-Z0-9\-_]+:). Like flex's longest match, a keyword
// starting any other word is a token of its own (closed is close, then d);
// the rest of the word, or a word with no keyword in front, such as a
// lowercase currency or a one-letter key, is an invalid token up to the next
// whitespace.
func (l *Lexer) scanKeywordOrIdent(start, line, col int) Token {
	// First character already consumed

	for l.pos < len(l.source) && isValidInIdentifier(l.source[l.pos]) {
		l.advance()
	}

	word := l.source[start:l.pos]
	if tokType := l.keywordType(word); tokType != IDENT {
		return Token{tokType, start, l.pos, line, col}
	}
	if len(word) > 1 && l.pos < len(l.source) && l.source[l.pos] == ':' {
		return Token{IDENT, start, l.pos, line, col}
	}
	for n := len(word) - 1; n > 0; n-- {
		if tokType := l.keywordType(word[:n]); tokType != IDENT {
			l.pos = start + n
			l.column = col + n
			return Token{tokType, start, l.pos, line, col}
		}
	}
	return l.invalidToken(start, line, col)
}

// keywordType returns the token type for a keyword, or IDENT if not a keyword.
// keywordMap maps keyword strings to their token types.
// Using a map allows O(1) lookup instead of sequential comparisons.
// Go optimizes map[string] lookups with []byte keys to avoid allocation.
var keywordMap = map[string]TokenType{
	"txn":       TXN,
	"balance":   BALANCE,
	"open":      OPEN,
	"close":     CLOSE,
	"commodity": COMMODITY,
	"pad":       PAD,
	"note":      NOTE,
	"document":  DOCUMENT,
	"price":     PRICE,
	"event":     EVENT,
	"query":     QUERY,
	"custom":    CUSTOM,
	"option":    OPTION,
	"include":   INCLUDE,
	"plugin":    PLUGIN,
	"pushtag":   PUSHTAG,
	"poptag":    POPTAG,
	"pushmeta":  PUSHMETA,
	"popmeta":   POPMETA,
}

func (l *Lexer) keywordType(word []byte) TokenType {
	if tt, ok := keywordMap[string(word)]; ok {
		return tt
	}
	return IDENT
}

// scanComment scans the rest of a comment line (;... or a non-directive
// line) and returns a COMMENT token
func (l *Lexer) scanComment() Token {
	start := l.pos
	startLine := l.line
	startCol := l.column

	// Scan to end of line
	for l.pos < len(l.source) && l.lineBreakLenAt(l.pos) == 0 {
		l.advance()
	}

	// Consume the trailing line break if present, as it's part of the comment's line.
	if l.lineBreakLenAt(l.pos) > 0 {
		l.consumeLineBreak()
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

// lineBreakLenAt returns the length of the line break at pos: 1 for \n, 2
// for \r\n, else 0. Like beancount's lexer, a lone \r breaks no line; it
// is whitespace (isBlank).
func (l *Lexer) lineBreakLenAt(pos int) int {
	if pos >= len(l.source) {
		return 0
	}
	switch l.source[pos] {
	case '\n':
		return 1
	case '\r':
		if pos+1 < len(l.source) && l.source[pos+1] == '\n' {
			return 2
		}
		return 0
	default:
		return 0
	}
}

func (l *Lexer) consumeLineBreak() {
	if l.pos >= len(l.source) {
		return
	}
	breakLen := l.lineBreakLenAt(l.pos)
	if breakLen == 0 {
		return
	}
	l.pos += breakLen
	l.line++
	l.column = 1
}

// Character classification helpers

// isBlank reports whether ch is whitespace within a line: a space, a tab or,
// as beancount's lexer reads it, a carriage return.
func isBlank(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r'
}

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
	return isLetter(ch) || isDigit(ch) || ch == '_' || ch == '-' || ch == '.' || ch == '/'
}

func isValidInIdentifier(ch byte) bool {
	return isLetter(ch) || isDigit(ch) || ch == '_' || ch == '-'
}

func isValidInAccountOrIdent(ch byte) bool {
	return isLetter(ch) || isDigit(ch) || isUTF8Byte(ch) || ch == ':' || ch == '-' || ch == '_' || ch == '.' || ch == '\''
}

// whitespaceAhead reports whether a space, a tab or a line break comes
// next, the trailing context of beancount v3's CAPITAL token.
func (l *Lexer) whitespaceAhead() bool {
	return l.pos < len(l.source) && (l.source[l.pos] == ' ' || l.source[l.pos] == '\t' || l.lineBreakLenAt(l.pos) > 0)
}

func isASCII(value []byte) bool {
	for _, ch := range value {
		if ch >= 0x80 {
			return false
		}
	}
	return true
}

// isCurrencyChar reports whether ch may follow a currency's first character.
func isCurrencyChar(ch byte) bool {
	return isUppercaseLetter(ch) || isDigit(ch) || ch == '\'' || ch == '.' || ch == '_' || ch == '-'
}

// isValidCurrencyLiteral reports whether value is a currency starting with
// a letter, as beancount v3's lexer matches one, of any length:
// [A-Z][A-Z0-9'._-]*[A-Z0-9].
func isValidCurrencyLiteral(value []byte) bool {
	if len(value) < 2 || !isUppercaseLetter(value[0]) {
		return false
	}
	last := value[len(value)-1]
	if !isUppercaseLetter(last) && !isDigit(last) {
		return false
	}
	for _, ch := range value[1 : len(value)-1] {
		if !isCurrencyChar(ch) {
			return false
		}
	}
	return true
}

// currencyPrefixLen returns the length of the longest currency starting
// value, as beancount v3's lexer matches one, or 0 when none does.
func currencyPrefixLen(value []byte) int {
	if !isUppercaseLetter(value[0]) {
		return 0
	}
	end := 0
	for i := 1; i < len(value) && isCurrencyChar(value[i]); i++ {
		if isUppercaseLetter(value[i]) || isDigit(value[i]) {
			end = i + 1
		}
	}
	return end
}

// slashCurrencyLen returns the length of the currency starting with the
// slash at s[0], as beancount v3's lexer matches one (a futures contract
// such as /ESZ24), or 0 when the slash is a division:
// /[A-Z0-9'._-]*[A-Z]([A-Z0-9'._-]*[A-Z0-9])?, the longest match. It holds
// at least one letter and ends in a letter, or in a digit after a letter.
func slashCurrencyLen(s []byte) int {
	end, hasLetter := 0, false
	for i := 1; i < len(s) && isCurrencyChar(s[i]); i++ {
		switch {
		case isUppercaseLetter(s[i]):
			hasLetter = true
			end = i + 1
		case hasLetter && isDigit(s[i]):
			end = i + 1
		}
	}
	return end
}

// isNonDirectiveLine reports whether the column-1 line at the lexer's
// position is one beancount's lexer skips with its rules ^[*:#]/. and
// ^[!&#?%]/. (org-mode headings and properties, flagged lines): its first
// character is one of those and a second one follows on the line. The
// rules match those two characters only, trailing context included, so
// under flex's longest match a TAG token of two or more characters
// starting there wins, a tie included, since the token rules come first
// (#1). A line starting with a letter is lexed like any other.
func (l *Lexer) isNonDirectiveLine() bool {
	rest := l.source[l.pos:]
	if len(rest) < 2 || !isNonDirectiveLineFlag(rest[0]) || rest[1] == '\n' {
		return false
	}
	return rest[0] != '#' || !isValidInTag(rest[1])
}

// isNonDirectiveLineFlag reports whether ch is one of the characters
// beancount's skipped-line rules start with: an org-mode or flag character.
func isNonDirectiveLineFlag(ch byte) bool {
	switch ch {
	case '*', ':', '#', '!', '&', '?', '%':
		return true
	}
	return false
}

// isControlChar reports whether ch is a control character other than the
// whitespace and line breaks beancount's lexer reads.
func isControlChar(ch byte) bool {
	return ch < 0x20 && ch != '\t' && ch != '\n' && ch != '\r'
}

// isWhitespaceOrLineBreak reports whether ch ends an invalid token in
// beancount's lexer, which skips an invalid token up to [ \t\n\r].
func isWhitespaceOrLineBreak(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}
