package ledger

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

// newTestValidator is a helper for tests that need a validator with default config.
func newTestValidator(accounts map[string]*Account) *validator {
	return newValidator(accounts, nil, sharedconfig.New())
}

// bookAndValidate books txn against empty inventories and validates the
// result against the accounts, as Process does.
func bookAndValidate(accounts map[string]*Account, txn *ast.Transaction) ([]error, *bookedTransaction) {
	booked, errs := newBooker(sharedconfig.New(), newTolerances(nil), nil).book(txn)
	if len(errs) > 0 {
		return errs, nil
	}
	return newTestValidator(accounts).validateTransaction(context.Background(), txn, booked)
}

func TestValidateDateRange(t *testing.T) {
	tests := []struct {
		name      string
		dateStr   string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid year 2024",
			dateStr:   "2024-01-15",
			wantError: false,
		},
		{
			name:      "valid year 1 (minimum)",
			dateStr:   "0001-01-01",
			wantError: false,
		},
		{
			name:      "valid year 9999 (maximum)",
			dateStr:   "9999-12-31",
			wantError: false,
		},
		{
			name:      "valid year 100",
			dateStr:   "0100-06-15",
			wantError: false,
		},
		{
			name:      "valid year 1000",
			dateStr:   "1000-01-01",
			wantError: false,
		},
		{
			name:      "nil date is valid",
			dateStr:   "",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var date *ast.Date
			if tt.dateStr != "" {
				d, err := ast.NewDate(tt.dateStr)
				assert.NoError(t, err)
				date = d
			}

			err := validateDateRange(date)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}

	// Test year 0 separately - must use time.Date since ast.NewDate parses strings
	// and Go's time.Parse("2006-01-02", "0000-01-01") produces year 0
	t.Run("invalid year 0", func(t *testing.T) {
		date := ast.NewDateFromTime(time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC))
		err := validateDateRange(date)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "year 0 is out of range")
	})
}

func TestValidateAccountsOpen(t *testing.T) {
	// Setup test accounts
	date2024, _ := ast.NewDate("2024-01-15")
	date2025, _ := ast.NewDate("2025-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	openAccount := &Account{
		name:      checking,
		OpenDate:  date2024,
		inventory: newInventory(),
	}

	closedAccount := &Account{
		name:      checking,
		OpenDate:  date2024,
		CloseDate: date2024,
		inventory: newInventory(),
	}

	tests := []struct {
		name         string
		txn          *ast.Transaction
		accounts     map[string]*Account
		wantErrCount int
		wantErrType  string
	}{
		{
			name: "all accounts open",
			txn: ast.NewTransaction(date2025, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("-100", "USD")),
				),
			),
			accounts: map[string]*Account{
				"Assets:Checking": &Account{
					name:      checking,
					OpenDate:  date2024,
					inventory: newInventory(),
				},
				"Expenses:Groceries": &Account{
					name:      expenses,
					OpenDate:  date2024,
					inventory: newInventory(),
				},
			},
			wantErrCount: 0,
		},
		{
			name: "account not opened yet",
			txn: ast.NewTransaction(date2024, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
				),
			),
			accounts:     map[string]*Account{}, // No accounts
			wantErrCount: 1,
			wantErrType:  "AccountNotOpenError",
		},
		{
			name: "account closed",
			txn: ast.NewTransaction(date2025, "Test", // After close date
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
				),
			),
			accounts: map[string]*Account{
				"Assets:Checking": closedAccount,
			},
			wantErrCount: 1,
			wantErrType:  "AccountNotOpenError",
		},
		{
			name: "multiple errors - both postings to closed accounts",
			txn: ast.NewTransaction(date2025, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("-100", "USD")),
				),
			),
			accounts: map[string]*Account{
				"Assets:Checking": &Account{
					name:      checking,
					OpenDate:  date2024,
					CloseDate: date2024,
					inventory: newInventory(),
				},
				"Expenses:Groceries": &Account{
					name:      expenses,
					OpenDate:  date2024,
					CloseDate: date2024,
					inventory: newInventory(),
				},
			},
			wantErrCount: 2,
		},
		{
			name: "account open on exact open date",
			txn: ast.NewTransaction(date2024, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
				),
			),
			accounts: map[string]*Account{
				"Assets:Checking": openAccount,
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestValidator(tt.accounts)
			errs := v.validateAccountsOpen(tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrType != "" && len(errs) > 0 {
				// Check error type matches
				assert.Equal(t, tt.wantErrType, kindOf(errs[0]))
			}
		})
	}
}

