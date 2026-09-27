package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

func TestClassifyPostings(t *testing.T) {
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")
	stocks, _ := ast.NewAccount("Assets:Stocks")

	tests := []struct {
		name                 string
		postings             []*ast.Posting
		wantWithAmounts      int
		wantWithoutAmounts   int
		wantWithEmptyCosts   int
		wantWithExplicitCost int
	}{
		{
			name: "all postings have amounts",
			postings: []*ast.Posting{
				ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
				ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   0,
			wantWithEmptyCosts:   0,
			wantWithExplicitCost: 0,
		},
		{
			name: "one posting without amount",
			postings: []*ast.Posting{
				ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
				ast.NewPosting(checking),
			},
			wantWithAmounts:      1,
			wantWithoutAmounts:   1,
			wantWithEmptyCosts:   0,
			wantWithExplicitCost: 0,
		},
		{
			name: "posting with empty cost",
			postings: []*ast.Posting{
				ast.NewPosting(stocks,
					ast.WithAmount("10", "HOOL"),
					ast.WithCost(ast.NewEmptyCost()),
				),
				ast.NewPosting(checking, ast.WithAmount("-5000", "USD")),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   0,
			wantWithEmptyCosts:   1,
			wantWithExplicitCost: 0,
		},
		{
			name: "posting with explicit cost",
			postings: []*ast.Posting{
				ast.NewPosting(stocks,
					ast.WithAmount("10", "HOOL"),
					ast.WithCost(ast.NewCost(ast.NewAmount("500", "USD"))),
				),
				ast.NewPosting(checking, ast.WithAmount("-5000", "USD")),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   0,
			wantWithEmptyCosts:   0,
			wantWithExplicitCost: 1,
		},
		{
			name: "mixed posting types",
			postings: []*ast.Posting{
				ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
				ast.NewPosting(stocks,
					ast.WithAmount("5", "HOOL"),
					ast.WithCost(ast.NewEmptyCost()),
				),
				ast.NewPosting(checking),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   1,
			wantWithEmptyCosts:   1,
			wantWithExplicitCost: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := classifyPostings(tt.postings)

			assert.Equal(t, tt.wantWithAmounts, len(pc.withAmounts))

			assert.Equal(t, tt.wantWithoutAmounts, len(pc.withoutAmounts))

			assert.Equal(t, tt.wantWithEmptyCosts, len(pc.withEmptyCosts))

			assert.Equal(t, tt.wantWithExplicitCost, len(pc.withExplicitCost))
		})
	}
}

func BenchmarkClassifyPostings(b *testing.B) {
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	postings := []*ast.Posting{
		ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
		ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifyPostings(postings)
	}
}

func TestRoundInterpolated(t *testing.T) {
	// Expected values follow beancount's quantize_with_tolerance, computed
	// with Python decimal for a transaction stating 10.00 USD: the step is
	// twice the tolerance, and a step of five or more significant digits
	// leaves the number unrounded.
	stated := map[string][]decimal.Decimal{"USD": {decimal.RequireFromString("10.00")}}
	number := decimal.RequireFromString("-6.666666666")
	for multiplier, want := range map[string]string{
		"0.5":     "-6.67",
		"1.1":     "-6.667",
		"0.3333":  "-6.666667",
		"0.33333": "-6.666666666",
	} {
		b := newBooker(NewConfig(), nil)
		b.config.Tolerance.Multiplier = decimal.RequireFromString(multiplier)
		assert.Equal(t, want, formatInferredNumber(roundInterpolated(number, b.transactionTolerance("USD", stated["USD"], nil))), multiplier)
	}

	// Without a stated amount or default there is nothing to round to; a
	// configured default gives a step even for whole-number transactions.
	b := newBooker(NewConfig(), nil)
	assert.Equal(t, "-6.666666666", formatInferredNumber(roundInterpolated(number, b.transactionTolerance("EUR", stated["EUR"], nil))))
	b.config.Tolerance.Defaults["EUR"] = decimal.RequireFromString("0.005")
	assert.Equal(t, "-6.67", formatInferredNumber(roundInterpolated(number, b.transactionTolerance("EUR", stated["EUR"], nil))))
}

func TestCostTolerances(t *testing.T) {
	// Beancount's infer_tolerances docstring example: two postings of
	// 18.572 units at 30.96 USD each add 0.0005 x 30.96 = 0.01548 to USD.
	d := decimal.RequireFromString
	shares := []toleranceShare{
		{units: d("18.572"), hasCost: true, costNumbers: []decimal.Decimal{d("30.96")}, costCurrency: "USD"},
		{units: d("18.572"), hasCost: true, costNumbers: []decimal.Decimal{d("30.96")}, costCurrency: "USD"},
		{units: d("1.5"), hasCost: true, costNumbers: []decimal.Decimal{d("1000.00")}, costCurrency: "EUR"},
		{units: d("2.25"), price: &priceAmount{number: d("4"), currency: "GBP"}},
		{units: d("10"), hasCost: true, costNumbers: []decimal.Decimal{d("99")}, costCurrency: "CHF"},
		{units: d("1.5"), hasCost: true, costCurrency: "JPY"},
	}

	b := newBooker(NewConfig(), nil)
	assert.Zero(t, b.costTolerances(shares), "off unless infer_tolerance_from_cost is set")

	b.config.Tolerance.InferFromCost = true
	got := b.costTolerances(shares)
	assert.Equal(t, "0.03096", got["USD"].String())
	assert.Equal(t, "0.5", got["EUR"].String(), "one posting adds at most 0.5")
	assert.Equal(t, "0.02", got["GBP"].String(), "prices widen their price currency")
	_, ok := got["CHF"]
	assert.False(t, ok, "whole-number units add nothing")
	assert.Equal(t, "0.5", got["JPY"].String(), "a cost without numbers adds the cap")
}
