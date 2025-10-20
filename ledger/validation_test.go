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

func TestValidateAccountsOpen(t *testing.T) {
	// Setup test accounts
	date2024, _ := ast.NewDate("2024-01-15")
	date2025, _ := ast.NewDate("2025-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	openAccount := &Account{
		Name:      checking,
		OpenDate:  date2024,
		Inventory: NewInventory(),
	}

	closedAccount := &Account{
		Name:      checking,
		OpenDate:  date2024,
		CloseDate: date2024,
		Inventory: NewInventory(),
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
					Name:      checking,
					OpenDate:  date2024,
					Inventory: NewInventory(),
				},
				"Expenses:Groceries": &Account{
					Name:      expenses,
					OpenDate:  date2024,
					Inventory: NewInventory(),
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
					Name:      checking,
					OpenDate:  date2024,
					CloseDate: date2024,
					Inventory: NewInventory(),
				},
				"Expenses:Groceries": &Account{
					Name:      expenses,
					OpenDate:  date2024,
					CloseDate: date2024,
					Inventory: NewInventory(),
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
			v := newValidator(tt.accounts, nil)
			errs := v.validateAccountsOpen(context.Background(), tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrType != "" && len(errs) > 0 {
				// Check error type matches
				assert.Equal(t, tt.wantErrType, getErrorType(errs[0]))
			}
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
			v := newValidator(nil, nil) // validateAmounts doesn't need accounts
			errs := v.validateAmounts(context.Background(), tt.txn)

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
			// amounts at -3 and -4 decimals, minExp = -4
			// tolerance = 10^-4 * 0.5 = 0.00005
			// diff = 0.0005, which is > 0.00005
			// Actually, let me use a smaller difference
			wantBalanced: false,
			wantResiduals: map[string]string{
				"USD": "0.0005",
			},
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
			v := newValidator(nil, nil) // calculateBalance doesn't need accounts
			delta, validation, errs := v.calculateBalance(context.Background(), tt.txn)

			assert.Equal(t, 0, len(errs))

			assert.Equal(t, tt.wantBalanced, validation.isBalanced)

			assert.Equal(t, tt.wantInferred, len(delta.InferredAmounts))

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

func TestValidateTransaction_Integration(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")
	closed, _ := ast.NewAccount("Assets:OldAccount")

	// Setup accounts for validator
	accounts := map[string]*Account{
		"Assets:Checking": {
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Expenses:Groceries": {
			Name:      expenses,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Assets:OldAccount": {
			Name:      closed,
			OpenDate:  date,
			CloseDate: date,
			Inventory: NewInventory(),
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
			v := newValidator(accounts, nil)
			errs, result := v.validateTransaction(context.Background(), tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantBalanceResult {
				assert.NotEqual(t, nil, result)
			}
		})
	}
}

// Helper to get error type name for testing
func getErrorType(err error) string {
	switch err.(type) {
	case *AccountNotOpenError:
		return "AccountNotOpenError"
	case *InvalidAmountError:
		return "InvalidAmountError"
	case *TransactionNotBalancedError:
		return "TransactionNotBalancedError"
	default:
		return "UnknownError"
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
			Name:      checking,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
		"Expenses:Groceries": {
			Name:      expenses,
			OpenDate:  date,
			Inventory: NewInventory(),
		},
	}

	v := newValidator(accounts, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.validateTransaction(context.Background(), txn)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &validator{accounts: make(map[string]*Account)}
			errs := v.validateCosts(context.Background(), tt.txn)

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
			v := &validator{accounts: make(map[string]*Account)}
			errs := v.validatePrices(context.Background(), tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))
		})
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
				inv123 := "INV-123"
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
				inv123 := "INV-123"
				inv456 := "INV-456"
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
			errs := v.validateMetadata(context.Background(), tt.txn)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrMsg != "" && len(errs) > 0 {
				assert.Contains(t, errs[0].Error(), tt.wantErrMsg)
			}
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

	v := &validator{accounts: make(map[string]*Account)}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.validateCosts(ctx, txn)
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

	v := &validator{accounts: make(map[string]*Account)}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.validatePrices(ctx, txn)
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
	inv123 := "INV-123"
	food := "food"
	txn.Metadata = []*ast.Metadata{
		{Key: "invoice", Value: &ast.MetadataValue{StringValue: &inv123}},
		{Key: "category", Value: &ast.MetadataValue{StringValue: &food}},
	}

	v := &validator{accounts: make(map[string]*Account)}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.validateMetadata(ctx, txn)
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
				  Assets:Cash         500 USD
				  Income:CapitalGains    -500 USD
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
				  Assets:Cash         550 USD
				  Income:CapitalGains    -550 USD
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
		})
	}
}

