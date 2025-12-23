package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// TestAccountPostings_SimpleTransaction verifies that postings are recorded correctly
// when transactions are applied.
func TestAccountPostings_SimpleTransaction(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening balance",
				ast.WithPostings(
					ast.NewPosting(assets, ast.WithAmount("100", "USD")),
					ast.NewPosting(equity),
				),
			),
		},
	})

	// Find Assets:Cash and verify postings were recorded
	accounts := l.Accounts()
	account := accounts[string(assets)]
	assert.True(t, account != nil, "account should exist")
	assert.Equal(t, account.Name, assets)
	assert.Equal(t, len(account.Postings), 1)
	assert.Equal(t, account.Postings[0].Posting.Account, assets)
}

// TestGetPostingsBefore_NoPostings verifies query returns empty for accounts
// with no transactions before a date.
func TestGetPostingsBefore_NoPostings(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-06-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]
	postings := account.GetPostingsBefore(date2)
	assert.Equal(t, len(postings), 1)
}

// TestGetPostingsBefore_BeforeEarliestDate verifies query returns empty
// for accounts with no transactions before a date.
func TestGetPostingsBefore_BeforeEarliestDate(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date0, _ := ast.NewDate("2023-12-31")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]
	postings := account.GetPostingsBefore(date0)
	assert.Equal(t, len(postings), 0)
}

// TestGetPostingsInPeriod_MultipleTransactions verifies period filtering
// correctly includes transactions within [start, end].
func TestGetPostingsInPeriod_MultipleTransactions(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")
	expenses, _ := ast.NewAccount("Expenses:Food")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")
	date3Txn, _ := ast.NewDate("2024-03-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			&ast.Open{Date: date1, Account: expenses},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date2, "Food", ast.WithPostings(
				ast.NewPosting(expenses, ast.WithAmount("50", "USD")),
				ast.NewPosting(assets),
			)),
			ast.NewTransaction(date3Txn, "More food", ast.WithPostings(
				ast.NewPosting(expenses, ast.WithAmount("75", "USD")),
				ast.NewPosting(assets),
			)),
		},
	})

	expensesAccount := l.Accounts()[string(expenses)]
	assert.True(t, expensesAccount != nil, "expenses account should exist")
	assert.Equal(t, expensesAccount.Name, expenses)

	// Query period [2024-02-01, 2024-02-28] - should get one posting
	periodStart, _ := ast.NewDate("2024-02-01")
	periodEnd, _ := ast.NewDate("2024-02-28")
	postings := expensesAccount.GetPostingsInPeriod(periodStart, periodEnd)
	assert.Equal(t, len(postings), 1)
	assert.Equal(t, postings[0].Transaction.Date, date2)
}

// TestGetBalancesAsOf_SimpleCase verifies balance calculation for a single account.
func TestGetBalancesAsOf_SimpleCase(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	// Get balances as of 2024-02-01 (after the transaction)
	balances := l.GetBalancesAsOf(date2)
	// Both Assets:Cash and Equity:Opening have postings
	assert.Equal(t, len(balances), 2)

	// Find each account in the result
	var assetBal, equityBal *AccountBalance
	for i := range balances {
		switch balances[i].Account {
		case "Assets:Cash":
			assetBal = &balances[i]
		case "Equity:Opening":
			equityBal = &balances[i]
		}
	}
	assert.True(t, assetBal != nil, "Assets:Cash should exist")
	assert.True(t, equityBal != nil, "Equity:Opening should exist")
	assert.True(t, assetBal.Balances["USD"].Equal(decimal.NewFromInt(100)))
	assert.True(t, equityBal.Balances["USD"].Equal(decimal.NewFromInt(-100)))
}

