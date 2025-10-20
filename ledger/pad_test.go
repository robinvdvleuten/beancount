package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestPadGeneratesSyntheticTransaction(t *testing.T) {
	source := `
		2020-01-01 open Assets:Checking
		2020-01-01 open Equity:Opening-Balances
		
		2020-01-01 pad Assets:Checking Equity:Opening-Balances
		2020-01-15 balance Assets:Checking 1000.00 USD
	`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)

	ledger := New()
	err = ledger.Process(context.Background(), tree)
	assert.NoError(t, err)

	// Find padding transactions in AST
	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(paddingTxns), "Expected 1 padding transaction")

	txn := paddingTxns[0]
	assert.Equal(t, "P", txn.Flag, "Expected flag P for padding transaction")
	assert.Contains(t, txn.Narration, "Padding inserted", "Expected narration to contain 'Padding inserted'")
	assert.Contains(t, txn.Narration, "1000.00 USD", "Expected narration to contain amount with decimals")
	assert.Equal(t, 2, len(txn.Postings), "Expected 2 postings")

	// Verify first posting
	assert.Equal(t, "Assets:Checking", string(txn.Postings[0].Account))
	assert.Equal(t, "1000.00", txn.Postings[0].Amount.Value)
	assert.Equal(t, "USD", txn.Postings[0].Amount.Currency)

	// Verify second posting
	assert.Equal(t, "Equity:Opening-Balances", string(txn.Postings[1].Account))
	assert.Equal(t, "-1000.00", txn.Postings[1].Amount.Value)
	assert.Equal(t, "USD", txn.Postings[1].Amount.Currency)

	// Verify inventory was updated
	account, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok, "Assets:Checking should exist")
	balance := account.Inventory.Get("USD")
	assert.Equal(t, "1000", balance.String(), "Balance should be 1000 USD")
}

func TestPadWithMultipleCurrencies(t *testing.T) {
	source := `
		2020-01-01 open Assets:Investment
		2020-01-01 open Equity:Opening-Balances
		
		2020-01-01 pad Assets:Investment Equity:Opening-Balances
		2020-02-01 balance Assets:Investment 500.00 EUR
		2020-02-01 balance Assets:Investment 750.00 GBP
	`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)

	ledger := New()
	err = ledger.Process(context.Background(), tree)
	assert.NoError(t, err)

	// Should generate 2 padding transactions (one per currency)
	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 2, len(paddingTxns), "Expected 2 padding transactions (one per currency)")

	// Verify both currencies are in narrations (with proper decimal formatting)
	foundEUR := false
	foundGBP := false
	for _, txn := range paddingTxns {
		if containsString(txn.Narration, "500.00 EUR") {
			foundEUR = true
		}
		if containsString(txn.Narration, "750.00 GBP") {
			foundGBP = true
		}
	}
	assert.True(t, foundEUR, "Expected EUR padding transaction")
	assert.True(t, foundGBP, "Expected GBP padding transaction")

	// Verify inventory
	account, _ := ledger.GetAccount("Assets:Investment")
	assert.Equal(t, "500", account.Inventory.Get("EUR").String())
	assert.Equal(t, "750", account.Inventory.Get("GBP").String())
}

func TestPadWithExistingBalance(t *testing.T) {
	source := `
		2020-01-01 open Assets:Savings
		2020-01-01 open Equity:Opening-Balances
		
		2020-01-05 * "Initial deposit"
		  Assets:Savings  100.00 USD
		  Equity:Opening-Balances
		
		2020-01-10 pad Assets:Savings Equity:Opening-Balances
		2020-01-20 balance Assets:Savings 550.00 USD
	`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)

	ledger := New()
	err = ledger.Process(context.Background(), tree)
	assert.NoError(t, err)

	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(paddingTxns), "Expected 1 padding transaction")

	// Should pad 450.00 USD (550.00 - 100.00)
	txn := paddingTxns[0]
	assert.Contains(t, txn.Narration, "450.00 USD", "Expected padding of 450.00 USD with decimals")
	assert.Equal(t, "450.00", txn.Postings[0].Amount.Value)

	// Final balance should be 550
	account, _ := ledger.GetAccount("Assets:Savings")
	assert.Equal(t, "550", account.Inventory.Get("USD").String())
}

func TestPadWithinTolerance(t *testing.T) {
	source := `
		2020-01-01 open Assets:Cash
		2020-01-01 open Equity:Opening-Balances
		
		2020-01-05 * "Cash on hand"
		  Assets:Cash  200.00 USD
		  Equity:Opening-Balances
		
		2020-01-10 pad Assets:Cash Equity:Opening-Balances
		2020-01-15 balance Assets:Cash 200.00 USD
	`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)

	ledger := New()
	err = ledger.Process(context.Background(), tree)
	assert.NoError(t, err)

	// No padding needed - balance already matches
	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 0, len(paddingTxns), "Expected no padding transactions when balance already matches")
}

func TestUnusedPadWarning(t *testing.T) {
	source := `
		2020-01-01 open Assets:Cash
		2020-01-01 open Equity:Opening-Balances
		
		2020-01-10 pad Assets:Cash Equity:Opening-Balances
	`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)

	ledger := New()
	err = ledger.Process(context.Background(), tree)

	// Should have a warning about unused pad
	assert.Error(t, err, "Expected error for unused pad")

	valErrs, ok := err.(*ValidationErrors)
	assert.True(t, ok, "Expected ValidationErrors")
	assert.Equal(t, 1, len(valErrs.Errors))

	_, ok = valErrs.Errors[0].(*UnusedPadWarning)
	assert.True(t, ok, "Expected UnusedPadWarning")
}

func TestPadDateIsUsedNotBalanceDate(t *testing.T) {
	source := `
		2020-01-01 open Assets:Checking
		2020-01-01 open Equity:Opening-Balances
		
		2020-01-05 pad Assets:Checking Equity:Opening-Balances
		2020-02-15 balance Assets:Checking 1000.00 USD
	`

	tree, err := parser.ParseBytes(context.Background(), []byte(source))
	assert.NoError(t, err)

	ledger := New()
	err = ledger.Process(context.Background(), tree)
	assert.NoError(t, err)

	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(paddingTxns))

	// Transaction date should be pad date (2020-01-05), not balance date (2020-02-15)
	txn := paddingTxns[0]
	assert.Equal(t, "2020-01-05", txn.Date.Format("2006-01-02"))
}

// Helper function to find padding transactions in AST
func findPaddingTransactions(tree *ast.AST) []*ast.Transaction {
	var paddingTxns []*ast.Transaction
	for _, directive := range tree.Directives {
		if txn, ok := directive.(*ast.Transaction); ok {
			if txn.Flag == "P" {
				paddingTxns = append(paddingTxns, txn)
			}
		}
	}
	return paddingTxns
}

// Helper to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
