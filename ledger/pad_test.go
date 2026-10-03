package ledger

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func TestPadGeneratesSyntheticTransaction(t *testing.T) {
	source := `
2020-01-01 open Assets:Checking
2020-01-01 open Equity:Opening-Balances

2020-01-01 pad Assets:Checking Equity:Opening-Balances
2020-01-15 balance Assets:Checking 1000.00 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	ledger := New()
	tree, err := processErr(context.Background(), ledger, tree)
	assert.NoError(t, err)

	// Find padding transactions in AST
	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(paddingTxns), "Expected 1 padding transaction")

	txn := paddingTxns[0]
	assert.Equal(t, "P", txn.Flag, "Expected flag P for padding transaction")
	assert.Contains(t, txn.Narration.Value, "Padding inserted", "Expected narration to contain 'Padding inserted'")
	assert.Contains(t, txn.Narration.Value, "1000.00 USD", "Expected narration to contain amount with decimals")
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

// TestPaddingLocationsAndMetadata checks that, like beancount's, the padding
// transaction carries the pad's position and metadata, and its postings the
// balance assertion's.
func TestPaddingLocationsAndMetadata(t *testing.T) {
	source := `
2020-01-01 open Assets:Checking
2020-01-01 open Equity:Opening-Balances

2020-01-01 pad Assets:Checking Equity:Opening-Balances
  padnote: "from pad"
2020-01-15 balance Assets:Checking 1000.00 USD
  note: "counted"
`
	tree := parser.MustParseBytes(context.Background(), []byte(source))
	tree = New().MustProcess(context.Background(), tree)

	var pad *ast.Pad
	var balance *ast.Balance
	for _, directive := range tree.Directives {
		switch d := directive.(type) {
		case *ast.Pad:
			pad = d
		case *ast.Balance:
			balance = d
		}
	}
	txn := findPaddingTransactions(tree)[0]
	assert.Equal(t, pad.Position(), txn.Position())
	assert.Equal(t, pad.GetMetadata(), txn.GetMetadata())
	for _, posting := range txn.Postings {
		assert.Equal(t, balance.Position(), posting.Position())
		assert.Equal(t, balance.GetMetadata(), posting.GetMetadata())
	}
}

func TestPadWithMultipleCurrencies(t *testing.T) {
	source := `
2020-01-01 open Assets:Investment
2020-01-01 open Equity:Opening-Balances

2020-01-01 pad Assets:Investment Equity:Opening-Balances
2020-02-01 balance Assets:Investment 500.00 EUR
2020-02-01 balance Assets:Investment 750.00 GBP
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	ledger := New()
	tree, err := processErr(context.Background(), ledger, tree)
	assert.NoError(t, err)

	// Should generate 2 padding transactions (one per currency)
	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 2, len(paddingTxns), "Expected 2 padding transactions (one per currency)")

	// Verify both currencies are in narrations (with proper decimal formatting)
	foundEUR := false
	foundGBP := false
	for _, txn := range paddingTxns {
		if containsString(txn.Narration.Value, "500.00 EUR") {
			foundEUR = true
		}
		if containsString(txn.Narration.Value, "750.00 GBP") {
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

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	ledger := New()
	tree, err := processErr(context.Background(), ledger, tree)
	assert.NoError(t, err)

	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(paddingTxns), "Expected 1 padding transaction")

	// Should pad 450.00 USD (550.00 - 100.00)
	txn := paddingTxns[0]
	assert.Contains(t, txn.Narration.Value, "450.00 USD", "Expected padding of 450.00 USD with decimals")
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

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	tree, validationErrors := processDiagnostics(t, New(), tree)

	// No padding needed - balance already matches, so the pad inserts
	// nothing and beancount reports it as unused.
	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 0, len(paddingTxns), "Expected no padding transactions when balance already matches")
	assert.Equal(t, 1, len(validationErrors))
	assert.Equal(t, "UnusedPadWarning", kindOf(validationErrors[0]))
}

func TestUnusedPadWarning(t *testing.T) {
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Equity:Opening-Balances

2020-01-10 pad Assets:Cash Equity:Opening-Balances
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	// Should have a warning about unused pad
	_, diagnostics := processDiagnostics(t, New(), tree)
	assert.Equal(t, 1, len(diagnostics))
	assert.Equal(t, "UnusedPadWarning", kindOf(diagnostics[0]))
}

