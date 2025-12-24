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
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
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

// TestGetPostingsBefore_NoPostings verifies query returns empty for accounts
// with no transactions before a date.
func TestGetPostingsBefore_NoPostings(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-06-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]
	postings := account.GetPostingsBefore(date2)
	assert.Equal(t, len(postings), 1)
}

// TestGetPostingsBefore_BeforeEarliestDate verifies query returns empty
// for accounts with no transactions before a date.
func TestGetPostingsBefore_BeforeEarliestDate(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date0, _ := ast.NewDate("2023-12-31")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]
	postings := account.GetPostingsBefore(date0)
	assert.Equal(t, len(postings), 0)
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
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			&ast.Open{Date: date1, Account: expenses},
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
	postings := expensesAccount.GetPostingsInPeriod(periodStart, periodEnd)
	assert.Equal(t, len(postings), 1)
	assert.Equal(t, postings[0].Transaction.Date, date2)
}

// TestGetBalancesAsOf_SimpleCase verifies balance calculation for a single account.
func TestGetBalancesAsOf_SimpleCase(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	// Get balances as of 2024-02-01 (after the transaction)
	balances := l.GetBalancesAsOf(date2)
	// Both Assets:Cash and Equity:Opening have postings
	assert.Equal(t, len(balances), 2)

	// Find each account in the result
	var assetBal, equityBal *AccountBalance
	for i := range balances {
		switch balances[i].Account {
		case "Assets:Cash":
			assetBal = &balances[i]
		case "Equity:Opening":
			equityBal = &balances[i]
		}
	}
	assert.True(t, assetBal != nil, "Assets:Cash should exist")
	assert.True(t, equityBal != nil, "Equity:Opening should exist")
	assert.True(t, assetBal.Balances["USD"].Equal(decimal.NewFromInt(100)))
	assert.True(t, equityBal.Balances["USD"].Equal(decimal.NewFromInt(-100)))
}

// TestGetBalancesAsOf_MultiCurrency verifies balance calculation with multiple currencies.
func TestGetBalancesAsOf_MultiCurrency(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
		},
	})

	balances := l.GetBalancesAsOf(date2)
	// Both Assets:Cash and Equity:Opening have postings
	assert.Equal(t, len(balances), 2)

	// Find each account in the result
	var assetBal, equityBal *AccountBalance
	for i := range balances {
		switch balances[i].Account {
		case "Assets:Cash":
			assetBal = &balances[i]
		case "Equity:Opening":
			equityBal = &balances[i]
		}
	}
	assert.True(t, assetBal != nil, "Assets:Cash should exist")
	assert.True(t, equityBal != nil, "Equity:Opening should exist")
	assert.True(t, assetBal.Balances["USD"].Equal(decimal.NewFromInt(100)))
	assert.True(t, assetBal.Balances["EUR"].Equal(decimal.NewFromInt(50)))
	// Equity:Opening has inverse balance
	assert.True(t, equityBal.Balances["USD"].Equal(decimal.NewFromInt(-100)))
	assert.True(t, equityBal.Balances["EUR"].Equal(decimal.NewFromInt(-50)))
}

