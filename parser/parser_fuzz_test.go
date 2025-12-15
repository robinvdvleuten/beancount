package parser

import (
	"context"
	"testing"
)

func FuzzParser(f *testing.F) {
	// Seed corpus with representative valid inputs
	seeds := []string{
		// Basic directives
		"2014-01-01 open Assets:Checking USD",
		"2014-12-31 close Assets:Checking",
		"2014-08-09 balance Assets:Checking 100.00 USD",

		// Simple transaction
		"2014-05-05 * \"Cafe\" \"Coffee\"\n  Expenses:Food  4.50 USD\n  Assets:Cash",

		// Transaction with inferred amounts
		"2014-05-06 * \"Store\"\n  Expenses:Shopping  50.00 USD\n  Assets:Checking",

		// Options and includes
		"option \"title\" \"Example\"",
		"option \"operating_currency\" \"USD\"",
		"include \"accounts.beancount\"",

		// Comments and pragmas
		"; This is a comment",
		"pushtag #trip",
		"poptag #trip",

		// Edge cases
		"",                   // Empty input
		"  \n\n  \n",         // Whitespace only
		"; Just a comment\n", // Comment only

		// Metadata
		"2014-01-01 open Assets:Checking USD\n  description: \"Primary checking account\"",

		// Price directive
		"2014-07-09 price HOOL 579.18 USD",

		// Note directive
		"2014-07-09 note Assets:Checking \"Called about rebate\"",

		// Document directive
		"2014-07-09 document Assets:Checking \"/path/to/statement.pdf\"",

		// Event directive
		"2014-07-09 event \"location\" \"New York, USA\"",

		// Query directive
		"2014-07-09 query \"cash\" \"SELECT * FROM accounts WHERE account ~ 'Cash'\"",

		// Pad directive
		"2014-07-09 pad Assets:Checking Equity:Opening-Balances",

		// Custom directive
		"2014-07-09 custom \"budget\" Expenses:Food \"monthly\" 500.00 USD",
	}

	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// CRITICAL: Parser must never panic on any input
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Parser panicked on input %q: %v", data, r)
			}
		}()

		ctx := context.Background()
		ast, err := ParseBytes(ctx, data)

		// Validate invariants
		if err == nil {
			if ast == nil {
				t.Error("ParseBytes returned nil AST with nil error")
			}
		}
		// If err != nil, that's expected for invalid syntax - parser handled it gracefully
	})
}
