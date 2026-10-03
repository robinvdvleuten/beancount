package ledger_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

var balanceSheet = []ast.AccountType{ast.AccountTypeAssets, ast.AccountTypeLiabilities, ast.AccountTypeEquity}

func loadLedger(t *testing.T, source string) *ledger.Ledger {
	t.Helper()
	ctx := context.Background()
	tree, err := parser.ParseString(ctx, source)
	assert.NoError(t, err)
	l := ledger.New()
	l.MustProcess(ctx, tree)
	return l
}

// equityBalances returns the balances accountBalances finds under prefix.
func equityBalances(tree *ledger.BalanceTree, prefix string) map[string]map[string]string {
	balances := map[string]map[string]string{}
	for account, balance := range accountBalances(tree) {
		if strings.HasPrefix(account, prefix+":") {
			balances[account] = balance
		}
	}
	return balances
}

// rootsTotal sums tree's roots per currency, leaving out a currency that
// sums to zero.
func rootsTotal(tree *ledger.BalanceTree) map[string]string {
	sums := map[string]decimal.Decimal{}
	for _, root := range tree.Roots {
		for _, entry := range root.Balance.Entries() {
			sums[entry.Currency] = pydecimal.Add(sums[entry.Currency], entry.Amount)
		}
	}
	total := map[string]string{}
	for currency, sum := range sums {
		if !sum.IsZero() {
			total[currency] = sum.String()
		}
	}
	return total
}

// The expected Equity comes from bean-query on testdata/closed.beancount,
// where CLOSE ON excludes its own date and As of includes it:
//
//	select account, sum(position) from close on <asOf + 1 day> clear
//	  where account ~ '^Equity' group by account
func TestClosedBalances(t *testing.T) {
	source, err := os.ReadFile("testdata/closed.beancount")
	assert.NoError(t, err)
	l := loadLedger(t, string(source))

	for _, test := range []struct {
		name   string
		asOf   string
		equity map[string]map[string]string
	}{
		{
			name: "NoConversions",
			asOf: "2024-01-31",
			equity: map[string]map[string]string{
				"Equity:Opening":          {"USD": "-1000"},
				"Equity:Earnings:Current": {"USD": "-1954.5"},
			},
		},
		{
			name: "MidLedgerWithAConversion",
			asOf: "2024-02-16",
			equity: map[string]map[string]string{
				"Equity:Opening":             {"USD": "-1000"},
				"Equity:Conversions:Current": {"EUR": "-500", "USD": "550"},
				"Equity:Earnings:Current":    {"EUR": "20", "USD": "-1954.5"},
			},
		},
		{
			// Equity:Earnings:Current holds 100 USD of its own.
			name: "MergesIntoARealAccount",
			asOf: "2024-03-31",
			equity: map[string]map[string]string{
				"Equity:Opening":             {"USD": "-1100"},
				"Equity:Conversions:Current": {"EUR": "-500", "USD": "550"},
				"Equity:Earnings:Current":    {"EUR": "20", "USD": "-3854.5"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tree, err := l.GetBalanceTree(balanceSheet, nil, mustDate(t, test.asOf), ledger.ValuationAtCost, true)
			assert.NoError(t, err)
			assert.Equal(t, test.equity, equityBalances(tree, "Equity"))
			assert.Equal(t, map[string]string{}, rootsTotal(tree))
		})
	}

	t.Run("UnrealizedAtMarket", func(t *testing.T) {
		// select value(sum(position)) from close clear
		//   where account ~ '^(Assets|Liabilities|Equity)'
		// is 100.00 USD, HOOL's gain.
		tree, err := l.GetBalanceTree(balanceSheet, nil, mustDate(t, "2024-03-31"), ledger.ValuationAtMarket, true)
		assert.NoError(t, err)
		assert.Equal(t, map[string]string{"USD": "-100"}, accountBalances(tree)["Equity:Earnings:Unrealized"])
		assert.Equal(t, map[string]string{}, rootsTotal(tree))
	})

	t.Run("OnlyEquity", func(t *testing.T) {
		tree, err := l.GetBalanceTree([]ast.AccountType{ast.AccountTypeEquity}, nil, mustDate(t, "2024-03-31"), ledger.ValuationAtMarket, true)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(tree.Roots))
		assert.Equal(t, map[string]string{"USD": "-100"}, accountBalances(tree)["Equity:Earnings:Unrealized"])
	})
}

