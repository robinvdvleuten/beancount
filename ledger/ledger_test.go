package ledger

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
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
				assert.Equal(t, "Assets", acc.Type) // Account.Type is now the root name string
				assert.Equal(t, BookingSTRICT, l.booker.method(acc.Name))
				assert.False(t, acc.IsClosed())
			},
		},
		{
			name: "open account inherits configured booking method",
			input: `
option "booking_method" "LIFO"
2020-01-01 open Assets:Brokerage USD
`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				assert.Equal(t, BookingLIFO, l.booker.method(acc.Name))
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
option "booking_method" "LIFO"
2020-01-01 open Assets:Brokerage USD "STRICT"
`,
			wantErr: false,
			checkFunc: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				assert.Equal(t, BookingSTRICT, l.booker.method(acc.Name))
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
				ok := kindOf(errs[0]) == "AccountAlreadyOpenError"
				assert.True(t, ok, "should be AccountAlreadyOpenError")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			err := l.Process(context.Background(), ast)

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

func TestLedgerProcessPreparesRawParserAST(t *testing.T) {
	source := `pushtag #trip
2024-01-02 * "Seed"
  Assets:Checking           10.00 USD
  Equity:Opening-Balances  -10.00 USD
poptag #trip
2024-01-01 open Assets:Checking USD
2024-01-01 open Equity:Opening-Balances USD
`

	tree := parser.MustParseString(context.Background(), source)
	txn := tree.Directives[0].(*ast.Transaction)
	assert.Equal(t, 0, len(txn.Tags))

	l := New()
	err := l.Process(context.Background(), tree)
	assert.NoError(t, err)

	assert.Equal(t, "Assets:Checking", string(tree.Directives[0].(*ast.Open).Account))
	assert.Equal(t, 1, len(txn.Tags))
	assert.Equal(t, ast.Tag("trip"), txn.Tags[0])
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
				ok := kindOf(errs[0]) == "AccountNotClosedError"
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
				ok := kindOf(errs[0]) == "AccountAlreadyClosedError"
				assert.True(t, ok, "should be AccountAlreadyClosedError")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			err := l.Process(context.Background(), ast)

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
				ok := kindOf(errs[0]) == "AccountNotOpenError"
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
				ok := kindOf(errs[0]) == "AccountNotOpenError"
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
				ok := kindOf(errs[0]) == "TransactionNotBalancedError"
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
				assert.Equal(t, "TransactionNotBalancedError", kindOf(errs[0]))
				// Should have residuals for both currencies
				assert.Contains(t, errs[0].Error(), "(50 EUR, -100 USD)")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			err := l.Process(context.Background(), ast)

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
			wantErr: false, // Within inferred 0.005 tolerance
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
				assert.Contains(t, balErr.Error(), "Expected: 500 USD")
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
				ok := kindOf(errs[0]) == "BalanceMismatchError"
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
				ok := kindOf(errs[0]) == "AccountNotOpenError"
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
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			err := l.Process(context.Background(), ast)

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
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			err := l.Process(context.Background(), ast)

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
				// Options are parsed into Config during Process()
				// Just verify no errors occurred
				assert.Equal(t, 0, len(l.errors))
			},
		},
		{
			name: "single value option",
			input: `
option "title" "Test Ledger"
`,
			checkFunc: func(t *testing.T, l *Ledger) {
				// Options are now parsed into Config and stored in context
				// Just verify processing succeeds without errors
				assert.Equal(t, 0, len(l.errors))
			},
		},
		{
			name: "non-existent option",
			input: `
option "title" "Test"
`,
			checkFunc: func(t *testing.T, l *Ledger) {
				// Options are now parsed into Config and stored in context
				// Just verify processing succeeds without errors
				assert.Equal(t, 0, len(l.errors))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			err := l.Process(context.Background(), ast)
			assert.NoError(t, err)

			if tt.checkFunc != nil {
				tt.checkFunc(t, l)
			}
		})
	}
}

func TestDatedAndLabeledLotReduction(t *testing.T) {
	// Two lots acquired on different dates with different labels. A reduction
	// that specifies only a date or only a label must select the matching lot,
	// mirroring official beancount behavior (bean-check 2.3.6).
	prefix := `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash
2020-01-01 open Income:Gains

2020-02-01 * "Buy lot a"
  Assets:Brokerage    5 HOOL {100.00 USD, "lot-a"}
  Assets:Cash        -500.00 USD

2020-03-01 * "Buy lot b"
  Assets:Brokerage    5 HOOL {110.00 USD, "lot-b"}
  Assets:Cash        -550.00 USD
`

	tests := []struct {
		name          string
		spec          string
		remainingCost string // per-unit cost of the lot left behind
	}{
		{name: "DateOnly", spec: "{2020-02-01}", remainingCost: "110"},
		{name: "LabelOnly", spec: `{"lot-b"}`, remainingCost: "100"},
		{name: "DateAndAmountReversed", spec: "{2020-03-01, 110.00 USD}", remainingCost: "100"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := prefix + `
2020-04-01 * "Sell"
  Assets:Brokerage    -5 HOOL ` + test.spec + `
                Assets:Cash         550.00 USD
                Income:Gains
        `
			ast := parser.MustParseString(context.Background(), input)

			l := New()
			err := l.Process(context.Background(), ast)
			assert.NoError(t, err)

			acc, ok := l.GetAccount("Assets:Brokerage")
			assert.True(t, ok)
			lots := acc.Inventory.GetLots("HOOL")
			assert.Equal(t, 1, len(lots))
			assert.Equal(t, "5", lots[0].Amount.String())
			assert.True(t, lots[0].Spec != nil && lots[0].Spec.Cost != nil)
			assert.Equal(t, test.remainingCost, lots[0].Spec.Cost.String())
		})
	}
}

func TestMustProcess(t *testing.T) {
	ctx := context.Background()

	// Valid beancount file that should process without errors
	source := `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Groceries
2024-01-15 * "Buy groceries"
  Assets:Checking     -45.60 USD
  Expenses:Groceries   45.60 USD
`

	ast := parser.MustParseString(ctx, source)
	ledger := New()

	// Should not panic on valid AST
	ledger.MustProcess(ctx, ast)

	// Verify ledger state was built
	account, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok)
	assert.True(t, account != nil)
}

func TestMustProcessInvalidPanics(t *testing.T) {
	ctx := context.Background()

	// Invalid: transaction doesn't balance
	source := `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Groceries
2024-01-15 * "Unbalanced transaction"
  Assets:Checking     -45.60 USD
  Expenses:Groceries   50.00 USD
`

	ast := parser.MustParseString(ctx, source)
	ledger := New()

	// Should panic due to validation error
	assert.Panics(t, func() {
		ledger.MustProcess(ctx, ast)
	})
}

func TestMustProcessMultipleCurrencies(t *testing.T) {
	ctx := context.Background()

	source := `2024-01-01 open Assets:Checking
2024-01-01 open Assets:Savings
2024-01-01 open Expenses:Groceries
2024-01-15 * "Multi-currency transaction"
  Assets:Checking     -45.60 USD
  Assets:Savings      100.00 EUR
  Expenses:Groceries   45.60 USD
  Expenses:Groceries -100.00 EUR
`

	ast := parser.MustParseString(ctx, source)
	ledger := New()

	// Should process successfully with multiple currencies
	ledger.MustProcess(ctx, ast)

	// Verify both accounts exist
	checking, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok)
	assert.True(t, checking != nil)

	savings, ok := ledger.GetAccount("Assets:Savings")
	assert.True(t, ok)
	assert.True(t, savings != nil)
}

func TestMustProcessWithMetadata(t *testing.T) {
	ctx := context.Background()

	source := `2024-01-01 open Assets:Checking USD
2024-01-01 open Expenses:Other USD
2024-01-15 * "Transaction with metadata"
  invoice: "INV-001"
  Assets:Checking -100.00 USD
  Expenses:Other   100.00 USD
`

	ast := parser.MustParseString(ctx, source)
	ledger := New()

	// Should process metadata without errors
	ledger.MustProcess(ctx, ast)

	checking, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok)
	assert.True(t, checking != nil)
}

func TestMustProcessEmpty(t *testing.T) {
	ctx := context.Background()

	// Empty file should process successfully
	ast := parser.MustParseString(ctx, "")
	ledger := New()

	// Should not panic on empty AST
	ledger.MustProcess(ctx, ast)

	// No accounts should be opened
	accounts := ledger.Accounts()
	assert.Equal(t, len(accounts), 0)
}

// Price directive integration tests

func TestLedger_PriceDirectiveProcessing(t *testing.T) {
	source := `
plugin "beancount.plugins.auto_accounts"

2024-01-15 price USD 1.08 CAD
2024-01-15 price EUR 0.92 USD
2024-01-20 price USD 1.10 CAD
`

	ctx := context.Background()
	tree := parser.MustParseString(ctx, source)

	ledger := New()
	err := ledger.Process(ctx, tree)
	assert.NoError(t, err)

	// Verify prices were indexed
	date115 := newTestDate("2024-01-15")
	rate1, found1 := ledger.GetPrice(date115, "USD", "CAD")
	assert.True(t, found1)
	assert.True(t, rate1.Equal(mustParseDec("1.08")))

	// Verify bidirectional lookup (inverse created automatically)
	rate2, found2 := ledger.GetPrice(date115, "CAD", "USD")
	assert.True(t, found2)
	expectedInv := pydecimal.Quo(mustParseDec("1"), mustParseDec("1.08"))
	assert.True(t, rate2.Equal(expectedInv))

	// Verify EUR price
	rate3, found3 := ledger.GetPrice(date115, "EUR", "USD")
	assert.True(t, found3)
	assert.True(t, rate3.Equal(mustParseDec("0.92")))

	// Verify forward-fill: price from 2024-01-15 used for 2024-01-18
	date118 := newTestDate("2024-01-18")
	rate4, found4 := ledger.GetPrice(date118, "USD", "CAD")
	assert.True(t, found4)
	assert.True(t, rate4.Equal(mustParseDec("1.08")))

	// Verify most recent price used after 2024-01-20
	date125 := newTestDate("2024-01-25")
	rate5, found5 := ledger.GetPrice(date125, "USD", "CAD")
	assert.True(t, found5)
	assert.True(t, rate5.Equal(mustParseDec("1.10")))
}

func TestLedger_OpensAnAccountThatIsAlsoAParent(t *testing.T) {
	source := `
2021-01-01 open Assets:Cash USD
2021-01-01 open Expenses:Taxes:Y2021:US:Federal:PreTax401k USD
2021-01-01 open Expenses:Taxes:Y2021:US:Federal USD

2021-01-02 * "Federal tax"
  Assets:Cash                            -100 USD
  Expenses:Taxes:Y2021:US:Federal        100 USD
`

	ctx := context.Background()
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	err := ledger.Process(ctx, tree)
	assert.NoError(t, err)

	account, ok := ledger.GetAccount("Expenses:Taxes:Y2021:US:Federal")
	assert.True(t, ok)
	assert.True(t, account.Inventory.Get("USD").Equal(mustParseDec("100")))
}

func TestLedger_GetPriceSameCurrency(t *testing.T) {
	ledger := New()

	date := newTestDate("2024-01-15")

	// Same-currency price should always return 1.0, no prices needed
	rate, found := ledger.GetPrice(date, "USD", "USD")
	assert.True(t, found)
	assert.True(t, rate.Equal(mustParseDec("1")))
}

func TestLedger_GetPriceBeforeAnyPrice(t *testing.T) {
	source := `
2024-01-15 price USD 1.08 CAD
`

	ctx := context.Background()
	tree := parser.MustParseString(ctx, source)

	ledger := New()
	err := ledger.Process(ctx, tree)
	assert.NoError(t, err)

	// Before the first price, should not be found
	dateBefore := newTestDate("2024-01-10")
	rate, found := ledger.GetPrice(dateBefore, "USD", "CAD")
	assert.False(t, found)
	assert.True(t, rate.IsZero())
}

func TestLedger_InvalidPriceMissingAmount(t *testing.T) {
	// Test validatePrice directly with a manually constructed price
	date := newTestDate("2024-01-15")

	price := ast.NewPrice(date, "USD", nil)

	errs := validatePrice(price)
	assert.True(t, len(errs) > 0)

	ok := kindOf(errs[0]) == "InvalidDirectivePriceError"
	assert.True(t, ok)
}

func TestLedger_PricesWithAccounts(t *testing.T) {
	// Verify prices work alongside normal ledger operations
	source := `
2024-01-01 open Assets:Cash USD
2024-01-01 open Assets:Savings USD

2024-01-15 price USD 1.08 CAD
2024-01-15 price EUR 0.92 USD

2024-01-15 * "Transfer money"
  Assets:Cash    100.00 USD
  Assets:Savings
`

	ctx := context.Background()
	tree := parser.MustParseString(ctx, source)

	ledger := New()
	err := ledger.Process(ctx, tree)
	assert.NoError(t, err)

	// Verify account was created
	acc, ok := ledger.GetAccount("Assets:Cash")
	assert.True(t, ok)
	assert.NotZero(t, acc)

	// Verify prices were indexed
	date := newTestDate("2024-01-15")
	_, found1 := ledger.GetPrice(date, "USD", "CAD")
	assert.True(t, found1)
	_, found2 := ledger.GetPrice(date, "EUR", "USD")
	assert.True(t, found2)
}

func TestZeroPrice(t *testing.T) {
	// Like beancount, a zero price is recorded and converts to zero, and it
	// has no inverse: the inverse falls back to an earlier price, or none.
	source := `
2019-12-01 price GOOG 4 USD
2020-01-01 price HOOL 0 USD
2020-01-02 price GOOG 0 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	assert.NoError(t, l.Process(context.Background(), tree))

	date, _ := ast.NewDate("2020-01-03")
	rate, ok := l.GetPrice(date, "HOOL", "USD")
	assert.True(t, ok)
	assert.True(t, rate.IsZero(), "rate: %s", rate)

	_, ok = l.GetPrice(date, "USD", "HOOL")
	assert.False(t, ok)

	rate, ok = l.GetPrice(date, "USD", "GOOG")
	assert.True(t, ok)
	assert.Equal(t, "0.25", rate.String())
}

