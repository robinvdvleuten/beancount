package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestLedger_ProcessOpen(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkFunc func(*testing.T, *Ledger)
	}{
		{
			name: "open account successfully",
			input: `
				2020-01-01 open Assets:Checking
			`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Checking")
				assert.True(t, ok, "account should exist")
				assert.Equal(t, "Assets:Checking", string(acc.Name))
				assert.Equal(t, AccountTypeAssets, acc.Type)
				assert.False(t, acc.IsClosed())
			},
		},
		{
			name: "open account with currencies",
			input: `
				2020-01-01 open Assets:Checking USD, EUR
			`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Checking")
				assert.True(t, ok)
				assert.Equal(t, []string{"USD", "EUR"}, acc.ConstraintCurrencies)
			},
		},
		{
			name: "open account with booking method",
			input: `
				2020-01-01 open Assets:Brokerage USD "STRICT"
			`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				assert.Equal(t, "STRICT", acc.BookingMethod)
			},
		},
		{
			name: "error: open same account twice",
			input: `
				2020-01-01 open Assets:Checking
				2020-06-01 open Assets:Checking
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*AccountAlreadyOpenError)
				assert.True(t, ok, "should be AccountAlreadyOpenError")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, l)
			}
		})
	}
}

func TestLedger_ProcessClose(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkFunc func(*testing.T, *Ledger)
	}{
		{
			name: "close account successfully",
			input: `
				2020-01-01 open Assets:Checking
				2020-12-31 close Assets:Checking
			`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Checking")
				assert.True(t, ok)
				assert.True(t, acc.IsClosed())
				assert.NotZero(t, acc.CloseDate)
			},
		},
		{
			name: "error: close account that was never opened",
			input: `
				2020-12-31 close Assets:Checking
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*AccountNotClosedError)
				assert.True(t, ok, "should be AccountNotClosedError")
			},
		},
		{
			name: "error: close account twice",
			input: `
				2020-01-01 open Assets:Checking
				2020-06-01 close Assets:Checking
				2020-12-31 close Assets:Checking
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*AccountAlreadyClosedError)
				assert.True(t, ok, "should be AccountAlreadyClosedError")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, l)
			}
		})
	}
}

func TestLedger_ProcessTransaction(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkFunc func(*testing.T, *Ledger)
	}{
		{
			name: "transaction with opened accounts",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary

				2020-01-15 * "Salary"
				  Assets:Checking  1000.00 USD
				  Income:Salary   -1000.00 USD
			`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				// Check inventory updated
				checking, _ := l.GetAccount("Assets:Checking")
				assert.Equal(t, "1000", checking.Inventory.Get("USD").String())

				income, _ := l.GetAccount("Income:Salary")
				assert.Equal(t, "-1000", income.Inventory.Get("USD").String())
			},
		},
		{
			name: "multi-posting transaction",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Expenses:Rent
				2020-01-01 open Expenses:Food

				2020-02-01 * "Monthly expenses"
				  Assets:Checking  -2000.00 USD
				  Expenses:Rent     1500.00 USD
				  Expenses:Food      500.00 USD
			`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				checking, _ := l.GetAccount("Assets:Checking")
				assert.Equal(t, "-2000", checking.Inventory.Get("USD").String())

				rent, _ := l.GetAccount("Expenses:Rent")
				assert.Equal(t, "1500", rent.Inventory.Get("USD").String())

				food, _ := l.GetAccount("Expenses:Food")
				assert.Equal(t, "500", food.Inventory.Get("USD").String())
			},
		},
		{
			name: "multi-currency transaction",
			input: `
				2020-01-01 open Assets:USD
				2020-01-01 open Assets:EUR
				2020-01-01 open Expenses:Travel

				2020-03-01 * "European trip"
				  Assets:USD         -500.00 USD
				  Assets:EUR         -200.00 EUR
				  Expenses:Travel     500.00 USD
				  Expenses:Travel     200.00 EUR
			`,
			wantErr: false,
		},
		{
			name: "error: transaction with unopened account",
			input: `
				2020-01-01 open Assets:Checking

				2020-01-15 * "Salary"
				  Assets:Checking  1000.00 USD
				  Income:Salary   -1000.00 USD
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*AccountNotOpenError)
				assert.True(t, ok, "should be AccountNotOpenError")
			},
		},
		{
			name: "error: transaction with closed account",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary
				2020-06-01 close Assets:Checking

				2020-07-15 * "Salary"
				  Assets:Checking  1000.00 USD
				  Income:Salary   -1000.00 USD
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*AccountNotOpenError)
				assert.True(t, ok, "should be AccountNotOpenError")
			},
		},
		{
			name: "error: transaction doesn't balance",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary

				2020-01-15 * "Oops"
				  Assets:Checking  1000.00 USD
				  Income:Salary    -500.00 USD
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*TransactionNotBalancedError)
				assert.True(t, ok, "should be TransactionNotBalancedError")
			},
		},
		{
			name: "error: multi-currency doesn't balance",
			input: `
				2020-01-01 open Assets:USD
				2020-01-01 open Assets:EUR

				2020-01-15 * "Broken exchange"
				  Assets:USD  -100.00 USD
				  Assets:EUR    50.00 EUR
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				balErr, ok := errs[0].(*TransactionNotBalancedError)
				assert.True(t, ok, "should be TransactionNotBalancedError")
				// Should have residuals for both currencies
				assert.Equal(t, 2, len(balErr.Residuals))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, l)
			}
		})
	}
}

func TestLedger_ProcessBalance(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkFunc func(*testing.T, *Ledger)
	}{
		{
			name: "balance assertion passes",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary

				2020-01-15 * "Salary"
				  Assets:Checking  1000.00 USD
				  Income:Salary   -1000.00 USD

				2020-01-16 balance Assets:Checking  1000.00 USD
			`,
			wantErr: false,
		},
		{
			name: "balance assertion with tolerance passes",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary

				2020-01-15 * "Salary"
				  Assets:Checking  1000.004 USD
				  Income:Salary   -1000.004 USD

				2020-01-16 balance Assets:Checking  1000.00 USD
			`,
			wantErr: false, // Within 0.005 tolerance
		},
		{
			name: "balance after multiple transactions",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary
				2020-01-01 open Expenses:Rent

				2020-01-15 * "Salary"
				  Assets:Checking  3000.00 USD
				  Income:Salary   -3000.00 USD

				2020-02-01 * "Rent"
				  Assets:Checking  -1500.00 USD
				  Expenses:Rent     1500.00 USD

				2020-02-02 balance Assets:Checking  1500.00 USD
			`,
			wantErr: false,
		},
		{
			name: "error: balance mismatch",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary

				2020-01-15 * "Salary"
				  Assets:Checking  1000.00 USD
				  Income:Salary   -1000.00 USD

				2020-01-16 balance Assets:Checking  500.00 USD
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				balErr, ok := errs[0].(*BalanceMismatchError)
				assert.True(t, ok, "should be BalanceMismatchError")
				assert.Equal(t, "500", balErr.Expected)
				assert.Equal(t, "1000", balErr.Actual)
				assert.Equal(t, "USD", balErr.Currency)
			},
		},
		{
			name: "error: balance exceeds tolerance",
			input: `
				2020-01-01 open Assets:Checking
				2020-01-01 open Income:Salary

				2020-01-15 * "Salary"
				  Assets:Checking  1000.00 USD
				  Income:Salary   -1000.00 USD

				2020-01-16 balance Assets:Checking  1000.10 USD
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*BalanceMismatchError)
				assert.True(t, ok, "should be BalanceMismatchError")
			},
		},
		{
			name: "error: balance on unopened account",
			input: `
				2020-01-16 balance Assets:Checking  1000.00 USD
			`,
			wantErr: true,
			checkFunc: func(t *testing.T, l *Ledger) {
				errs := l.Errors()
				assert.Equal(t, 1, len(errs))
				_, ok := errs[0].(*AccountNotOpenError)
				assert.True(t, ok, "should be AccountNotOpenError")
			},
		},
		{
			name: "balance zero when no transactions",
			input: `
				2020-01-01 open Assets:Checking

				2020-01-16 balance Assets:Checking  0.00 USD
			`,
			wantErr: false,
		},
		{
			name: "multi-currency balance checking",
			input: `
				2020-01-01 open Assets:Account
				2020-01-01 open Income:Source

				2020-01-15 * "Income"
				  Assets:Account  1000.00 USD
				  Assets:Account   500.00 EUR
				  Income:Source  -1000.00 USD
				  Income:Source   -500.00 EUR

				2020-01-16 balance Assets:Account  1000.00 USD
				2020-01-16 balance Assets:Account   500.00 EUR
			`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, l)
			}
		})
	}
}

func TestAccount_IsOpen(t *testing.T) {
	tests := []struct {
		name      string
		account   *Account
		checkDate string
		want      bool
	}{
		{
			name: "account is open on exact open date",
			account: &Account{
				OpenDate: mustParseDate("2020-01-01"),
			},
			checkDate: "2020-01-01",
			want:      true,
		},
		{
			name: "account is open after open date",
			account: &Account{
				OpenDate: mustParseDate("2020-01-01"),
			},
			checkDate: "2020-06-01",
			want:      true,
		},
		{
			name: "account is not open before open date",
			account: &Account{
				OpenDate: mustParseDate("2020-01-01"),
			},
			checkDate: "2019-12-31",
			want:      false,
		},
		{
			name: "account is open before close date",
			account: &Account{
				OpenDate:  mustParseDate("2020-01-01"),
				CloseDate: mustParseDate("2020-12-31"),
			},
			checkDate: "2020-06-01",
			want:      true,
		},
		{
			name: "account is open on close date",
			account: &Account{
				OpenDate:  mustParseDate("2020-01-01"),
				CloseDate: mustParseDate("2020-12-31"),
			},
			checkDate: "2020-12-31",
			want:      true,
		},
		{
			name: "account is not open after close date",
			account: &Account{
				OpenDate:  mustParseDate("2020-01-01"),
				CloseDate: mustParseDate("2020-12-31"),
			},
			checkDate: "2021-01-01",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkDate := mustParseDate(tt.checkDate)
			got := tt.account.IsOpen(checkDate)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseAccountType(t *testing.T) {
	tests := []struct {
		account ast.Account
		want    AccountType
	}{
		{"Assets:Checking", AccountTypeAssets},
		{"Liabilities:CreditCard", AccountTypeLiabilities},
		{"Equity:Opening-Balances", AccountTypeEquity},
		{"Income:Salary", AccountTypeIncome},
		{"Expenses:Rent", AccountTypeExpenses},
		{"Invalid:Account", AccountTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(string(tt.account), func(t *testing.T) {
			got := ParseAccountType(tt.account)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Helper function to parse dates in tests
func mustParseDate(s string) *ast.Date {
	date := &ast.Date{}
	err := date.Capture([]string{s})
	if err != nil {
		panic(err)
	}
	return date
}

// TestAccountLifecycleEdgeCases tests edge cases in account lifecycle:
// - Closing account with non-zero inventory (should succeed)
// - Balance assertion on account open date (valid)
// - Reopening closed account (valid)
func TestAccountLifecycleEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *Ledger)
	}{
		{
			name: "close account with non-zero inventory - should succeed",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Deposit"
				  Assets:Checking    100 USD
				  Equity:Opening    -100 USD

				2020-01-03 close Assets:Checking
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Checking")
				assert.True(t, ok)
				assert.True(t, acc.IsClosed())
				// Should still have balance
				assert.Equal(t, "100", acc.Inventory.Get("USD").String())
			},
		},
		{
			name: "balance assertion on account open date - valid",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 balance Assets:Checking 0 USD
			`,
			wantErr: false,
		},
		{
			name: "reopen closed account - invalid (duplicate open)",
			input: `
				2020-01-01 open Assets:OldAccount
				2020-01-02 close Assets:OldAccount
				2020-01-03 open Assets:OldAccount
			`,
			wantErr: true, // Beancount does NOT allow reopening accounts - duplicate open directives are errors
		},
		{
			name: "use account after close - should error",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening
				2020-01-02 close Assets:Checking

				2020-01-03 * "Try to use closed account"
				  Assets:Checking    100 USD
				  Equity:Opening    -100 USD
			`,
			wantErr: true,
		},
		{
			name: "close then reopen then use - invalid (cannot reopen)",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening
				2020-01-02 close Assets:Checking
				2020-01-03 open Assets:Checking USD

				2020-01-04 * "Use reopened account"
				  Assets:Checking    100 USD
				  Equity:Opening    -100 USD
			`,
			wantErr: true, // Beancount does NOT allow reopening accounts - duplicate open at line 4 is an error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.check != nil {
					tt.check(t, l)
				}
			}
		})
	}
}

