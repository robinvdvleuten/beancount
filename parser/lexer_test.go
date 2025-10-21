package parser

import (
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
			tokens := lexer.ScanAll()

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
			tokens := lexer.ScanAll()

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
			tokens := lexer.ScanAll()

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
			tokens := lexer.ScanAll()

			if len(tokens) < 1 {
				t.Fatal("expected at least 1 token")
			}

			if tokens[0].Type != ACCOUNT {
				t.Errorf("got type %s, want ACCOUNT", tokens[0].Type)
			}

			got := tokens[0].String(lexer.source)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
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
			tokens := lexer.ScanAll()

			if len(tokens) < 1 {
				t.Fatal("expected at least 1 token")
			}

			if tokens[0].Type != DATE {
				t.Errorf("got type %s, want DATE", tokens[0].Type)
			}

			got := tokens[0].String(lexer.source)
			if got != input {
				t.Errorf("got %q, want %q", got, input)
			}
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
			tokens := lexer.ScanAll()

			if len(tokens) < 1 {
				t.Fatal("expected at least 1 token")
			}

			if tokens[0].Type != want {
				t.Errorf("got type %s, want %s", tokens[0].Type, want)
			}
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
			tokens := lexer.ScanAll()

			if len(tokens) < 1 {
				t.Fatal("expected at least 1 token")
			}

			if tokens[0].Type != tt.want {
				t.Errorf("got type %s, want %s", tokens[0].Type, tt.want)
			}
		})
	}
}

func TestLexerComments(t *testing.T) {
	input := `; This is a comment
2014-01-01 open Assets:Bank
; Another comment
`

	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

	// Should have: DATE, OPEN, ACCOUNT, EOF
	// Comments should be skipped
	expectedTypes := []TokenType{DATE, OPEN, ACCOUNT, EOF}

	if len(tokens) != len(expectedTypes) {
		t.Fatalf("got %d tokens, want %d (comments should be skipped)", len(tokens), len(expectedTypes))
	}

	for i, tok := range tokens {
		if tok.Type != expectedTypes[i] {
			t.Errorf("token %d: got type %s, want %s", i, tok.Type, expectedTypes[i])
		}
	}
}

func TestLexerTransaction(t *testing.T) {
	input := `2014-05-05 * "Cafe" "Coffee"
  Expenses:Food:Coffee  4.50 USD
  Assets:Cash
`

	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

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
	}

	if len(tokens) != len(expectedTypes) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expectedTypes))
	}

	for i, tok := range tokens {
		if tok.Type != expectedTypes[i] {
			t.Errorf("token %d: got type %s, want %s (text: %q)",
				i, tok.Type, expectedTypes[i], tok.String(lexer.source))
		}
	}
}

func TestLexerBalance(t *testing.T) {
	input := `2014-08-09 balance Assets:Checking 100.00 USD`

	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

	expectedTypes := []TokenType{
		DATE,    // 2014-08-09
		BALANCE, // balance
		ACCOUNT, // Assets:Checking
		NUMBER,  // 100.00
		IDENT,   // USD
		EOF,
	}

	if len(tokens) != len(expectedTypes) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expectedTypes))
	}

	for i, tok := range tokens {
		if tok.Type != expectedTypes[i] {
			t.Errorf("token %d: got type %s, want %s", i, tok.Type, expectedTypes[i])
		}
	}
}

func TestLexerCost(t *testing.T) {
	input := `10 HOOL {518.73 USD}`

	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

	expectedTypes := []TokenType{
		NUMBER, // 10
		IDENT,  // HOOL
		LBRACE, // {
		NUMBER, // 518.73
		IDENT,  // USD
		RBRACE, // }
		EOF,
	}

	if len(tokens) != len(expectedTypes) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expectedTypes))
	}

	for i, tok := range tokens {
		if tok.Type != expectedTypes[i] {
			t.Errorf("token %d: got type %s, want %s", i, tok.Type, expectedTypes[i])
		}
	}
}

func TestLexerStringInterner(t *testing.T) {
	input := `Assets:Bank:Checking
Assets:Bank:Checking
Assets:Bank:Checking
`

	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

	// Should have 3 ACCOUNT tokens + EOF
	if len(tokens) != 4 {
		t.Fatalf("got %d tokens, want 4", len(tokens))
	}

	// All three should be the same account
	for i := 0; i < 3; i++ {
		if tokens[i].Type != ACCOUNT {
			t.Errorf("token %d: expected ACCOUNT", i)
		}
	}

	// Test that the interner is available and works
	interner := lexer.Interner()
	if interner == nil {
		t.Fatal("interner should be available")
	}

	// Manually intern the account strings (this is what the parser will do)
	acc1 := interner.InternBytes(tokens[0].Bytes(lexer.source))
	acc2 := interner.InternBytes(tokens[1].Bytes(lexer.source))
	acc3 := interner.InternBytes(tokens[2].Bytes(lexer.source))

	// All three should return the same pointer (string interning)
	// Note: We can't use == on strings for pointer comparison in Go,
	// but we can verify they're equal and the pool has only 1 entry
	if acc1 != acc2 || acc2 != acc3 {
		t.Error("all three account names should be equal")
	}

	// The interner should have exactly 1 unique string after interning
	if interner.Size() != 1 {
		t.Errorf("interner size: got %d, want 1 (string deduplication)", interner.Size())
	}
}

func TestLexerLineAndColumn(t *testing.T) {
	input := `2014-01-01 open Assets:Bank
2014-01-02 * "Test"
`

	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

	// First token should be on line 1, column 1
	if tokens[0].Line != 1 || tokens[0].Column != 1 {
		t.Errorf("first token: got line %d col %d, want line 1 col 1",
			tokens[0].Line, tokens[0].Column)
	}

	// Find the second DATE token (should be on line 2)
	secondDateIdx := -1
	for i := 1; i < len(tokens); i++ {
		if tokens[i].Type == DATE {
			secondDateIdx = i
			break
		}
	}

	if secondDateIdx == -1 {
		t.Fatal("didn't find second DATE token")
	}

	if tokens[secondDateIdx].Line != 2 {
		t.Errorf("second date: got line %d, want line 2", tokens[secondDateIdx].Line)
	}
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
		_ = lexer.ScanAll()
	}
}