// TestPadTiming tests CRITICAL pad directive timing rules.
// In beancount, pad directives MUST come chronologically BEFORE the balance assertion.
// This is one of the most important compliance rules.
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
			name: "pad on same date as balance - invalid (CRITICAL)",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-05 pad Assets:Checking Equity:Opening
				2020-01-05 balance Assets:Checking 100 USD
			`,
			wantErr: true,
			errMsg:  "must come before balance",
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
			name: "multiple pads for same account - only last one before balance applies",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 pad Assets:Checking Equity:Opening
				2020-01-03 pad Assets:Checking Equity:Opening
				2020-01-04 pad Assets:Checking Equity:Opening
				2020-01-05 balance Assets:Checking 100 USD
			`,
			wantErr: false,
		},
		{
			name: "pad without subsequent balance - allowed",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 pad Assets:Checking Equity:Opening
			`,
			wantErr: false,
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
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

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

// TestBalanceTolerance tests balance assertion tolerance handling.
// Beancount uses a default tolerance of 0.005 for balance checks.
func TestBalanceTolerance(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "balance matches exactly",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Deposit"
				  Assets:Checking    100.00 USD
				  Equity:Opening    -100.00 USD

				2020-01-03 balance Assets:Checking 100.00 USD
			`,
			wantErr: false,
		},
		{
			name: "balance within default tolerance (0.005)",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Deposit"
				  Assets:Checking    100.004 USD
				  Equity:Opening    -100.004 USD

				2020-01-03 balance Assets:Checking 100.00 USD
			`,
			wantErr: false,
		},
		{
			name: "balance exceeds tolerance - should error",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Deposit"
				  Assets:Checking    100.01 USD
				  Equity:Opening    -100.01 USD

				2020-01-03 balance Assets:Checking 100.00 USD
			`,
			wantErr: true,
		},
		{
			name: "tolerance applied after padding",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Deposit"
				  Assets:Checking    50.004 USD
				  Equity:Opening    -50.004 USD

				2020-01-03 pad Assets:Checking Equity:Opening
				2020-01-04 balance Assets:Checking 100.00 USD
			`,
			wantErr: false,
		},
		{
			name: "balance assertion of exactly 0 (empty account)",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Deposit and withdraw"
				  Assets:Checking    100 USD
				  Equity:Opening    -100 USD

				2020-01-03 * "Withdraw all"
				  Assets:Checking   -100 USD
				  Equity:Opening     100 USD

				2020-01-04 balance Assets:Checking 0 USD
			`,
			wantErr: false,
		},
		{
			name: "negative balance within tolerance",
			input: `
				2020-01-01 open Assets:Checking USD
				2020-01-01 open Equity:Opening

				2020-01-02 * "Overdraft"
				  Assets:Checking    -50.003 USD
				  Equity:Opening      50.003 USD

				2020-01-03 balance Assets:Checking -50.00 USD
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
		})
	}
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
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

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
			name:     "valid open directive",
			accounts: map[string]*Account{},
			open: &ast.Open{
				Date:    date2024,
				Account: checking,
			},
			wantErrCount: 0,
		},
		{
			name: "account already open",
			accounts: map[string]*Account{
				"Assets:Checking": {
					Name:      checking,
					OpenDate:  date2024,
					Inventory: NewInventory(),
				},
			},
			open: &ast.Open{
				Date:    date2025,
				Account: checking,
			},
			wantErrCount: 1,
		},
		{
			name: "reopening closed account - error (duplicate open)",
			accounts: map[string]*Account{
				"Assets:Checking": {
					Name:      checking,
					OpenDate:  date2024,
					CloseDate: date2024,
					Inventory: NewInventory(),
				},
			},
			open: &ast.Open{
				Date:    date2025,
				Account: checking,
			},
			wantErrCount: 1, // Beancount does NOT allow reopening - duplicate open is an error
		},
		{
			name:     "metadata copying",
			accounts: map[string]*Account{},
			open: func() *ast.Open {
				open := &ast.Open{
					Date:    date2024,
					Account: checking,
				}
				note := "Test account"
				open.Metadata = []*ast.Metadata{
					{Key: "note", Value: &ast.MetadataValue{StringValue: &note}},
				}
				return open
			}(),
			wantErrCount:     0,
			wantMetadataCopy: true,
		},
		{
			name:     "constraint currencies copying",
			accounts: map[string]*Account{},
			open: &ast.Open{
				Date:                 date2024,
				Account:              checking,
				ConstraintCurrencies: []string{"USD", "EUR"},
			},
			wantErrCount:      0,
			wantConstraintLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newValidator(tt.accounts, nil)
			errs, delta := v.validateOpen(context.Background(), tt.open)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrCount == 0 && delta != nil {
				if tt.wantMetadataCopy {
					assert.NotEqual(t, 0, len(delta.Metadata))
				}

				if tt.wantConstraintLen > 0 {
					assert.Equal(t, tt.wantConstraintLen, len(delta.ConstraintCurrencies))
				}

				// Verify no shared references
				if len(tt.open.Metadata) > 0 && len(delta.Metadata) > 0 {
					// Check that the slices don't point to the same backing array by checking addresses
					// Using %p format to get pointer addresses as strings
					openPtr := fmt.Sprintf("%p", &tt.open.Metadata[0])
					deltaPtr := fmt.Sprintf("%p", &delta.Metadata[0])
					assert.NotEqual(t, openPtr, deltaPtr)
				}

				if len(tt.open.ConstraintCurrencies) > 0 && len(delta.ConstraintCurrencies) > 0 {
					// Modify delta's copy to verify independence
					originalFirst := tt.open.ConstraintCurrencies[0]
					delta.ConstraintCurrencies[0] = "TEST"
					assert.Equal(t, originalFirst, tt.open.ConstraintCurrencies[0])
				}
			}
		})
	}
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
			name:     "closing non-existent account",
			accounts: map[string]*Account{},
			close: &ast.Close{
				Date:    date2024,
				Account: checking,
			},
			wantErrCount: 1,
			wantErrType:  "*ledger.AccountNotClosedError",
		},
		{
			name: "closing already closed account",
			accounts: map[string]*Account{
				"Assets:Checking": {
					Name:      checking,
					OpenDate:  date2024,
					CloseDate: date2024,
					Inventory: NewInventory(),
				},
			},
			close: &ast.Close{
				Date:    date2025,
				Account: checking,
			},
			wantErrCount: 1,
			wantErrType:  "*ledger.AccountAlreadyClosedError",
		},
		{
			name: "valid close directive",
			accounts: map[string]*Account{
				"Assets:Checking": {
					Name:      checking,
					OpenDate:  date2024,
					Inventory: NewInventory(),
				},
			},
			close: &ast.Close{
				Date:    date2025,
				Account: checking,
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newValidator(tt.accounts, nil)
			errs, delta := v.validateClose(context.Background(), tt.close)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrType != "" && len(errs) > 0 {
				errType := fmt.Sprintf("%T", errs[0])
				assert.Equal(t, tt.wantErrType, errType)
			}

			if tt.wantErrCount == 0 {
				assert.NotEqual(t, nil, delta)
			}
		})
	}
}

// TestCalculateBalanceDelta tests the calculateBalanceDelta() function
func TestCalculateBalanceDelta(t *testing.T) {
	date2024Jan, _ := ast.NewDate("2024-01-10")
	date2024Feb, _ := ast.NewDate("2024-02-15")
	date2024Mar, _ := ast.NewDate("2024-03-20")
	checking, _ := ast.NewAccount("Assets:Checking")
	equity, _ := ast.NewAccount("Equity:Opening-Balances")

	tests := []struct {
		name                string
		accountInventory    map[string]decimal.Decimal
		balanceAmount       string
		balanceCurrency     string
		padEntry            *ast.Pad
		wantErr             bool
		wantPadding         bool
		wantShouldRemovePad bool
	}{
		{
			name: "balance matches (no padding needed)",
			accountInventory: map[string]decimal.Decimal{
				"USD": decimal.NewFromFloat(1000.00),
			},
			balanceAmount:       "1000.00",
			balanceCurrency:     "USD",
			padEntry:            nil,
			wantErr:             false,
			wantPadding:         false,
			wantShouldRemovePad: false,
		},
		{
			name: "balance matches within tolerance",
			accountInventory: map[string]decimal.Decimal{
				"USD": decimal.NewFromFloat(1000.0049),
			},
			balanceAmount:       "1000.00",
			balanceCurrency:     "USD",
			padEntry:            nil,
			wantErr:             false,
			wantPadding:         false,
			wantShouldRemovePad: false,
		},
		{
			name: "balance mismatch exceeds tolerance",
			accountInventory: map[string]decimal.Decimal{
				"USD": decimal.NewFromFloat(1000.00),
			},
			balanceAmount:   "1500.00",
			balanceCurrency: "USD",
			padEntry:        nil,
			wantErr:         true,
		},
		{
			name: "padding calculation (with pad entry)",
			accountInventory: map[string]decimal.Decimal{
				"USD": decimal.NewFromFloat(500.00),
			},
			balanceAmount:   "1000.00",
			balanceCurrency: "USD",
			padEntry: &ast.Pad{
				Date:       date2024Jan,
				Account:    checking,
				AccountPad: equity,
			},
			wantErr:             false,
			wantPadding:         true,
			wantShouldRemovePad: true,
		},
		{
			name: "pad timing validation - pad must come before balance",
			accountInventory: map[string]decimal.Decimal{
				"USD": decimal.NewFromFloat(500.00),
			},
			balanceAmount:   "1000.00",
			balanceCurrency: "USD",
			padEntry: &ast.Pad{
				Date:       date2024Mar, // After balance date
				Account:    checking,
				AccountPad: equity,
			},
			wantErr: true, // Pad after balance should error
		},
		{
			name: "pad on same date as balance",
			accountInventory: map[string]decimal.Decimal{
				"USD": decimal.NewFromFloat(500.00),
			},
			balanceAmount:   "1000.00",
			balanceCurrency: "USD",
			padEntry: &ast.Pad{
				Date:       date2024Feb, // Same as balance date
				Account:    checking,
				AccountPad: equity,
			},
			wantErr: true, // Pad on same date should error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup account with inventory
			account := &Account{
				Name:      checking,
				OpenDate:  date2024Jan,
				Inventory: NewInventory(),
			}
			for currency, amount := range tt.accountInventory {
				account.Inventory.Add(currency, amount)
			}

			accounts := map[string]*Account{
				"Assets:Checking": account,
			}

			// Create balance directive
			balance := &ast.Balance{
				Date:    date2024Feb,
				Account: checking,
				Amount:  ast.NewAmount(tt.balanceAmount, tt.balanceCurrency),
			}

			v := newValidator(accounts, NewToleranceConfig())
			delta, err := v.calculateBalanceDelta(context.Background(), balance, tt.padEntry)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				assert.NotEqual(t, nil, delta)

				if tt.wantPadding {
					assert.NotEqual(t, 0, len(delta.PaddingAdjustments))
				} else {
					assert.Equal(t, 0, len(delta.PaddingAdjustments))
				}

				assert.Equal(t, tt.wantShouldRemovePad, delta.ShouldRemovePad)
			}
		})
	}
}

// TestValidateInventoryOperations tests the validateInventoryOperations() function
func TestValidateInventoryOperations(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	stock, _ := ast.NewAccount("Assets:Investments:Stock")

	tests := []struct {
		name         string
		setupInv     func(*Inventory)
		txn          *ast.Transaction
		delta        *TransactionDelta
		wantErrCount int
		wantErrType  string
	}{
		{
			name: "sufficient inventory",
			setupInv: func(inv *Inventory) {
				// Add 100 shares at $50 cost
				costVal := decimal.NewFromFloat(50.00)
				cost := &lotSpec{Cost: &costVal, CostCurrency: "USD"}
				inv.AddLot("HOOL", decimal.NewFromFloat(100), cost)
			},
			txn: ast.NewTransaction(date, "Sell stock",
				ast.WithPostings(
					ast.NewPosting(stock,
						ast.WithAmount("-10", "HOOL"),
						ast.WithCost(ast.NewCost(ast.NewAmount("50.00", "USD"))),
					),
					ast.NewPosting(checking, ast.WithAmount("500", "USD")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
				InferredCosts:   make(map[*ast.Posting]*ast.Amount),
			},
			wantErrCount: 0,
		},
		{
			name: "insufficient lots",
			setupInv: func(inv *Inventory) {
				// Add only 5 shares
				costVal := decimal.NewFromFloat(50.00)
				cost := &lotSpec{Cost: &costVal, CostCurrency: "USD"}
				inv.AddLot("HOOL", decimal.NewFromFloat(5), cost)
			},
			txn: ast.NewTransaction(date, "Sell stock",
				ast.WithPostings(
					ast.NewPosting(stock,
						ast.WithAmount("-10", "HOOL"),
						ast.WithCost(ast.NewCost(ast.NewAmount("50.00", "USD"))),
					),
					ast.NewPosting(checking, ast.WithAmount("500", "USD")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
				InferredCosts:   make(map[*ast.Posting]*ast.Amount),
			},
			wantErrCount: 1,
			wantErrType:  "*ledger.InsufficientInventoryError",
		},
		{
			name: "lot not found",
			setupInv: func(inv *Inventory) {
				// Add lot with different cost
				costVal := decimal.NewFromFloat(60.00)
				cost := &lotSpec{Cost: &costVal, CostCurrency: "USD"}
				inv.AddLot("HOOL", decimal.NewFromFloat(100), cost)
			},
			txn: ast.NewTransaction(date, "Sell stock",
				ast.WithPostings(
					ast.NewPosting(stock,
						ast.WithAmount("-10", "HOOL"),
						ast.WithCost(ast.NewCost(ast.NewAmount("50.00", "USD"))),
					),
					ast.NewPosting(checking, ast.WithAmount("500", "USD")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
				InferredCosts:   make(map[*ast.Posting]*ast.Amount),
			},
			wantErrCount: 1,
		},
		{
			name: "validates inferred amounts",
			setupInv: func(inv *Inventory) {
				// Inventory is empty
			},
			txn: ast.NewTransaction(date, "Sell stock",
				ast.WithPostings(
					ast.NewPosting(stock,
						ast.WithCost(ast.NewCost(ast.NewAmount("50.00", "USD"))),
					),
					ast.NewPosting(checking, ast.WithAmount("500", "USD")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: map[*ast.Posting]*ast.Amount{},
				InferredCosts:   make(map[*ast.Posting]*ast.Amount),
			},
			wantErrCount: 0, // No amount, so no reduction check
		},
		{
			name: "empty cost spec uses booking method",
			setupInv: func(inv *Inventory) {
				// Add lot with cost
				costVal := decimal.NewFromFloat(50.00)
				cost := &lotSpec{Cost: &costVal, CostCurrency: "USD"}
				inv.AddLot("HOOL", decimal.NewFromFloat(100), cost)
			},
			txn: ast.NewTransaction(date, "Sell stock",
				ast.WithPostings(
					ast.NewPosting(stock,
						ast.WithAmount("-10", "HOOL"),
						ast.WithCost(ast.NewEmptyCost()),
					),
					ast.NewPosting(checking, ast.WithAmount("500", "USD")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
				InferredCosts:   make(map[*ast.Posting]*ast.Amount),
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup accounts
			stockAccount := &Account{
				Name:          stock,
				OpenDate:      date,
				Inventory:     NewInventory(),
				BookingMethod: "FIFO",
			}
			tt.setupInv(stockAccount.Inventory)

			accounts := map[string]*Account{
				"Assets:Investments:Stock": stockAccount,
				"Assets:Checking": {
					Name:      checking,
					OpenDate:  date,
					Inventory: NewInventory(),
				},
			}

			// Link delta to transaction postings
			if len(tt.delta.InferredAmounts) > 0 {
				for _, posting := range tt.txn.Postings {
					if posting.Amount == nil {
						tt.delta.InferredAmounts[posting] = ast.NewAmount("-10", "HOOL")
					}
				}
			}

			v := newValidator(accounts, nil)
			errs := v.validateInventoryOperations(context.Background(), tt.txn, tt.delta)

			assert.Equal(t, tt.wantErrCount, len(errs))

			if tt.wantErrType != "" && len(errs) > 0 {
				errType := fmt.Sprintf("%T", errs[0])
				assert.Equal(t, tt.wantErrType, errType)
			}
		})
	}
}

// TestValidateConstraintCurrencies tests the validateConstraintCurrencies() function
func TestValidateConstraintCurrencies(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	tests := []struct {
		name         string
		constraints  []string
		txn          *ast.Transaction
		delta        *TransactionDelta
		wantErrCount int
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
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
			},
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
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
			},
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
			delta: &TransactionDelta{
				InferredAmounts: make(map[*ast.Posting]*ast.Amount),
			},
			wantErrCount: 1,
		},
		{
			name:        "allowed currency inferred amount (passes)",
			constraints: []string{"USD", "EUR"},
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking),
					ast.NewPosting(expenses, ast.WithAmount("-100", "USD")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: map[*ast.Posting]*ast.Amount{},
			},
			wantErrCount: 0,
		},
		{
			name:        "disallowed currency inferred amount (error)",
			constraints: []string{"USD", "EUR"},
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(checking),
					ast.NewPosting(expenses, ast.WithAmount("-100", "GBP")),
				),
			),
			delta: &TransactionDelta{
				InferredAmounts: map[*ast.Posting]*ast.Amount{},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := map[string]*Account{
				"Assets:Checking": {
					Name:                 checking,
					OpenDate:             date,
					Inventory:            NewInventory(),
					ConstraintCurrencies: tt.constraints,
				},
				"Expenses:Groceries": {
					Name:      expenses,
					OpenDate:  date,
					Inventory: NewInventory(),
				},
			}

			// Setup inferred amounts for testing
			for _, posting := range tt.txn.Postings {
				if posting.Amount == nil {
					// Infer opposite currency from other posting
					for _, p := range tt.txn.Postings {
						if p.Amount != nil {
							tt.delta.InferredAmounts[posting] = ast.NewAmount("100", p.Amount.Currency)
							break
						}
					}
				}
			}

			v := newValidator(accounts, nil)
			errs := v.validateConstraintCurrencies(context.Background(), tt.txn, tt.delta)

			assert.Equal(t, tt.wantErrCount, len(errs))
		})
	}
}
