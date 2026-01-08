package ledger_test

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func TestGetBalanceTree_TrialBalance(t *testing.T) {
	// Trial balance: all account types, no date filter
	l := ledger.New()
	source := `
2024-01-01 open Assets:Checking USD
2024-01-01 open Assets:Savings USD
2024-01-01 open Liabilities:CreditCard USD
2024-01-01 open Equity:Opening USD
2024-01-01 open Income:Salary USD
2024-01-01 open Expenses:Food USD

2024-01-15 * "Opening balance"
  Assets:Checking  1000.00 USD
  Equity:Opening

2024-01-20 * "Transfer to savings"
  Assets:Checking  -200.00 USD
  Assets:Savings    200.00 USD

2024-02-01 * "Salary"
  Assets:Checking  3000.00 USD
  Income:Salary

2024-02-15 * "Groceries"
  Expenses:Food     150.00 USD
  Assets:Checking

2024-02-20 * "Credit card charge"
  Liabilities:CreditCard  500.00 USD
  Assets:Checking
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	// Get trial balance (all types, current state)
	balanceTree, err := l.GetBalanceTree(nil, nil, nil)
	assert.NoError(t, err)

	// Should have 5 root nodes (one per account type with activity)
	assert.Equal(t, len(balanceTree.Roots), 5)

	// Verify Assets root
	assetsRoot := findRoot(balanceTree, "Assets")
	assert.True(t, assetsRoot != nil, "Assets root should exist")
	assert.Equal(t, assetsRoot.Account, "") // Virtual root
	assert.Equal(t, assetsRoot.Depth, 0)

	// Assets total: 1000 - 200 + 200 + 3000 - 150 - 500 = 3350
	assetsTotal := assetsRoot.Balance.Get("USD")
	assert.True(t, assetsTotal.Equal(decimal.NewFromInt(3350)), "Assets total should be 3350, got %s", assetsTotal)

	// Verify Income root (negative because income is credited)
	incomeRoot := findRoot(balanceTree, "Income")
	assert.True(t, incomeRoot != nil, "Income root should exist")
	incomeTotal := incomeRoot.Balance.Get("USD")
	assert.True(t, incomeTotal.Equal(decimal.NewFromInt(-3000)), "Income total should be -3000, got %s", incomeTotal)

	// Verify Expenses root
	expensesRoot := findRoot(balanceTree, "Expenses")
	assert.True(t, expensesRoot != nil, "Expenses root should exist")
	expensesTotal := expensesRoot.Balance.Get("USD")
	assert.True(t, expensesTotal.Equal(decimal.NewFromInt(150)), "Expenses total should be 150, got %s", expensesTotal)
}

func TestGetBalanceTree_BalanceSheet(t *testing.T) {
	// Balance sheet: Assets, Liabilities, Equity at a point in time
	l := ledger.New()
	source := `
2024-01-01 open Assets:Checking USD
2024-01-01 open Liabilities:CreditCard USD
2024-01-01 open Equity:Opening USD

2024-01-15 * "Opening"
  Assets:Checking  1000.00 USD
  Equity:Opening

2024-02-01 * "Credit card charge"
  Liabilities:CreditCard  200.00 USD
  Assets:Checking
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	// Get balance sheet (Assets, Liabilities, Equity) as of 2024-01-31
	date, _ := ast.NewDate("2024-01-31")
	balanceTree, err := l.GetBalanceTree(
		[]ast.AccountType{ast.AccountTypeAssets, ast.AccountTypeLiabilities, ast.AccountTypeEquity},
		date, date, // Point-in-time
	)
	assert.NoError(t, err)

	// Should have 3 roots (Assets, Equity, and Liabilities - all have accounts opened)
	// Liabilities has zero balance but is still included (Fava behavior)
	assert.Equal(t, len(balanceTree.Roots), 3)

	// Verify Assets as of 2024-01-31 (only opening balance)
	assetsRoot := findRoot(balanceTree, "Assets")
	assert.True(t, assetsRoot != nil, "Assets root should exist")
	assetsTotal := assetsRoot.Balance.Get("USD")
	assert.True(t, assetsTotal.Equal(decimal.NewFromInt(1000)), "Assets should be 1000 as of Jan 31, got %s", assetsTotal)

	// Verify Equity
	equityRoot := findRoot(balanceTree, "Equity")
	assert.True(t, equityRoot != nil, "Equity root should exist")
	equityTotal := equityRoot.Balance.Get("USD")
	assert.True(t, equityTotal.Equal(decimal.NewFromInt(-1000)), "Equity should be -1000, got %s", equityTotal)
}

