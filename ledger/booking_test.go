package ledger

import (
	"context"
	"errors"
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
	_, _ = l.Process(context.Background(), tree)

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
	_, _ = l.Process(context.Background(), tree)

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
	tree, _ = l.Process(context.Background(), tree)

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

	// The error carries the transaction as written, the dropped postings
	// and their missing number included, like beancount's.
	unbooked, ok := errs[0].(*Diagnostic).GetDirective().(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(unbooked.Postings))
	assert.Equal(t, (*ast.Amount)(nil), unbooked.Postings[1].Amount)
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
	tree, _ = l.Process(context.Background(), tree)

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

// bookAll books the source's transactions with a new booker, and returns
// it with each transaction's booked postings, rendered, and errors.
func bookAll(t *testing.T, source string) (*booker, map[string][]string, []error) {
	t.Helper()
	tree := parser.MustParseString(context.Background(), source)
	b := newBooker(NewConfig(), newTolerances(nil), tree.Directives)
	postings := make(map[string][]string)
	var errs []error
	for _, directive := range tree.Directives {
		txn, ok := directive.(*ast.Transaction)
		if !ok {
			continue
		}
		booked, bookErrs := b.book(txn)
		errs = append(errs, bookErrs...)
		if booked == nil {
			continue // A Dropped transaction
		}
		for _, bp := range booked.postings {
			for _, position := range bp.positions {
				postings[txn.Narration.String()] = append(postings[txn.Narration.String()],
					fmt.Sprintf("%s %s %s", position.Units, bp.commodity, position.lotSpec()))
			}
		}
	}
	return b, postings, errs
}

func TestBookingStagesTheGroupsItBooks(t *testing.T) {
	// A Currency group's reductions change the inventories once the group
	// is booked; a Dropped group's, which failed booking or interpolation
	// after an earlier posting reduced a lot, change nothing. bean-check
	// 2.3.6 agrees: the lots left are sold in full at the end.
	b, postings, errs := bookAll(t, `
2024-01-01 open Assets:Stock
2024-01-01 open Assets:Cash

2024-01-02 * "hold"
  Assets:Stock   10 AA {5 USD}
  Assets:Stock   10 BB {2 EUR}
  Assets:Stock   10 CC {1 GBP}
  Assets:Cash   -50 USD
  Assets:Cash   -20 EUR
  Assets:Cash   -10 GBP

2024-01-03 * "the EUR group fails booking"
  Assets:Stock   -4 AA {5 USD}
  Assets:Cash    20 USD
  Assets:Stock   -3 BB {2 EUR}
  Assets:Stock  -30 BB {2 EUR}
  Assets:Cash    66 EUR

2024-01-04 * "the GBP group fails interpolation"
  Assets:Stock   -3 CC {1 GBP}
  Assets:Stock      CC {}
  Assets:Cash

2024-01-05 * "sell the rest"
  Assets:Stock   -6 AA {5 USD}
  Assets:Stock  -10 BB {2 EUR}
  Assets:Stock  -10 CC {1 GBP}
  Assets:Cash
`)
	assert.Equal(t, 2, len(errs), "errors: %v", errs)
	assert.Equal(t, "InsufficientInventoryError", kindOf(errs[0]), "got %v", errs[0])
	assert.Equal(t, "CurrencyGroupError", kindOf(errs[1]), "got %v", errs[1])
	assert.Equal(t, []string{"-4 AA {5 USD, 2024-01-02}", "20 USD {}"}, postings["the EUR group fails booking"])
	assert.Zero(t, postings["the GBP group fails interpolation"])
	assert.Equal(t, "()", b.inventory("Assets:Stock").String())
}

func TestBookingAddsAugmentationsOnceTheTransactionIsBooked(t *testing.T) {
	// Like beancount, a transaction's postings are booked against the lots
	// held before it: its augmentations are neither reduced by its later
	// postings nor matched by them, as bean-check 2.3.6 agrees.
	b, postings, errs := bookAll(t, `
2024-01-01 open Assets:Stock
2024-01-01 open Assets:Cash

2024-01-02 * "buy and sell different lots"
  Assets:Stock   10 HOOL {5 USD}
  Assets:Stock  -10 HOOL {6 USD}
  Assets:Cash    10 USD

2024-01-03 * "hold"
  Assets:Stock   10 AA {5 USD}
  Assets:Cash   -50 USD

2024-01-04 * "split: buy the new lot first, then sell the old one"
  Assets:Stock   20 AA {2.5 USD}
  Assets:Stock  -10 AA {}
  Assets:Cash
`)
	assert.Zero(t, errs)
	assert.Equal(t, []string{"20 AA {2.5 USD, 2024-01-04}", "-10 AA {5 USD, 2024-01-03}"},
		postings["split: buy the new lot first, then sell the old one"])
	assert.Equal(t, "(20 AA {2.5 USD, 2024-01-04}, 10 HOOL {5 USD, 2024-01-02}, -10 HOOL {6 USD, 2024-01-02})",
		b.inventory("Assets:Stock").String())
}

func TestImplicitPricesSkipPositionsThatReduceALot(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			// The probe of #482, which bean-query 2.3.6 agrees with. Under
			// NONE, a cost without a date names a lot dated by its own
			// transaction, so it opens a new lot and emits a price, while
			// naming a held lot, overselling it or buying back into it once
			// it is short reduces it and emits none. A STRICT augmentation
			// on a later date is a new lot too.
			name: "probe",
			source: `
option "operating_currency" "USD"
plugin "beancount.plugins.implicit_prices"

2024-01-01 open Assets:Strict "STRICT"
2024-01-01 open Assets:None "NONE"
2024-01-01 open Assets:Cash

2024-01-02 * "strict buy"
  Assets:Strict  10 XX {5 USD}
  Assets:Cash

2024-01-03 * "strict augment same lot"
  Assets:Strict  5 XX {5 USD}
  Assets:Cash

2024-02-01 * "none buy"
  Assets:None  10 YY {7 USD}
  Assets:Cash

2024-02-02 * "none sell same cost, new lot"
  Assets:None  -4 YY {7 USD}
  Assets:Cash

2024-02-03 * "none sell the held lot"
  Assets:None  -4 YY {7 USD, 2024-02-01}
  Assets:Cash

2024-02-04 * "none sell different cost, new lot"
  Assets:None  -4 YY {8 USD, 2024-02-01}
  Assets:Cash

2024-02-05 * "none oversell the held lot"
  Assets:None  -20 YY {7 USD, 2024-02-01}
  Assets:Cash

2024-02-06 * "none buy back into the short lot"
  Assets:None  3 YY {7 USD, 2024-02-01}
  Assets:Cash

2024-03-01 * "none short first"
  Assets:None  -10 ZZ {2 USD}
  Assets:Cash

2024-03-02 * "none cover the short lot"
  Assets:None  4 ZZ {2 USD, 2024-03-01}
  Assets:Cash

2024-03-03 * "none cover without lot date, new lot"
  Assets:None  4 ZZ {2 USD}
  Assets:Cash
`,
			want: []string{
				"2024-01-02 XX 5 USD",
				"2024-01-03 XX 5 USD",
				"2024-02-01 YY 7 USD",
				"2024-02-02 YY 7 USD",
				"2024-02-04 YY 8 USD",
				"2024-03-01 ZZ 2 USD",
				"2024-03-03 ZZ 2 USD",
			},
		},
		{
			// A zero-units posting leaves a lot of zero units, which
			// beancount never holds, so selling into it reduces nothing.
			name: "zero units lot",
			source: `
plugin "beancount.plugins.implicit_prices"

2024-01-01 open Assets:N "NONE"
2024-01-01 open Assets:Cash

2024-01-07 * "zero units none"
  Assets:N  0 PP {5 USD}
  Assets:Cash  0 USD

2024-01-08 * "sell into zero lot none"
  Assets:N  -5 PP {5 USD, 2024-01-07}
  Assets:Cash  25 USD
`,
			want: []string{
				"2024-01-07 PP 5 USD",
				"2024-01-08 PP 5 USD",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parser.MustParseString(context.Background(), tt.source)
			tree, _ = New().Process(context.Background(), tree)

			var prices []string
			for _, d := range tree.Directives {
				if price, ok := d.(*ast.Price); ok {
					prices = append(prices, fmt.Sprintf("%s %s %s %s", price.Date(), price.Commodity, price.Amount.Value, price.Amount.Currency))
				}
			}
			assert.Equal(t, tt.want, prices)
		})
	}
}

