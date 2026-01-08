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
			ast.NewOpen(date1, assets, nil, ""),
			ast.NewOpen(date1, equity, nil, ""),
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
			ast.NewOpen(date1, assets, nil, ""),
			ast.NewOpen(date1, equity, nil, ""),
			ast.NewOpen(date1, expenses, nil, ""),
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
	postings := expensesAccount.GetPostingsInPeriod(*periodStart, *periodEnd)
	assert.Equal(t, len(postings), 1)
	assert.Equal(t, postings[0].Transaction.Date(), date2)
}

// TestGetPostingsInPeriod_PointInTime verifies that when start == end,
// all postings up to and including that date are returned.
func TestGetPostingsInPeriod_PointInTime(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")
	date3, _ := ast.NewDate("2024-03-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			ast.NewOpen(date1, assets, nil, ""),
			ast.NewOpen(date1, equity, nil, ""),
			ast.NewTransaction(date1, "First", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date2, "Second", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("200", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date3, "Third", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("300", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]

	// Point-in-time query: start == end
	// Should return all postings up to and including date2
	postings := account.GetPostingsInPeriod(*date2, *date2)
	assert.Equal(t, len(postings), 2) // First and Second transactions
}

// TestGetBalanceInPeriod_PointInTime verifies point-in-time balance calculation.
func TestGetBalanceInPeriod_PointInTime(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")
	date3, _ := ast.NewDate("2024-03-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			ast.NewOpen(date1, assets, nil, ""),
			ast.NewOpen(date1, equity, nil, ""),
			ast.NewTransaction(date1, "First", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date2, "Second", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("200", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date3, "Third", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("300", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]

	// Point-in-time: balance as of date2 (should include first two transactions)
	balance := account.GetBalanceInPeriod(*date2, *date2)
	assert.True(t, balance.Get("USD").Equal(decimal.NewFromInt(300))) // 100 + 200
}

// TestGetBalanceInPeriod_Range verifies period balance calculation.
func TestGetBalanceInPeriod_Range(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")
	date3, _ := ast.NewDate("2024-03-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			ast.NewOpen(date1, assets, nil, ""),
			ast.NewOpen(date1, equity, nil, ""),
			ast.NewTransaction(date1, "First", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date2, "Second", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("200", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date3, "Third", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("300", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]

	// Period: only transactions in [date2, date3]
	balance := account.GetBalanceInPeriod(*date2, *date3)
	assert.True(t, balance.Get("USD").Equal(decimal.NewFromInt(500))) // 200 + 300
}

// TestGetBalanceInPeriod_MultiCurrency verifies multi-currency balance calculation.
func TestGetBalanceInPeriod_MultiCurrency(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			ast.NewOpen(date1, assets, nil, ""),
			ast.NewOpen(date1, equity, nil, ""),
			ast.NewTransaction(date1, "USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]
	balance := account.GetBalanceInPeriod(*date1, *date1)

	assert.True(t, balance.Get("USD").Equal(decimal.NewFromInt(100)))
	assert.True(t, balance.Get("EUR").Equal(decimal.NewFromInt(50)))
}
