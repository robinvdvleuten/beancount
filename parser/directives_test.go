package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// Open directive tests

func TestParseOpen(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	open, ok := result.Directives[0].(*ast.Open)
	assert.True(t, ok)
	assert.Equal(t, "Assets:Checking", string(open.Account))
	assert.Equal(t, 1, len(open.ConstraintCurrencies))
	assert.Equal(t, "USD", open.ConstraintCurrencies[0])
}

func TestParseOpenMultipleCurrencies(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD,EUR,GBP`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	open, ok := result.Directives[0].(*ast.Open)
	assert.True(t, ok)
	assert.Equal(t, 3, len(open.ConstraintCurrencies))
	assert.Equal(t, "USD", open.ConstraintCurrencies[0])
	assert.Equal(t, "EUR", open.ConstraintCurrencies[1])
	assert.Equal(t, "GBP", open.ConstraintCurrencies[2])
}

func TestParseOpenNoCurrency(t *testing.T) {
	input := `2014-01-01 open Expenses:Food`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	open, ok := result.Directives[0].(*ast.Open)
	assert.True(t, ok)
	assert.Equal(t, "Expenses:Food", string(open.Account))
	assert.Equal(t, 0, len(open.ConstraintCurrencies))
}

func TestParseOpenWithMetadata(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD
  account-number: "123456"
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	open, ok := result.Directives[0].(*ast.Open)
	assert.True(t, ok)
	assert.Equal(t, 1, len(open.Metadata))
	assert.Equal(t, "account-number", open.Metadata[0].Key)
}

// Close directive tests

func TestParseClose(t *testing.T) {
	input := `2014-12-31 close Assets:Checking`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	close, ok := result.Directives[0].(*ast.Close)
	assert.True(t, ok)
	assert.Equal(t, "Assets:Checking", string(close.Account))
}

// Balance directive tests

func TestParseBalance(t *testing.T) {
	input := `2014-08-09 balance Assets:Checking 100.00 USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	balance, ok := result.Directives[0].(*ast.Balance)
	assert.True(t, ok)
	assert.Equal(t, "Assets:Checking", string(balance.Account))
	assert.Equal(t, "100.00", balance.Amount.Value)
	assert.Equal(t, "USD", balance.Amount.Currency)
}

func TestParseBalanceNegative(t *testing.T) {
	input := `2014-08-09 balance Liabilities:CreditCard -500.00 USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	balance, ok := result.Directives[0].(*ast.Balance)
	assert.True(t, ok)
	assert.Equal(t, "-500.00", balance.Amount.Value)
}

// Pad directive tests

func TestParsePad(t *testing.T) {
	input := `2014-01-01 pad Assets:Checking Equity:Opening-Balances`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	pad, ok := result.Directives[0].(*ast.Pad)
	assert.True(t, ok)
	assert.Equal(t, "Assets:Checking", string(pad.Account))
	assert.Equal(t, "Equity:Opening-Balances", string(pad.AccountPad))
}

// Note directive tests

func TestParseNote(t *testing.T) {
	input := `2014-07-09 note Assets:Checking "Called about rebate"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	note, ok := result.Directives[0].(*ast.Note)
	assert.True(t, ok)
	assert.Equal(t, "Assets:Checking", string(note.Account))
	assert.Equal(t, "Called about rebate", note.Description.Value)
}

// Document directive tests

func TestParseDocument(t *testing.T) {
	input := `2014-07-09 document Assets:Checking "/path/to/statement.pdf"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	doc, ok := result.Directives[0].(*ast.Document)
	assert.True(t, ok)
	assert.Equal(t, "Assets:Checking", string(doc.Account))
	assert.Equal(t, "/path/to/statement.pdf", doc.PathToDocument.Value)
}

// Price directive tests

func TestParsePrice(t *testing.T) {
	input := `2014-07-09 price HOOL 579.18 USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	price, ok := result.Directives[0].(*ast.Price)
	assert.True(t, ok)
	assert.Equal(t, "HOOL", price.Commodity)
	assert.Equal(t, "579.18", price.Amount.Value)
	assert.Equal(t, "USD", price.Amount.Currency)
}

// Event directive tests

func TestParseEvent(t *testing.T) {
	input := `2014-07-09 event "location" "New York, USA"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	event, ok := result.Directives[0].(*ast.Event)
	assert.True(t, ok)
	assert.Equal(t, "location", event.Name.Value)
	assert.Equal(t, "New York, USA", event.Value.Value)
}

// Commodity directive tests

func TestParseCommodity(t *testing.T) {
	input := `2014-01-01 commodity USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	commodity, ok := result.Directives[0].(*ast.Commodity)
	assert.True(t, ok)
	assert.Equal(t, "USD", commodity.Currency)
}