func TestValidateTransaction_Integration(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")
	closed, _ := ast.NewAccount("Assets:OldAccount")

	// Setup accounts for validator
	accounts := map[string]*Account{
		"Assets:Checking": {
			name:      checking,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Expenses:Groceries": {
			name:      expenses,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Assets:OldAccount": {
			name:      closed,
			OpenDate:  date,
			CloseDate: date,
			inventory: newInventory(),
		},
	}

	tests := []struct {
		name              string
		txn               *ast.Transaction
		wantErrCount      int
		wantBalanceResult bool
	}{
		{
			name: "valid balanced transaction",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantErrCount:      0,
			wantBalanceResult: true,
		},
		{
			name: "transaction with closed account",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(closed, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantErrCount:      0, // Allowed on close date
			wantBalanceResult: true,
		},
		{
			name: "unbalanced transaction",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-40.00", "USD")),
				),
			),
			wantErrCount:      1,
			wantBalanceResult: false,
		},
		{
			name: "transaction with amount inference",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking),
				),
			),
			wantErrCount:      0,
			wantBalanceResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, result := bookAndValidate(accounts, tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantBalanceResult {
				assert.NotEqual(t, nil, result)
			}
		})
	}
}

// TestImplicitPostings tests the implicit posting (amount interpolation) feature.
// Beancount allows exactly one posting per transaction to have no amount specified.
// The missing amount is automatically calculated to make the transaction balance to zero.
func TestImplicitPostingPerCurrency(t *testing.T) {
	source := `
2024-01-01 open Assets:Cash
2024-01-01 open Assets:Euro
2024-01-01 open Expenses:Travel

2024-01-15 * "Trip"
  Expenses:Travel  200.00 EUR
  Expenses:Travel   50.00 USD
  Assets:Euro     -200.00 EUR
  Assets:Cash

2024-01-16 * "Fully balanced"
  Expenses:Travel   10.00 USD
  Assets:Euro      -10.00 USD
  Assets:Cash

2024-01-17 * "Split"
  Expenses:Travel   20.00 EUR
  Expenses:Travel    5.00 USD
  Assets:Cash
`
	tree, err := parser.ParseString(context.Background(), source)
	assert.NoError(t, err)
	l := New()
	tree = l.MustProcess(context.Background(), tree)

	var booked []string
	for _, directive := range tree.Directives {
		txn, ok := directive.(*ast.Transaction)
		if !ok {
			continue
		}
		for _, posting := range txn.Postings {
			booked = append(booked, fmt.Sprintf("%s %s %s", posting.Account, posting.Amount.Value, posting.Amount.Currency))
		}
	}
	// Assets:Cash is booked once per currency with a non-zero residual: USD
	// only in the trip (EUR nets to zero), not at all when fully balanced,
	// and in both currencies in the split. Booked postings are grouped by
	// currency, like beancount's booking.
	assert.Equal(t, []string{
		"Expenses:Travel 200.00 EUR",
		"Assets:Euro -200.00 EUR",
		"Expenses:Travel 50.00 USD",
		"Assets:Cash -50.00 USD",
		"Expenses:Travel 10.00 USD",
		"Assets:Euro -10.00 USD",
		"Expenses:Travel 20.00 EUR",
		"Assets:Cash -20.00 EUR",
		"Expenses:Travel 5.00 USD",
		"Assets:Cash -5.00 USD",
	}, booked)

	cash, ok := l.GetAccount("Assets:Cash")
	assert.True(t, ok)
	assert.Equal(t, "-55", cash.inventory.get("USD").String())
	assert.Equal(t, "-20", cash.inventory.get("EUR").String())
}

