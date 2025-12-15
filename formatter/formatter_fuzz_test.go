package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/robinvdvleuten/beancount/parser"
)

func FuzzFormatter(f *testing.F) {
	// Seed corpus - ONLY valid beancount syntax
	seeds := []string{
		// Simple directives
		"2014-01-01 open Assets:Checking USD",
		"2014-12-31 close Assets:Checking",
		"2014-08-09 balance Assets:Checking 100.00 USD",

		// Simple transaction
		"2014-05-05 * \"Cafe\" \"Coffee\"\n  Expenses:Food  4.50 USD\n  Assets:Cash",

		// Transaction with inferred amount
		"2014-05-06 * \"Store\"\n  Expenses:Shopping  50.00 USD\n  Assets:Checking",

		// Option directive
		"option \"title\" \"Example\"",

		// Price directive
		"2014-07-09 price HOOL 579.18 USD",

		// Note directive
		"2014-07-09 note Assets:Checking \"Called about rebate\"",

		// Event directive
		"2014-07-09 event \"location\" \"New York, USA\"",

		// Pad directive
		"2014-07-09 pad Assets:Checking Equity:Opening-Balances",

		// Transaction with metadata
		"2014-01-05 * \"Coffee\"\n  description: \"Morning coffee\"\n  Expenses:Food  5.00 USD\n  Assets:Cash",

		// Transaction with tags and links
		"2014-01-06 * \"Lunch\" #food ^receipt-001\n  Expenses:Food  15.00 USD\n  Assets:Cash",

		// Multiple transactions
		"2014-01-01 * \"A\"\n  Assets:Cash  10 USD\n  Income:Salary\n\n2014-01-02 * \"B\"\n  Expenses:Food  5 USD\n  Assets:Cash",
	}

	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// CRITICAL: Must never panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Formatter panicked: %v\nInput: %q", r, data)
			}
		}()

		ctx := context.Background()

		// Parse original (filter invalid inputs)
		ast1, err := parser.ParseBytes(ctx, data)
		if err != nil {
			return // Skip invalid inputs - formatter only works on valid syntax
		}

		// Format
		var buf bytes.Buffer
		fmtr := New()
		if err := fmtr.Format(ctx, ast1, data, &buf); err != nil {
			t.Errorf("Format failed: %v", err)
			return
		}

		formatted := buf.Bytes()

		// Property 1: Parse(Format(Parse(x))) succeeds
		ast2, err := parser.ParseBytes(ctx, formatted)
		if err != nil {
			t.Errorf("Re-parsing failed: %v\nOriginal: %q\nFormatted: %q", err, data, formatted)
			return
		}

		if ast2 == nil {
			t.Error("Re-parsed AST is nil")
			return
		}

		// Property 2: Format(Format(x)) == Format(x) (idempotency)
		var buf2 bytes.Buffer
		if err := fmtr.Format(ctx, ast2, formatted, &buf2); err != nil {
			t.Errorf("Second format failed: %v", err)
			return
		}

		if !bytes.Equal(buf.Bytes(), buf2.Bytes()) {
			t.Errorf("Not idempotent:\nFirst:  %q\nSecond: %q", buf.Bytes(), buf2.Bytes())
		}
	})
}
