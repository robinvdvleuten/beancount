package parser

import (
	"context"
	"testing"

	"github.com/robinvdvleuten/beancount/ast"
)

func FuzzParser(f *testing.F) {
	// Seed corpus with representative valid inputs
	seeds := []string{
		// Basic directives
		"2014-01-01 open Assets:Checking USD",
		"2014-12-31 close Assets:Checking",
		"2014-08-09 balance Assets:Checking 100.00 USD",
		"2014-08-09 balance Assets:Checking 100.00 ~ 0.05 USD",

		// Simple transaction
		"2014-05-05 * \"Cafe\" \"Coffee\"\n  Expenses:Food  4.50 USD\n  Assets:Cash",
		"2014-05-05 *\n  Expenses:Food  4.50 USD\n  Assets:Cash",
		"2014-05-05 txn\n  Expenses:Food  4.50 USD\n  Assets:Cash",
		"2014-05-05 P \"Opening balance\"\n  Assets:Checking  1,000.00 USD\n  Equity:Opening-Balances",
		"2014-05-05 * \"With comments\"\n  ; before\n  Expenses:Food  4.50 USD\n  ; between\n  Assets:Cash",
		"2014-05-05 * \"With blank\"\n  Expenses:Food  4.50 USD\n\n  Assets:Cash",
		"2014-05-05 * \"In progress\"\n  Assets:Cash  -10 USD",

		// Transaction with inferred amounts
		"2014-05-06 * \"Store\"\n  Expenses:Shopping  50.00 USD\n  Assets:Checking",
		"2014-05-06 * \"Store\"\n  Assets:Checking  +50.00 USD\n  Income:Salary",
		"2014-05-06 * \"Store\"\n  Assets:Checking  -(20 + 30) USD\n  Expenses:Shopping",

		// Options and includes
		"option \"title\" \"Example\"",
		"option \"operating_currency\" \"USD\"",
		"include \"accounts.beancount\"",

		// Comments and pragmas
		"; This is a comment",
		"option \"title\" \"Example\" ; comment",
		"option \"title\" \"line1\nline2\"",
		"plugin \"beancount.plugins.auto_accounts\" ; comment",
		"2024-01-01 query \"cash\" \"SELECT 1\" ; comment",
		"pushtag #trip",
		"poptag #trip",

		// Edge cases
		"",                   // Empty input
		"  \n\n  \n",         // Whitespace only
		"; Just a comment\n", // Comment only

		// Metadata
		"2014-01-01 open Assets:Checking USD\n  description: \"Primary checking account\"",
		"2014-01-01 open Assets:Checking USD\n  note:",
		"2014-01-01 commodity USD\n  target: USD\n  active: TRUE\n  budget: 1,234.56 USD",
		"2014-01-01 commodity USD\n  name: foo",

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

		// Compatibility edge cases
		"2014-01-01 open Assets:Checking USD\r\n2014-01-02 close Assets:Checking\r\n",
		"2014/01/01 open Assets:Checking USD",
		"junk\n2014-01-01 open Assets:Checking USD",
		"2014-01-01 open Assets:Checking USD garbage",
		"2014-01-01\nopen Assets:Checking USD",
		"2014-05-05 txn *\n  Expenses:Food  4.50 USD\n  Assets:Cash",
		"2014-05-05 txn !\n  Expenses:Food  4.50 USD\n  Assets:Cash",
		"2014-05-05 \"String-first\"\n  Assets:Checking  1.00 USD\n  Equity:Opening-Balances",

		// Pad directive
		"2014-07-09 pad Assets:Checking Equity:Opening-Balances",

		// Custom directive
		"2014-07-09 custom \"budget\" Expenses:Food \"monthly\" 500.00 USD",
		"2014-07-09 custom \"schedule\" 2014-07-15",

		// Regression test for position tracking bug
		"2000-01-01 open Assets:0",
	}

	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	for _, seed := range curatedValidParserSeeds() {
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
		tree, err := ParseBytes(ctx, data)

		// Validate invariants
		if err == nil {
			if tree == nil {
				t.Error("ParseBytes returned nil AST with nil error")
				return
			}
			assertASTInvariants(t, data, tree)
		}
		// If err != nil, that's expected for invalid syntax - parser handled it gracefully
	})
}