func TestPadDateIsUsedNotBalanceDate(t *testing.T) {
	source := `
2020-01-01 open Assets:Checking
2020-01-01 open Equity:Opening-Balances

2020-01-05 pad Assets:Checking Equity:Opening-Balances
2020-02-15 balance Assets:Checking 1000.00 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	ledger := New()
	tree, err := processErr(context.Background(), ledger, tree)
	assert.NoError(t, err)

	paddingTxns := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(paddingTxns))

	// Transaction date should be pad date (2020-01-05), not balance date (2020-02-15)
	txn := paddingTxns[0]
	assert.Equal(t, "2020-01-05", txn.Date().String())
}

func TestPadSyntheticTransactionValidationErrorDoesNotPanic(t *testing.T) {
	source := `
2020-01-01 open Assets:Checking
2020-01-01 open Equity:Opening-Balances EUR

2020-01-05 pad Assets:Checking Equity:Opening-Balances
2020-01-15 balance Assets:Checking 1000.00 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))
	processDiagnostics(t, New(), tree)
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

func TestPadFromItselfDoesNotSatisfyBalance(t *testing.T) {
	// Both legs of the padding post to the same account, so the balance
	// assertion fails, as in beancount.
	source := `
2020-01-01 open Assets:Cash
2020-01-02 pad Assets:Cash Assets:Cash
2020-01-03 balance Assets:Cash 5 USD
`
	tree := parser.MustParseBytes(context.Background(), []byte(source))

	_, validationErrors := processDiagnostics(t, New(), tree)
	var mismatch *BalanceMismatchError
	assert.True(t, slices.ContainsFunc(validationErrors, func(err error) bool { return errors.As(err, &mismatch) }))
}

func TestPaddingAppliesAtItsBalanceAssertion(t *testing.T) {
	// Like bean-check, the spending after the padded opening balance sees
	// the padding, so the later assertion passes.
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Expenses:Food
2020-01-01 open Equity:Opening-Balances
2020-01-02 pad Assets:Cash Equity:Opening-Balances
2020-01-03 balance Assets:Cash 5 USD
2020-01-04 * "spend"
  Expenses:Food  5 USD
  Assets:Cash
2020-01-05 balance Assets:Cash 0 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	tree = l.MustProcess(context.Background(), tree)
	assert.Equal(t, 1, len(findPaddingTransactions(tree)))
}

func TestPadFillsOnlyTheFirstAssertion(t *testing.T) {
	// The second assertion is checked against the padded 5 USD, as
	// bean-check reports "accumulated 5 USD".
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Equity:Opening-Balances
2020-01-02 pad Assets:Cash Equity:Opening-Balances
2020-01-03 balance Assets:Cash 5 USD
2020-01-04 balance Assets:Cash 7 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_, _ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 1, len(errs), "errors: %v", errs)
	var mismatch *BalanceMismatchError
	assert.True(t, errors.As(errs[0], &mismatch), "got %v", errs[0])
	assert.Equal(t, "5", mismatch.Actual)
}

func TestPaddingKeepsTheDifferencesPrecision(t *testing.T) {
	// Like beancount, the padding is 5.0 - 1.234 as the subtraction leaves
	// it, so the account holds exactly the asserted 5.0 USD.
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Equity:O
2020-01-01 * "deposit"
  Assets:Cash  1.234 USD
  Equity:O
2020-01-02 pad Assets:Cash Equity:O
2020-01-03 balance Assets:Cash 5.0 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	tree = l.MustProcess(context.Background(), tree)

	padding := findPaddingTransactions(tree)
	assert.Equal(t, 1, len(padding))
	assert.Equal(t, "3.766", padding[0].Postings[0].Amount.Value)
	assert.Equal(t, "-3.766", padding[0].Postings[1].Amount.Value)

	cash, ok := l.GetAccount("Assets:Cash")
	assert.True(t, ok)
	assert.Equal(t, "5", cash.Inventory.Get("USD").String())
}

