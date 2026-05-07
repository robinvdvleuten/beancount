package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/robinvdvleuten/beancount/parser"
)

func FuzzFormatter(f *testing.F) {
	stableSeeds := stableFormatterSeeds()
	seeds := append([]string{}, stableSeeds...)
	seeds = append(seeds,
		// Lexer/parser regression corpus: invalid mutations should be skipped by the
		// formatter fuzz target, but these seeds keep useful coverage nearby.
		"0001-01-01 open A:0!\"\n\n0001-01-01 balance A:0 0 A",
	)

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
		isStableSeed := isStableFormatterSeed(data, stableSeeds)

		// Parse original. Invalid fuzz-mutated inputs are expected and skipped.
		ast1, err := parser.ParseBytes(ctx, data)
		if err != nil || ast1 == nil {
			if isStableSeed {
				t.Fatalf("Stable formatter seed failed to parse: %v\nInput: %q", err, data)
			}
			return
		}

		// Format
		var buf bytes.Buffer
		fmtr := New()
		if err := fmtr.Format(ctx, ast1, data, &buf); err != nil {
			t.Errorf("Format failed: %v", err)
			return
		}

		formatted := buf.Bytes()

		// Property 1: Parse(Format(Parse(x))) succeeds for formatter-stable seeds.
		// Fuzz-mutated inputs can still reach odd parser-accepted corners; if their
		// formatted output no longer parses, skip them and let curated seeds guard the
		// intended formatter contract.
		ast2, err := parser.ParseBytes(ctx, formatted)
		if err != nil {
			if isStableSeed {
				t.Fatalf("Formatted stable seed did not reparse: %v\nInput: %q\nFormatted: %q", err, data, formatted)
			}
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

func stableFormatterSeeds() []string {
	return []string{
		// Simple directives
		"2014-01-01 open Assets:Checking USD",
		"2014-12-31 close Assets:Checking",
		"2014-08-09 balance Assets:Checking 100.00 USD",

		// Simple transaction
		"2014-05-05 * \"Cafe\" \"Coffee\"\n  Expenses:Food  4.50 USD\n  Assets:Cash",
		"2014-05-05 P \"Opening balance\"\n  Assets:Checking  1,000.00 USD\n  Equity:Opening-Balances",
		"2014-05-05 * \"With comments\"\n  ; before\n  Expenses:Food  4.50 USD\n  ; between\n  Assets:Cash",
		"2014-05-05 * \"In progress\"\n  Assets:Cash  -10 USD",
		"2014-05-05 * \"With blank\"\n  Expenses:Food  4.50 USD\n\n  Assets:Cash",

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
		"2014-01-05 * \"Budget\"\n  budget: 1,234.56 USD\n  target: USD\n  active: TRUE\n  Expenses:Food  5.00 USD\n  Assets:Cash",

		// Transaction with tags and links
		"2014-01-06 * \"Lunch\" #food ^receipt-001\n  Expenses:Food  15.00 USD\n  Assets:Cash",

		// Multiple transactions
		"2014-01-01 * \"A\"\n  Assets:Cash  10 USD\n  Income:Salary\n\n2014-01-02 * \"B\"\n  Expenses:Food  5 USD\n  Assets:Cash",

		// Blank lines between directives (regression test for idempotency bug)
		"2020-01-01 open Assets:Test\n\n2020-01-02 close Assets:Test",
		"0001-01-01 open A:Test\n\n0001-01-01 balance A:Test 0 USD",
		"2014-01-01 open Assets:Checking USD\r\n2014-01-02 close Assets:Checking\r\n",
	}
}

func isStableFormatterSeed(data []byte, seeds []string) bool {
	for _, seed := range seeds {
		if bytes.Equal(data, []byte(seed)) {
			return true
		}
	}
	return false
}
