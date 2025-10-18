package formatter

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestExpressionPreservation(t *testing.T) {
	t.Run("Format preserves expressions", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test expressions"
  Assets:Cash         (10 + 20) USD
  Expenses:Food       100 / 3 EUR
  Expenses:Transport  -50.00 USD
  Assets:Bank`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()

		// Check that expressions are preserved
		assert.True(t, strings.Contains(output, "(10 + 20)"), "expected expression '(10 + 20)' to be preserved")
		assert.True(t, strings.Contains(output, "100 / 3"), "expected expression '100 / 3' to be preserved")
		assert.True(t, strings.Contains(output, "-50.00"), "expected '-50.00' to be preserved")
	})

	t.Run("FormatTransaction with source preserves expressions", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test"
  Assets:Cash  (10 + 20) USD
  Assets:Bank`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		txn, ok := tree.Directives[0].(*ast.Transaction)
		assert.True(t, ok, "expected Transaction directive")

		var buf bytes.Buffer
		f := New(WithSource(source))

		// With source - should preserve expressions
		err = f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "(10 + 20)"), "expected expression '(10 + 20)' to be preserved with source")
	})

	t.Run("FormatTransaction without source shows evaluated values", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test"
  Assets:Cash  (10 + 20) USD
  Assets:Bank`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		txn, ok := tree.Directives[0].(*ast.Transaction)
		assert.True(t, ok, "expected Transaction directive")

		var buf bytes.Buffer
		f := New() // No WithSource - should show evaluated values

		// Without source - should show evaluated value
		err = f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, !strings.Contains(output, "(10 + 20)"), "expected expression to be evaluated without source")
		assert.True(t, strings.Contains(output, "30"), "expected evaluated value '30' without source")
	})

	t.Run("Cost expressions preserved", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Buy stock"
  Assets:Stock  10 STOCK {(100 + 50) USD}
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "{(100 + 50) USD}"), "expected cost expression to be preserved")
	})

	t.Run("Expression alignment", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test alignment"
  Assets:Cash         100.00 USD
  Expenses:Food    (10 + 20) USD
  Expenses:Other        5.00 USD
  Assets:Bank`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		lines := strings.Split(output, "\n")

		// Find currency positions for each line
		var currencyPositions []int
		for _, line := range lines {
			if strings.Contains(line, "USD") {
				pos := strings.Index(line, "USD")
				currencyPositions = append(currencyPositions, pos)
			}
		}

		// All currency positions should be the same (aligned)
		assert.True(t, len(currencyPositions) > 1, "expected multiple USD positions")
		firstPos := currencyPositions[0]
		for i, pos := range currencyPositions {
			assert.Equal(t, firstPos, pos, "currency misaligned at line %d", i)
		}
	})

	t.Run("Price annotation expressions preserved", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test price annotations"
  Assets:Stock  10 AAPL @ (100 + 50) USD
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "@ (100 + 50) USD"), "expected price expression '@ (100 + 50) USD' to be preserved")
	})

	t.Run("Total price annotation expressions preserved", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test total price annotations"
  Assets:Stock  10 AAPL @@ (100 * 10) USD
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "@@ (100 * 10) USD"), "expected total price expression '@@ (100 * 10) USD' to be preserved")
	})

	t.Run("All expression types preserved together", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test all expression types"
  Assets:Stock  10 AAPL @ (100 + 50) USD
  Expenses:Food  (20 + 30) EUR
  Assets:Stock  5 GOOG {(150 * 2) USD}
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "@ (100 + 50) USD"), "expected price expression to be preserved")
		assert.True(t, strings.Contains(output, "(20 + 30) EUR"), "expected amount expression to be preserved")
		assert.True(t, strings.Contains(output, "{(150 * 2) USD}"), "expected cost expression to be preserved")
	})

	t.Run("Merge cost preserved", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test merge cost"
  Assets:Stock  10 AAPL {*}
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "{*}"), "expected merge cost '{*}' to be preserved")
	})

	t.Run("Empty cost preserved", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test empty cost"
  Assets:Stock  10 AAPL {}
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "{}"), "expected empty cost '{}' to be preserved")
	})

	t.Run("All cost types together", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test all cost types"
  Assets:Stock  10 AAPL {100.00 USD}
  Assets:Stock  5 GOOG {*}
  Assets:Stock  3 MSFT {}
  Assets:Cash`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		var buf bytes.Buffer
		f := New(WithSource(source))
		err = f.Format(context.Background(), tree, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, "{100.00 USD}"), "expected cost with amount")
		assert.True(t, strings.Contains(output, "{*}"), "expected merge cost")
		assert.True(t, strings.Contains(output, "{}"), "expected empty cost")
	})
}