// Like beancount, an implicit price keeps the number as beancount holds it:
// a stated cost or price as written, a quotient at the exponent Python's
// division gives it. bean-query 2.3.6 prints these same numbers (#494).
func TestImplicitPricesKeepTheNumberAsBeancountHoldsIt(t *testing.T) {
	tree := parser.MustParseString(context.Background(), `
plugin "beancount.plugins.implicit_prices"

2024-01-01 open Assets:Cash
2024-01-01 open Assets:Stock
2024-01-01 open Assets:EUR

2024-04-01 * "cost"
  Assets:Stock  10 WW {5.0 USD}
  Assets:Cash

2024-04-02 * "cost with two trailing zeros"
  Assets:Stock  2 XX {100.00 USD}
  Assets:Cash

2024-04-03 * "total cost"
  Assets:Stock  4 YY {{50.0 USD}}
  Assets:Cash

2024-04-04 * "total cost with two trailing zeros"
  Assets:Stock  4 YY {{50.00 USD}}
  Assets:Cash

2024-04-05 * "compound cost"
  Assets:Stock  4 VV {2.50 # 1.0 USD}
  Assets:Cash

2024-04-06 * "price"
  Assets:EUR  -100 EUR @ 1.50 USD
  Assets:Cash

2024-04-07 * "total price"
  Assets:EUR  100 EUR @@ 150.0 USD
  Assets:Cash

2024-04-08 * "the same price twice keeps the first spelling"
  Assets:Stock  1 WW {5 USD}
  Assets:Stock  1 WW {5.00 USD}
  Assets:Cash
`)
	tree, _ = New().Process(context.Background(), tree)

	var prices []string
	for _, d := range tree.Directives {
		if price, ok := d.(*ast.Price); ok {
			prices = append(prices, fmt.Sprintf("%s %s %s %s", price.Date(), price.Commodity, price.Amount.Value, price.Amount.Currency))
		}
	}
	assert.Equal(t, []string{
		"2024-04-01 WW 5.0 USD",
		"2024-04-02 XX 100.00 USD",
		"2024-04-03 YY 12.5 USD",
		"2024-04-04 YY 12.50 USD",
		"2024-04-05 VV 2.75 USD",
		"2024-04-06 EUR 1.50 USD",
		"2024-04-07 EUR 1.5 USD",
		"2024-04-08 WW 5 USD",
	}, prices)
}