func TestGetPriceUsesOnlyTheDirectPair(t *testing.T) {
	// Like beancount's get_price, a pair converts through its own price or
	// its inverse, never through a third currency.
	source := `
2020-01-01 price EUR 2 USD
2020-01-01 price HOOL 5 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	assert.NoError(t, l.Process(context.Background(), tree))
	date, _ := ast.NewDate("2020-01-02")

	rate, ok := l.GetPrice(date, "EUR", "USD")
	assert.True(t, ok)
	assert.Equal(t, "2", rate.String())

	rate, ok = l.GetPrice(date, "USD", "HOOL")
	assert.True(t, ok)
	assert.Equal(t, "0.2", rate.String())

	_, ok = l.GetPrice(date, "EUR", "HOOL")
	assert.False(t, ok)
}

// TestLedger_BookedPositions pins the record Booking publishes: a reduction
// books one position per lot it was booked against, an augmentation its own
// at its per-unit cost dated by its transaction, and a posting without cost
// its units alone, Reduced when it takes from units held with the opposite
// sign, like beancount's Booking.REDUCED.
func TestLedger_BookedPositions(t *testing.T) {
	tree := parser.MustParseString(context.Background(), `
2024-01-01 open Assets:Stock
2024-01-01 open Assets:Cash

2024-01-10 * "Buy"
  Assets:Stock  10 HOOL {{50 USD}}
  Assets:Cash  -50 USD

2024-02-01 * "Sell"
  Assets:Stock  -4 HOOL {}
  Assets:Cash
`)
	l := New()
	assert.NoError(t, l.Process(context.Background(), tree))

	posting := func(narration string, account ast.Account) *ast.Posting {
		t.Helper()
		for _, directive := range tree.Directives {
			txn, ok := directive.(*ast.Transaction)
			if !ok || txn.Narration.Value != narration {
				continue
			}
			for _, posting := range txn.Postings {
				if posting.Account == account {
					return posting
				}
			}
		}
		t.Fatalf("no %s posting in %q", account, narration)
		return nil
	}
	lot := &BookedCost{Number: mustParseDec("5"), Currency: "USD", Date: newTestDate("2024-01-10")}

	assert.Equal(t, []BookedPosition{{Units: mustParseDec("10"), Cost: lot}}, l.BookedPositions(posting("Buy", "Assets:Stock")))
	assert.Equal(t, []BookedPosition{{Units: mustParseDec("-4"), Cost: lot, Reduced: true}}, l.BookedPositions(posting("Sell", "Assets:Stock")))
	assert.Equal(t, []BookedPosition{{Units: mustParseDec("20"), Reduced: true}}, l.BookedPositions(posting("Sell", "Assets:Cash")))
}

func TestLedger_InterpolatedUnitsLeaveTheLotUndated(t *testing.T) {
	// beancount dates an augmentation before interpolation, so a lot whose
	// units it interpolates has no date, and a reduction naming the
	// transaction's date does not match it.
	tree := parser.MustParseString(context.Background(), `
2024-01-01 open Assets:Stock
2024-01-01 open Assets:Cash

2024-01-10 * "Buy"
  Assets:Stock  HOOL {10 USD}
  Assets:Cash  -20.00 USD

2024-02-01 * "Sell"
  Assets:Stock  -1 HOOL {10 USD, 2024-01-10}
  Assets:Cash
`)
	l := New()
	var validationErrors *ValidationErrors
	assert.True(t, errors.As(l.Process(context.Background(), tree), &validationErrors))
	assert.Equal(t, 1, len(validationErrors.Errors))
	assert.Contains(t, validationErrors.Errors[0].Error(), "No position matches")

	buy := tree.Directives[2].(*ast.Transaction)
	assert.Equal(t, []BookedPosition{{
		Units: mustParseDec("2.00"),
		Cost:  &BookedCost{Number: mustParseDec("10"), Currency: "USD"},
	}}, l.BookedPositions(buy.Postings[0]))
}

func TestLedger_UnknownVersusInactiveAccount(t *testing.T) {
	// Like v2's validate_active_accounts, an account opened anywhere in the
	// ledger, even later, is inactive outside its open interval; only an
	// account never opened is unknown.
	source := `
2020-02-01 open Assets:Cash
2020-02-01 open Income:Salary

2020-01-15 * "before open"
  Assets:Cash    10 USD
  Income:Salary

2020-01-16 note Assets:Cash "before open"

2020-03-01 close Assets:Cash

2020-03-02 * "after close"
  Assets:Cash    10 USD
  Income:Salary

2020-03-03 note Assets:Cash "after close is allowed"

2020-03-04 * "never opened"
  Assets:Never   10 USD
  Income:Salary
`
	l := New()
	_ = l.Process(context.Background(), parser.MustParseString(context.Background(), source))

	var messages []string
	for _, err := range l.Errors() {
		assert.Equal(t, "AccountNotOpenError", kindOf(err))
		messages = append(messages, err.Error())
	}
	assert.Equal(t, []string{
		"2020-01-15: Invalid reference to inactive account 'Assets:Cash'",
		"2020-01-15: Invalid reference to inactive account 'Income:Salary'",
		"2020-01-16: Invalid reference to inactive account 'Assets:Cash'",
		"2020-03-02: Invalid reference to inactive account 'Assets:Cash'",
		"2020-03-04: Invalid reference to unknown account 'Assets:Never'",
	}, messages)
}

func TestLedger_PadAndBalanceOnAccountsNotOpen(t *testing.T) {
	// Like beancount, a pad or balance assertion on an account outside its
	// open interval is reported and still applied: the pad still pads, its
	// padding transaction reporting its own accounts on the pad's line, and
	// the assertion is still checked. An assertion on an account never
	// opened is beancount's "does not exist", and is not checked.
	messages := func(source string) []string {
		l := New()
		_ = l.Process(context.Background(), parser.MustParseString(context.Background(), source))
		var messages []string
		for _, err := range l.Errors() {
			messages = append(messages, err.Error())
		}
		return messages
	}

	t.Run("pad on inactive accounts", func(t *testing.T) {
		assert.Equal(t, []string{
			"2020-01-19: Invalid reference to inactive account 'Assets:Cash'",
			"2020-01-19: Invalid reference to inactive account 'Equity:E'",
			"2020-01-19: Invalid reference to inactive account 'Assets:Cash'",
			"2020-01-19: Invalid reference to inactive account 'Equity:E'",
		}, messages(`
2020-02-01 open Assets:Cash
2020-02-01 open Equity:E
2020-01-19 pad Assets:Cash Equity:E
2020-03-03 balance Assets:Cash 10 USD
`))
	})

	t.Run("balance on an inactive account", func(t *testing.T) {
		got := messages(`
2020-02-01 open Assets:Cash
2020-01-01 open Equity:E
2020-01-15 * "early"
  Assets:Cash    10 USD
  Equity:E
2020-01-16 balance Assets:Cash 0 USD
`)
		assert.Equal(t, 3, len(got), "%v", got)
		assert.Equal(t, "2020-01-15: Invalid reference to inactive account 'Assets:Cash'", got[0])
		assert.Equal(t, "2020-01-16: Invalid reference to inactive account 'Assets:Cash'", got[1])
		assert.HasPrefix(t, got[2], "2020-01-16: Balance mismatch for Assets:Cash:")
	})

	t.Run("balance before the open, in a currency the open does not allow", func(t *testing.T) {
		assert.Equal(t, []string{
			"2020-01-16: Invalid reference to inactive account 'Assets:Cash'",
			"2020-01-16: Invalid currency 'EUR' for Balance directive: ",
		}, messages(`
2020-02-01 open Assets:Cash USD
2020-01-16 balance Assets:Cash 0 EUR
`))
	})

	t.Run("balance on an account never opened", func(t *testing.T) {
		assert.Equal(t, []string{
			"2020-01-07: Invalid reference to unknown account 'Assets:Never'",
		}, messages(`
2020-01-01 open Equity:E
2020-01-07 balance Assets:Never 0 USD
`))
	})

	t.Run("pad of an account never opened", func(t *testing.T) {
		assert.Equal(t, []string{
			"2020-01-07: Invalid reference to unknown account 'Assets:Never'",
			"2020-01-08: Invalid reference to unknown account 'Assets:Never'",
			"2020-01-07: Invalid reference to unknown account 'Assets:Never'",
		}, messages(`
2020-01-01 open Equity:E
2020-01-07 pad Assets:Never Equity:E
2020-01-08 balance Assets:Never 10 USD
`))
	})

	t.Run("pad from an account never opened", func(t *testing.T) {
		assert.Equal(t, []string{
			"2020-01-07: Invalid reference to unknown account 'Equity:Never'",
			"2020-01-07: Invalid reference to unknown account 'Equity:Never'",
		}, messages(`
2020-01-01 open Assets:Cash
2020-01-07 pad Assets:Cash Equity:Never
2020-01-08 balance Assets:Cash 10 USD
`))
	})
}

func TestLedger_CurrencyGroupErrorsReadAsBeancounts(t *testing.T) {
	// Like beancount's categorization and interpolation errors, the
	// messages carry no account; the diagnostics still do.
	tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:I
2020-01-01 open Assets:C
2020-01-01 open Assets:D
2020-01-01 open Income:G

2020-02-01 * "uncategorized"
  Assets:I  1 GOOG {7}
  Assets:C  -7 USD
  Assets:C  -1 EUR
  Income:G  1 EUR

2020-02-02 * "two autos"
  Assets:C  -7 USD
  Assets:D
  Income:G

2020-02-03 * "too many missing"
  Assets:I  1 GOOG {}
  Assets:C  -7 USD
  Assets:D
`)
	var validationErrors *ValidationErrors
	assert.True(t, errors.As(New().Process(context.Background(), tree), &validationErrors))

	var messages []string
	for _, err := range validationErrors.Errors {
		diagnostic := err.(*Diagnostic)
		assert.Equal(t, "CurrencyGroupError", diagnostic.kind)
		assert.NotZero(t, diagnostic.account)
		messages = append(messages, diagnostic.message)
	}
	assert.Equal(t, []string{
		"Failed to categorize posting 1",
		"You may not have more than one auto-posting per currency",
		"Too many missing numbers for currency group 'USD'",
	}, messages)
}

