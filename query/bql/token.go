package bql

// TokenType represents the type of token scanned from a BQL query string.
type TokenType uint8

const (
	// Special tokens
	EOF TokenType = iota
	ILLEGAL

	// Keywords - statements
	SELECT
	BALANCES
	JOURNAL
	PRINT

	// Keywords - clauses
	DISTINCT
	FROM
	WHERE
	GROUP
	HAVING
	ORDER
	PIVOT
	BY
	ASC
	DESC
	LIMIT
	AS
	AT

	// Keywords - FROM transforms
	OPEN
	CLOSE
	CLEAR
	ON

	// Keywords - operators and literals
	AND
	OR
	NOT
	IN
	IS
	TRUE
	FALSE
	NULL

	// Literals
	IDENT   // column or function name
	STRING  // "quoted" or 'quoted'
	INTEGER // 123
	DECIMAL // 123.45
	DATE    // YYYY-MM-DD
	TABLE   // #name, or # alone: a Table reference

	// Symbols
	LPAREN    // (
	RPAREN    // )
	COMMA     // ,
	SEMICOLON // ;
	ASTERISK  // *
	SLASH     // /
	PLUS      // +
	MINUS     // -
	TILDE     // ~
	DOT       // .
	LBRACKET  // [
	RBRACKET  // ]
	EQ        // =
	NE        // !=
	LT        // <
	LTE       // <=
	GT        // >
	GTE       // >=
	PERCENT   // %
	NOTTILDE  // !~
	QTILDE    // ?~

	// NOTIN is the operator of x NOT IN y, the two keywords NOT and IN.
	NOTIN
)

var tokenNames = map[TokenType]string{
	EOF:     "EOF",
	ILLEGAL: "ILLEGAL",

	SELECT:   "SELECT",
	BALANCES: "BALANCES",
	JOURNAL:  "JOURNAL",
	PRINT:    "PRINT",

	DISTINCT: "DISTINCT",
	FROM:     "FROM",
	WHERE:    "WHERE",
	GROUP:    "GROUP",
	HAVING:   "HAVING",
	ORDER:    "ORDER",
	PIVOT:    "PIVOT",
	BY:       "BY",
	ASC:      "ASC",
	DESC:     "DESC",
	LIMIT:    "LIMIT",
	AS:       "AS",
	AT:       "AT",

	OPEN:  "OPEN",
	CLOSE: "CLOSE",
	CLEAR: "CLEAR",
	ON:    "ON",

	AND:   "AND",
	OR:    "OR",
	NOT:   "NOT",
	IN:    "IN",
	IS:    "IS",
	TRUE:  "TRUE",
	FALSE: "FALSE",
	NULL:  "NULL",

	IDENT:   "IDENT",
	STRING:  "STRING",
	INTEGER: "INTEGER",
	DECIMAL: "DECIMAL",
	DATE:    "DATE",
	TABLE:   "TABLE",

	LPAREN:    "(",
	RPAREN:    ")",
	COMMA:     ",",
	SEMICOLON: ";",
	ASTERISK:  "*",
	SLASH:     "/",
	PLUS:      "+",
	MINUS:     "-",
	TILDE:     "~",
	DOT:       ".",
	LBRACKET:  "[",
	RBRACKET:  "]",
	EQ:        "=",
	NE:        "!=",
	LT:        "<",
	LTE:       "<=",
	GT:        ">",
	GTE:       ">=",
	PERCENT:   "%",
	NOTTILDE:  "!~",
	QTILDE:    "?~",
	NOTIN:     "NOT IN",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return "UNKNOWN"
}

// keywords maps upper-cased identifier text to keyword token types.
// BQL keywords are case-insensitive.
var keywords = map[string]TokenType{
	"SELECT":   SELECT,
	"BALANCES": BALANCES,
	"JOURNAL":  JOURNAL,
	"PRINT":    PRINT,
	"DISTINCT": DISTINCT,
	"FROM":     FROM,
	"WHERE":    WHERE,
	"GROUP":    GROUP,
	"HAVING":   HAVING,
	"ORDER":    ORDER,
	"PIVOT":    PIVOT,
	"BY":       BY,
	"ASC":      ASC,
	"DESC":     DESC,
	"LIMIT":    LIMIT,
	"AS":       AS,
	"AT":       AT,
	"OPEN":     OPEN,
	"CLOSE":    CLOSE,
	"CLEAR":    CLEAR,
	"ON":       ON,
	"AND":      AND,
	"OR":       OR,
	"NOT":      NOT,
	"IS":       IS,
	"IN":       IN,
	"TRUE":     TRUE,
	"FALSE":    FALSE,
	"NULL":     NULL,
}

// unreservedKeywords are the keywords beanquery does not reserve
// (parser.py's KEYWORDS): outside the clauses they start, they are names.
// NULL is one too, a literal wherever an expression takes one and a name
// elsewhere (an alias, an attribute, a function).
var unreservedKeywords = map[TokenType]struct{}{
	AT: {}, OPEN: {}, CLOSE: {}, CLEAR: {}, ON: {}, NULL: {},
}

// reservedKeywords holds the keyword token types that cannot be a name.
var reservedKeywords = func() map[TokenType]struct{} {
	types := make(map[TokenType]struct{}, len(keywords))
	for _, t := range keywords {
		if _, ok := unreservedKeywords[t]; !ok {
			types[t] = struct{}{}
		}
	}
	return types
}()

// Token represents a lexical token with zero-copy semantics. Like the core
// beancount parser, tokens store byte offsets into the source buffer instead
// of materialized strings.
type Token struct {
	Type   TokenType
	Start  int // Byte offset into source buffer
	End    int // End offset (exclusive)
	Line   int // Line number (1-indexed)
	Column int // Column number (1-indexed)
}

// String materializes the token text from the source buffer.
func (t Token) String(source []byte) string {
	if t.Start >= len(source) || t.End > len(source) || t.Start > t.End {
		return ""
	}
	return string(source[t.Start:t.End])
}

// Bytes returns a zero-copy view of the token text.
func (t Token) Bytes(source []byte) []byte {
	if t.Start >= len(source) || t.End > len(source) || t.Start > t.End {
		return nil
	}
	return source[t.Start:t.End]
}