func TestParseCommodityWithMetadata(t *testing.T) {
	input := `2014-01-01 commodity USD
  name: "US Dollar"
  precision: 2
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	commodity, ok := result.Directives[0].(*ast.Commodity)
	assert.True(t, ok)
	assert.Equal(t, 2, len(commodity.Metadata))
}

// Custom directive tests

func TestParseCustom(t *testing.T) {
	input := `2014-07-09 custom "budget" Expenses:Food "monthly" 500.00 USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	custom, ok := result.Directives[0].(*ast.Custom)
	assert.True(t, ok)
	assert.Equal(t, "budget", custom.Type.Value)
}

func TestParseCustomIdentAsString(t *testing.T) {
	input := `2024-01-01 custom "ticker" HOOL`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	custom, ok := result.Directives[0].(*ast.Custom)
	assert.True(t, ok)
	assert.Equal(t, 1, len(custom.Values))

	// A lone IDENT (not TRUE/FALSE) should be stored as String, not Number
	assert.NotEqual(t, (*string)(nil), custom.Values[0].String)
	assert.Equal(t, "HOOL", *custom.Values[0].String)
	assert.Equal(t, (*string)(nil), custom.Values[0].Number)
}

func TestParseCustomAccountValue(t *testing.T) {
	input := `2024-01-01 custom "budget" Expenses:Food "monthly" 500.00 USD`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	custom, ok := result.Directives[0].(*ast.Custom)
	assert.True(t, ok)

	// Should have 3 values: account, string, amount
	assert.Equal(t, 3, len(custom.Values))

	// First value: Expenses:Food (ACCOUNT token stored as String)
	assert.NotEqual(t, (*string)(nil), custom.Values[0].String)
	assert.Equal(t, "Expenses:Food", *custom.Values[0].String)

	// Second value: "monthly" (STRING token)
	assert.NotEqual(t, (*string)(nil), custom.Values[1].String)
	assert.Equal(t, "monthly", *custom.Values[1].String)

	// Third value: 500.00 USD (Amount)
	assert.NotEqual(t, (*ast.Amount)(nil), custom.Values[2].Amount)
	assert.Equal(t, "500.00", custom.Values[2].Amount.Value)
	assert.Equal(t, "USD", custom.Values[2].Amount.Currency)
}

func TestParseCustomNumberNotGrabbingNextLineCurrency(t *testing.T) {
	// The number 42 is the last token on its line. The next line has metadata
	// starting with an IDENT. The parser must not consume that IDENT as a
	// currency for the number.
	input := `2024-01-01 custom "test" 42
  note: "hello"
`
	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	custom, ok := result.Directives[0].(*ast.Custom)
	assert.True(t, ok)

	// 42 should be a standalone number, not an amount
	assert.Equal(t, 1, len(custom.Values))
	assert.NotEqual(t, (*string)(nil), custom.Values[0].Number)
	assert.Equal(t, "42", *custom.Values[0].Number)
	assert.Equal(t, (*ast.Amount)(nil), custom.Values[0].Amount)

	// Metadata should still be parsed
	assert.Equal(t, 1, len(custom.Metadata))
	assert.Equal(t, "note", custom.Metadata[0].Key)
}

// Option tests

func TestParseOption(t *testing.T) {
	input := `option "title" "My Ledger"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Options))
	assert.Equal(t, "title", result.Options[0].Name.Value)
	assert.Equal(t, "My Ledger", result.Options[0].Value.Value)
}

func TestParseOptionOperatingCurrency(t *testing.T) {
	input := `option "operating_currency" "USD"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Options))
	assert.Equal(t, "operating_currency", result.Options[0].Name.Value)
}

// Include tests

func TestParseInclude(t *testing.T) {
	input := `include "accounts.beancount"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Includes))
	assert.Equal(t, "accounts.beancount", result.Includes[0].Filename.Value)
}

// Plugin tests

func TestParsePlugin(t *testing.T) {
	input := `plugin "beancount.plugins.auto_accounts"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Plugins))
	assert.Equal(t, "beancount.plugins.auto_accounts", result.Plugins[0].Name.Value)
}

func TestParsePluginWithConfig(t *testing.T) {
	input := `plugin "my.plugin" "config_value"`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Plugins))
	assert.Equal(t, "my.plugin", result.Plugins[0].Name.Value)
	assert.Equal(t, "config_value", result.Plugins[0].Config.Value)
}

// Tag stack tests

func TestParsePushtag(t *testing.T) {
	input := `pushtag #trip`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Pushtags))
	assert.Equal(t, "trip", string(result.Pushtags[0].Tag))
}

func TestParsePoptag(t *testing.T) {
	input := `poptag #trip`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Poptags))
	assert.Equal(t, "trip", string(result.Poptags[0].Tag))
}

// Error cases

func TestParseTransactionRequiresNarration(t *testing.T) {
	input := `2000-01-01 !Assets:0 Income:0`

	_, err := ParseString(context.Background(), input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected transaction payee or narration string")
}

func TestParsePadMissingAccount(t *testing.T) {
	input := `2023-01-01 pad Assets:Checking
`
	_, err := ParseString(context.Background(), input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected account")
}

func TestParseNoteMissingString(t *testing.T) {
	input := `2023-01-01 note Assets:Checking
`
	_, err := ParseString(context.Background(), input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected string")
}
