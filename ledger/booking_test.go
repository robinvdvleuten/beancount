package ledger

import (
	"context"
	"fmt"
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
	assert.Equal(t, "TransactionNotBalancedError", kindOf(errs[0]), "got %v", errs[0])
	assert.Equal(t, "AccountNotOpenError", kindOf(errs[1]), "got %v", errs[1])
}

func TestBookingErrorListsLotsAtTheFailedPosting(t *testing.T) {
	// The later sale reduces the same lot; the error must still show the
	// 10 HOOL the lot held when the first reduction failed, like bean-check.
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Assets:Stock

2020-01-02 * "buy"
  Assets:Stock  10 HOOL {100 USD}
  Assets:Cash

2020-01-03 * "sell too many"
  Assets:Stock  -50 HOOL {}
  Assets:Cash

2020-01-04 * "sell"
  Assets:Stock  -4 HOOL {}
  Assets:Cash
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 1, len(errs), "errors: %v", errs)
	assert.Contains(t, errs[0].Error(), `"-50 HOOL {}": 10 HOOL {100 USD, 2020-01-02}`)
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
	assert.Equal(t, "InsufficientInventoryError", kindOf(errs[0]), "got %v", errs[0])

	// Like beancount, the transaction stays without its only group's postings.
	postings := map[string]int{}
	for _, d := range tree.Directives {
		if txn, ok := d.(*ast.Transaction); ok {
			postings[txn.Narration.String()] = len(txn.Postings)
		}
	}
	assert.Equal(t, map[string]int{"buy": 2, "sell too many": 0}, postings)
}

func TestBookingFixesUpPricesLikeBeancountsParser(t *testing.T) {
	// A negative price is reported on its posting and made positive; a total
	// price without units is reported and dropped, and the posting, whose
	// units interpolate to a zero weight, leaves the transaction.
	source := `
2020-01-01 open Assets:A
2020-01-01 open Assets:Cash

2020-01-03 * "units missing total price"
  Assets:A      HOOL @@ 10 USD
  Assets:Cash  -10 USD

2020-01-04 * "negative per unit"
  Assets:A      2 HOOL @ -3 USD
  Assets:Cash  6 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_ = l.Process(context.Background(), tree)

	var kinds []string
	for _, err := range l.Errors() {
		kinds = append(kinds, fmt.Sprintf("%s@%d", kindOf(err), err.(interface{ GetPosition() ast.Position }).GetPosition().Line))
	}
	assert.Equal(t, []string{
		"TotalPriceWithoutUnitsError@6",
		"NegativePriceError@10",
		"TransactionNotBalancedError@5",
		"TransactionNotBalancedError@9",
	}, kinds)

	postings := map[string]int{}
	for _, d := range tree.Directives {
		if txn, ok := d.(*ast.Transaction); ok {
			postings[txn.Narration.String()] = len(txn.Postings)
			if txn.Narration.String() == "negative per unit" {
				assert.Equal(t, "3", txn.Postings[0].Price.Value)
			}
		}
	}
	assert.Equal(t, map[string]int{"units missing total price": 1, "negative per unit": 2}, postings)
}