func TestImplicitPostings(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	deposit, _ := ast.NewAccount("Assets:Deposit")
	savings, _ := ast.NewAccount("Assets:Savings")
	expenses, _ := ast.NewAccount("Expenses:Food")
	income, _ := ast.NewAccount("Income:Salary")
	multiCurr, _ := ast.NewAccount("Assets:MultiCurr")

	// Setup accounts for validator
	accounts := map[string]*Account{
		"Assets:Checking": {
			name:      checking,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Assets:Deposit": {
			name:      deposit,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Assets:Savings": {
			name:      savings,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Expenses:Food": {
			name:      expenses,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Income:Salary": {
			name:      income,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Assets:MultiCurr": {
			name:      multiCurr,
			OpenDate:  date,
			inventory: newInventory(),
		},
	}

	tests := []struct {
		name            string
		txn             *ast.Transaction
		wantErrCount    int
		wantBalanced    bool
		wantInferred    bool
		wantInferredAmt string // Expected inferred amount value
		wantBookedAs    int    // Postings the inferred one is booked as; 1 when zero
	}{
		{
			name: "implicit posting - simple two-way transfer",
			txn: ast.NewTransaction(date, "Transfer",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("-400", "EUR")),
					ast.NewPosting(deposit), // Amount should be inferred as 400 EUR
				),
			),
			wantErrCount:    0,
			wantBalanced:    true,
			wantInferred:    true,
			wantInferredAmt: "400",
		},
		{
			name: "implicit posting - three-way split with implicit last",
			txn: ast.NewTransaction(date, "Split",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("-100", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("60", "USD")),
					ast.NewPosting(savings), // Should be inferred as 40 USD
				),
			),
			wantErrCount:    0,
			wantBalanced:    true,
			wantInferred:    true,
			wantInferredAmt: "40",
		},
		{
			name: "implicit posting - negative inferred amount",
			txn: ast.NewTransaction(date, "Expense split",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("500", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("300", "USD")),
					ast.NewPosting(savings), // Should be inferred as -800 USD
				),
			),
			wantErrCount:    0,
			wantBalanced:    true,
			wantInferred:    true,
			wantInferredAmt: "-800",
		},
		{
			name: "error: multiple postings without amounts",
			txn: ast.NewTransaction(date, "Ambiguous",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("-100", "USD")),
					ast.NewPosting(deposit), // First missing
					ast.NewPosting(savings), // Second missing
				),
			),
			wantErrCount: 1,
			wantBalanced: false,
			wantInferred: false,
		},
		{
			name: "implicit posting with multiple currencies",
			txn: ast.NewTransaction(date, "Multi-currency",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("-100", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("60", "EUR")),
					ast.NewPosting(multiCurr), // Booked once per currency; see TestImplicitPostingPerCurrency
				),
			),
			wantErrCount:    0,
			wantBalanced:    true,
			wantInferred:    true,
			wantInferredAmt: "100",
			wantBookedAs:    2,
		},
		{
			name: "implicit posting - income transaction",
			txn: ast.NewTransaction(date, "Paycheck",
				ast.WithPostings(
					ast.NewPosting(income, ast.WithAmount("-5000", "USD")),
					ast.NewPosting(checking, ast.WithAmount("4500", "USD")),
					ast.NewPosting(expenses), // Taxes: should be inferred as 500 USD
				),
			),
			wantErrCount:    0,
			wantBalanced:    true,
			wantInferred:    true,
			wantInferredAmt: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, result := bookAndValidate(accounts, tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs), fmt.Sprintf("errors: %v", errs))

			inferredCount := 0
			var inferredAmount *ast.Amount
			for _, posting := range tt.txn.Postings {
				if posting.Inferred {
					inferredCount++
					if inferredAmount == nil {
						inferredAmount = posting.Amount
					}
				}
			}

			if tt.wantInferred {
				assert.Equal(t, max(tt.wantBookedAs, 1), inferredCount, "inferred postings")
				if inferredAmount != nil {
					assert.Equal(t, tt.wantInferredAmt, inferredAmount.Value)
				}
			} else {
				assert.Equal(t, 0, inferredCount, "expected no inferred postings")
			}

			if tt.wantBalanced {
				assert.NotEqual(t, nil, result, "expected validation to pass")
			}
		})
	}
}

// Benchmark validation functions
func BenchmarkValidateTransaction(b *testing.B) {
	// Setup
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	txn := ast.NewTransaction(date, "Benchmark",
		ast.WithPostings(
			ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
			ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
		),
	)

	accounts := map[string]*Account{
		"Assets:Checking": {
			name:      checking,
			OpenDate:  date,
			inventory: newInventory(),
		},
		"Expenses:Groceries": {
			name:      expenses,
			OpenDate:  date,
			inventory: newInventory(),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bookAndValidate(accounts, txn)
	}
}

func TestValidateMetadata(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	tests := []struct {
		name         string
		txn          *ast.Transaction
		wantErrCount int
		wantErrMsg   string
	}{
		{
			name: "no metadata - valid",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name: "valid transaction metadata",
			txn: func() *ast.Transaction {
				txn := ast.NewTransaction(date, "Test",
					ast.WithPostings(
						ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
						ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
					),
				)
				inv123 := ast.NewRawString("INV-123")
				txn.Metadata = []*ast.Metadata{
					{Key: "invoice", Value: &ast.MetadataValue{StringValue: &inv123}},
				}
				return txn
			}(),
			wantErrCount: 0,
		},
		{
			name: "duplicate metadata keys",
			txn: func() *ast.Transaction {
				txn := ast.NewTransaction(date, "Test",
					ast.WithPostings(
						ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
						ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
					),
				)
				inv123 := ast.NewRawString("INV-123")
				inv456 := ast.NewRawString("INV-456")
				txn.Metadata = []*ast.Metadata{
					{Key: "invoice", Value: &ast.MetadataValue{StringValue: &inv123}},
					{Key: "invoice", Value: &ast.MetadataValue{StringValue: &inv456}},
				}
				return txn
			}(),
			wantErrCount: 1,
			wantErrMsg:   "duplicate key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &validator{accounts: make(map[string]*Account)}
			errs := v.validateMetadata(tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrMsg != "" && len(errs) > 0 {
				assert.Contains(t, errs[0].Error(), tt.wantErrMsg)
			}
		})
	}
}

func BenchmarkValidateMetadata(b *testing.B) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	txn := ast.NewTransaction(date, "Test",
		ast.WithPostings(
			ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
			ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
		),
	)
	inv123 := ast.NewRawString("INV-123")
	food := ast.NewRawString("food")
	txn.Metadata = []*ast.Metadata{
		{Key: "invoice", Value: &ast.MetadataValue{StringValue: &inv123}},
		{Key: "category", Value: &ast.MetadataValue{StringValue: &food}},
	}

	v := &validator{accounts: make(map[string]*Account)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.validateMetadata(txn)
	}
}

