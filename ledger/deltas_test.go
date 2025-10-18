package ledger

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// TestDelta_PureValidation verifies that validators don't mutate state
func TestDelta_PureValidation(t *testing.T) {
	ctx := context.Background()

	// Setup
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	ledger := New()
	ledger.processOpen(ctx, ast.NewOpen(date, checking, nil, ""))
	ledger.processOpen(ctx, ast.NewOpen(date, expenses, nil, ""))

	// Get initial inventory state
	initialBalance := ledger.accounts["Assets:Checking"].Inventory.Get("USD")

	// Create transaction
	txn := ast.NewTransaction(date, "Test transaction",
		ast.WithPostings(
			ast.NewPosting(checking, ast.WithAmount("-100", "USD")),
			ast.NewPosting(expenses, ast.WithAmount("100", "USD")),
		),
	)

	// Validate but don't apply
	v := newValidator(ledger.accounts, ledger.padEntries, ledger.toleranceConfig)
	errs, delta := v.validateTransaction(ctx, txn)

	// Validation should succeed
	assert.Zero(t, len(errs), "validation should succeed")
	assert.NotZero(t, delta, "delta should be returned")

	// CRITICAL: State should NOT have changed after validation
	afterValidationBalance := ledger.accounts["Assets:Checking"].Inventory.Get("USD")
	assert.Equal(t, initialBalance, afterValidationBalance, "validation should not mutate state")

	// Now apply the delta
	ledger.ApplyTransactionDelta(delta)

	// NOW state should have changed
	afterApplicationBalance := ledger.accounts["Assets:Checking"].Inventory.Get("USD")
	assert.Equal(t, decimal.NewFromInt(-100), afterApplicationBalance, "state should change after apply")
}

// TestTransactionDelta_Creation tests that transaction deltas are created correctly
func TestTransactionDelta_Creation(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Expenses:Groceries": {
			Name:      expenses,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
	}

	txn := ast.NewTransaction(date, "Groceries",
		ast.WithPostings(
			ast.NewPosting(checking, ast.WithAmount("-50.25", "USD")),
			ast.NewPosting(expenses, ast.WithAmount("50.25", "USD")),
		),
	)

	v := newValidator(accounts, nil, NewToleranceConfig())
	errs, delta := v.validateTransaction(ctx, txn)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)
	assert.Equal(t, txn, delta.Transaction)
	assert.Equal(t, 2, len(delta.InventoryChanges), "should have 2 inventory changes")

	// Check first change (checking account reduction)
	change1 := delta.InventoryChanges[0]
	assert.Equal(t, "Assets:Checking", change1.Account)
	assert.Equal(t, "USD", change1.Currency)
	assert.True(t, change1.Amount.Equal(decimal.NewFromFloat(50.25)), "amount should be 50.25 (positive, operation indicates direction)")
	assert.Equal(t, OpReduce, change1.Operation, "negative posting amount becomes OpReduce")

	// Check second change (expenses addition)
	change2 := delta.InventoryChanges[1]
	assert.Equal(t, "Expenses:Groceries", change2.Account)
	assert.Equal(t, "USD", change2.Currency)
	assert.True(t, change2.Amount.Equal(decimal.NewFromFloat(50.25)))
	assert.Equal(t, OpAdd, change2.Operation)
}

// TestTransactionDelta_WithInferredAmount tests delta with inferred amounts
func TestTransactionDelta_WithInferredAmount(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Expenses:Groceries": {
			Name:      expenses,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
	}

	// Transaction with one missing amount (will be inferred)
	txn := ast.NewTransaction(date, "Groceries",
		ast.WithPostings(
			ast.NewPosting(checking, ast.WithAmount("-100", "USD")),
			ast.NewPosting(expenses), // Amount will be inferred
		),
	)

	v := newValidator(accounts, nil, NewToleranceConfig())
	errs, delta := v.validateTransaction(ctx, txn)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)

	// Check that amount was inferred
	assert.Equal(t, 1, len(delta.InferredAmounts), "should have 1 inferred amount")
	inferredAmount := delta.InferredAmounts[txn.Postings[1]]
	assert.NotZero(t, inferredAmount)
	assert.Equal(t, "100", inferredAmount.Value)
	assert.Equal(t, "USD", inferredAmount.Currency)
}

// TestTransactionDelta_WithLots tests delta with lot-based inventory
func TestTransactionDelta_WithLots(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	stock, _ := ast.NewAccount("Assets:Stock")
	checking, _ := ast.NewAccount("Assets:Checking")

	accounts := map[string]*Account{
		"Assets:Stock": {
			Name:      stock,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
	}

	// Buy stock with explicit cost
	cost := ast.NewCost(ast.NewAmount("500", "USD"))
	txn := ast.NewTransaction(date, "Buy stock",
		ast.WithPostings(
			ast.NewPosting(stock, ast.WithAmount("10", "HOOL"), ast.WithCost(cost)),
			ast.NewPosting(checking, ast.WithAmount("-5000", "USD")),
		),
	)

	v := newValidator(accounts, nil, NewToleranceConfig())
	errs, delta := v.validateTransaction(ctx, txn)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)
	assert.Equal(t, 2, len(delta.InventoryChanges))

	// Check stock addition with lot
	stockChange := delta.InventoryChanges[0]
	assert.Equal(t, "Assets:Stock", stockChange.Account)
	assert.Equal(t, "HOOL", stockChange.Currency)
	assert.Equal(t, OpAdd, stockChange.Operation)
	assert.NotZero(t, stockChange.LotSpec, "should have lot spec")
	assert.True(t, stockChange.LotSpec.Cost.Equal(decimal.NewFromInt(500)))
}

