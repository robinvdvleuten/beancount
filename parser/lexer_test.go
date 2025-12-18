package parser

import (
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestLexerBasicTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []TokenType
	}{
		{
			name:  "single asterisk",
			input: "*",
			want:  []TokenType{ASTERISK, EOF},
		},
		{
			name:  "exclamation",
			input: "!",
			want:  []TokenType{EXCLAIM, EOF},
		},
		{
			name:  "colon",
			input: ":",
			want:  []TokenType{COLON, EOF},
		},
		{
			name:  "comma",
			input: ",",
			want:  []TokenType{COMMA, EOF},
		},
		{
			name:  "at symbol",
			input: "@",
			want:  []TokenType{AT, EOF},
		},
		{
			name:  "double at",
			input: "@@",
			want:  []TokenType{ATAT, EOF},
		},
		{
			name:  "braces",
			input: "{ }",
			want:  []TokenType{LBRACE, RBRACE, EOF},
		},
		{
			name:  "transaction symbols",
			input: "* !",
			want:  []TokenType{ASTERISK, EXCLAIM, EOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.Equal(t, len(tt.want), len(tokens), "token count mismatch")

			for i, tok := range tokens {
				assert.Equal(t, tt.want[i], tok.Type, "token type mismatch")
			}
		})
	}
}

