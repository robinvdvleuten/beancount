package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
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

func TestBalanceInACurrencyTheAccountDoesNotAllow(t *testing.T) {
	source := `
2020-01-01 open Assets:Euro EUR
2020-01-01 open Assets:Any

2020-02-01 balance Assets:Euro 0 USD
2020-02-01 balance Assets:Euro 0 EUR
2020-02-01 balance Assets:Any 0 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 1, len(errs), "errors: %v", errs)
	assert.Equal(t, "BalanceCurrencyError", kindOf(errs[0]), "got %v", errs[0])
	currencyErr := errs[0]
	assert.Contains(t, errs[0].Error(), "Invalid currency 'USD'")
	assert.Equal(t, 5, currencyErr.(*Diagnostic).GetPosition().Line)
}

func TestDuplicateBalanceWithADifferentAmount(t *testing.T) {
	source := `
2020-01-01 open Assets:Euro
2020-01-01 open Equity:O

2020-01-10 * "x"
  Assets:Euro  5 EUR
  Equity:O

2020-02-01 balance Assets:Euro 5 EUR
2020-02-01 balance Assets:Euro 6 EUR
2020-02-01 balance Assets:Euro 5 EUR
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_ = l.Process(context.Background(), tree)

	// Line 10 fails and repeats line 9 with another amount; line 11 is an
	// identical repeat.
	errs := l.Errors()
	assert.Equal(t, 2, len(errs), "errors: %v", errs)
	var mismatch *BalanceMismatchError
	assert.True(t, errors.As(errs[0], &mismatch), "got %v", errs[0])
	assert.Equal(t, "DuplicateBalanceError", kindOf(errs[1]), "got %v", errs[1])
	duplicate := errs[1]
	assert.Equal(t, 10, duplicate.(*Diagnostic).GetPosition().Line)
}
