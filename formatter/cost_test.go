package formatter

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name     string
		cost     *ast.Cost
		expected string
	}{
		{
			name:     "NilCost",
			cost:     nil,
			expected: "",
		},
		{
			name: "PerUnitCostSimple",
			cost: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
			},
			expected: "{100.00 USD}",
		},
		{
			name: "TotalCostSimple",
			cost: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
			},
			expected: "{{1000.00 USD}}",
		},
		{
			name: "PerUnitCostWithDate",
			cost: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
			expected: "{100.00 USD, 2020-01-01}",
		},
		{
			name: "TotalCostWithDate",
			cost: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
			expected: "{{1000.00 USD, 2020-01-01}}",
		},
		{
			name: "PerUnitCostWithLabel",
			cost: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				Label:   "lot-1",
			},
			expected: `{100.00 USD, "lot-1"}`,
		},
		{
			name: "TotalCostWithLabel",
			cost: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				Label:   "lot-1",
			},
			expected: `{{1000.00 USD, "lot-1"}}`,
		},
		{
			name: "PerUnitCostWithDateAndLabel",
			cost: &ast.Cost{
				IsTotal: false,
				Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
				Label:   "lot-1",
			},
			expected: `{100.00 USD, 2020-01-01, "lot-1"}`,
		},
		{
			name: "TotalCostWithDateAndLabel",
			cost: &ast.Cost{
				IsTotal: true,
				Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
				Label:   "lot-1",
			},
			expected: `{{1000.00 USD, 2020-01-01, "lot-1"}}`,
		},
		{
			name: "MergeCost",
			cost: &ast.Cost{
				IsMerge: true,
			},
			expected: "{*}",
		},
		{
			name:     "EmptyCost",
			cost:     &ast.Cost{},
			expected: "{}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := New()
			var buf strings.Builder
			f.formatCost(test.cost, &buf)
			result := buf.String()
			assert.Equal(t, test.expected, result, "Cost formatting mismatch for test: %s", test.name)
		})
	}
}

func TestFormatCostIntegration(t *testing.T) {
	t.Run("TransactionWithTotalCost", func(t *testing.T) {
		source := `2020-01-01 * "Buy shares"
  Assets:Stock   10 AAPL {{1000.00 USD}}
  Assets:Cash   -1000.00 USD`

		// Parse the source
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		// Format it back
		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		formatted := buf.String()

		// Should preserve {{}} syntax
		assert.Contains(t, formatted, "{{1000.00 USD}}")
	})

	t.Run("TransactionWithPerUnitCost", func(t *testing.T) {
		source := `2020-01-01 * "Buy shares"
  Assets:Stock   10 AAPL {100.00 USD}
  Assets:Cash   -1000.00 USD`

		// Parse the source
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		// Format it back
		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		formatted := buf.String()

		// Should preserve {} syntax
		assert.Contains(t, formatted, "{100.00 USD}")
	})

	t.Run("TransactionWithTotalCostAndLabel", func(t *testing.T) {
		source := `2020-01-01 * "Buy shares"
  Assets:Stock   8 AAPL {{800.00 USD, "lot-1"}}
  Assets:Cash   -800.00 USD`

		// Parse the source
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		// Format it back
		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		formatted := buf.String()

		// Should preserve {{}} syntax with label
		assert.Contains(t, formatted, `{{800.00 USD, "lot-1"}}`)
	})
}