// TestBalanceDelta_WithPadding tests balance delta with padding
func TestBalanceDelta_WithPadding(t *testing.T) {
	ctx := context.Background()
	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	equity, _ := ast.NewAccount("Equity:Opening-Balances")

	ledger := New()
	ledger.processOpen(ctx, ast.NewOpen(date1, checking, nil, ""))
	ledger.processOpen(ctx, ast.NewOpen(date1, equity, nil, ""))

	// Add pad directive
	pad := &ast.Pad{
		Date:       date1,
		Account:    checking,
		AccountPad: equity,
	}
	ledger.processPad(ctx, pad)

	// Create balance assertion
	balance := &ast.Balance{
		Date:    date2,
		Account: checking,
		Amount:  ast.NewAmount("1000", "USD"),
	}

	v := newValidator(ledger.accounts, ledger.padEntries, ledger.toleranceConfig)
	errs, delta := v.validateBalance(ctx, balance)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)
	assert.True(t, delta.PadRequired, "padding should be required")
	assert.Equal(t, "USD", delta.PadCurrency)
	assert.True(t, delta.PadAmount.Equal(decimal.NewFromInt(1000)))
	assert.Equal(t, "Equity:Opening-Balances", delta.PadAccount)
}

// TestOpenDelta_Creation tests open delta creation
func TestOpenDelta_Creation(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")

	open := ast.NewOpen(date, checking, nil, "")
	v := newValidator(make(map[string]*Account), nil, NewToleranceConfig())
	errs, delta := v.validateOpen(ctx, open)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)
	assert.Equal(t, open, delta.Open)
	assert.NotZero(t, delta.Account, "account should be pre-created")
	assert.Equal(t, checking, delta.Account.Name)
	assert.Equal(t, date, delta.Account.OpenDate)
	assert.NotZero(t, delta.Account.Inventory, "inventory should be initialized")
}

// TestCloseDelta_Creation tests close delta creation
func TestCloseDelta_Creation(t *testing.T) {
	ctx := context.Background()
	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-12-31")
	checking, _ := ast.NewAccount("Assets:Checking")

	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date1,
			Inventory: NewInventory(),
		},
	}

	close := &ast.Close{
		Date:    date2,
		Account: checking,
	}

	v := newValidator(accounts, nil, NewToleranceConfig())
	errs, delta := v.validateClose(ctx, close)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)
	assert.Equal(t, close, delta.Close)
	assert.Equal(t, "Assets:Checking", delta.AccountName)
}

// TestPadDelta_Creation tests pad delta creation
func TestPadDelta_Creation(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-01")
	checking, _ := ast.NewAccount("Assets:Checking")
	equity, _ := ast.NewAccount("Equity:Opening-Balances")

	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Equity:Opening-Balances": {
			Name:      equity,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
	}

	pad := &ast.Pad{
		Date:       date,
		Account:    checking,
		AccountPad: equity,
	}

	v := newValidator(accounts, nil, NewToleranceConfig())
	errs, delta := v.validatePad(ctx, pad)

	assert.Zero(t, len(errs))
	assert.NotZero(t, delta)
	assert.Equal(t, pad, delta.Pad)
	assert.Equal(t, "Assets:Checking", delta.AccountName)
}

// TestPadDelta_DuplicateDetection tests that duplicate pads are caught in validation
func TestPadDelta_DuplicateDetection(t *testing.T) {
	ctx := context.Background()
	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	equity, _ := ast.NewAccount("Equity:Opening-Balances")

	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date1,
			Inventory: NewInventory(),
		},
		"Equity:Opening-Balances": {
			Name:      equity,
			OpenDate:  date1,
			Inventory: NewInventory(),
		},
	}

	// First pad directive
	firstPad := &ast.Pad{
		Date:       date1,
		Account:    checking,
		AccountPad: equity,
	}

	// Create padEntries map with the first pad
	padEntries := map[string]*ast.Pad{
		"Assets:Checking": firstPad,
	}

	// Try to add second pad for same account
	secondPad := &ast.Pad{
		Date:       date2,
		Account:    checking,
		AccountPad: equity,
	}

	v := newValidator(accounts, padEntries, NewToleranceConfig())
	errs, delta := v.validatePad(ctx, secondPad)

	// Should have validation error
	assert.Equal(t, 1, len(errs), "should have duplicate pad error")
	assert.Zero(t, delta, "delta should be nil when validation fails")
	assert.True(t, strings.Contains(errs[0].Error(), "Duplicate pad directive"))
}