func TestParseCuratedValidSeeds(t *testing.T) {
	for _, seed := range curatedValidParserSeeds() {
		t.Run("", func(t *testing.T) {
			if _, err := ParseString(context.Background(), seed); err != nil {
				t.Fatalf("ParseString failed for curated valid seed:\n%s\nerror: %v", seed, err)
			}
		})
	}
}

func curatedValidParserSeeds() []string {
	// These fixtures have been verified with bean-check.
	return []string{
		"option \"operating_currency\" \"USD\"\n2024-01-01 open Assets:Cash USD\n2024-01-02 balance Assets:Cash 0 USD",
		"option \"operating_currency\" \"USD\"\n2024-01-01 open Assets:Cash USD\n2024-01-01 open Expenses:Food USD\n\n2024-01-02 * \"Lunch\"\n  Assets:Cash  -10 USD\n  Expenses:Food  10 USD",
		"option \"operating_currency\" \"USD\"\n2024-01-01 open Assets:Cash USD\n2024-01-01 open Expenses:Food USD\n\n2024-01-02 * \"Lunch\"\n  ; body comment\n  Assets:Cash  -10 USD\n  Expenses:Food  10 USD",
		"option \"operating_currency\" \"USD\"\r\n2024-01-01 open Assets:Cash USD\r\n",
	}
}

func assertASTInvariants(t *testing.T, data []byte, tree *ast.AST) {
	t.Helper()

	for i, d := range tree.Directives {
		assertPositionInBounds(t, data, "directive", i, d.Position())
		if txn, ok := d.(*ast.Transaction); ok {
			assertTransactionBodyInvariants(t, data, txn)
		}
	}
	for i, option := range tree.Options {
		assertPositionInBounds(t, data, "option", i, option.Position())
	}
	for i, include := range tree.Includes {
		assertPositionInBounds(t, data, "include", i, include.Position())
	}
	for i, plugin := range tree.Plugins {
		assertPositionInBounds(t, data, "plugin", i, plugin.Position())
	}
	for i, pushtag := range tree.Pushtags {
		assertPositionInBounds(t, data, "pushtag", i, pushtag.Position())
	}
	for i, poptag := range tree.Poptags {
		assertPositionInBounds(t, data, "poptag", i, poptag.Position())
	}
	for i, pushmeta := range tree.Pushmetas {
		assertPositionInBounds(t, data, "pushmeta", i, pushmeta.Position())
	}
	for i, popmeta := range tree.Popmetas {
		assertPositionInBounds(t, data, "popmeta", i, popmeta.Position())
	}
	for i, comment := range tree.Comments {
		assertPositionInBounds(t, data, "comment", i, comment.Position())
	}
	for i, blankLine := range tree.BlankLines {
		assertPositionInBounds(t, data, "blankLine", i, blankLine.Position())
	}
}

func assertTransactionBodyInvariants(t *testing.T, data []byte, txn *ast.Transaction) {
	t.Helper()

	postingIndex := 0
	for i, item := range txn.BodyItems {
		populated := 0
		if item.Posting != nil {
			populated++
			if postingIndex >= len(txn.Postings) {
				t.Fatalf("transaction body item %d has extra posting beyond Postings length %d", i, len(txn.Postings))
			}
			if item.Posting != txn.Postings[postingIndex] {
				t.Fatalf("transaction body posting %d does not match txn.Postings order", i)
			}
			assertPositionInBounds(t, data, "bodyPosting", i, item.Posting.Position())
			postingIndex++
		}
		if item.Comment != nil {
			populated++
			assertPositionInBounds(t, data, "bodyComment", i, item.Comment.Position())
		}
		if item.BlankLine != nil {
			populated++
			assertPositionInBounds(t, data, "bodyBlankLine", i, item.BlankLine.Position())
		}
		if populated != 1 {
			t.Fatalf("transaction body item %d has %d populated fields, want 1", i, populated)
		}
	}

	if postingIndex != len(txn.Postings) {
		t.Fatalf("transaction body has %d posting items, want %d", postingIndex, len(txn.Postings))
	}
}

func assertPositionInBounds(t *testing.T, data []byte, label string, index int, pos ast.Position) {
	t.Helper()

	if pos.Line < 1 {
		t.Errorf("%s %d has invalid line %d", label, index, pos.Line)
	}
	if pos.Column < 1 {
		t.Errorf("%s %d has invalid column %d", label, index, pos.Column)
	}
	if pos.Offset < 0 || pos.Offset > len(data) {
		t.Errorf("%s %d has offset %d outside input length %d", label, index, pos.Offset, len(data))
	}
}