func TestPadOnACurrencyHeldAtCost(t *testing.T) {
	// Like beancount's ops/pad.py: each lot of the asserted currency held at
	// cost is a PadError on the balance line, listing the whole inventory,
	// and the padding, without cost, still applies.
	source := `
option "booking_method" "FIFO"
2020-01-01 open Assets:Invest
2020-01-01 open Equity:Opening
2020-01-01 * "buy"
  Assets:Invest  5 HOOL {10 USD}
  Equity:Opening
2020-01-01 * "buy2"
  Assets:Invest  3 HOOL {12.50 USD, 2019-12-01, "lot"}
  Assets:Invest  2 GOOG {1 EUR}
  Assets:Invest  7 USD
  Equity:Opening
2020-01-02 pad Assets:Invest Equity:Opening
2020-01-03 balance Assets:Invest 10 HOOL
2020-01-03 balance Assets:Invest 10 USD
2020-01-04 balance Assets:Invest 10 HOOL
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_, _ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 2, len(errs), "errors: %v", errs)
	for _, err := range errs {
		var diag *Diagnostic
		assert.True(t, errors.As(err, &diag))
		assert.Equal(t, "PadError", diag.Kind())
		assert.Equal(t, "Attempt to pad an entry with cost for balance: "+
			`(7 USD, 2 GOOG {1 EUR, 2020-01-01}, 5 HOOL {10 USD, 2020-01-01}, 3 HOOL {12.50 USD, 2019-12-01, "lot"})`,
			diag.Message())
		assert.Equal(t, 14, diag.GetPosition().Line)
		_, isPad := diag.GetDirective().(*ast.Pad)
		assert.True(t, isPad)
	}
}

func TestPadOnACurrencyHeldWithoutCostReportsNothing(t *testing.T) {
	source := `
2020-01-01 open Assets:Invest
2020-01-01 open Equity:Opening
2020-01-01 * "buy"
  Assets:Invest  5 HOOL {10 USD}
  Assets:Invest  5 USD
  Equity:Opening
2020-01-02 pad Assets:Invest Equity:Opening
2020-01-03 balance Assets:Invest 10 USD
`
	l := New()
	_, _ = l.Process(context.Background(), parser.MustParseString(context.Background(), source))

	assert.Equal(t, 0, len(l.Errors()), "errors: %v", l.Errors())
}

// fillCase builds a pad on Assets:Checking from source, a balance
// assertion of amount on Assets:Checking, and an inventory holding held
// USD.
func fillCase(t *testing.T, source ast.Account, amount, held string) (*pads, *ast.Balance, *Inventory) {
	t.Helper()
	date, err := ast.NewDate("2020-01-15")
	assert.NoError(t, err)
	p := newPads()
	p.add(ast.NewPad(date, "Assets:Checking", source))
	inventory := NewInventory()
	inventory.AddLot("USD", decimal.RequireFromString(held), nil)
	return p, ast.NewBalance(date, "Assets:Checking", ast.NewAmount(amount, "USD")), inventory
}

func TestPadsFill(t *testing.T) {
	tolerance := decimal.RequireFromString("0.005")

	t.Run("pads the difference", func(t *testing.T) {
		p, balance, inventory := fillCase(t, "Equity:Opening-Balances", "1000.00", "250")
		padding, held, errs := p.fill(balance, inventory, tolerance)
		assert.Zero(t, errs)
		assert.True(t, held.Equal(decimal.RequireFromString("1000")), "held %s", held)
		assert.Equal(t, "P", padding.Flag)
		assert.Equal(t, "750.00", padding.Postings[0].Amount.Value)
		assert.Equal(t, "-750.00", padding.Postings[1].Amount.Value)
		assert.Equal(t, ast.Account("Equity:Opening-Balances"), padding.Postings[1].Account)
	})

	t.Run("keeps the difference's precision", func(t *testing.T) {
		p, balance, inventory := fillCase(t, "Equity:Opening-Balances", "100", "99.995")
		padding, _, _ := p.fill(balance, inventory, decimal.Zero)
		assert.Equal(t, "0.005", padding.Postings[0].Amount.Value)
	})

	t.Run("pads nothing within tolerance", func(t *testing.T) {
		p, balance, inventory := fillCase(t, "Equity:Opening-Balances", "1000.00", "1000.004")
		padding, held, errs := p.fill(balance, inventory, tolerance)
		assert.Zero(t, padding)
		assert.Zero(t, errs)
		assert.Equal(t, "1000.004", held.String())
	})

	t.Run("a self-pad changes nothing", func(t *testing.T) {
		p, balance, inventory := fillCase(t, "Assets:Checking", "1000.00", "0")
		padding, held, _ := p.fill(balance, inventory, tolerance)
		assert.NotZero(t, padding)
		assert.Equal(t, "0", held.String())
	})

	t.Run("fills only the first assertion after the pad", func(t *testing.T) {
		p, balance, inventory := fillCase(t, "Equity:Opening-Balances", "1000.00", "0")
		padding, _, _ := p.fill(balance, inventory, tolerance)
		p.consume("Assets:Checking", "USD", padding)
		padding, held, _ := p.fill(balance, inventory, tolerance)
		assert.Zero(t, padding)
		assert.Equal(t, "0", held.String())
	})

	t.Run("a later pad replaces the first", func(t *testing.T) {
		p, balance, inventory := fillCase(t, "Equity:Opening-Balances", "1000.00", "0")
		first := p.latest["Assets:Checking"].pad
		date, _ := ast.NewDate("2020-01-10")
		p.add(ast.NewPad(date, "Assets:Checking", "Income:Other"))
		padding, _, _ := p.fill(balance, inventory, tolerance)
		assert.Equal(t, ast.Account("Income:Other"), padding.Postings[1].Account)
		assert.Equal(t, []*ast.Pad{first}, p.superseded)
	})

	t.Run("without a pad", func(t *testing.T) {
		_, balance, inventory := fillCase(t, "Equity:Opening-Balances", "1000.00", "10")
		padding, held, errs := newPads().fill(balance, inventory, tolerance)
		assert.Zero(t, padding)
		assert.Zero(t, errs)
		assert.Equal(t, "10", held.String())
	})
}
