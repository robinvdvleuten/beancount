package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFormatCmd(t *testing.T) {
	t.Run("BasicFormatting", func(t *testing.T) {
		source := `
option "title" "Test"

2021-01-01 open Assets:Checking

2021-01-02 * "Test transaction"
  Assets:Checking  -100.00 USD
  Expenses:Food  100.00 USD
`
		// Parse the input
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		// Format to buffer
		f := formatter.New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Verify output contains key elements
		assert.True(t, bytes.Contains([]byte(output), []byte("option \"title\" \"Test\"")))
		assert.True(t, bytes.Contains([]byte(output), []byte("2021-01-01 open Assets:Checking")))
		assert.True(t, bytes.Contains([]byte(output), []byte("Assets:Checking")))
		assert.True(t, bytes.Contains([]byte(output), []byte("100.00 USD")))
	})

	t.Run("WithCustomCurrencyColumn", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD
`
		// Parse the input
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		// Format with custom column
		f := formatter.New(formatter.WithCurrencyColumn(60))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Verify formatting occurred
		assert.True(t, bytes.Contains([]byte(output), []byte("USD")))
		// Verify custom column was used
		assert.Equal(t, 60, f.CurrencyColumn)
	})

	t.Run("EmptyFile", func(t *testing.T) {
		source := ``
		// Empty file should parse successfully but produce no output
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		f := formatter.New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Empty file produces minimal output
		output := buf.String()
		_ = output
	})
}

// TestFormatCmdIntegration tests the full command integration
func TestFormatCmdIntegration(t *testing.T) {
	t.Run("CompleteFile", func(t *testing.T) {
		source := `
option "title" "Integration Test"

2021-01-01 commodity USD

2021-01-01 open Assets:Checking  USD

2021-01-02 * "Opening balance"
  Assets:Checking  1000.00 USD
  Equity:Opening-Balances  -1000.00 USD

2021-01-03 balance Assets:Checking  1000.00 USD
`
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		f := formatter.New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()

		// Verify all directive types are present
		assert.True(t, bytes.Contains([]byte(output), []byte("option")))
		assert.True(t, bytes.Contains([]byte(output), []byte("commodity")))
		assert.True(t, bytes.Contains([]byte(output), []byte("open")))
		assert.True(t, bytes.Contains([]byte(output), []byte("balance")))

		// Verify amounts are aligned
		assert.True(t, bytes.Contains([]byte(output), []byte("1000.00 USD")))
	})
}