func TestLedger_MultipleOptions(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		checkFunc func(*testing.T, *Ledger)
	}{
		{
			name: "multiple values for same option key",
			input: `
				option "inferred_tolerance_default" "USD:0.01"
				option "inferred_tolerance_default" "EUR:0.01"
				option "inferred_tolerance_default" "BTC:0.0001"
				option "title" "My Ledger"
			`,
			checkFunc: func(t *testing.T, l *Ledger) {
				// GetOptions should return all values
				tolerances := l.GetOptions("inferred_tolerance_default")
				assert.Equal(t, 3, len(tolerances))
				assert.Equal(t, "USD:0.01", tolerances[0])
				assert.Equal(t, "EUR:0.01", tolerances[1])
				assert.Equal(t, "BTC:0.0001", tolerances[2])

				// GetOption should return first value
				title, ok := l.GetOption("title")
				assert.True(t, ok)
				assert.Equal(t, "My Ledger", title)

				// Verify tolerances were parsed correctly
				usdTol := l.toleranceConfig.GetDefaultTolerance("USD")
				assert.Equal(t, "0.01", usdTol.String())

				eurTol := l.toleranceConfig.GetDefaultTolerance("EUR")
				assert.Equal(t, "0.01", eurTol.String())

				btcTol := l.toleranceConfig.GetDefaultTolerance("BTC")
				assert.Equal(t, "0.0001", btcTol.String())
			},
		},
		{
			name: "single value option",
			input: `
				option "title" "Test Ledger"
			`,
			checkFunc: func(t *testing.T, l *Ledger) {
				// GetOption for single value
				title, ok := l.GetOption("title")
				assert.True(t, ok)
				assert.Equal(t, "Test Ledger", title)

				// GetOptions should also work
				titles := l.GetOptions("title")
				assert.Equal(t, 1, len(titles))
				assert.Equal(t, "Test Ledger", titles[0])
			},
		},
		{
			name: "non-existent option",
			input: `
				option "title" "Test"
			`,
			checkFunc: func(t *testing.T, l *Ledger) {
				// GetOption for non-existent key
				_, ok := l.GetOption("non_existent")
				assert.False(t, ok)

				// GetOptions for non-existent key
				vals := l.GetOptions("non_existent")
				assert.Equal(t, 0, len(vals))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)
			assert.NoError(t, err)

			if tt.checkFunc != nil {
				tt.checkFunc(t, l)
			}
		})
	}
}
