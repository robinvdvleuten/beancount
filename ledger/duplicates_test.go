package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestLedgerDuplicate(t *testing.T) {
	const opens = `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Food
2024-01-01 open Equity:Opening
`
	tests := []struct {
		name      string
		ledger    string
		extracted string
		line      int  // Line of the matched ledger directive, 0 when kept
		invalid   bool // The ledger reports an error and is still applied
	}{
		{
			name: "same Import ID",
			ledger: `2024-02-01 * "Latte"
  import-id: "TX-001"
  Expenses:Food 9.00 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  import-id: "TX-001"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			line: 4,
		},
		{
			name: "no Import ID in the ledger, interpolated posting, 2 days apart",
			ledger: `2024-01-13 * "Coffee"
  Expenses:Food 4.5 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  import-id: "TX-001"
  Assets:Checking -4.50 USD
`,
			line: 4,
		},
		{
			name: "no Import ID in the ledger, 3 days apart",
			ledger: `2024-01-18 * "Coffee"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Assets:Checking -4.50 USD
`,
		},
		{
			name: "different Import IDs",
			ledger: `2024-01-15 * "Latte"
  import-id: "TX-000"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  import-id: "TX-001"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
		},
		{
			name: "Import ID only in the ledger",
			ledger: `2024-01-15 * "Latte"
  import-id: "TX-000"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
		},
		{
			name: "other currency",
			ledger: `2024-01-15 * "Latte"
  Expenses:Food 4.50 EUR
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
		},
		{
			name: "other account",
			ledger: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Equity:Opening
`,
			extracted: `2024-01-15 * "Latte"
  Assets:Checking -4.50 USD
`,
		},
		{
			name: "ledger 1 day after",
			ledger: `2024-01-16 * "Coffee"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Assets:Checking -4.50 USD
`,
			line: 4,
		},
		{
			name: "ledger 2 days after",
			ledger: `2024-01-17 * "Coffee"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Assets:Checking -4.50 USD
`,
			line: 4,
		},
		{
			name: "ledger 3 days before",
			ledger: `2024-01-12 * "Coffee"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Assets:Checking -4.50 USD
`,
		},
		{
			// Neither transaction's accounts include the other's.
			name: "same posting against another funding account",
			ledger: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Equity:Opening
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
		},
		{
			name: "ledger transaction with an extra account",
			ledger: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Equity:Opening 1.00 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			line: 4,
		},
		{
			name: "extracted transaction with an extra account",
			ledger: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Equity:Opening 1.00 USD
  Assets:Checking
`,
			line: 4,
		},
		{
			// The first candidate fails the accounts check; the next one
			// in the window still matches.
			name: "later candidate with nested accounts",
			ledger: `2024-01-14 * "Lunch"
  Expenses:Food 4.50 USD
  Equity:Opening
2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			line: 7,
		},
		{
			// The padding is applied after the 01-14 posting, so the
			// account's postings are not in date order.
			name: "padding transaction",
			ledger: `2024-01-13 pad Assets:Checking Equity:Opening
2024-01-14 * "Coffee"
  Expenses:Food 1.00 USD
  Assets:Checking
2024-01-20 balance Assets:Checking 99.00 USD
`,
			extracted: `2024-01-15 * "Deposit"
  Assets:Checking 100.00 USD
`,
			line: 4,
		},
		{
			name: "number Import ID and a different Import ID",
			ledger: `2024-01-15 * "Latte"
  import-id: 12345
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  import-id: "TX-001"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
		},
		{
			name: "number Import ID and the same Import ID as a string",
			ledger: `2024-01-20 * "Latte"
  import-id: 12345
  Expenses:Food 9.00 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  import-id: "12345"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			line: 4,
		},
		{
			// An empty Import ID still keeps its transaction out of the
			// date window. The empty value is reported as invalid metadata.
			name:    "empty Import ID in the ledger",
			invalid: true,
			ledger: `2024-01-15 * "Latte"
  import-id: ""
  Expenses:Food 4.50 USD
  Assets:Checking
`,
			extracted: `2024-01-15 * "Latte"
  Expenses:Food 4.50 USD
  Assets:Checking
`,
		},
		{
			name:      "identical balance assertion",
			ledger:    "2024-01-16 balance Assets:Checking 0.0 USD\n",
			extracted: "2024-01-16 balance Assets:Checking 0 USD\n",
			line:      4,
		},
		{
			name:      "balance assertion with a different amount",
			ledger:    "2024-01-16 balance Assets:Checking 0 USD\n",
			extracted: "2024-01-16 balance Assets:Checking -4.50 USD\n",
		},
		{
			name:      "balance assertion on another date",
			ledger:    "2024-01-16 balance Assets:Checking 0 USD\n",
			extracted: "2024-01-17 balance Assets:Checking 0 USD\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			l := New()
			err := l.Process(ctx, parser.MustParseBytes(ctx, []byte(opens+tt.ledger)))
			if tt.invalid {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			extracted := parser.MustParseBytes(ctx, []byte(tt.extracted))

			match, ok := l.Duplicate(extracted.Directives[0])
			assert.Equal(t, tt.line != 0, ok)
			if ok {
				assert.Equal(t, tt.line, match.Position().Line)
			}
		})
	}
}
