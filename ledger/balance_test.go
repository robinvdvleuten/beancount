package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
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
	assert.Equal(t, account.name, assets)
	assert.Equal(t, len(account.postings), 1)
	assert.Equal(t, account.postings[0].posting.Account, assets)
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
	_, _ = l.Process(context.Background(), tree)

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
	_, _ = l.Process(context.Background(), tree)

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