func TestLexerNumbers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"123", "123"},
		{"123.45", "123.45"},
		{"-123", "-123"},
		{"-123.45", "-123.45"},
		{"0.50", "0.50"},
		{"1000000", "1000000"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.True(t, len(tokens) >= 1, "expected at least 1 token")
			assert.Equal(t, NUMBER, tokens[0].Type)
			got := tokens[0].String(lexer.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLexerStrings(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"hello"`, `"hello"`},
		{`"hello world"`, `"hello world"`},
		{`""`, `""`},
		{`"with \"quotes\""`, `"with \"quotes\""`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.True(t, len(tokens) >= 1, "expected at least 1 token")
			assert.Equal(t, STRING, tokens[0].Type)
			got := tokens[0].String(lexer.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLexerAccounts(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Assets:Bank:Checking", "Assets:Bank:Checking"},
		{"Liabilities:CreditCard", "Liabilities:CreditCard"},
		{"Expenses:Food:Restaurant", "Expenses:Food:Restaurant"},
		{"Income:Salary", "Income:Salary"},
		{"Equity:Opening-Balances", "Equity:Opening-Balances"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.True(t, len(tokens) >= 1, "expected at least 1 token")
			assert.Equal(t, ACCOUNT, tokens[0].Type)

			got := tokens[0].String(lexer.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLexerDates(t *testing.T) {
	tests := []string{
		"2014-01-01",
		"2023-12-31",
		"2024-06-15",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lexer := NewLexer([]byte(input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.True(t, len(tokens) >= 1, "expected at least 1 token")
			assert.Equal(t, DATE, tokens[0].Type)

			got := tokens[0].String(lexer.source)
			assert.Equal(t, input, got)
		})
	}
}

func TestLexerKeywords(t *testing.T) {
	tests := map[string]TokenType{
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
		"custom":    CUSTOM,
		"option":    OPTION,
		"include":   INCLUDE,
		"plugin":    PLUGIN,
		"pushtag":   PUSHTAG,
		"poptag":    POPTAG,
		"pushmeta":  PUSHMETA,
		"popmeta":   POPMETA,
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			lexer := NewLexer([]byte(input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.True(t, len(tokens) >= 1, "expected at least 1 token")
			assert.Equal(t, want, tokens[0].Type)
		})
	}
}

func TestLexerTagsAndLinks(t *testing.T) {
	tests := []struct {
		input string
		want  TokenType
	}{
		{"#tag", TAG},
		{"#trip-europe", TAG},
		{"#budget_2024", TAG},
		{"^link", LINK},
		{"^invoice-123", LINK},
		{"^payment_ref", LINK},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input), "test")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)

			assert.True(t, len(tokens) >= 1, "expected at least 1 token")
			assert.Equal(t, tt.want, tokens[0].Type)
		})
	}
}

func TestLexerComments(t *testing.T) {
	input := `; This is a comment
2014-01-01 open Assets:Bank
; Another comment
`

	lexer := NewLexer([]byte(input), "test")
	tokens, err := lexer.ScanAll()
	assert.NoError(t, err)

	// Should have: COMMENT, DATE, OPEN, ACCOUNT, COMMENT, EOF
	// Comments are now emitted as tokens
	// Note: trailing newline at EOF does not generate a NEWLINE token
	// (only internal blank lines generate NEWLINE tokens)
	expectedTypes := []TokenType{COMMENT, DATE, OPEN, ACCOUNT, COMMENT, EOF}

	assert.Equal(t, len(expectedTypes), len(tokens), "comments should be emitted as tokens")

	for i, tok := range tokens {
		assert.Equal(t, expectedTypes[i], tok.Type, "token %d type mismatch (got %v)", i, tok.Type)
	}
}

func TestLexerTransaction(t *testing.T) {
	input := `2014-05-05 * "Cafe" "Coffee"
  Expenses:Food:Coffee  4.50 USD
  Assets:Cash
`

	lexer := NewLexer([]byte(input), "test")
	tokens, err := lexer.ScanAll()
	assert.NoError(t, err)

	expectedTypes := []TokenType{
		DATE,     // 2014-05-05
		ASTERISK, // *
		STRING,   // "Cafe"
		STRING,   // "Coffee"
		ACCOUNT,  // Expenses:Food:Coffee
		NUMBER,   // 4.50
		IDENT,    // USD
		ACCOUNT,  // Assets:Cash
		EOF,
		// Note: trailing newline at EOF does not generate a NEWLINE token
	}

	assert.Equal(t, len(expectedTypes), len(tokens))

	for i, tok := range tokens {
		assert.Equal(t, expectedTypes[i], tok.Type,
			"token %d type mismatch (text: %q)", i, tok.String(lexer.source))
	}
}

func TestLexerBalance(t *testing.T) {
	input := `2014-08-09 balance Assets:Checking 100.00 USD`

	lexer := NewLexer([]byte(input), "test")
	tokens, err := lexer.ScanAll()
	assert.NoError(t, err)

	expectedTypes := []TokenType{
		DATE,    // 2014-08-09
		BALANCE, // balance
		ACCOUNT, // Assets:Checking
		NUMBER,  // 100.00
		IDENT,   // USD
		EOF,
	}

	assert.Equal(t, len(expectedTypes), len(tokens))

	for i, tok := range tokens {
		assert.Equal(t, expectedTypes[i], tok.Type, "token %d type mismatch", i)
	}
}

func TestLexerCost(t *testing.T) {
	input := `10 HOOL {518.73 USD}`

	lexer := NewLexer([]byte(input), "test")
	tokens, err := lexer.ScanAll()
	assert.NoError(t, err)

	expectedTypes := []TokenType{
		NUMBER, // 10
		IDENT,  // HOOL
		LBRACE, // {
		NUMBER, // 518.73
		IDENT,  // USD
		RBRACE, // }
		EOF,
	}

	assert.Equal(t, len(expectedTypes), len(tokens))

	for i, tok := range tokens {
		assert.Equal(t, expectedTypes[i], tok.Type, "token %d type mismatch", i)
	}
}

func TestLexerStringInterner(t *testing.T) {
	input := `Assets:Bank:Checking
Assets:Bank:Checking
Assets:Bank:Checking
`

	lexer := NewLexer([]byte(input), "test")
	tokens, err := lexer.ScanAll()
	assert.NoError(t, err)

	// Should have 3 ACCOUNT tokens + EOF (trailing newline at EOF doesn't generate NEWLINE)
	assert.Equal(t, 4, len(tokens))

	// All three should be the same account
	for i := 0; i < 3; i++ {
		assert.Equal(t, ACCOUNT, tokens[i].Type, "token %d type mismatch", i)
	}

	// Test that the interner is available and works
	interner := lexer.Interner()
	assert.NotEqual(t, nil, interner, "interner should be available")

	// Manually intern the account strings (this is what the parser will do)
	acc1 := interner.InternBytes(tokens[0].Bytes(lexer.source))
	acc2 := interner.InternBytes(tokens[1].Bytes(lexer.source))
	acc3 := interner.InternBytes(tokens[2].Bytes(lexer.source))

	// All three should return the same pointer (string interning)
	// Note: We can't use == on strings for pointer comparison in Go,
	// but we can verify they're equal and the pool has only 1 entry
	assert.True(t, acc1 == acc2 && acc2 == acc3, "all three account names should be equal")

	// The interner should have exactly 1 unique string after interning
	assert.Equal(t, 1, interner.Size(), "string deduplication")
}

func TestLexerLineAndColumn(t *testing.T) {
	input := `2014-01-01 open Assets:Bank
2014-01-02 * "Test"
`

	lexer := NewLexer([]byte(input), "test")
	tokens, err := lexer.ScanAll()
	assert.NoError(t, err)

	// First token should be on line 1, column 1
	assert.Equal(t, 1, tokens[0].Line, "first token line")
	assert.Equal(t, 1, tokens[0].Column, "first token column")

	// Find the second DATE token (should be on line 2)
	secondDateIdx := -1
	for i := 1; i < len(tokens); i++ {
		if tokens[i].Type == DATE {
			secondDateIdx = i
			break
		}
	}

	assert.NotEqual(t, -1, secondDateIdx, "didn't find second DATE token")
	assert.Equal(t, 2, tokens[secondDateIdx].Line, "second date line")
}

// Benchmark zero-copy performance
func BenchmarkLexer(b *testing.B) {
	input := []byte(`2014-05-05 * "Cafe Mogador" "Lamb tagine with wine"
  Liabilities:CreditCard:CapitalOne  -37.45 USD
  Expenses:Food:Restaurant

2014-05-06 balance Assets:Checking 500.00 USD

2014-05-07 ! "Pending" #tag ^link
  Assets:Bank:Checking  -100.00 USD
  Expenses:Shopping     100.00 USD
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewLexer(input, "bench")
		_, _ = lexer.ScanAll()
	}
}

func TestInvalidUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantLine int
		wantByte byte
	}{
		{
			name:     "invalid byte 0xff",
			input:    []byte("2024-01-01\xff"),
			wantLine: 1,
			wantByte: 0xff,
		},
		{
			name:     "null byte",
			input:    []byte("2024-01-01\x00"),
			wantLine: 1,
			wantByte: 0x00,
		},
		{
			name:     "control char 0x01",
			input:    []byte("2024-01-01\x01"),
			wantLine: 1,
			wantByte: 0x01,
		},
		{
			name:     "control char 0x1f",
			input:    []byte("2024-01-01\x1f"),
			wantLine: 1,
			wantByte: 0x1f,
		},
		{
			name:     "invalid UTF-8 after valid chars",
			input:    []byte("2024-01-01 * \"desc\"\xff"),
			wantLine: 1,
			wantByte: 0xff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input, "test.beancount")
			_, err := lexer.ScanAll()
			assert.Error(t, err)

			var utf8Err *InvalidUTF8Error
			assert.True(t, errors.As(err, &utf8Err), "expected InvalidUTF8Error")
			assert.Equal(t, utf8Err.Line, tt.wantLine)
			assert.Equal(t, utf8Err.Byte, tt.wantByte)
		})
	}
}

func TestValidUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "ASCII only",
			input: "2024-01-01 * \"test\"",
		},
		{
			name:  "valid UTF-8 with accents",
			input: "2024-01-01 * \"Café\"",
		},
		{
			name:  "valid UTF-8 with Japanese",
			input: "2024-01-01 * \"日本語\"",
		},
		{
			name:  "valid UTF-8 with Chinese",
			input: "2024-01-01 * \"中文\"",
		},
		{
			name:  "valid UTF-8 with emoji",
			input: "2024-01-01 * \"test 😀\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input), "test.beancount")
			tokens, err := lexer.ScanAll()
			assert.NoError(t, err)
			assert.True(t, len(tokens) > 0, "expected tokens")
		})
	}
}