// TestEmptyCostBehavior tests empty cost {} augmentation vs reduction behavior.
// In beancount, empty costs {} have different meanings:
// - Positive amount: augments position by inferring cost from residual
// - Negative amount: reduces position using booking method (FIFO/LIFO)
func TestEmptyCostBehavior(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "positive amount with {} infers cost from residual",
			input: `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD

2020-01-02 * "Buy stock with empty cost"
  Assets:Brokerage    10 STOCK {}
  Assets:Cash        -1000 USD
`,
			wantErr: false,
		},
		{
			name: "negative amount with {} uses FIFO booking",
			input: `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy stock"
  Assets:Brokerage    10 STOCK {100 USD}
  Assets:Cash        -1000 USD

2020-01-03 * "Sell stock with empty cost (FIFO)"
  Assets:Brokerage    -5 STOCK {}
  Assets:Cash         600 USD
  Income:CapitalGains    -100 USD
`,
			wantErr: false,
		},
		{
			name: "negative amount with {} on LIFO account",
			input: `
2020-01-01 open Assets:Brokerage "LIFO"
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy first lot"
  Assets:Brokerage    10 STOCK {100 USD}
  Assets:Cash        -1000 USD

2020-01-03 * "Buy second lot"
  Assets:Brokerage    10 STOCK {110 USD}
  Assets:Cash        -1100 USD

2020-01-04 * "Sell stock with empty cost (LIFO - newest first)"
  Assets:Brokerage    -5 STOCK {}
  Assets:Cash         560 USD
  Income:CapitalGains    -10 USD
`,
			wantErr: false,
		},
		{
			name: "zero amount with {} does not panic on cost inference",
			input: `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD

2020-01-02 * "Zero amount with empty cost"
  Assets:Brokerage    0 STOCK {}
  Assets:Cash        0 USD
`,
			wantErr: false,
		},
		{
			name: "multiple empty costs - cannot infer costs unambiguously",
			input: `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD

2020-01-02 * "Multiple empty costs for different commodities"
  Assets:Brokerage    10 STOCK {}
  Assets:Brokerage    5 AAPL {}
  Assets:Cash        -2000 USD
`,
			wantErr: true, // Beancount cannot infer costs when multiple postings have empty cost specs
		},
		{
			name: "FIFO insufficient inventory - cannot reduce more than available",
			input: `
2020-01-01 open Assets:Brokerage "FIFO"
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy stock"
  Assets:Brokerage    10 STOCK {100 USD}
  Assets:Cash        -1000 USD

2020-01-03 * "Try to sell more than available"
  Assets:Brokerage    -20 STOCK {}
  Assets:Cash         2000 USD
  Income:CapitalGains    -2000 USD
`,
			wantErr: true, // Beancount error: trying to reduce 20 shares when only 10 available
		},
		{
			name: "{} reduction resolves weight from booked lot for interpolation",
			input: `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy stock"
  Assets:Brokerage    10 STOCK {100 USD}
  Assets:Cash        -1000 USD

2020-01-03 * "Sell with auto gains posting"
  Assets:Brokerage    -5 STOCK {}
  Assets:Cash         600 USD
  Income:CapitalGains

2020-01-04 balance Income:CapitalGains -100 USD
`,
			wantErr: false, // gains inferred from lot cost basis, not the full proceeds
		},
		{
			name: "NONE booking interpolates {} cost from residual",
			input: `
option "booking_method" "NONE"
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy stock"
  Assets:Brokerage    10 STOCK {100 USD}
  Assets:Cash        -1000 USD

2020-01-03 * "Sell, cost interpolated as 100"
  Assets:Brokerage    -5 STOCK {}
  Assets:Cash         600 USD
  Income:CapitalGains    -100 USD
`,
			wantErr: false, // verified against bean-check 2.3.6
		},
		{
			name: "NONE booking with {} and auto posting has too many unknowns",
			input: `
option "booking_method" "NONE"
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy stock"
  Assets:Brokerage    10 STOCK {100 USD}
  Assets:Cash        -1000 USD

2020-01-03 * "Sell with two unknowns"
  Assets:Brokerage    -5 STOCK {}
  Assets:Cash         600 USD
  Income:CapitalGains
`,
			wantErr: true, // beancount: "Too many missing numbers for currency group"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			_, err := processErr(context.Background(), l, ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPadTiming tests pad directive timing rules.
// In beancount, pad directives must be processed before the balance assertion;
// same-date balance assertions run before pads and therefore do not consume them.
func TestPadTiming(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name: "pad before balance - valid",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-02 pad Assets:Checking Equity:Opening
2020-01-05 balance Assets:Checking 100 USD
`,
			wantErr: false,
		},
		{
			name: "pad on same date as balance - balance runs before pad",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-05 pad Assets:Checking Equity:Opening
2020-01-05 balance Assets:Checking 100 USD
`,
			wantErr: true,
			errMsg:  "Balance mismatch",
		},
		{
			name: "pad after balance - balance fails without pad",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-05 balance Assets:Checking 100 USD
2020-01-06 pad Assets:Checking Equity:Opening
`,
			wantErr: true,
			errMsg:  "Balance mismatch",
		},
		{
			name: "multiple pads for same account - superseded pads are unused errors",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-02 pad Assets:Checking Equity:Opening
2020-01-03 pad Assets:Checking Equity:Opening
2020-01-04 pad Assets:Checking Equity:Opening
2020-01-05 balance Assets:Checking 100 USD
`,
			wantErr: true, // bean-check: "Unused Pad entry" for each superseded pad
		},
		{
			name: "pad without subsequent balance - generates warning",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-02 pad Assets:Checking Equity:Opening
`,
			wantErr: true,
			errMsg:  "Unused Pad entry",
		},
		{
			name: "pad then transaction then balance",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening
2020-01-01 open Expenses:Groceries USD

2020-01-02 pad Assets:Checking Equity:Opening
2020-01-03 * "Spend some money"
  Assets:Checking    -50 USD
  Expenses:Groceries  50 USD
2020-01-05 balance Assets:Checking 50 USD
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			_, err := processErr(context.Background(), l, ast)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestBalanceTolerance checks a balance assertion against its account's
// balance within the assertion's tolerance: twice the multiplier on the
// asserted number's last digit, or the tolerance it states after ~.
func TestBalanceTolerance(t *testing.T) {
	tests := []struct {
		name      string
		asserted  string
		tolerance string // stated after ~
		actual    string
		wantErr   bool
	}{
		{name: "balance matches exactly", asserted: "100.00", actual: "100.00"},
		{name: "balance within inferred tolerance", asserted: "100.00", actual: "100.004"},
		{name: "integer precision balance is exact", asserted: "100", actual: "100.4", wantErr: true},
		{name: "a difference of the doubled tolerance passes", asserted: "100.00", actual: "100.01"},
		{name: "balance exceeds tolerance", asserted: "100.00", actual: "100.02", wantErr: true},
		{name: "balance uses local tolerance override", asserted: "100.00", tolerance: "0.02", actual: "100.01"},
		{name: "local tolerance override can be exceeded", asserted: "100.00", tolerance: "0.002", actual: "100.004", wantErr: true},
		{name: "balance assertion of exactly 0", asserted: "0", actual: "0"},
		{name: "negative balance within tolerance", asserted: "-50.00", actual: "-50.003"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date := newTestDate("2020-01-02")
			account := &Account{name: "Assets:Checking", OpenDate: date, inventory: newInventory()}
			account.inventory.addLot("USD", decimal.RequireFromString(tt.actual), nil)
			balance := ast.NewBalance(date, account.name, ast.NewAmount(tt.asserted, "USD"))
			if tt.tolerance != "" {
				balance.Tolerance = ast.NewAmount(tt.tolerance, "USD")
			}

			v := newValidator(map[string]*Account{string(account.name): account}, map[string]*ast.Open{string(account.name): ast.NewOpen(date, account.name, nil, "")}, sharedconfig.New())
			tolerance, err := newTolerances(nil).balance(balance)
			assert.NoError(t, err)
			errs := v.checkBalance(balance, account.inventory.get("USD"), tolerance)
			assert.Equal(t, tt.wantErr, len(errs) > 0, "errors: %v", errs)
		})
	}

	t.Run("tolerance applied after padding", func(t *testing.T) {
		tree := parser.MustParseString(context.Background(), `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-02 * "Deposit"
  Assets:Checking    50.004 USD
  Equity:Opening    -50.004 USD

2020-01-03 pad Assets:Checking Equity:Opening
2020-01-04 balance Assets:Checking 100.00 USD
`)
		New().MustProcess(context.Background(), tree)
	})
}

// TestConstraintCurrencyEnforcement tests that currency constraints are enforced
// for both explicit and inferred amounts.
func TestConstraintCurrencyEnforcement(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "explicit amount violates constraint - should error",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-02 * "Invalid currency"
  Assets:Checking    100 EUR
  Equity:Opening    -100 EUR
`,
			wantErr: true,
		},
		{
			name: "inferred amount violates constraint - should error",
			input: `
2020-01-01 open Assets:Checking USD
2020-01-01 open Equity:Opening

2020-01-02 * "Inferred EUR violates USD constraint"
  Assets:Checking
  Equity:Opening    -100 EUR
`,
			wantErr: true,
		},
		{
			name: "explicit and inferred amounts both validated",
			input: `
2020-01-01 open Assets:Checking USD, EUR
2020-01-01 open Equity:Opening

2020-01-02 * "Multiple currencies OK"
  Assets:Checking    100 USD
  Assets:Checking    50 EUR
  Equity:Opening    -100 USD
  Equity:Opening    -50 EUR
`,
			wantErr: false,
		},
		{
			name: "no constraint allows any currency",
			input: `
2020-01-01 open Assets:Checking
2020-01-01 open Equity:Opening

2020-01-02 * "Any currency OK"
  Assets:Checking    100 XYZ
  Equity:Opening    -100 XYZ
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast := parser.MustParseString(context.Background(), tt.input)

			l := New()
			_, err := processErr(context.Background(), l, ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateOpen tests the validateOpen() function
func TestValidateOpen(t *testing.T) {
	date2024, _ := ast.NewDate("2024-01-15")
	date2025, _ := ast.NewDate("2025-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")

	tests := []struct {
		name              string
		accounts          map[string]*Account
		open              *ast.Open
		wantErrCount      int
		wantMetadataCopy  bool
		wantConstraintLen int
	}{
		{
			name:         "valid open directive",
			accounts:     map[string]*Account{},
			open:         ast.NewOpen(date2024, checking, nil, ""),
			wantErrCount: 0,
		},
		{
			name: "account already open",
			accounts: map[string]*Account{
				"Assets:Checking": {
					name:      checking,
					OpenDate:  date2024,
					inventory: newInventory(),
				},
			},
			open:         ast.NewOpen(date2025, checking, nil, ""),
			wantErrCount: 1,
		},
		{
			name: "reopening closed account - error (duplicate open)",
			accounts: map[string]*Account{
				"Assets:Checking": {
					name:      checking,
					OpenDate:  date2024,
					CloseDate: date2024,
					inventory: newInventory(),
				},
			},
			open:         ast.NewOpen(date2025, checking, nil, ""),
			wantErrCount: 1, // Beancount does NOT allow reopening - duplicate open is an error
		},
		{
			name:     "metadata copying",
			accounts: map[string]*Account{},
			open: func() *ast.Open {
				open := ast.NewOpen(date2024, checking, nil, "")
				note := ast.NewRawString("Test account")
				open.Metadata = []*ast.Metadata{
					{Key: "note", Value: &ast.MetadataValue{StringValue: &note}},
				}
				return open
			}(),
			wantErrCount:     0,
			wantMetadataCopy: true,
		},
		{
			name:              "constraint currencies copying",
			accounts:          map[string]*Account{},
			open:              ast.NewOpen(date2024, checking, []string{"USD", "EUR"}, ""),
			wantErrCount:      0,
			wantConstraintLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestValidator(tt.accounts)
			errs, delta := v.validateOpen(context.Background(), tt.open)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrCount == 0 && delta != nil {
				if tt.wantMetadataCopy {
					assert.True(t, delta.hasMetadata(), "expected metadata on delta")
				}

				if tt.wantConstraintLen > 0 {
					assert.Equal(t, tt.wantConstraintLen, len(delta.constraintCurrencies))
				}

				// Verify no shared references
				if tt.open.HasMetadata() && delta.hasMetadata() {
					// Check that the slices don't point to the same backing array by checking addresses
					// Using %p format to get pointer addresses as strings
					openPtr := fmt.Sprintf("%p", &tt.open.Metadata[0])
					deltaPtr := fmt.Sprintf("%p", &delta.metadata[0])
					assert.NotEqual(t, openPtr, deltaPtr)
				}

				if len(tt.open.ConstraintCurrencies) > 0 && len(delta.constraintCurrencies) > 0 {
					// Modify delta's copy to verify independence
					originalFirst := tt.open.ConstraintCurrencies[0]
					delta.constraintCurrencies[0] = "TEST"
					assert.Equal(t, originalFirst, tt.open.ConstraintCurrencies[0])
				}
			}
		})
	}
}

// TestValidateOpenWithCustomAccountTypes tests account type validation with custom names
func TestValidateOpenWithCustomAccountTypes(t *testing.T) {
	date2024, _ := ast.NewDate("2024-01-15")
	// Create account directly without using NewAccount (which validates against hardcoded types)
	customAccount := ast.Account("Vermoegen:Checking")

	t.Run("valid custom account type", func(t *testing.T) {
		cfg := sharedconfig.New()
		cfg.AccountNames.Assets = "Vermoegen"

		v := newValidator(map[string]*Account{}, nil, cfg)
		errs, delta := v.validateOpen(context.Background(), ast.NewOpen(date2024, customAccount, nil, ""))

		assert.Equal(t, 0, len(errs))
		assert.True(t, delta != nil)
		assert.Equal(t, customAccount, delta.account)
	})

	t.Run("invalid custom account type", func(t *testing.T) {
		cfg := sharedconfig.New()
		// Don't set custom Vermoegen - should reject it

		v := newValidator(map[string]*Account{}, nil, cfg)
		errs, delta := v.validateOpen(context.Background(), ast.NewOpen(date2024, customAccount, nil, ""))

		assert.Equal(t, 1, len(errs))
		assert.True(t, delta == nil)
		assert.True(t, strings.Contains(errs[0].Error(), "invalid type"))
	})
}

// TestValidateClose tests the validateClose() function
func TestValidateClose(t *testing.T) {
	date2024, _ := ast.NewDate("2024-01-15")
	date2025, _ := ast.NewDate("2025-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")

	tests := []struct {
		name         string
		accounts     map[string]*Account
		close        *ast.Close
		wantErrCount int
		wantErrType  string
	}{
		{
			name:         "closing non-existent account",
			accounts:     map[string]*Account{},
			close:        ast.NewClose(date2024, checking),
			wantErrCount: 1,
			wantErrType:  "AccountNotClosedError",
		},
		{
			name: "closing already closed account",
			accounts: map[string]*Account{
				"Assets:Checking": {
					name:      checking,
					OpenDate:  date2024,
					CloseDate: date2024,
					inventory: newInventory(),
				},
			},
			close:        ast.NewClose(date2025, checking),
			wantErrCount: 1,
			wantErrType:  "AccountAlreadyClosedError",
		},
		{
			name: "valid close directive",
			accounts: map[string]*Account{
				"Assets:Checking": {
					name:      checking,
					OpenDate:  date2024,
					inventory: newInventory(),
				},
			},
			close:        ast.NewClose(date2025, checking),
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestValidator(tt.accounts)
			errs, delta := v.validateClose(context.Background(), tt.close)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrType != "" && len(errs) > 0 {
				errType := kindOf(errs[0])
				assert.Equal(t, tt.wantErrType, errType)
			}

			if tt.wantErrCount == 0 {
				assert.NotEqual(t, nil, delta)
			}
		})
	}
}

// TestBookingReductions checks how Booking matches reductions to the lots
// an account holds.
func TestBookingReductions(t *testing.T) {
	tests := []struct {
		name     string
		held     string // Units the stock account holds at 50 or 60 USD
		posting  string
		wantErrs int
		wantType string
	}{
		{"sufficient inventory", "100 HOOL {50.00 USD}", "-10 HOOL {50.00 USD}", 0, ""},
		{"insufficient lots", "5 HOOL {50.00 USD}", "-10 HOOL {50.00 USD}", 1, "InsufficientInventoryError"},
		{"lot not found", "100 HOOL {60.00 USD}", "-10 HOOL {50.00 USD}", 1, "InsufficientInventoryError"},
		{"empty cost spec uses booking method", "100 HOOL {50.00 USD}", "-10 HOOL {}", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf(`
2024-01-01 open Assets:Checking
2024-01-01 open Assets:Stock "FIFO"

2024-01-02 * "buy"
  Assets:Stock  %s
  Assets:Checking

2024-01-15 * "sell"
  Assets:Stock  %s
  Assets:Checking
`, tt.held, tt.posting)
			tree := parser.MustParseString(context.Background(), source)
			l := New()
			_, _ = l.Process(context.Background(), tree)

			errs := l.Errors()
			assert.Equal(t, tt.wantErrs, len(errs), "errors: %v", errs)
			if tt.wantType != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantType, kindOf(errs[0]))
			}
		})
	}
}

func TestBookingDropsAFailedGroupsReductions(t *testing.T) {
	// The second -6 cannot be booked once the first has reduced the lot, so
	// the group is dropped; like bean-check, the sale of all 10 HOOL still
	// books, because the failed group left the lot untouched.
	source := `
2020-01-01 open Assets:Stock
2020-01-01 open Assets:Cash

2020-01-02 * "buy"
  Assets:Stock  10 HOOL {100 USD}
  Assets:Cash

2020-01-03 * "double reduce"
  Assets:Stock  -6 HOOL {}
  Assets:Stock  -6 HOOL {}
  Assets:Cash   1200 USD

2020-01-04 * "sell all"
  Assets:Stock  -10 HOOL {}
  Assets:Cash   1000 USD
`
	tree := parser.MustParseString(context.Background(), source)
	l := New()
	_, _ = l.Process(context.Background(), tree)

	errs := l.Errors()
	assert.Equal(t, 1, len(errs), "errors: %v", errs)
	assert.Contains(t, errs[0].Error(), `"-6 HOOL {}": 4 HOOL {100 USD, 2020-01-02}`)

	stock, ok := l.GetAccount("Assets:Stock")
	assert.True(t, ok)
	assert.True(t, stock.inventory.isEmpty(), "inventory: %s", stock.inventory)
}

func TestBookingMethodSemantics(t *testing.T) {
	tests := []struct {
		name                    string
		input                   string
		wantErr                 bool
		wantAmbiguousBookingErr bool
	}{
		{
			name: "STRICT ambiguous reduction errors",
			input: `
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy lot 1"
  Assets:Brokerage       10 HOOL {100 USD, 2020-01-02}
  Assets:Cash         -1000 USD

2020-01-03 * "Buy lot 2"
  Assets:Brokerage       10 HOOL {110 USD, 2020-01-03}
  Assets:Cash         -1100 USD

2020-01-04 * "Sell"
  Assets:Brokerage       -5 HOOL {}
  Assets:Cash           500 USD
  Income:CapitalGains  -500 USD
`,
			wantErr:                 true,
			wantAmbiguousBookingErr: true,
		},
		{
			name: "global NONE allows mixed-sign inventory",
			input: `
option "booking_method" "NONE"
2020-01-01 open Assets:Brokerage
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy lot 1"
  Assets:Brokerage       10 HOOL {100 USD, 2020-01-02}
  Assets:Cash         -1000 USD

2020-01-03 * "Buy lot 2"
  Assets:Brokerage       10 HOOL {110 USD, 2020-01-03}
  Assets:Cash         -1100 USD

2020-01-04 * "Sell"
  Assets:Brokerage       -5 HOOL {}
  Assets:Cash           500 USD
  Income:CapitalGains  -500 USD
`,
			wantErr: false,
		},
		{
			name: "per-account NONE overrides global STRICT",
			input: `
option "booking_method" "STRICT"
2020-01-01 open Assets:Brokerage "NONE"
2020-01-01 open Assets:Cash USD
2020-01-01 open Income:CapitalGains

2020-01-02 * "Buy lot 1"
  Assets:Brokerage       10 HOOL {100 USD, 2020-01-02}
  Assets:Cash         -1000 USD

2020-01-03 * "Buy lot 2"
  Assets:Brokerage       10 HOOL {110 USD, 2020-01-03}
  Assets:Cash         -1100 USD

2020-01-04 * "Sell"
  Assets:Brokerage       -5 HOOL {}
  Assets:Cash           500 USD
  Income:CapitalGains  -500 USD
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parser.MustParseString(context.Background(), tt.input)

			l := New()
			_, err := processErr(context.Background(), l, tree)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantAmbiguousBookingErr {
					errs := l.Errors()
					assert.True(t, len(errs) > 0)
					ok := kindOf(errs[0]) == "AmbiguousBookingError"
					assert.True(t, ok)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

// TestValidateConstraintCurrencies tests the validateConstraintCurrencies() function
func TestValidateConstraintCurrencies(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	tests := []struct {
		name          string
		constraints   []string
		txn           *ast.Transaction
		setupInferred bool // whether to simulate inferred amounts
		inferCurrency string
		wantErrCount  int
	}{
		{
			name:        "no constraint (passes)",
			constraints: nil,
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("-100", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name:        "allowed currency explicit amount (passes)",
			constraints: []string{"USD", "EUR"},
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "USD")),
					ast.NewPosting(expenses, ast.WithAmount("-100", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name:        "disallowed currency explicit amount (error)",
			constraints: []string{"USD", "EUR"},
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking, ast.WithAmount("100", "GBP")),
					ast.NewPosting(expenses, ast.WithAmount("-100", "GBP")),
				),
			),
			wantErrCount: 1,
		},
		{
			name:          "allowed currency inferred amount (passes)",
			constraints:   []string{"USD", "EUR"},
			setupInferred: true,
			inferCurrency: "USD",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking),
					ast.NewPosting(expenses, ast.WithAmount("-100", "USD")),
				),
			),
			wantErrCount: 0,
		},
		{
			name:          "disallowed currency inferred amount (error)",
			constraints:   []string{"USD", "EUR"},
			setupInferred: true,
			inferCurrency: "GBP",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking),
					ast.NewPosting(expenses, ast.WithAmount("-100", "GBP")),
				),
			),
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := map[string]*Account{
				"Assets:Checking": {
					name:                 checking,
					OpenDate:             date,
					inventory:            newInventory(),
					constraintCurrencies: tt.constraints,
				},
				"Expenses:Groceries": {
					name:      expenses,
					OpenDate:  date,
					inventory: newInventory(),
				},
			}

			// Setup inferred amounts directly on postings
			if tt.setupInferred {
				for _, posting := range tt.txn.Postings {
					if posting.Amount == nil {
						posting.Amount = ast.NewAmount("100", tt.inferCurrency)
						posting.Inferred = true
					}
				}
			}

			v := newTestValidator(accounts)
			errs := v.validateConstraintCurrencies(tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))
		})
	}
}