func TestGetBalanceTree_IncomeStatement(t *testing.T) {
	// Income statement: Income and Expenses for a period
	l := ledger.New()
	source := `
2024-01-01 open Assets:Checking USD
2024-01-01 open Income:Salary USD
2024-01-01 open Expenses:Food USD
2024-01-01 open Expenses:Rent USD

2024-01-15 * "January salary"
  Assets:Checking  3000.00 USD
  Income:Salary

2024-01-20 * "January rent"
  Expenses:Rent    1000.00 USD
  Assets:Checking

2024-02-15 * "February salary"
  Assets:Checking  3000.00 USD
  Income:Salary

2024-02-20 * "February food"
  Expenses:Food     200.00 USD
  Assets:Checking

2024-02-25 * "February rent"
  Expenses:Rent    1000.00 USD
  Assets:Checking
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	// Get income statement for February only
	startDate, _ := ast.NewDate("2024-02-01")
	endDate, _ := ast.NewDate("2024-02-28")
	balanceTree, err := l.GetBalanceTree(
		[]ast.AccountType{ast.AccountTypeIncome, ast.AccountTypeExpenses},
		startDate, endDate,
	)
	assert.NoError(t, err)

	// Should have 2 roots
	assert.Equal(t, len(balanceTree.Roots), 2)

	// Verify Income for February
	incomeRoot := findRoot(balanceTree, "Income")
	assert.True(t, incomeRoot != nil, "Income root should exist")
	incomeTotal := incomeRoot.Balance.Get("USD")
	assert.True(t, incomeTotal.Equal(decimal.NewFromInt(-3000)), "February income should be -3000, got %s", incomeTotal)

	// Verify Expenses for February (Food + Rent)
	expensesRoot := findRoot(balanceTree, "Expenses")
	assert.True(t, expensesRoot != nil, "Expenses root should exist")
	expensesTotal := expensesRoot.Balance.Get("USD")
	assert.True(t, expensesTotal.Equal(decimal.NewFromInt(1200)), "February expenses should be 1200, got %s", expensesTotal)

	// Verify children exist
	assert.Equal(t, len(expensesRoot.Children), 2) // Food and Rent
}

func TestGetBalanceTree_HierarchicalAggregation(t *testing.T) {
	// Test that parent accounts aggregate children's balances
	l := ledger.New()
	source := `
2024-01-01 open Assets:US:Checking USD
2024-01-01 open Assets:US:Savings USD
2024-01-01 open Assets:EU:Checking EUR
2024-01-01 open Equity:Opening

2024-01-15 * "US deposits"
  Assets:US:Checking  1000.00 USD
  Assets:US:Savings    500.00 USD
  Equity:Opening

2024-01-15 * "EU deposit"
  Assets:EU:Checking  200.00 EUR
  Equity:Opening
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	balanceTree, err := l.GetBalanceTree([]ast.AccountType{ast.AccountTypeAssets}, nil, nil)
	assert.NoError(t, err)

	// Should have 1 root (Assets)
	assert.Equal(t, len(balanceTree.Roots), 1)

	assetsRoot := balanceTree.Roots[0]
	assert.Equal(t, assetsRoot.Name, "Assets")

	// Assets root should have aggregated USD and EUR
	assert.True(t, assetsRoot.Balance.Get("USD").Equal(decimal.NewFromInt(1500)))
	assert.True(t, assetsRoot.Balance.Get("EUR").Equal(decimal.NewFromInt(200)))

	// Should have 2 children: Assets:EU and Assets:US
	assert.Equal(t, len(assetsRoot.Children), 2)

	// Find Assets:US child
	var usNode *ledger.BalanceNode
	for _, child := range assetsRoot.Children {
		if child.Name == "Assets:US" {
			usNode = child
			break
		}
	}
	assert.True(t, usNode != nil, "Assets:US should exist")

	// Assets:US should aggregate its children
	assert.True(t, usNode.Balance.Get("USD").Equal(decimal.NewFromInt(1500)))

	// Assets:US should have 2 children
	assert.Equal(t, len(usNode.Children), 2)
}

