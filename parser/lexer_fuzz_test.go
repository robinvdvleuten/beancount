package parser

import (
	"testing"
)

func FuzzLexer(f *testing.F) {
	// Seed corpus with various token types
	seeds := []string{
		// Symbols
		"*", "!", ":", ",", "@", "@@", "{", "}", "(", ")", "[", "]",

		// Dates
		"2014-01-01", "2023-12-31", "2024-02-29", // Leap year

		// Numbers
		"123", "123.45", "-123.45", "+123.45", "0.00", "1000000.00",
		"1,000", "1,234,567.89", "-1,000.50",
		"1,00", "1,0000", "1,,000", "1234,567",

		// Strings
		"\"hello\"",
		"\"with spaces\"",
		"\"with \\\"quotes\\\"\"",
		"\"empty string: \\\"\\\"\"",

		// Accounts
		"Assets:Checking",
		"Expenses:Food:Restaurant",
		"Liabilities:CreditCard:CapitalOne",
		"Income:Salary:Acme",
		"Equity:Opening-Balances",

		// Tags and links
		"#tag", "#vacation", "#2024-trip",
		"^link", "^invoice-001", "^receipt-2024-01-15",

		// Keywords
		"txn", "balance", "open", "close", "pad", "note",
		"document", "price", "event", "query", "custom",
		"option", "include", "plugin",
		"pushtag", "poptag", "pushmeta", "popmeta",

		// Currencies
		"USD", "EUR", "GBP", "JPY", "BTC", "ETH",

		// Comments
		"; comment",
		"  ; indented comment",
		"; comment with symbols: * @ { }",

		// Whitespace
		" ", "\t", "\n", "\r\n", "   ",

		// Edge cases
		"",          // Empty
		"0",         // Single zero
		".",         // Just a dot
		"-",         // Just a minus
		"Assets",    // Partial account
		"Assets:",   // Account with trailing colon
		":Checking", // Account with leading colon
	}

	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// CRITICAL: Lexer must never panic on any input
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Lexer panicked on input %q: %v", data, r)
			}
		}()

		lexer := NewLexer(data, "fuzz-test")
		tokens, err := lexer.ScanAll()

		// Invalid UTF-8 is an acceptable error - just return without further checks
		if err != nil {
			return
		}

		assertTokenStreamInvariants(t, data, tokens)
	})
}

func assertTokenStreamInvariants(t *testing.T, data []byte, tokens []Token) {
	t.Helper()

	if tokens == nil {
		t.Fatal("ScanAll returned nil tokens")
	}
	if len(tokens) == 0 {
		t.Fatal("ScanAll returned zero tokens (expected at least EOF)")
	}

	for i, tok := range tokens {
		if tok.Line < 1 {
			t.Errorf("Token %d has invalid line %d", i, tok.Line)
		}
		if tok.Column < 1 {
			t.Errorf("Token %d has invalid column %d", i, tok.Column)
		}
		if tok.Start < 0 || tok.Start > tok.End || tok.End > len(data) {
			t.Errorf("Token %d has invalid range [%d:%d] for input length %d", i, tok.Start, tok.End, len(data))
		}
		if i > 0 && tok.Start < tokens[i-1].End {
			t.Errorf("Token %d starts before previous token ends: start=%d previousEnd=%d", i, tok.Start, tokens[i-1].End)
		}
		if tok.Type != EOF && tok.Start == tok.End {
			t.Errorf("Token %d (%s) has empty range [%d:%d]", i, tok.Type, tok.Start, tok.End)
		}
	}

	eof := tokens[len(tokens)-1]
	if eof.Type != EOF {
		t.Fatalf("Last token must be EOF, got %v", eof.Type)
	}
	if eof.Start != eof.End || eof.End != len(data) {
		t.Errorf("EOF range = [%d:%d], want [%d:%d]", eof.Start, eof.End, len(data), len(data))
	}
}