func TestLedger_CostNumberWithoutCurrency(t *testing.T) {
	// Like beancount's replace_currencies, a cost that states its number
	// but not its currency takes its Currency group's, so it is booked in
	// that currency; an error still carries the transaction as written.
	tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:I
2020-01-01 open Assets:C

2020-01-02 * "buy"
  Assets:I  5 HOOL {10}
  Assets:C  -50 USD

2020-01-02 * "buy more"
  Assets:I  5 HOOL {11}
  Assets:C  -55 USD

2020-01-03 * "sell"
  Assets:I  -6 HOOL {10}
  Assets:C  60 USD
`)
	l := New()
	var validationErrors *ValidationErrors
	assert.True(t, errors.As(l.Process(context.Background(), tree), &validationErrors))

	buy := tree.Directives[2].(*ast.Transaction)
	positions := l.BookedPositions(buy.Postings[0])
	assert.Equal(t, 1, len(positions))
	assert.Equal(t, "USD", positions[0].Cost.Currency)

	assert.Equal(t, 1, len(validationErrors.Errors))
	sell := validationErrors.Errors[0].(*Diagnostic)
	assert.Equal(t, `Not enough lots to reduce "-6 HOOL {10 USD}": 5 HOOL {10 USD, 2020-01-02}`, sell.message)
	assert.Equal(t, "", sell.directive.(*ast.Transaction).Postings[0].Cost.Amount.Currency, "the error shows the cost as written")
}

func TestLedger_CompoundCostMissingNumber(t *testing.T) {
	// Like beancount's COST_PER and COST_TOTAL, Booking interpolates the
	// part of a compound cost the source leaves out, and the lot's cost is
	// compute_cost_number's (total + per-unit × |units|) / |units|. Inside
	// total braces the per-unit number is reported and ignored.
	tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:I
2020-01-01 open Assets:S
2020-01-01 open Assets:C

2020-01-02 * "per-unit number left out"
  Assets:I  5 HOOL {# 5 USD}
  Assets:C  -30 USD

2020-01-03 * "total left out"
  Assets:I  5 GOOG {5 # USD}
  Assets:C  -30 USD

2020-01-04 * "short, per-unit number left out"
  Assets:S  -5 IBM {# 5 USD}
  Assets:C  30 USD

2020-01-05 * "total braces"
  Assets:I  5 AAPL {{5 # 3 USD}}
  Assets:C  -3 USD
`)
	l := New()
	_ = l.Process(context.Background(), tree)

	var costs []string
	for _, directive := range tree.Directives {
		if txn, ok := directive.(*ast.Transaction); ok {
			positions := l.BookedPositions(txn.Postings[0])
			assert.Equal(t, 1, len(positions))
			costs = append(costs, positions[0].Cost.Number.String()+" "+positions[0].Cost.Currency)
		}
	}
	assert.Equal(t, []string{"6 USD", "6 USD", "8 USD", "0.6 USD"}, costs)

	var messages []string
	for _, err := range l.Errors() {
		messages = append(messages, err.(*Diagnostic).message)
	}
	assert.Equal(t, []string{
		"Per-unit cost may not be specified using total cost syntax: " +
			"'CompoundAmount(number_per=Decimal('5'), number_total=Decimal('3'), currency='USD')'; ignoring per-unit cost",
		"Transaction does not balance: (-10 USD)",
	}, messages)
}