// TestDelta_String tests String() methods for logging/debugging
func TestDelta_String(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Expenses:Groceries": {
			Name:      expenses,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
	}

	txn := ast.NewTransaction(date, "Groceries",
		ast.WithPostings(
			ast.NewPosting(checking, ast.WithAmount("-50", "USD")),
			ast.NewPosting(expenses, ast.WithAmount("50", "USD")),
		),
	)

	v := newValidator(accounts, nil, NewToleranceConfig())
	_, delta := v.validateTransaction(ctx, txn)

	// Test that String() produces useful output
	str := delta.String()
	assert.True(t, strings.Contains(str, "Transaction on 2024-01-15"))
	assert.True(t, strings.Contains(str, "Inventory changes"))
	assert.True(t, strings.Contains(str, "Assets:Checking"))
	assert.True(t, strings.Contains(str, "Expenses:Groceries"))
}

// TestDelta_InspectionBeforeApply tests that deltas can be inspected before applying
func TestDelta_InspectionBeforeApply(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	ledger := New()
	ledger.processOpen(ctx, ast.NewOpen(date, checking, nil, ""))
	ledger.processOpen(ctx, ast.NewOpen(date, expenses, nil, ""))

	txn := ast.NewTransaction(date, "Groceries",
		ast.WithPostings(
			ast.NewPosting(checking, ast.WithAmount("-100", "USD")),
			ast.NewPosting(expenses, ast.WithAmount("100", "USD")),
		),
	)

	v := newValidator(ledger.accounts, ledger.padEntries, ledger.toleranceConfig)
	errs, delta := v.validateTransaction(ctx, txn)

	assert.Zero(t, len(errs))

	// Inspect delta BEFORE applying
	assert.Equal(t, 2, len(delta.InventoryChanges))
	assert.Equal(t, "Assets:Checking", delta.InventoryChanges[0].Account)
	assert.Equal(t, "Expenses:Groceries", delta.InventoryChanges[1].Account)

	// Can log/debug without applying
	_ = delta.String()

	// Decision: apply only if we want to
	ledger.ApplyTransactionDelta(delta)

	// Now state has changed
	balance := ledger.accounts["Assets:Checking"].Inventory.Get("USD")
	assert.True(t, balance.Equal(decimal.NewFromInt(-100)))
}

// TestDelta_Application tests that apply methods correctly mutate state
func TestDelta_Application(t *testing.T) {
	ctx := context.Background()
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")

	ledger := New()

	// Test OpenDelta application
	open := ast.NewOpen(date, checking, nil, "")
	v := newValidator(ledger.accounts, ledger.padEntries, ledger.toleranceConfig)
	_, openDelta := v.validateOpen(ctx, open)
	ledger.ApplyOpenDelta(openDelta)

	account, exists := ledger.accounts["Assets:Checking"]
	assert.True(t, exists, "account should exist after applying OpenDelta")
	assert.Equal(t, checking, account.Name)

	// Test CloseDelta application
	closeDate, _ := ast.NewDate("2024-12-31")
	close := &ast.Close{Date: closeDate, Account: checking}
	v2 := newValidator(ledger.accounts, ledger.padEntries, ledger.toleranceConfig)
	_, closeDelta := v2.validateClose(ctx, close)
	ledger.ApplyCloseDelta(closeDelta)

	assert.True(t, account.IsClosed(), "account should be closed after applying CloseDelta")
	assert.Equal(t, closeDate, account.CloseDate)
}

// TestInventoryChange_String tests InventoryChange String() method
func TestInventoryChange_String(t *testing.T) {
	// Simple add
	change1 := InventoryChange{
		Account:   "Assets:Checking",
		Currency:  "USD",
		Amount:    decimal.NewFromInt(100),
		Operation: OpAdd,
	}
	str1 := change1.String()
	assert.True(t, strings.Contains(str1, "Add"))
	assert.True(t, strings.Contains(str1, "100"))
	assert.True(t, strings.Contains(str1, "USD"))
	assert.True(t, strings.Contains(str1, "to"))
	assert.True(t, strings.Contains(str1, "Assets:Checking"))

	// Reduce with lot
	cost := decimal.NewFromInt(500)
	lotSpec := &lotSpec{
		Cost:         &cost,
		CostCurrency: "USD",
	}
	change2 := InventoryChange{
		Account:   "Assets:Stock",
		Currency:  "HOOL",
		Amount:    decimal.NewFromInt(10),
		LotSpec:   lotSpec,
		Operation: OpReduce,
	}
	str2 := change2.String()
	assert.True(t, strings.Contains(str2, "Reduce"))
	assert.True(t, strings.Contains(str2, "HOOL"))
	assert.True(t, strings.Contains(str2, "from"))
	assert.True(t, strings.Contains(str2, "{500 USD}"))
}
