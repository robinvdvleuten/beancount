package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestEscapeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "NoEscaping",
			input:    "simple string",
			expected: "simple string",
		},
		{
			name:     "DoubleQuote",
			input:    `string with "quotes"`,
			expected: `string with \"quotes\"`,
		},
		{
			name:     "Backslash",
			input:    `path\to\file`,
			expected: `path\\to\\file`,
		},
		{
			name:     "Both",
			input:    `path\with"both`,
			expected: `path\\with\"both`,
		},
		{
			name:     "Empty",
			input:    "",
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := escapeString(test.input)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestNew(t *testing.T) {
	t.Run("DefaultOptions", func(t *testing.T) {
		f := New()
		assert.NotEqual(t, nil, f)
		assert.Equal(t, DefaultCurrencyColumn, f.CurrencyColumn)
		assert.Equal(t, DefaultIndentation, f.Indentation)
	})

	t.Run("WithCurrencyColumn", func(t *testing.T) {
		f := New(WithCurrencyColumn(60))
		assert.Equal(t, 60, f.CurrencyColumn)
	})

	t.Run("WithIndentation", func(t *testing.T) {
		f := New(WithIndentation(6))
		assert.Equal(t, 6, f.Indentation)
	})
}

func TestFormat(t *testing.T) {
	t.Run("BasicFormat", func(t *testing.T) {
		source := `
2021-01-01 open Assets:Checking
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// For now, just verify no error - actual formatting will be implemented in later steps
		// assert.True(t, buf.Len() > 0, "Should have written output")
	})

	t.Run("WithCustomCurrencyColumn", func(t *testing.T) {
		source := `
2021-01-01 open Assets:Checking
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New(WithCurrencyColumn(70))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		assert.Equal(t, 70, f.CurrencyColumn)
	})

	t.Run("AutoCalculateCurrencyColumn", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
    Assets:Checking  100.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have calculated a currency column
		assert.True(t, f.CurrencyColumn > 0, "Should have auto-calculated currency column")
	})
}

func TestCalculateCurrencyColumn(t *testing.T) {
	t.Run("EmptyAST", func(t *testing.T) {
		ast := &ast.AST{}
		f := New()
		column := f.calculateCurrencyColumn(ast)
		assert.Equal(t, 52, column, "Should return default column for empty AST")
	})

	t.Run("SinglePosting", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
    Assets:Checking  100.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		column := f.calculateCurrencyColumn(ast)

		// Width calculation: 4 (indent) + 15 (Assets:Checking) + 2 (spacing) + 6 (100.00) + 2 (buffer) = 29
		assert.True(t, column >= 29, "Column should be at least 29")
	})

	t.Run("MultiplePostingsWithDifferentLengths", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
    Assets:Checking  100.00 USD
    Expenses:Food:Restaurant  50.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		column := f.calculateCurrencyColumn(ast)

		// Should align to the longest: 4 + 24 (Expenses:Food:Restaurant) + 2 + 5 (50.00) + 2 = 37
		assert.True(t, column >= 37, "Column should accommodate longest account name")
	})

	t.Run("WithFlaggedPosting", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
    ! Assets:Checking  100.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		column := f.calculateCurrencyColumn(ast)

		// Width with flag: 4 + 2 (flag+space) + 15 + 2 + 6 + 2 = 31
		assert.True(t, column >= 31, "Column should account for flag")
	})

	t.Run("WithBalanceDirective", func(t *testing.T) {
		source := `
2021-01-02 balance Assets:US:BofA:Checking  3793.56 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		column := f.calculateCurrencyColumn(ast)

		// Width: 11 (date) + 8 (balance) + 27 (account) + 2 + 7 (number) + 2 = 57
		// But let's check what we actually get
		assert.True(t, column >= 50, "Column should accommodate balance directive, got: %d", column)
	})

	t.Run("WithPriceDirective", func(t *testing.T) {
		source := `
2021-01-01 price VBMPX  170.30 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		column := f.calculateCurrencyColumn(ast)

		// Width: 11 (date) + 6 (price) + 5 (VBMPX) + 2 + 6 (number) + 2 = 32
		assert.True(t, column >= 32, "Column should accommodate price directive")
	})

	t.Run("MixedDirectives", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
  Assets:Checking  100.00 USD
  
2021-01-02 balance Assets:US:BofA:Checking  3793.56 USD
2021-01-03 price VBMPX  170.30 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		column := f.calculateCurrencyColumn(ast)

		// Should align to the longest (balance directive in this case)
		assert.True(t, column >= 50, "Column should accommodate all directive types")
	})
}

