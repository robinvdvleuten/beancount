package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFormatCostIntegration(t *testing.T) {
	t.Run("TransactionWithTotalCost", func(t *testing.T) {
		source := `2020-01-01 * "Buy shares"
  Assets:Stock   10 AAPL {{1000.00 USD}}
  Assets:Cash   -1000.00 USD`

		// Parse the source
		ast := parser.MustParseString(context.Background(), source)

		// Format it back
		f := New()
		var buf bytes.Buffer
		err := f.Format(context.Background(), ast, []byte(source), &buf)
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
		ast := parser.MustParseString(context.Background(), source)

		// Format it back
		f := New()
		var buf bytes.Buffer
		err := f.Format(context.Background(), ast, []byte(source), &buf)
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
		ast := parser.MustParseString(context.Background(), source)

		// Format it back
		f := New()
		var buf bytes.Buffer
		err := f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		formatted := buf.String()

		// Should preserve {{}} syntax with label
		assert.Contains(t, formatted, `{{800.00 USD, "lot-1"}}`)
	})
}