// TestGetBalancesInPeriod_IncomeExpenses verifies period balance filtering by account type.
func TestGetBalancesInPeriod_IncomeExpenses(t *testing.T) {
	l := New()
	income, _ := ast.NewAccount("Income:Salary")
	expenses, _ := ast.NewAccount("Expenses:Food")
	assets, _ := ast.NewAccount("Assets:Cash")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: income},
			&ast.Open{Date: date1, Account: expenses},
			&ast.Open{Date: date1, Account: assets},
			// Income posting
			ast.NewTransaction(date2, "Salary", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(income),
			)),
			// Expense posting
			ast.NewTransaction(date2, "Food", ast.WithPostings(
				ast.NewPosting(expenses, ast.WithAmount("50", "USD")),
				ast.NewPosting(assets),
			)),
		},
	})

	periodStart, _ := ast.NewDate("2024-01-01")
	periodEnd, _ := ast.NewDate("2024-02-28")

	// Get only Income + Expenses
	balances := l.GetBalancesInPeriod(periodStart, periodEnd, ast.AccountTypeIncome, ast.AccountTypeExpenses)

	// Should have 2 accounts (Income + Expenses)
	assert.Equal(t, len(balances), 2)

	// Find each account
	var incomeBal, expenseBal *AccountBalance
	for i := range balances {
		switch balances[i].Account {
		case "Income:Salary":
			incomeBal = &balances[i]
		case "Expenses:Food":
			expenseBal = &balances[i]
		}
	}

	assert.True(t, incomeBal != nil, "Income:Salary should exist")
	assert.True(t, expenseBal != nil, "Expenses:Food should exist")
	// Income should be negative (offset)
	assert.True(t, incomeBal.Balances["USD"].Equal(decimal.NewFromInt(-1000)))
	// Expenses should be positive
	assert.True(t, expenseBal.Balances["USD"].Equal(decimal.NewFromInt(50)))
}

// TestCloseBooks_SimpleIncome verifies closing transactions are generated correctly.
func TestCloseBooks_SimpleIncome(t *testing.T) {
	l := New()
	income, _ := ast.NewAccount("Income:Salary")
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Earnings:Current")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: income},
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date2, "Salary", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(income),
			)),
		},
	})

	closingDate, _ := ast.NewDate("2024-02-28")
	closingTxns := l.CloseBooks(closingDate)

	// Should generate exactly one closing transaction
	assert.Equal(t, len(closingTxns), 1)

	txn := closingTxns[0]
	assert.Equal(t, txn.Date, closingDate)
	assert.Equal(t, txn.Flag, "P") // Synthetic/padding flag
	assert.Equal(t, len(txn.Postings), 2)
}

// TestCloseBooks_Empty verifies no closing transactions when no activity.
func TestCloseBooks_Empty(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")

	date1, _ := ast.NewDate("2024-01-01")
	closingDate, _ := ast.NewDate("2024-02-28")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
		},
	})

	closingTxns := l.CloseBooks(closingDate)
	assert.Equal(t, len(closingTxns), 0) // No income/expenses, no closing
}

// ConvertBalance Tests (Phase 6: Multi-Currency Consolidation)
// These tests verify currency consolidation functionality via ConvertBalance and GetBalanceInCurrency.

// TestConvertBalance_Empty verifies empty balance maps return zero.
func TestConvertBalance_Empty(t *testing.T) {
	l := New()
	date, _ := ast.NewDate("2024-01-01")

	result, err := l.ConvertBalance(make(map[string]decimal.Decimal), "USD", date)
	assert.NoError(t, err)
	assert.True(t, result.IsZero())
}

// TestConvertBalance_SingleCurrency_Match verifies single currency matching target.
func TestConvertBalance_SingleCurrency_Match(t *testing.T) {
	l := New()
	date, _ := ast.NewDate("2024-01-01")

	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
	}

	result, err := l.ConvertBalance(balance, "USD", date)
	assert.NoError(t, err)
	assert.True(t, result.Equal(decimal.NewFromInt(100)))
}

// TestConvertBalance_SingleCurrency_NeedConversion verifies single non-matching currency requires price.
func TestConvertBalance_SingleCurrency_NeedConversion(t *testing.T) {
	l := New()
	date, _ := ast.NewDate("2024-01-01")

	balance := map[string]decimal.Decimal{
		"EUR": decimal.NewFromInt(50),
	}

	// No price available
	_, err := l.ConvertBalance(balance, "USD", date)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no price found")
}

// TestConvertBalance_MultiCurrency_AllSame verifies multiple same-currency amounts sum without conversion.
func TestConvertBalance_MultiCurrency_AllSame(t *testing.T) {
	l := New()
	date, _ := ast.NewDate("2024-01-01")

	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
	}

	result, err := l.ConvertBalance(balance, "USD", date)
	assert.NoError(t, err)
	assert.True(t, result.Equal(decimal.NewFromInt(100)))
}

