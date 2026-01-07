package ledger

import (
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestInsufficientInventoryError(t *testing.T) {
	// Setup test fixtures
	date, _ := ast.NewDate("2024-01-15")
	account, _ := ast.NewAccount("Assets:Checking")
	txn := ast.NewTransaction(date, "Buy stocks",
		ast.WithFlag("*"),
		ast.WithPayee("Broker Inc"),
		ast.WithPostings(
			ast.NewPosting(account, ast.WithAmount("-100", "USD")),
		),
	)
	txn.Pos = ast.Position{Filename: "test.bean", Line: 10}

	// Create the error
	details := errors.New("needed -100 USD but only have 50 USD")
	err := NewInsufficientInventoryError(txn, account, details)

	t.Run("Error message formatting", func(t *testing.T) {
		msg := err.Error()
		assert.Contains(t, msg, "test.bean:10")
		assert.Contains(t, msg, "Insufficient inventory")
		assert.Contains(t, msg, "Assets:Checking")
		assert.Contains(t, msg, "needed -100 USD but only have 50 USD")
	})

	t.Run("All fields populated correctly", func(t *testing.T) {
		assert.Equal(t, date, err.GetDate())
		assert.Equal(t, "Broker Inc", err.Payee)
		assert.Equal(t, account, err.Account)
		assert.Equal(t, details, err.Details)
		assert.Equal(t, "test.bean", err.GetPosition().Filename)
		assert.Equal(t, 10, err.GetPosition().Line)
		assert.Equal(t, ast.Directive(txn), err.GetDirective())
	})

	t.Run("GetPosition method", func(t *testing.T) {
		pos := err.GetPosition()
		assert.Equal(t, "test.bean", pos.Filename)
		assert.Equal(t, 10, pos.Line)
	})

	t.Run("GetDirective method", func(t *testing.T) {
		dir := err.GetDirective()
		assert.Equal(t, ast.Directive(txn), dir)
	})

	t.Run("GetAccount method", func(t *testing.T) {
		acc := err.GetAccount()
		assert.Equal(t, account, acc)
	})

	t.Run("GetDate method", func(t *testing.T) {
		d := err.GetDate()
		assert.Equal(t, date, d)
	})

	t.Run("Error message without filename", func(t *testing.T) {
		// Test error message when Position has no filename - uses Date fallback
		txnNoPos := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPayee("Broker Inc"),
		)
		// Pos.Filename is empty by default
		errNoFile := NewInsufficientInventoryError(txnNoPos, account, details)
		msg := errNoFile.Error()
		assert.Contains(t, msg, "2024-01-15")
		assert.Contains(t, msg, "Insufficient inventory")
		assert.Contains(t, msg, "Assets:Checking")
	})
}

func TestCurrencyConstraintError(t *testing.T) {
	// Setup test fixtures
	date, _ := ast.NewDate("2024-02-20")
	account, _ := ast.NewAccount("Assets:Investment")
	txn := ast.NewTransaction(date, "Buy foreign stock",
		ast.WithFlag("*"),
		ast.WithPayee("Foreign Broker"),
		ast.WithPostings(
			ast.NewPosting(account, ast.WithAmount("100", "EUR")),
		),
	)
	txn.Pos = ast.Position{Filename: "ledger.bean", Line: 25}

	// Create the error
	allowedCurrencies := []string{"USD", "GBP"}
	err := NewCurrencyConstraintError(txn, account, "EUR", allowedCurrencies)

	t.Run("Error message formatting", func(t *testing.T) {
		msg := err.Error()
		assert.Contains(t, msg, "ledger.bean:25")
		assert.Contains(t, msg, "Currency EUR not allowed")
		assert.Contains(t, msg, "Assets:Investment")
		assert.Contains(t, msg, "[USD GBP]")
	})

	t.Run("All fields populated correctly", func(t *testing.T) {
		assert.Equal(t, date, err.GetDate())
		assert.Equal(t, "Foreign Broker", err.Payee)
		assert.Equal(t, account, err.Account)
		assert.Equal(t, "EUR", err.Currency)
		assert.Equal(t, allowedCurrencies, err.AllowedCurrencies)
		assert.Equal(t, "ledger.bean", err.GetPosition().Filename)
		assert.Equal(t, 25, err.GetPosition().Line)
		assert.Equal(t, ast.Directive(txn), err.GetDirective())
	})

	t.Run("GetPosition method", func(t *testing.T) {
		pos := err.GetPosition()
		assert.Equal(t, "ledger.bean", pos.Filename)
		assert.Equal(t, 25, pos.Line)
	})

	t.Run("GetDirective method", func(t *testing.T) {
		dir := err.GetDirective()
		assert.Equal(t, ast.Directive(txn), dir)
	})

	t.Run("GetAccount method", func(t *testing.T) {
		acc := err.GetAccount()
		assert.Equal(t, account, acc)
	})

	t.Run("GetDate method", func(t *testing.T) {
		d := err.GetDate()
		assert.Equal(t, date, d)
	})

	t.Run("Error message without filename", func(t *testing.T) {
		// Test error message when Position has no filename - uses Date fallback
		txnNoPos := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPayee("Foreign Broker"),
		)
		// Pos.Filename is empty by default
		errNoFile := NewCurrencyConstraintError(txnNoPos, account, "EUR", allowedCurrencies)
		msg := errNoFile.Error()
		assert.Contains(t, msg, "2024-02-20")
		assert.Contains(t, msg, "Currency EUR not allowed")
		assert.Contains(t, msg, "Assets:Investment")
	})

	t.Run("Empty allowed currencies list", func(t *testing.T) {
		// Test with empty allowed currencies
		err := NewCurrencyConstraintError(txn, account, "EUR", []string{})
		msg := err.Error()
		assert.Contains(t, msg, "[]")
	})

	t.Run("Single allowed currency", func(t *testing.T) {
		// Test with single currency
		err := NewCurrencyConstraintError(txn, account, "EUR", []string{"USD"})
		msg := err.Error()
		assert.Contains(t, msg, "[USD]")
	})
}