func TestOverReductionReportsNotEnoughLots(t *testing.T) {
	// Selling more than the lots hold is a booking error, reported alone:
	// the posting's weight is unknown, so the balance is not checked.
	for _, spec := range []string{"{}", "{10 USD}"} {
		source := "2020-01-01 open Assets:Stock \"FIFO\"\n2020-01-01 open Assets:Cash\n" +
			"2020-01-02 * \"buy\"\n  Assets:Stock  1 HOOL {10 USD}\n  Assets:Cash  -10 USD\n" +
			"2020-01-04 * \"sell\"\n  Assets:Stock  -2 HOOL " + spec + "\n  Assets:Cash  20 USD\n"
		tree, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)
		l := New()
		_, validationErrors := processDiagnostics(t, l, tree)
		assert.Equal(t, 1, len(validationErrors), spec)
		assert.Equal(t, "InsufficientInventoryError", kindOf(validationErrors[0]), spec)
		insufficient := validationErrors[0]
		assert.Equal(t, `Not enough lots to reduce "-2 HOOL `+spec+`": 1 HOOL {10 USD, 2020-01-02}`, insufficient.(*Diagnostic).message, spec)

		stock, _ := l.GetAccount("Assets:Stock")
		assert.Equal(t, "1", stock.inventory.get("HOOL").String(), "the failed sale changes nothing")
	}
}