func TestValidateAmounts(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	tests := []struct {
		name         string
		txn          *ast.Transaction
		wantErrCount int
	}{
		{
			name: "valid amounts",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "missing amount - not an error at this stage",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking), // Missing amount
				),
			),
			wantErrCount: 0, // Missing amounts are inferred, not validation errors
		},
		{
			name: "valid decimal amounts",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("123.456789", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-123.456789", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "valid negative amount",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("-1000.00", "USD")),
				),
			),
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateAmounts(tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))
		})
	}
}

func TestCalculateBalance(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")
	income, _ := ast.NewAccount("Income:Salary")

	tests := []struct {
		name          string
		txn           *ast.Transaction
		wantBalanced  bool
		wantResiduals map[string]string
		wantInferred  int // Number of inferred amounts
	}{
		{
			name: "simple balanced transaction",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantBalanced:  true,
			wantResiduals: map[string]string{},
			wantInferred:  0,
		},
		{
			name: "unbalanced transaction",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-40.00", "USD")),
				),
			),
			wantBalanced:  false,
			wantResiduals: map[string]string{}, // Will have residual but checking exact value is tricky
			wantInferred:  0,
		},
		{
			name: "inferred amount - one posting missing",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking), // Amount will be inferred
				),
			),
			wantBalanced: true,
			wantInferred: 1,
		},
		{
			name: "inferred amount counted once in tolerance calculation",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("33.33", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("33.33", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("33.34", "USD")),
					ast.NewPosting(checking), // Will be inferred as -100.00
				),
			),
			wantBalanced: true,
			wantInferred: 1,
		},
		{
			name: "multi-currency balanced",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("30.00", "EUR")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-30.00", "EUR")),
				),
			),
			wantBalanced: true,
		},
		{
			name: "three-way split",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("30.00", "USD")),
					ast.NewPosting(income, ast.WithAmount("20.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantBalanced: true,
		},
		{
			name: "within inferred tolerance balanced",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.001", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.0005", "USD")),
				),
			),
			// amounts at -3 and -4 decimals, coarsest is -3
			// tolerance = 10^-3 * 0.5 = 0.0005
			// diff = 0.0005, which is not greater than the tolerance
			// (verified against bean-check 2.3.6)
			wantBalanced: true,
		},
		{
			name: "exactly within inferred tolerance",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.0001", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.0000", "USD")),
				),
			),
			// amounts at -4 decimals, tolerance = 10^-4 * 0.5 = 0.00005
			// diff = 0.0001, which is > 0.00005, so NOT balanced
			wantBalanced: false,
			wantResiduals: map[string]string{
				"USD": "0.0001",
			},
		},
		{
			name: "high precision - balanced",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("10.22626", "RGAGX")),
					ast.NewPosting(checking, ast.WithAmount("-10.22626", "RGAGX")),
				),
			),
			wantBalanced: true, // Exact match
		},
		{
			name: "high precision - outside inferred tolerance",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("10.22626", "RGAGX")),
					ast.NewPosting(checking, ast.WithAmount("-10.22625", "RGAGX")),
				),
			),
			// Diff = 0.00001, tolerance = 10^-5 * 0.5 = 0.000005
			// 0.00001 > 0.000005, so this should NOT balance
			wantBalanced: false,
			wantResiduals: map[string]string{
				"RGAGX": "0.00001",
			},
		},
		{
			name: "high precision - also outside inferred tolerance",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("10.226260", "RGAGX")),
					ast.NewPosting(checking, ast.WithAmount("-10.226256", "RGAGX")),
				),
			),
			// amounts at -6 exponent, tolerance = 10^-6 * 0.5 = 0.0000005
			// Diff = 0.000004, which is > 0.0000005
			wantBalanced: false,
			wantResiduals: map[string]string{
				"RGAGX": "0.000004",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBooker(NewConfig(), newTolerances(nil), nil)
			delta, validation, _, errs := b.calculateBalance(tt.txn, currencyGroup{postings: tt.txn.Postings}, nil, b.tolerances.spec(tt.txn.Postings))

			assert.Equal(t, 0, len(errs))

			assert.Equal(t, tt.wantBalanced, validation.isBalanced)

			inferredCount := len(delta.InferredAmounts)
			assert.Equal(t, tt.wantInferred, inferredCount)
			for posting := range delta.InferredAmounts {
				assert.False(t, posting.Inferred, "validation must not mutate postings")
				assert.True(t, posting.Amount == nil, "validation must leave inferred amount unapplied")
			}

			// Check residuals if specified
			for currency, expected := range tt.wantResiduals {
				// Convert decimal.Decimal to string for comparison
				expectedStr := expected
				got, exists := validation.residuals[currency]
				assert.True(t, exists)
				assert.Equal(t, expectedStr, got.String())
			}
		})
	}
}

