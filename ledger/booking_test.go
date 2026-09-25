package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestBookingKeepsTransactionsThatFailValidation(t *testing.T) {
	// bean-check applies a transaction that does not balance or uses an
	// unknown account: Assets:Cash accumulates -999 + 5 + 1000 = 6 USD, and
	// the unbalanced buy's lot is there to reduce.
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Assets:Stock
2020-01-01 open Equity:Open

2020-01-02 * "unbalanced buy"
  Assets:Stock  10 HOOL {100 USD}
  Assets:Cash  -999 USD

2020-01-03 * "unknown account"
  Assets:Cash  5 USD
  Equity:Nope

2020-01-04 * "reduce"
  Assets:Stock  -10 HOOL {}
  Assets:Cash

2020-01-05 balance Assets:Cash 6 USD
2020-01-05 balance Assets:Stock 0 HOOL
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 2, len(errs), "errors: %v", errs)
	var notBalanced *TransactionNotBalancedError
	assert.True(t, errors.As(errs[0], &notBalanced), "got %v", errs[0])
	var notOpen *AccountNotOpenError
	assert.True(t, errors.As(errs[1], &notOpen), "got %v", errs[1])
}

func TestBookingDropsGroupsThatFailBooking(t *testing.T) {
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Assets:Stock

2020-01-02 * "buy"
  Assets:Stock  10 HOOL {100 USD}
  Assets:Cash

2020-01-03 * "sell too many"
  Assets:Stock  -50 HOOL {}
  Assets:Cash
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 1, len(errs), "errors: %v", errs)
	var insufficient *InsufficientInventoryError
	assert.True(t, errors.As(errs[0], &insufficient), "got %v", errs[0])

	// Like beancount, the transaction stays without its only group's postings.
	postings := map[string]int{}
	for _, d := range tree.Directives {
		if txn, ok := d.(*ast.Transaction); ok {
			postings[txn.Narration.String()] = len(txn.Postings)
		}
	}
	assert.Equal(t, map[string]int{"buy": 2, "sell too many": 0}, postings)
}