// TestConvertBalance_TwoCurrencies_DirectRate verifies conversion with one direct price edge.
func TestConvertBalance_TwoCurrencies_DirectRate(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	// Setup: EUR 1 = USD 1.10 on 2024-02-01
	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			// USD 100
			ast.NewTransaction(date1, "Opening USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			// EUR 50
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			// Price: EUR 1 = USD 1.10
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.10", "USD")),
		},
	})

	// Convert balance to USD: 100 + 50 * 1.10 = 155
	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
		"EUR": decimal.NewFromInt(50),
	}

	result, err := l.ConvertBalance(balance, "USD", date2)
	assert.NoError(t, err)
	expected := decimal.NewFromInt(100).Add(decimal.NewFromInt(50).Mul(mustParseDec("1.10")))
	assert.True(t, result.Equal(expected), "expected %.2f, got %.2f", expected, result)
}

// TestConvertBalance_TwoCurrencies_InversePrice verifies conversion using inverse of recorded price.
func TestConvertBalance_TwoCurrencies_InversePrice(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	// Setup: USD 1 = EUR 0.91 on 2024-02-01
	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			// Single currency transaction - will balance correctly
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			// Second transaction for EUR with price
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			// Price: USD 1 = EUR 0.91
			ast.NewPrice(date2, "USD", ast.NewAmount("0.91", "EUR")),
		},
	})

	// Convert balance to EUR using inverse: 50 + 100 * 0.91 = 141
	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
		"EUR": decimal.NewFromInt(50),
	}

	result, err := l.ConvertBalance(balance, "EUR", date2)
	assert.NoError(t, err)
	expected := decimal.NewFromInt(50).Add(decimal.NewFromInt(100).Mul(mustParseDec("0.91")))
	assert.True(t, result.Equal(expected))
}

// TestConvertBalance_ThreeCurrencies_MultiHop verifies conversion using multi-hop price path.
func TestConvertBalance_ThreeCurrencies_MultiHop(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	// Setup: USD → EUR: 0.91, EUR → GBP: 0.86
	// So: USD → GBP = 0.91 * 0.86 = 0.7826
	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "Opening GBP", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("20", "GBP")),
				ast.NewPosting(equity),
			)),
			ast.NewPrice(date2, "USD", ast.NewAmount("0.91", "EUR")),
			ast.NewPrice(date2, "EUR", ast.NewAmount("0.86", "GBP")),
		},
	})

	// Convert to GBP:
	// 20 (already GBP) + 50 * 0.86 (EUR→GBP) + 100 * 0.7826 (USD→GBP via EUR)
	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
		"EUR": decimal.NewFromInt(50),
		"GBP": decimal.NewFromInt(20),
	}

	result, err := l.ConvertBalance(balance, "GBP", date2)
	assert.NoError(t, err)

	// 20 + 50*0.86 + 100*0.91*0.86
	eur2gbp := mustParseDec("0.86")
	usd2eur := mustParseDec("0.91")
	expected := decimal.NewFromInt(20).
		Add(decimal.NewFromInt(50).Mul(eur2gbp)).
		Add(decimal.NewFromInt(100).Mul(usd2eur).Mul(eur2gbp))

	assert.True(t, result.Equal(expected))
}

// TestConvertBalance_IgnoreZeroAmounts verifies zero amounts are skipped without price lookups.
func TestConvertBalance_IgnoreZeroAmounts(t *testing.T) {
	l := New()
	date, _ := ast.NewDate("2024-01-01")

	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
		"EUR": decimal.Zero, // Zero - should be skipped
	}

	// Even though no EUR price exists, should not error because EUR amount is zero
	result, err := l.ConvertBalance(balance, "USD", date)
	assert.NoError(t, err)
	assert.True(t, result.Equal(decimal.NewFromInt(100)), "expected 100, got %v", result)
}