func TestGetBalanceTree_InvalidDateRange(t *testing.T) {
	l := ledger.New()
	source := `
2024-01-01 open Assets:Checking USD
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	// startDate > endDate should return error
	startDate, _ := ast.NewDate("2024-02-01")
	endDate, _ := ast.NewDate("2024-01-01")
	_, err = l.GetBalanceTree(nil, startDate, endDate)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after")
}

func TestGetBalanceTree_NoTransactions(t *testing.T) {
	l := ledger.New()
	source := `
2024-01-01 open Assets:Checking USD
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	// No transactions - account exists but has no balance
	// The account still appears in the tree (Fava behavior)
	balanceTree, err := l.GetBalanceTree(nil, nil, nil)
	assert.NoError(t, err)

	// Account with zero balance is still included
	assert.Equal(t, len(balanceTree.Roots), 1)
	assetsRoot := balanceTree.Roots[0]
	assert.Equal(t, assetsRoot.Name, "Assets")
	assert.True(t, assetsRoot.Balance.IsZero())
}

func TestGetBalanceTree_MultiCurrency(t *testing.T) {
	l := ledger.New()
	source := `
2024-01-01 open Assets:USD USD
2024-01-01 open Assets:EUR EUR
2024-01-01 open Assets:GBP GBP
2024-01-01 open Equity:Opening

2024-01-15 * "USD deposit"
  Assets:USD  1000.00 USD
  Equity:Opening  -1000.00 USD

2024-01-15 * "EUR deposit"
  Assets:EUR   500.00 EUR
  Equity:Opening  -500.00 EUR

2024-01-15 * "GBP deposit"
  Assets:GBP   200.00 GBP
  Equity:Opening  -200.00 GBP
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	balanceTree, err := l.GetBalanceTree([]ast.AccountType{ast.AccountTypeAssets}, nil, nil)
	assert.NoError(t, err)

	// Verify currencies are tracked
	assert.Equal(t, len(balanceTree.Currencies), 3)
	assert.Equal(t, balanceTree.Currencies[0], "EUR") // Sorted alphabetically
	assert.Equal(t, balanceTree.Currencies[1], "GBP")
	assert.Equal(t, balanceTree.Currencies[2], "USD")

	// Verify balances
	assetsRoot := balanceTree.Roots[0]
	assert.True(t, assetsRoot.Balance.Get("USD").Equal(decimal.NewFromInt(1000)))
	assert.True(t, assetsRoot.Balance.Get("EUR").Equal(decimal.NewFromInt(500)))
	assert.True(t, assetsRoot.Balance.Get("GBP").Equal(decimal.NewFromInt(200)))
}

func TestGetBalanceTree_ZeroBalancesIncluded(t *testing.T) {
	// Fava behavior: include accounts with zero balance in hierarchy
	l := ledger.New()
	source := `
2024-01-01 open Assets:Checking USD
2024-01-01 open Assets:Savings USD
2024-01-01 open Equity:Opening

2024-01-15 * "Deposit"
  Assets:Checking  1000.00 USD
  Equity:Opening

2024-01-20 * "Transfer all to savings"
  Assets:Checking  -1000.00 USD
  Assets:Savings    1000.00 USD
`
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, []byte(source))
	assert.NoError(t, err)
	assert.NoError(t, l.Process(ctx, tree))

	balanceTree, err := l.GetBalanceTree([]ast.AccountType{ast.AccountTypeAssets}, nil, nil)
	assert.NoError(t, err)

	// Assets:Checking has zero balance but should still appear
	assetsRoot := balanceTree.Roots[0]
	assert.Equal(t, len(assetsRoot.Children), 2) // Both Checking and Savings

	// Find Checking
	var checkingNode *ledger.BalanceNode
	for _, child := range assetsRoot.Children {
		if child.Name == "Assets:Checking" {
			checkingNode = child
			break
		}
	}
	assert.True(t, checkingNode != nil, "Assets:Checking should exist even with zero balance")
	assert.True(t, checkingNode.Balance.Get("USD").IsZero())
}

// Helper function to find a root node by name
func findRoot(tree *ledger.BalanceTree, name string) *ledger.BalanceNode {
	for _, root := range tree.Roots {
		if root.Name == name {
			return root
		}
	}
	return nil
}