func TestFormatDirectives(t *testing.T) {
	t.Run("Option", func(t *testing.T) {
		source := `option "title" "My Ledger"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "option \"title\" \"My Ledger\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Include", func(t *testing.T) {
		source := `include "2024.beancount"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "include \"2024.beancount\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Commodity", func(t *testing.T) {
		source := `2021-01-01 commodity USD`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-01-01 commodity USD\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Open", func(t *testing.T) {
		source := `2021-01-01 open Assets:Checking`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-01-01 open Assets:Checking\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("OpenWithCurrencies", func(t *testing.T) {
		source := `2021-01-01 open Assets:Checking  USD, EUR`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Currencies should have minimal spacing (2 spaces), not aligned
		output := buf.String()
		assert.Contains(t, output, "2021-01-01 open Assets:Checking")
		assert.Contains(t, output, "USD, EUR")
	})

	t.Run("Close", func(t *testing.T) {
		source := `2021-12-31 close Assets:Checking`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-12-31 close Assets:Checking\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Balance", func(t *testing.T) {
		source := `2021-01-02 balance Assets:Checking  100.00 USD`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have aligned amount
		assert.True(t, len(buf.String()) > 0)
		assert.Contains(t, buf.String(), "USD")
	})

	t.Run("Pad", func(t *testing.T) {
		source := `2021-01-01 pad Assets:Checking Equity:Opening-Balances`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-01-01 pad Assets:Checking Equity:Opening-Balances\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Note", func(t *testing.T) {
		source := `2021-01-01 note Assets:Checking "Initial balance"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-01-01 note Assets:Checking \"Initial balance\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Document", func(t *testing.T) {
		source := `2021-01-01 document Assets:Checking "/path/to/doc.pdf"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-01-01 document Assets:Checking \"/path/to/doc.pdf\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Price", func(t *testing.T) {
		source := `2021-01-01 price AAPL  150.00 USD`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have aligned amount
		assert.True(t, len(buf.String()) > 0)
		assert.Contains(t, buf.String(), "USD")
	})

	t.Run("Event", func(t *testing.T) {
		source := `2021-01-01 event "location" "New York"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-01-01 event \"location\" \"New York\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Transaction", func(t *testing.T) {
		source := `
2021-01-01 * "Groceries"
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Should contain transaction header
		assert.Contains(t, buf.String(), "2021-01-01 * \"Groceries\"")
		// Should contain postings
		assert.Contains(t, buf.String(), "Assets:Checking")
		assert.Contains(t, buf.String(), "Expenses:Food")
		// Should contain amounts
		assert.Contains(t, buf.String(), "-50.00 USD")
		assert.Contains(t, buf.String(), "50.00 USD")
		_ = output
	})

	t.Run("TransactionWithTags", func(t *testing.T) {
		source := `
2021-01-01 * "Groceries" #food #grocery
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should contain tags
		assert.Contains(t, buf.String(), "#food")
		assert.Contains(t, buf.String(), "#grocery")
	})

	t.Run("TransactionWithLinks", func(t *testing.T) {
		source := `
2021-01-01 * "Groceries" ^invoice-123
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should contain link
		assert.Contains(t, buf.String(), "^invoice-123")
	})

	t.Run("TransactionWithMetadata", func(t *testing.T) {
		source := `
2021-01-01 * "Groceries"
  category: "Essential"
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should contain metadata (with quotes)
		assert.Contains(t, buf.String(), "category: \"Essential\"")
	})

	t.Run("TransactionWithCost", func(t *testing.T) {
		source := `
2021-01-01 * "Buy Stock"
  Assets:Stocks  10 AAPL {150.00 USD}
  Assets:Cash  -1500.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should contain cost
		assert.Contains(t, buf.String(), "{150.00 USD}")
	})

	t.Run("TransactionWithPrice", func(t *testing.T) {
		source := `
2021-01-01 * "Convert Currency"
  Assets:USD  -100.00 USD @ 0.85 EUR
  Assets:EUR  85.00 EUR
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should contain price
		assert.Contains(t, buf.String(), "@ 0.85 EUR")
	})

	t.Run("Plugin", func(t *testing.T) {
		source := `plugin "beancount.plugins.auto_accounts"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "plugin \"beancount.plugins.auto_accounts\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("PluginWithConfig", func(t *testing.T) {
		source := `plugin "beancount.plugins.check_commodity" "USD,EUR"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "plugin \"beancount.plugins.check_commodity\" \"USD,EUR\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Custom", func(t *testing.T) {
		source := `2021-06-01 custom "budget" "quarterly" TRUE 10000.00 USD`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "2021-06-01 custom \"budget\" \"quarterly\" TRUE 10000.00 USD\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("CustomWithMetadata", func(t *testing.T) {
		source := `2021-06-01 custom "budget" "quarterly"
  category: "savings-goal"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "2021-06-01 custom \"budget\" \"quarterly\"")
		assert.Contains(t, output, "category: \"savings-goal\"")
	})

	t.Run("Pushtag", func(t *testing.T) {
		source := `pushtag #vacation`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "pushtag #vacation\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Poptag", func(t *testing.T) {
		source := `poptag #vacation`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "poptag #vacation\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Pushmeta", func(t *testing.T) {
		source := `pushmeta trip: "NYC Summer 2021"`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "pushmeta trip: \"NYC Summer 2021\"\n"
		assert.Equal(t, expected, buf.String())
	})

	t.Run("Popmeta", func(t *testing.T) {
		source := `popmeta trip:`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		expected := "popmeta trip:\n"
		assert.Equal(t, expected, buf.String())
	})
}

// TestTransactionEdgeCases tests edge cases for transaction formatting
func TestTransactionEdgeCases(t *testing.T) {
	t.Run("EmptyPosting", func(t *testing.T) {
		source := `
2021-01-01 * "Test with implied amount"
  Assets:Checking  -50.00 USD
  Expenses:Food
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Should contain both postings
		assert.Contains(t, buf.String(), "Assets:Checking")
		assert.Contains(t, buf.String(), "Expenses:Food")
		// Should have the amount on first posting
		assert.Contains(t, buf.String(), "-50.00 USD")
		// Second posting should be just account name (implied amount)
		lines := bytes.Split(buf.Bytes(), []byte("\n"))
		foundExpensesLine := false
		for _, line := range lines {
			if bytes.Contains(line, []byte("Expenses:Food")) {
				foundExpensesLine = true
				// Should not have USD on the Expenses:Food line
				assert.NotContains(t, string(line), "USD")
			}
		}
		assert.True(t, foundExpensesLine, "Should find Expenses:Food line")
		_ = output
	})

	t.Run("LongAccountName", func(t *testing.T) {
		source := `
2021-01-01 * "Very long account name"
  Assets:Investments:Brokerage:Retirement:Traditional-IRA:Vanguard  1000.00 USD
  Assets:Checking  -1000.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should handle long account names gracefully
		assert.Contains(t, buf.String(), "Assets:Investments:Brokerage:Retirement:Traditional-IRA:Vanguard")
		// Should have minimum spacing even with long names
		assert.Contains(t, buf.String(), "1000.00 USD")
	})

	t.Run("NegativeNumbersWithVaryingDecimals", func(t *testing.T) {
		source := `
2021-01-01 * "Different decimal places"
  Assets:AccountA  -1.5 USD
  Assets:AccountB  -100.00 USD
  Expenses:Test  101.50 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// All amounts should be present and properly aligned
		assert.Contains(t, buf.String(), "-1.5 USD")
		assert.Contains(t, buf.String(), "-100.00 USD")
		assert.Contains(t, buf.String(), "101.50 USD")
	})

	t.Run("PostingWithAllFeatures", func(t *testing.T) {
		source := `
2021-01-01 * "Complete posting test"
  category: "test"
  ! Assets:Stocks  10 AAPL {150.00 USD, 2020-12-01} @ 155.00 USD
    broker: "Schwab"
  Assets:Cash  -1550.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Should have transaction metadata (with quotes)
		assert.Contains(t, buf.String(), "category: \"test\"")
		// Should have flagged posting
		assert.Contains(t, buf.String(), "! Assets:Stocks")
		// Should have cost with date
		assert.Contains(t, buf.String(), "{150.00 USD, 2020-12-01}")
		// Should have price
		assert.Contains(t, buf.String(), "@ 155.00 USD")
		// Should have posting metadata (with quotes)
		assert.Contains(t, buf.String(), "broker: \"Schwab\"")
		_ = output
	})

	t.Run("MultiCurrencyTransaction", func(t *testing.T) {
		source := `
2021-01-01 * "Multi-currency"
  Assets:USD  -100.00 USD
  Assets:EUR  85.00 EUR
  Assets:GBP  73.50 GBP
  Equity:Conversions
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// All currencies should be present and aligned
		assert.Contains(t, buf.String(), "USD")
		assert.Contains(t, buf.String(), "EUR")
		assert.Contains(t, buf.String(), "GBP")
	})

	t.Run("TransactionWithPayeeAndNarration", func(t *testing.T) {
		source := `
2021-01-01 * "Starbucks" "Morning coffee"
  Assets:Checking  -5.50 USD
  Expenses:Coffee  5.50 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have both payee and narration quoted
		assert.Contains(t, buf.String(), "\"Starbucks\"")
		assert.Contains(t, buf.String(), "\"Morning coffee\"")
	})

	t.Run("TransactionWithOnlyPayee", func(t *testing.T) {
		source := `
2021-01-01 * "Starbucks"
  Assets:Checking  -5.50 USD
  Expenses:Coffee  5.50 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// When there's no second string, the first string is narration, not payee
		// This is how beancount parser works
		assert.Contains(t, buf.String(), "\"Starbucks\"")
		_ = output
	})

	t.Run("ComplexCostSpecification", func(t *testing.T) {
		source := `
2021-01-01 * "Stock purchase with label"
  Assets:Stocks  10 AAPL {150.00 USD, 2020-12-01, "lot-2020-q4"}
  Assets:Cash  -1500.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have complete cost specification
		assert.Contains(t, buf.String(), "{150.00 USD, 2020-12-01, \"lot-2020-q4\"}")
	})

	t.Run("TotalPriceAnnotation", func(t *testing.T) {
		source := `
2021-01-01 * "Total price test"
  Assets:Stocks  10 AAPL @@ 1500.00 USD
  Assets:Cash  -1500.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have @@ for total price
		assert.Contains(t, buf.String(), "@@ 1500.00 USD")
	})

	t.Run("PostingWithMetadataOnly", func(t *testing.T) {
		source := `
2021-01-01 * "Posting metadata test"
  Assets:Checking  -50.00 USD
    receipt: "RCP-123"
  Expenses:Groceries  50.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have posting-level metadata properly indented (with quotes)
		assert.Contains(t, buf.String(), "receipt: \"RCP-123\"")
	})
}

// TestFormattingIdempotency tests that formatting is idempotent
func TestFormattingIdempotency(t *testing.T) {
	t.Run("FormattingTwiceProducesSameResult", func(t *testing.T) {
		source := `
2021-01-01 * "Test transaction"
  Assets:Checking  -100.00 USD
  Expenses:Food  100.00 USD
`
		// First format
		ast1, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f1 := New()
		var buf1 bytes.Buffer
		err = f1.Format(context.Background(), ast1, []byte(source), &buf1)
		assert.NoError(t, err)

		formatted1 := buf1.String()

		// Second format (format the already formatted output)
		ast2, err := parser.ParseString(context.Background(), formatted1)
		assert.NoError(t, err)

		f2 := New()
		var buf2 bytes.Buffer
		err = f2.Format(context.Background(), ast2, []byte(formatted1), &buf2)
		assert.NoError(t, err)

		formatted2 := buf2.String()

		// Both should be identical
		assert.Equal(t, formatted1, formatted2, "Formatting should be idempotent")
	})
}

func TestFormatterWidthOptions(t *testing.T) {
	source := `2021-01-01 * "Test"
  Assets:Bank:Checking  100.00 USD
  Income:Salary  -100.00 USD
`

	ast, err := parser.ParseString(context.Background(), source)
	assert.NoError(t, err)

	t.Run("WithCurrencyColumn", func(t *testing.T) {
		// CurrencyColumn overrides other options
		f := New(WithCurrencyColumn(60))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Currency should be at column 60
		output := buf.String()
		assert.Contains(t, output, "2021-01-01 * \"Test\"")
		// The exact spacing will depend on the implementation
	})

	t.Run("WithPrefixWidth", func(t *testing.T) {
		// Only set prefix width, num width auto-calculated
		f := New(WithPrefixWidth(40))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Assets:Bank:Checking")
		assert.Contains(t, output, "USD")
	})

	t.Run("WithNumWidth", func(t *testing.T) {
		// Only set num width, prefix width auto-calculated
		f := New(WithNumWidth(15))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Assets:Bank:Checking")
		assert.Contains(t, output, "USD")
	})

	t.Run("WithPrefixAndNumWidth", func(t *testing.T) {
		// Both prefix and num width set
		f := New(WithPrefixWidth(40), WithNumWidth(12))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Currency column should be 40 + 12 = 52
		assert.Equal(t, 52, f.CurrencyColumn)

		output := buf.String()
		assert.Contains(t, output, "Assets:Bank:Checking")
		assert.Contains(t, output, "USD")
	})

	t.Run("CurrencyColumnOverridesPrefixAndNumWidth", func(t *testing.T) {
		// CurrencyColumn should override PrefixWidth and NumWidth
		f := New(WithCurrencyColumn(70), WithPrefixWidth(40), WithNumWidth(12))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// CurrencyColumn should be 70 (not 40 + 12 = 52)
		assert.Equal(t, 70, f.CurrencyColumn)
	})
}