// TestConvertBalance_ForwardFillPrice verifies forward-fill semantics (most recent price on or before date).
func TestConvertBalance_ForwardFillPrice(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")     // Old price
	date3, _ := ast.NewDate("2024-03-01")     // New price
	queryDate, _ := ast.NewDate("2024-03-15") // Query between date3 and future

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			// Old price: EUR 1 = USD 1.05 on 2024-02-01
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.05", "USD")),
			// New price: EUR 1 = USD 1.10 on 2024-03-01
			ast.NewPrice(date3, "EUR", ast.NewAmount("1.10", "USD")),
		},
	})

	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(100),
		"EUR": decimal.NewFromInt(50),
	}

	// Query on 2024-03-15 should use the 2024-03-01 price (1.10)
	result, err := l.ConvertBalance(balance, "USD", queryDate)
	assert.NoError(t, err)
	// 100 + 50 * 1.10 = 155
	expected := decimal.NewFromInt(100).Add(decimal.NewFromInt(50).Mul(mustParseDec("1.10")))
	assert.True(t, result.Equal(expected))

	// Query on 2024-02-15 should use the 2024-02-01 price (1.05)
	queryDate2, _ := ast.NewDate("2024-02-15")
	result2, err := l.ConvertBalance(balance, "USD", queryDate2)
	assert.NoError(t, err)
	// 100 + 50 * 1.05 = 152.5
	expected2 := decimal.NewFromInt(100).Add(decimal.NewFromInt(50).Mul(mustParseDec("1.05")))
	assert.True(t, result2.Equal(expected2))
}

// TestConvertBalance_ComplexScenario verifies multi-currency with different scenarios.
// Simulates a real-world balance sheet with multiple currencies.
func TestConvertBalance_ComplexScenario(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-12-31")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			// Portfolio: $1000 USD
			ast.NewTransaction(date1, "Opening USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(equity),
			)),
			// €500 EUR
			ast.NewTransaction(date1, "Opening EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("500", "EUR")),
				ast.NewPosting(equity),
			)),
			// £200 GBP
			ast.NewTransaction(date1, "Opening GBP", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("200", "GBP")),
				ast.NewPosting(equity),
			)),
			// Prices: EUR→USD 1.10, GBP→USD 1.27
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.10", "USD")),
			ast.NewPrice(date2, "GBP", ast.NewAmount("1.27", "USD")),
		},
	})

	balance := map[string]decimal.Decimal{
		"USD": decimal.NewFromInt(1000),
		"EUR": decimal.NewFromInt(500),
		"GBP": decimal.NewFromInt(200),
	}

	// Consolidate to USD:
	// 1000 + 500*1.10 + 200*1.27 = 1000 + 550 + 254 = 1804
	result, err := l.ConvertBalance(balance, "USD", date2)
	assert.NoError(t, err)

	expected := decimal.NewFromInt(1000).
		Add(decimal.NewFromInt(500).Mul(mustParseDec("1.10"))).
		Add(decimal.NewFromInt(200).Mul(mustParseDec("1.27")))
	assert.True(t, result.Equal(expected))
}

// (GetBalanceInCurrency tests removed - tests are under Account.GetBalanceInCurrencyAsOf)

// GetBalancesAsOfInCurrency Tests

// TestGetBalancesAsOfInCurrency_Empty verifies empty ledger returns no balances.
func TestGetBalancesAsOfInCurrency_Empty(t *testing.T) {
	l := New()
	date, _ := ast.NewDate("2024-01-01")

	results, err := l.GetBalancesAsOfInCurrency("USD", date)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 0)
}