func TestValidateCosts(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	stock, _ := ast.NewAccount("Assets:Investments:Stock")

	tests := []struct {
		name         string
		txn          *ast.Transaction
		wantErrCount int
		wantErrType  string
	}{
		{
			name: "valid explicit cost",
			txn: ast.NewTransaction(date, "Buy stock",
				ast.WithPostings(
					ast.NewPosting(stock, ast.WithAmount("10", "HOOL"), ast.WithCost(ast.NewCost(ast.NewAmount("500.00", "USD")))),
					ast.NewPosting(checking, ast.WithAmount("-5000.00", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "valid empty cost",
			txn: ast.NewTransaction(date, "Sell stock",
				ast.WithPostings(
					ast.NewPosting(stock, ast.WithAmount("-10", "HOOL"), ast.WithCost(ast.NewEmptyCost())),
					ast.NewPosting(checking, ast.WithAmount("5500.00", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "no cost specs - valid",
			txn: ast.NewTransaction(date, "Regular transaction",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "valid cost label",
			txn: ast.NewTransaction(date, "Buy stock with label",
				ast.WithPostings(
					ast.NewPosting(stock, ast.WithAmount("10", "HOOL"), ast.WithCost(ast.NewCostWithLabel(ast.NewAmount("500.00", "USD"), nil, "lot-1"))),
					ast.NewPosting(checking, ast.WithAmount("-5000.00", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "whitespace-only cost label rejected",
			txn: ast.NewTransaction(date, "Buy stock with bad label",
				ast.WithPostings(
					ast.NewPosting(stock, ast.WithAmount("10", "HOOL"), ast.WithCost(ast.NewCostWithLabel(ast.NewAmount("500.00", "USD"), nil, "   "))),
					ast.NewPosting(checking, ast.WithAmount("-5000.00", "USD")),
				),
			),
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateCosts(tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))
		})
	}
}

func TestValidatePrices(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Foreign")

	tests := []struct {
		name         string
		txn          *ast.Transaction
		wantErrCount int
	}{
		{
			name: "valid per-unit price",
			txn: ast.NewTransaction(date, "Foreign expense",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("100", "EUR"), ast.WithPrice(ast.NewAmount("1.20", "USD"))),
					ast.NewPosting(checking, ast.WithAmount("-120", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "no price specs - valid",
			txn: ast.NewTransaction(date, "Regular transaction",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
				),
			),
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validatePrices(tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))
		})
	}
}

func BenchmarkValidateCosts(b *testing.B) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	stock, _ := ast.NewAccount("Assets:Investments:Stock")

	txn := ast.NewTransaction(date, "Buy stock",
		ast.WithPostings(
			ast.NewPosting(stock, ast.WithAmount("10", "HOOL"), ast.WithCost(ast.NewCost(ast.NewAmount("500.00", "USD")))),
			ast.NewPosting(checking, ast.WithAmount("-5000.00", "USD")),
		),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validateCosts(txn)
	}
}

func BenchmarkValidatePrices(b *testing.B) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Foreign")

	txn := ast.NewTransaction(date, "Foreign expense",
		ast.WithPostings(
			ast.NewPosting(expenses, ast.WithAmount("100", "EUR"), ast.WithPrice(ast.NewAmount("1.20", "USD"))),
			ast.NewPosting(checking, ast.WithAmount("-120", "USD")),
		),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validatePrices(txn)
	}
}

func TestTransactionAddedAfterBookingIsReportedAndNotApplied(t *testing.T) {
	// No Built-in Plugin adds a transaction, so a test Plugin simulates one
	// that does. Booking has already run, so the transaction has no booking
	// result: it is reported, naming it, and left out of the balances.
	const name = "test.add_transaction"
	var added *ast.Transaction
	pluginRegistry[name] = func(ctx context.Context, l *Ledger, tree *ast.AST) []error {
		date, _ := ast.NewDate("2020-01-02")
		added = ast.NewTransaction(date, "added by a plugin", ast.WithPostings(
			ast.NewPosting("Assets:Cash", ast.WithAmount("10", "USD")),
			ast.NewPosting("Equity:Open", ast.WithAmount("-10", "USD")),
		))
		tree.Directives = append(tree.Directives, added)
		_ = ast.SortDirectives(tree)
		return nil
	}
	t.Cleanup(func() { delete(pluginRegistry, name) })

	source := `
plugin "test.add_transaction"

2020-01-01 open Assets:Cash
2020-01-01 open Equity:Open
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_, _ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 1, len(errs), "errors: %v", errs)
	assert.Equal(t, "UnbookedTransactionError", kindOf(errs[0]))
	var diagnostic *Diagnostic
	assert.True(t, errors.As(errs[0], &diagnostic))
	assert.Equal(t, ast.Directive(added), diagnostic.GetDirective())
	assert.Equal(t, "2020-01-02: Transaction was not booked: it was added after Booking", errs[0].Error())

	cash, ok := l.GetAccount("Assets:Cash")
	assert.True(t, ok)
	assert.Equal(t, 0, len(cash.Postings))
	assert.True(t, cash.Inventory.IsEmpty())
}
