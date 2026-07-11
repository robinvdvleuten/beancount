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
	TRUE
	FALSE
	NULL

	// Literals
	IDENT   // column or function name
	STRING  // "quoted" or 'quoted'
	INTEGER // 123
	DECIMAL // 123.45
	DATE    // YYYY-MM-DD

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
	EQ        // =
	NE        // !=
	LT        // <
	LTE       // <=
	GT        // >
	GTE       // >=
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
	TRUE:  "TRUE",
	FALSE: "FALSE",
	NULL:  "NULL",

	IDENT:   "IDENT",
	STRING:  "STRING",
	INTEGER: "INTEGER",
	DECIMAL: "DECIMAL",
	DATE:    "DATE",

	LPAREN:    "(",
	RPAREN:    ")",
	COMMA:     ",",
	SEMICOLON: ";",
	ASTERISK:  "*",
	SLASH:     "/",
	PLUS:      "+",
	MINUS:     "-",
	TILDE:     "~",
	EQ:        "=",
	NE:        "!=",
	LT:        "<",
	LTE:       "<=",
	GT:        ">",
	GTE:       ">=",
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
	"IN":       IN,
	"TRUE":     TRUE,
	"FALSE":    FALSE,
	"NULL":     NULL,
}

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
