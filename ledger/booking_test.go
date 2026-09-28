package ledger

import (
	"context"
	"fmt"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
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

func TestBookingRecordsBookedPositions(t *testing.T) {
	// Each booked posting records the positions it holds: its own for an
	// augmentation, per unit and dated, one per lot for a reduction, the
	// position of its own spec under NONE, and its units alone without cost.
	source := `
2024-01-01 open Assets:Invest
2024-01-01 open Assets:Strict "STRICT"
2024-01-01 open Assets:None "NONE"
2024-01-01 open Assets:Cash

2024-03-01 * "one of each"
  Assets:Invest  10 AA {5.0 USD, 2024-01-01}
  Assets:Invest  4 BB {{20 USD}}
  Assets:Invest  2 CC {3 # 4 USD}
  Assets:Invest  1 DD {7 USD, "lbl"}
  Assets:Strict  -15 XX {}
  Assets:None    -4 YY {7 USD, 2024-01-01}
  Assets:None    -4 YY {7 USD}
  Assets:Cash    49.00 USD
`
	tree := parser.MustParseString(context.Background(), source)
	b := newBooker(NewConfig(), tree.Directives)
	usd := func(n int64, date string) *lotSpec {
		cost := decimal.NewFromInt(n)
		return &lotSpec{Cost: &cost, CostCurrency: "USD", Date: newTestDate(date)}
	}
	b.inventory("Assets:Strict").AddLot("XX", decimal.NewFromInt(10), usd(5, "2024-01-01"))
	b.inventory("Assets:Strict").AddLot("XX", decimal.NewFromInt(5), usd(6, "2024-01-02"))
	b.inventory("Assets:None").AddLot("YY", decimal.NewFromInt(10), usd(7, "2024-01-01"))

	txn := tree.Directives[len(tree.Directives)-1].(*ast.Transaction)
	booked, errs := b.book(txn)
	assert.Zero(t, errs)
	assert.Zero(t, booked.residuals)

	got := make([][]BookedPosition, 0, len(booked.postings))
	for _, bp := range booked.postings {
		got = append(got, bp.positions)
	}
	at := func(number, date, label string) *BookedCost {
		return &BookedCost{Number: mustParseDec(number), Currency: "USD", Date: newTestDate(date), Label: label}
	}
	assert.Equal(t, [][]BookedPosition{
		{{Units: mustParseDec("10"), Cost: at("5.0", "2024-01-01", "")}},
		{{Units: mustParseDec("4"), Cost: at("5", "2024-03-01", "")}},
		{{Units: mustParseDec("2"), Cost: at("5", "2024-03-01", "")}},
		{{Units: mustParseDec("1"), Cost: at("7", "2024-03-01", "lbl")}},
		{
			{Units: mustParseDec("-10"), Cost: at("5", "2024-01-01", ""), Reduced: true},
			{Units: mustParseDec("-5"), Cost: at("6", "2024-01-02", ""), Reduced: true},
		},
		{{Units: mustParseDec("-4"), Cost: at("7", "2024-01-01", ""), Reduced: true}},
		{{Units: mustParseDec("-4"), Cost: at("7", "2024-03-01", "")}},
		{{Units: mustParseDec("49.00")}},
	}, got)
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
			_ = New().Process(context.Background(), tree)

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
			b := newBooker(NewConfig(), nil)
			scratch := &scratchInventories{booker: b, own: make(map[string]*Inventory)}
			delta, validation, _, errs := b.calculateBalance(tt.txn, currencyGroup{postings: tt.txn.Postings}, nil, scratch)

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