func TestLedger_PreciseInterpolation(t *testing.T) {
	// Interpolated numbers as beancount 3.2.3 books them, at the precision
	// it holds them; query/interpolation_precise.bql pins them against
	// beanquery.
	const transactions = `
2020-01-01 open Assets:A
2020-01-01 open Assets:B
2020-01-01 open Assets:C

2020-01-02 * "units"
  Assets:A  10.12345 USD
  Assets:B  -3.1 USD
  Assets:C

2020-01-03 * "one posting per currency"
  Assets:A  10.12345 USD
  Assets:B  -3.1 USD
  Assets:A  5.123 EUR
  Assets:B  -1.10 EUR
  Assets:C

2020-01-04 * "units held at cost"
  Assets:C  HOOL {3.00 USD}
  Assets:A  1.1 HOOL {3.00 USD}
  Assets:A  1.12345 HOOL {3.00 USD}
  Assets:B  -100.12 USD

2020-01-05 * "units at a price"
  Assets:C  EUR @ 3.00 USD
  Assets:A  1.1 EUR
  Assets:A  -1.12345 EUR
  Assets:B  -100.12 USD

2020-01-06 * "whole numbers leave the number as computed"
  Assets:A  10 HOOL @ 1.23456 USD
  Assets:B  -3 USD
  Assets:C
`
	tests := []struct {
		name    string
		options string
		want    []string
	}{
		{
			name: "off",
			want: []string{"-7.0 USD", "-7.0 USD", "-4.02 EUR", "31.1 HOOL", "33.4 EUR", "-9.34560 USD"},
		},
		{
			name:    "on",
			options: `option "use_precise_interpolation" "TRUE"`,
			want:    []string{"-7.02345 USD", "-7.02345 USD", "-4.023 EUR", "31.14988 HOOL", "33.37333 EUR", "-9.34560 USD"},
		},
		{
			name: "on: a quantum finer than the number pads it",
			options: `option "use_precise_interpolation" "TRUE"
option "tolerance_multiplier" "1.1"`,
			want: []string{"-7.023450 USD", "-7.023450 USD", "-4.0230 EUR", "31.149883 HOOL", "33.373333 EUR", "-9.34560 USD"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parser.MustParseString(context.Background(), tt.options+"\n"+transactions)
			l := New()
			_ = l.Process(context.Background(), tree)

			var got []string
			for _, directive := range tree.Directives {
				txn, ok := directive.(*ast.Transaction)
				if !ok {
					continue
				}
				for _, posting := range txn.Postings {
					if posting.Account == "Assets:C" {
						got = append(got, posting.Amount.Value+" "+posting.Amount.Currency)
					}
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLedger_InterpolatedCostKeepsItsExponent(t *testing.T) {
	// Like beancount, an interpolated cost is the weight divided by the
	// units with Python's decimal, which keeps the weight's exponent.
	tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:Invest
2020-01-01 open Assets:Cash

2020-01-02 * "per unit"
  Assets:Invest  10 HOOL {USD}
  Assets:Cash   -50.00 USD

2020-01-03 * "total"
  Assets:Invest  4 GOOG {{USD}}
  Assets:Cash   -30.00 USD
`)
	l := New()
	assert.NoError(t, l.Process(context.Background(), tree))

	var costs []string
	for _, directive := range tree.Directives {
		if txn, ok := directive.(*ast.Transaction); ok {
			positions := l.BookedPositions(txn.Postings[0])
			assert.Equal(t, 1, len(positions))
			costs = append(costs, formatInferredNumber(positions[0].Cost.Number))
		}
	}
	assert.Equal(t, []string{"5.00", "7.50"}, costs)
}