// TestGetBalancesAsOf_MultiCurrency verifies balance calculation with multiple currencies.
func TestGetBalancesAsOf_MultiCurrency(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
		},
	})

	balances := l.GetBalancesAsOf(date2)
	// Both Assets:Cash and Equity:Opening have postings
	assert.Equal(t, len(balances), 2)

	// Find each account in the result
	var assetBal, equityBal *AccountBalance
	for i := range balances {
		switch balances[i].Account {
		case "Assets:Cash":
			assetBal = &balances[i]
		case "Equity:Opening":
			equityBal = &balances[i]
		}
	}
	assert.True(t, assetBal != nil, "Assets:Cash should exist")
	assert.True(t, equityBal != nil, "Equity:Opening should exist")
	assert.True(t, assetBal.Balances["USD"].Equal(decimal.NewFromInt(100)))
	assert.True(t, assetBal.Balances["EUR"].Equal(decimal.NewFromInt(50)))
	// Equity:Opening has inverse balance
	assert.True(t, equityBal.Balances["USD"].Equal(decimal.NewFromInt(-100)))
	assert.True(t, equityBal.Balances["EUR"].Equal(decimal.NewFromInt(-50)))
}

// TestGetBalancesInPeriod_IncomeExpenses verifies period balance filtering by account type.
func TestGetBalancesInPeriod_IncomeExpenses(t *testing.T) {
	l := New()
	income, _ := ast.NewAccount("Income:Salary")
	expenses, _ := ast.NewAccount("Expenses:Food")
	assets, _ := ast.NewAccount("Assets:Cash")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: income},
			&ast.Open{Date: date1, Account: expenses},
			&ast.Open{Date: date1, Account: assets},
			// Income posting
			ast.NewTransaction(date2, "Salary", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(income),
			)),
			// Expense posting
			ast.NewTransaction(date2, "Food", ast.WithPostings(
				ast.NewPosting(expenses, ast.WithAmount("50", "USD")),
				ast.NewPosting(assets),
			)),
		},
	})

	periodStart, _ := ast.NewDate("2024-01-01")
	periodEnd, _ := ast.NewDate("2024-02-28")

	// Get only Income + Expenses
	balances := l.GetBalancesInPeriod(periodStart, periodEnd, ast.AccountTypeIncome, ast.AccountTypeExpenses)

	// Should have 2 accounts (Income + Expenses)
	assert.Equal(t, len(balances), 2)

	// Find each account
	var incomeBal, expenseBal *AccountBalance
	for i := range balances {
		switch balances[i].Account {
		case "Income:Salary":
			incomeBal = &balances[i]
		case "Expenses:Food":
			expenseBal = &balances[i]
		}
	}

	assert.True(t, incomeBal != nil, "Income:Salary should exist")
	assert.True(t, expenseBal != nil, "Expenses:Food should exist")
	// Income should be negative (offset)
	assert.True(t, incomeBal.Balances["USD"].Equal(decimal.NewFromInt(-1000)))
	// Expenses should be positive
	assert.True(t, expenseBal.Balances["USD"].Equal(decimal.NewFromInt(50)))
}

// TestCloseBooks_SimpleIncome verifies closing transactions are generated correctly.
func TestCloseBooks_SimpleIncome(t *testing.T) {
	l := New()
	income, _ := ast.NewAccount("Income:Salary")
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Earnings:Current")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: income},
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date2, "Salary", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(income),
			)),
		},
	})

	closingDate, _ := ast.NewDate("2024-02-28")
	closingTxns := l.CloseBooks(closingDate)

	// Should generate exactly one closing transaction
	assert.Equal(t, len(closingTxns), 1)

	txn := closingTxns[0]
	assert.Equal(t, txn.Date, closingDate)
	assert.Equal(t, txn.Flag, "P") // Synthetic/padding flag
	assert.Equal(t, len(txn.Postings), 2)
}

// TestCloseBooks_Empty verifies no closing transactions when no activity.
func TestCloseBooks_Empty(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")

	date1, _ := ast.NewDate("2024-01-01")
	closingDate, _ := ast.NewDate("2024-02-28")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
		},
	})

	closingTxns := l.CloseBooks(closingDate)
	assert.Equal(t, len(closingTxns), 0) // No income/expenses, no closing
}
