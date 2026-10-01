package ledger_test

import (
	"context"
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func loadValuationLedger(t *testing.T) *ledger.Ledger {
	t.Helper()
	source, err := os.ReadFile("testdata/valuation.beancount")
	assert.NoError(t, err)
	ctx := context.Background()
	tree, err := parser.ParseBytes(ctx, source)
	assert.NoError(t, err)
	l := ledger.New()
	assert.NoError(t, l.Process(ctx, tree))
	return l
}

// accountBalances returns each account's balance in tree, keyed by account
// name, its numbers normalized so that 127.00 and 127 compare equal.
func accountBalances(tree *ledger.BalanceTree) map[string]map[string]string {
	balances := map[string]map[string]string{}
	var walk func(node *ledger.BalanceNode)
	walk = func(node *ledger.BalanceNode) {
		if node.Account != "" && len(node.Children) == 0 {
			amounts := map[string]string{}
			for _, entry := range node.Balance.Entries() {
				amounts[entry.Currency] = entry.Amount.String()
			}
			balances[node.Account] = amounts
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range tree.Roots {
		walk(root)
	}
	return balances
}

func mustDate(t *testing.T, s string) *ast.Date {
	t.Helper()
	date, err := ast.NewDate(s)
	assert.NoError(t, err)
	return date
}

// The expected balances come from bean-query on testdata/valuation.beancount,
// with <expr> one of cost(sum(position)), value(sum(position), 2024-01-31),
// convert(sum(position), 'USD', 2024-01-31) and
// convert(sum(position), 'EUR', 2024-01-31); the exact numbers from
// number(only('<currency>', <expr>)):
//
//	select account, <expr> where date <= 2024-01-31 group by account
func TestGetBalanceTreeValuations(t *testing.T) {
	l := loadValuationLedger(t)
	asOf := mustDate(t, "2024-01-31")

	tests := []struct {
		name      string
		valuation ledger.Valuation
		want      map[string]map[string]string
	}{
		{
			name:      "Units",
			valuation: ledger.ValuationUnits,
			want: map[string]map[string]string{
				"Assets:Broker":    {"HOOL": "6", "NOP": "3", "EHOOL": "2"},
				"Assets:Cash":      {"EUR": "90", "CHF": "50", "XYZ": "7"},
				"Equity:Opening":   {"USD": "-80", "EUR": "-140", "CHF": "-50", "XYZ": "-7"},
				"Income:Dividends": {"USD": "-11"},
				"Expenses:Fees":    {"EUR": "10"},
			},
		},
		{
			// A holding without cost (100 EUR) keeps its units.
			name:      "AtCost",
			valuation: ledger.ValuationAtCost,
			want: map[string]map[string]string{
				"Assets:Broker":    {"USD": "91", "EUR": "40"},
				"Assets:Cash":      {"EUR": "90", "CHF": "50", "XYZ": "7"},
				"Equity:Opening":   {"USD": "-80", "EUR": "-140", "CHF": "-50", "XYZ": "-7"},
				"Income:Dividends": {"USD": "-11"},
				"Expenses:Fees":    {"EUR": "10"},
			},
		},
		{
			// HOOL at 12 USD, not the 15 USD dated after the valuation
			// date; NOP has no price, so it keeps its units.
			name:      "AtMarket",
			valuation: ledger.ValuationAtMarket,
			want: map[string]map[string]string{
				"Assets:Broker":    {"USD": "72", "NOP": "3", "EUR": "50"},
				"Assets:Cash":      {"EUR": "90", "CHF": "50", "XYZ": "7"},
				"Equity:Opening":   {"USD": "-80", "EUR": "-140", "CHF": "-50", "XYZ": "-7"},
				"Income:Dividends": {"USD": "-11"},
				"Expenses:Fees":    {"EUR": "10"},
			},
		},
		{
			// EUR through its direct price, CHF through the inverse of
			// USD's, EHOOL through its cost currency EUR; NOP and XYZ have
			// no path to USD and keep their units.
			name:      "ConvertedToUSD",
			valuation: ledger.ValuationConvertedTo("USD"),
			want: map[string]map[string]string{
				"Assets:Broker":    {"USD": "127", "NOP": "3"},
				"Assets:Cash":      {"USD": "154.5555555555555555555555556", "XYZ": "7"},
				"Equity:Opening":   {"USD": "-289.5555555555555555555555556", "XYZ": "-7"},
				"Income:Dividends": {"USD": "-11"},
				"Expenses:Fees":    {"USD": "11"},
			},
		},
		{
			name:      "ConvertedToEUR",
			valuation: ledger.ValuationConvertedTo("EUR"),
			want: map[string]map[string]string{
				"Assets:Broker":    {"EUR": "115.4545454545454545454545455", "NOP": "3"},
				"Assets:Cash":      {"EUR": "90", "CHF": "50", "XYZ": "7"},
				"Equity:Opening":   {"EUR": "-212.7272727272727272727272727", "CHF": "-50", "XYZ": "-7"},
				"Income:Dividends": {"EUR": "-10"},
				"Expenses:Fees":    {"EUR": "10"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree, err := l.GetBalanceTree(nil, nil, asOf, test.valuation)
			assert.NoError(t, err)
			assert.Equal(t, test.want, accountBalances(tree))
		})
	}
}

func TestGetBalanceTreeValuesAPeriodAtItsEnd(t *testing.T) {
	// select account, convert(sum(position), 'EUR', 2024-01-31)
	//   where date >= 2024-01-05 and date <= 2024-01-31 group by account
	l := loadValuationLedger(t)
	tree, err := l.GetBalanceTree(
		[]ast.AccountType{ast.AccountTypeIncome, ast.AccountTypeExpenses},
		mustDate(t, "2024-01-05"), mustDate(t, "2024-01-31"),
		ledger.ValuationConvertedTo("EUR"),
	)
	assert.NoError(t, err)
	assert.Equal(t, map[string]map[string]string{
		"Income:Dividends": {"EUR": "-10"},
		"Expenses:Fees":    {"EUR": "10"},
	}, accountBalances(tree))
	assert.Equal(t, []string{"EUR"}, tree.Currencies)
}

func TestGetBalanceTreeAtCostListsTheValuedCurrencies(t *testing.T) {
	l := loadValuationLedger(t)
	tree, err := l.GetBalanceTree([]ast.AccountType{ast.AccountTypeAssets}, nil, mustDate(t, "2024-01-31"), ledger.ValuationAtCost)
	assert.NoError(t, err)
	assert.Equal(t, []string{"CHF", "EUR", "USD", "XYZ"}, tree.Currencies)
}

func TestParseValuation(t *testing.T) {
	for input, want := range map[string]ledger.Valuation{
		"units":  ledger.ValuationUnits,
		"cost":   ledger.ValuationAtCost,
		"market": ledger.ValuationAtMarket,
		"USD":    ledger.ValuationConvertedTo("USD"),
		"VBMPX":  ledger.ValuationConvertedTo("VBMPX"),
	} {
		got, err := ledger.ParseValuation(input)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, input, got.String())
	}
	for _, input := range []string{"bogus", "usd", "Cost", "U$D", ""} {
		_, err := ledger.ParseValuation(input)
		assert.Error(t, err, input)
	}
}

func TestPositionValuation(t *testing.T) {
	l := loadValuationLedger(t)
	date := mustDate(t, "2024-01-31")
	cost := &ledger.BookedCost{Number: decimal.RequireFromString("10"), Currency: "USD"}
	hool := ledger.Position{Number: decimal.RequireFromString("2"), Currency: "HOOL", Cost: cost}

	assert.Equal(t, "20 USD", hool.AtCost().Amount.String()+" "+hool.AtCost().Currency)
	assert.Equal(t, "24", l.MarketValue(hool, date).Amount.String())
	assert.Equal(t, "USD", l.Convert(hool, "USD", date).Currency)
	// No price from NOP to USD: market value keeps its units.
	nop := ledger.Position{Number: decimal.RequireFromString("3"), Currency: "NOP", Cost: cost}
	assert.Equal(t, ledger.CurrencyAmount{Currency: "NOP", Amount: nop.Number}, l.MarketValue(nop, date))
}