func TestClosedBalancesUseTheAccountOptions(t *testing.T) {
	// select account, sum(position) from close clear
	//   where account ~ '^Capital' group by account
	l := loadLedger(t, `option "name_equity" "Capital"
option "account_current_earnings" "Retained"
option "account_current_conversions" "Exchange"
2024-01-01 open Assets:Checking
2024-01-01 open Assets:Euro
2024-01-01 open Income:Salary

2024-01-15 * "salary"
  Assets:Checking  2000.00 USD
  Income:Salary

2024-02-01 * "to euros"
  Assets:Euro       500.00 EUR @ 1.10 USD
  Assets:Checking  -550.00 USD
`)
	tree, err := l.GetBalanceTree(balanceSheet, nil, nil, ledger.ValuationAtCost, true)
	assert.NoError(t, err)
	assert.Equal(t, map[string]map[string]string{
		"Capital:Exchange": {"EUR": "-500", "USD": "550"},
		"Capital:Retained": {"USD": "-2000"},
	}, equityBalances(tree, "Capital"))
	assert.Equal(t, "Capital", tree.Roots[len(tree.Roots)-1].Name)
}

// The expected Unrealized gains are what bean-query's
//
//	select <expr> from close on 2024-02-01 clear
//	  where account ~ '^(Assets|Liabilities|Equity)'
//
// leaves over on testdata/valuation.beancount, negated, with <expr>
// value(sum(position), 2024-01-31) or convert(sum(position), '<X>',
// 2024-01-31). The unpriced 3 NOP {10 USD} shows up in it. Converted to
// EUR, the 28-digit roundings differ in the last digits, since bean-query
// sums before it converts, so the numbers are compared to 20 places.
func TestClosedBalancesUnrealizedGains(t *testing.T) {
	l := loadValuationLedger(t)
	asOf := mustDate(t, "2024-01-31")
	for _, test := range []struct {
		valuation  ledger.Valuation
		unrealized map[string]string
	}{
		{ledger.ValuationAtCost, nil},
		{ledger.ValuationUnits, nil},
		{ledger.ValuationAtMarket, map[string]string{"EUR": "-10", "NOP": "-3", "USD": "19"}},
		{ledger.ValuationConvertedTo("USD"), map[string]string{"NOP": "-3", "USD": "8"}},
		{ledger.ValuationConvertedTo("EUR"), map[string]string{"EUR": "7.27272727272727272727", "NOP": "-3"}},
	} {
		t.Run(test.valuation.String(), func(t *testing.T) {
			tree, err := l.GetBalanceTree(balanceSheet, nil, asOf, test.valuation, true)
			assert.NoError(t, err)
			assert.Equal(t, test.unrealized, rounded(accountBalances(tree)["Equity:Earnings:Unrealized"]))
			if test.valuation != ledger.ValuationUnits {
				assert.Equal(t, map[string]string{}, rootsTotal(tree))
			}
		})
	}
}

// rounded rounds each amount of balance to 20 places.
func rounded(balance map[string]string) map[string]string {
	if balance == nil {
		return nil
	}
	out := make(map[string]string, len(balance))
	for currency, amount := range balance {
		out[currency] = decimal.RequireFromString(amount).Round(20).String()
	}
	return out
}

func TestClosedBalancesRejectAPeriodOrIncomeAndExpenses(t *testing.T) {
	l := loadValuationLedger(t)
	for name, test := range map[string]struct {
		types []ast.AccountType
		start *ast.Date
	}{
		"Period":   {balanceSheet, mustDate(t, "2024-01-01")},
		"Income":   {[]ast.AccountType{ast.AccountTypeAssets, ast.AccountTypeIncome}, nil},
		"Expenses": {[]ast.AccountType{ast.AccountTypeExpenses}, nil},
		"AllTypes": {nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := l.GetBalanceTree(test.types, test.start, nil, ledger.ValuationAtCost, true)
			assert.Error(t, err)
		})
	}
}