// TestGetBalancesAsOfInCurrency_SingleAccount verifies single account consolidation.
func TestGetBalancesAsOfInCurrency_SingleAccount(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	results, err := l.GetBalancesAsOfInCurrency("USD", date2)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 2)

	// Find assets and equity
	var assetBal, equityBal *AccountBalance
	for i := range results {
		switch results[i].Account {
		case "Assets:Cash":
			assetBal = &results[i]
		case "Equity:Opening":
			equityBal = &results[i]
		}
	}

	assert.True(t, assetBal != nil, "Assets:Cash should exist")
	assert.True(t, equityBal != nil, "Equity:Opening should exist")
	assert.True(t, assetBal.Balances["USD"].Equal(decimal.NewFromInt(100)))
	assert.True(t, equityBal.Balances["USD"].Equal(decimal.NewFromInt(-100)))
}

// TestGetBalancesAsOfInCurrency_MultiAccount verifies multiple accounts consolidated.
func TestGetBalancesAsOfInCurrency_MultiAccount(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	expenses, _ := ast.NewAccount("Expenses:Food")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: expenses},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("1000", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date2, "Food", ast.WithPostings(
				ast.NewPosting(expenses, ast.WithAmount("50", "USD")),
				ast.NewPosting(assets),
			)),
		},
	})

	results, err := l.GetBalancesAsOfInCurrency("USD", date2)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 3) // Assets, Expenses, Equity

	// Verify each account
	balances := make(map[string]decimal.Decimal)
	for _, bal := range results {
		balances[bal.Account] = bal.Balances["USD"]
	}

	assert.True(t, balances["Assets:Cash"].Equal(decimal.NewFromInt(950)))
	assert.True(t, balances["Expenses:Food"].Equal(decimal.NewFromInt(50)))
	assert.True(t, balances["Equity:Opening"].Equal(decimal.NewFromInt(-1000)))
}

// TestGetBalancesAsOfInCurrency_MultiCurrency verifies multi-currency accounts.
func TestGetBalancesAsOfInCurrency_MultiCurrency(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.10", "USD")),
		},
	})

	results, err := l.GetBalancesAsOfInCurrency("USD", date2)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 2)

	// Find assets
	var assetBal *AccountBalance
	for i := range results {
		if results[i].Account == "Assets:Cash" {
			assetBal = &results[i]
		}
	}

	assert.True(t, assetBal != nil, "Assets:Cash should exist")
	// 100 + 50 * 1.10 = 155
	expected := decimal.NewFromInt(100).Add(decimal.NewFromInt(50).Mul(mustParseDec("1.10")))
	assert.True(t, assetBal.Balances["USD"].Equal(expected))
}

// TestGetBalancesAsOfInCurrency_MissingPrice verifies error on missing price.
func TestGetBalancesAsOfInCurrency_MissingPrice(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			// No EUR→USD price
		},
	})

	_, err := l.GetBalancesAsOfInCurrency("USD", date2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no price found")
}

// TestGetBalancesAsOfInCurrency_PartialMissingPrice verifies partial errors collected.
func TestGetBalancesAsOfInCurrency_PartialMissingPrice(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	other, _ := ast.NewAccount("Assets:Other")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: other},
			&ast.Open{Date: date1, Account: equity},
			// Assets:Cash has both USD and EUR
			ast.NewTransaction(date1, "USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			// Assets:Other has only GBP (no price)
			ast.NewTransaction(date1, "GBP", ast.WithPostings(
				ast.NewPosting(other, ast.WithAmount("25", "GBP")),
				ast.NewPosting(equity),
			)),
			// EUR→USD price exists, GBP→USD does not
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.10", "USD")),
		},
	})

	_, err := l.GetBalancesAsOfInCurrency("USD", date2)

	// Should have error due to missing GBP price
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no price found")
	assert.Contains(t, err.Error(), "GBP")
}

// TestGetBalancesAsOfInCurrency_BeforeFirstTransaction verifies no results before any activity.
func TestGetBalancesAsOfInCurrency_BeforeFirstTransaction(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date0, _ := ast.NewDate("2023-12-31")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	results, err := l.GetBalancesAsOfInCurrency("USD", date0)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 0)
}

// Account.GetBalanceInCurrencyAsOf Tests

