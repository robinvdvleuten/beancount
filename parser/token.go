package parser

// TokenType represents the type of token scanned from the input.
type TokenType uint8

const (
	// Special tokens
	EOF TokenType = iota
	ILLEGAL

	// Keywords - directive types
	TXN       // txn
	BALANCE   // balance
	OPEN      // open
	CLOSE     // close
	COMMODITY // commodity
	PAD       // pad
	NOTE      // note
	DOCUMENT  // document
	PRICE     // price
	EVENT     // event
	CUSTOM    // custom
	OPTION    // option
	INCLUDE   // include
	PLUGIN    // plugin
	PUSHTAG   // pushtag
	POPTAG    // poptag
	PUSHMETA  // pushmeta
	POPMETA   // popmeta

	// Literals
	DATE    // YYYY-MM-DD
	ACCOUNT // Assets:Bank:Checking
	STRING  // "quoted string"
	NUMBER  // 123.45 or -123.45
	IDENT   // USD, TRUE, FALSE, currency codes

	// Special literals
	TAG  // #tag
	LINK // ^link

	// Symbols
	ASTERISK // *
	EXCLAIM  // !
	COLON    // :
	COMMA    // ,
	AT       // @
	ATAT     // @@
	LBRACE   // {
	RBRACE   // }
	LDBRACE  // {{
	RDBRACE  // }}
	MINUS    // - (for negative numbers)
)

var tokenNames = map[TokenType]string{
	EOF:     "EOF",
	ILLEGAL: "ILLEGAL",

	TXN:       "txn",
	BALANCE:   "balance",
	OPEN:      "open",
	CLOSE:     "close",
	COMMODITY: "commodity",
	PAD:       "pad",
	NOTE:      "note",
	DOCUMENT:  "document",
	PRICE:     "price",
	EVENT:     "event",
	CUSTOM:    "custom",
	OPTION:    "option",
	INCLUDE:   "include",
	PLUGIN:    "plugin",
	PUSHTAG:   "pushtag",
	POPTAG:    "poptag",
	PUSHMETA:  "pushmeta",
	POPMETA:   "popmeta",

	DATE:    "DATE",
	ACCOUNT: "ACCOUNT",
	STRING:  "STRING",
	NUMBER:  "NUMBER",
	IDENT:   "IDENT",

	TAG:  "TAG",
	LINK: "LINK",

	ASTERISK: "*",
	EXCLAIM:  "!",
	COLON:    ":",
	COMMA:    ",",
	AT:       "@",
	ATAT:     "@@",
	LBRACE:   "{",
	RBRACE:   "}",
	LDBRACE:  "{{",
	RDBRACE:  "}}",
	MINUS:    "-",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return "UNKNOWN"
}

// Token represents a lexical token with zero-copy semantics.
// Instead of storing the token text as a string (which would allocate),
// we store byte offsets into the original source buffer.
type Token struct {
	Type   TokenType
	Start  int // Byte offset into source buffer
	End    int // End offset (exclusive)
	Line   int // Line number (1-indexed)
	Column int // Column number (1-indexed)
}

// String materializes the token text from the source buffer.
// This allocation only happens when the token text is actually needed,
// not during lexing (zero-copy).
func (t Token) String(source []byte) string {
	if t.Start >= len(source) || t.End > len(source) || t.Start > t.End {
		return ""
	}
	return string(source[t.Start:t.End])
}

// Bytes returns a zero-copy view of the token text.
// No allocation occurs - this is a slice into the source buffer.
func (t Token) Bytes(source []byte) []byte {
	if t.Start >= len(source) || t.End > len(source) || t.Start > t.End {
		return nil
	}
	return source[t.Start:t.End]
}

// Len returns the length of the token in bytes.
func (t Token) Len() int {
	return t.End - t.Start
}
