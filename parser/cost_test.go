package parser

import (
	"context"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestParseCost(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ast.Cost
		hasError bool
	}{
		{
			name:  "PerUnitCostSimple",
			input: "{100.00 USD}",
			expected: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
			},
		},
		{
			name:  "TotalCostSimple",
			input: "{{1000.00 USD}}",
			expected: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
			},
		},
		{
			name:  "PerUnitCostWithDate",
			input: "{100.00 USD, 2020-01-01}",
			expected: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
		},
		{
			name:  "TotalCostWithDate",
			input: "{{1000.00 USD, 2020-01-01}}",
			expected: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
		},
		{
			name:  "PerUnitCostWithLabel",
			input: `{100.00 USD, "lot-1"}`,
			expected: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				Label:   "lot-1",
			},
		},
		{
			name:  "TotalCostWithLabel",
			input: `{{1000.00 USD, "lot-1"}}`,
			expected: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				Label:   "lot-1",
			},
		},
		{
			name:  "PerUnitCostWithDateAndLabel",
			input: `{100.00 USD, 2020-01-01, "lot-1"}`,
			expected: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
				Label:   "lot-1",
			},
		},
		{
			name:  "TotalCostWithDateAndLabel",
			input: `{{1000.00 USD, 2020-01-01, "lot-1"}}`,
			expected: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
				Label:   "lot-1",
			},
		},
		{
			name:  "MergeCost",
			input: "{*}",
			expected: &ast.Cost{
				IsMerge: true,
			},
		},
		{
			name:  "EmptyCost",
			input: "{}",
			expected: &ast.Cost{},
		},
		{
			name:     "TotalEmptyCost",
			input:    "{{}}",
			hasError: true,
		},
		{
			name:     "TotalMergeCost",
			input:    "{{*}}",
			hasError: true,
		},
		{
			name:     "TotalCostWithoutAmount",
			input:    "{{, 2020-01-01}}",
			hasError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "2020-01-01 * \"test\"\n  Assets:Test " + test.input + "\n  Assets:Cash -100.00 USD"
			tree, err := ParseString(context.Background(), source)

			if test.hasError {
				assert.Error(t, err, "Expected parsing to fail for input: %s", test.input)
				return
			}

			assert.NoError(t, err, "Expected parsing to succeed for input: %s", test.input)
			assert.Equal(t, 1, len(tree.Directives), "Expected exactly one directive")

			txn, ok := tree.Directives[0].(*ast.Transaction)
			assert.True(t, ok, "Expected transaction directive")
			assert.Equal(t, 2, len(txn.Postings), "Expected two postings")

			posting := txn.Postings[0]
			assert.Equal(t, test.expected, posting.Cost, "Cost mismatch for input: %s", test.input)
		})
	}
}