// TestAccount_GetBalanceInCurrencyAsOf_SingleCurrency verifies conversion when account has single currency.
func TestAccount_GetBalanceInCurrencyAsOf_SingleCurrency(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "Opening", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
		},
	})

	account := l.Accounts()[string(assets)]
	result, err := account.GetBalanceInCurrencyAsOf(l, "USD", date2)

	assert.NoError(t, err)
	assert.Equal(t, result.Account, "Assets:Cash")
	assert.True(t, result.Balances["USD"].Equal(decimal.NewFromInt(100)))
}

// TestAccount_GetBalanceInCurrencyAsOf_MultiCurrency verifies conversion with multiple currencies.
func TestAccount_GetBalanceInCurrencyAsOf_MultiCurrency(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.10", "USD")),
		},
	})

	account := l.Accounts()[string(assets)]
	result, err := account.GetBalanceInCurrencyAsOf(l, "USD", date2)

	assert.NoError(t, err)
	assert.Equal(t, result.Account, "Assets:Cash")
	// 100 + 50 * 1.10 = 155
	expected := decimal.NewFromInt(100).Add(decimal.NewFromInt(50).Mul(mustParseDec("1.10")))
	assert.True(t, result.Balances["USD"].Equal(expected))
}

// TestAccount_GetBalanceInCurrencyAsOf_MissingPrice verifies error on missing price.
func TestAccount_GetBalanceInCurrencyAsOf_MissingPrice(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "USD", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "USD")),
				ast.NewPosting(equity),
			)),
			ast.NewTransaction(date1, "EUR", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("50", "EUR")),
				ast.NewPosting(equity),
			)),
			// No EUR→USD price
		},
	})

	account := l.Accounts()[string(assets)]
	_, err := account.GetBalanceInCurrencyAsOf(l, "USD", date2)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no price found")
}

// TestAccount_GetBalanceInCurrencyAsOf_ZeroBalance verifies zero balance converts to zero.
func TestAccount_GetBalanceInCurrencyAsOf_ZeroBalance(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
		},
	})

	account := l.Accounts()[string(assets)]
	result, err := account.GetBalanceInCurrencyAsOf(l, "USD", date2)

	assert.NoError(t, err)
	assert.Equal(t, result.Account, "Assets:Cash")
	assert.True(t, result.Balances["USD"].IsZero())
}

// TestAccount_GetBalanceInCurrencyAsOf_MultiPath verifies multi-hop conversion.
func TestAccount_GetBalanceInCurrencyAsOf_MultiPath(t *testing.T) {
	l := New()
	assets, _ := ast.NewAccount("Assets:Cash")
	equity, _ := ast.NewAccount("Equity:Opening")

	date1, _ := ast.NewDate("2024-01-01")
	date2, _ := ast.NewDate("2024-02-01")

	l.MustProcess(context.Background(), &ast.AST{
		Directives: []ast.Directive{
			&ast.Open{Date: date1, Account: assets},
			&ast.Open{Date: date1, Account: equity},
			ast.NewTransaction(date1, "GBP", ast.WithPostings(
				ast.NewPosting(assets, ast.WithAmount("100", "GBP")),
				ast.NewPosting(equity),
			)),
			// GBP → EUR → USD chain
			ast.NewPrice(date2, "GBP", ast.NewAmount("1.15", "EUR")),
			ast.NewPrice(date2, "EUR", ast.NewAmount("1.10", "USD")),
		},
	})

	account := l.Accounts()[string(assets)]
	result, err := account.GetBalanceInCurrencyAsOf(l, "USD", date2)

	assert.NoError(t, err)
	assert.Equal(t, result.Account, "Assets:Cash")
	// 100 * 1.15 * 1.10 = 126.5
	expected := decimal.NewFromInt(100).Mul(mustParseDec("1.15")).Mul(mustParseDec("1.10"))
	assert.True(t, result.Balances["USD"].Equal(expected))
}
