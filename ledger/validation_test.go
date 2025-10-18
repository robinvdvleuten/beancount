package ledger

import (
	"context"
	"testing"

	"github.com/robinvdvleuten/beancount/ast"
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
			v := newValidator(tt.accounts)
			errs := v.validateAccountsOpen(context.Background(), tt.txn)

			if got := len(errs); got != tt.wantErrCount {
				t.Errorf("validateAccountsOpen() error count = %v, want %v", got, tt.wantErrCount)
				for i, err := range errs {
					t.Logf("  error[%d]: %v", i, err)
				}
			}

			if tt.wantErrType != "" && len(errs) > 0 {
				// Check error type matches
				if got := getErrorType(errs[0]); got != tt.wantErrType {
					t.Errorf("error type = %v, want %v", got, tt.wantErrType)
				}
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
			v := newValidator(nil) // validateAmounts doesn't need accounts
			errs := v.validateAmounts(context.Background(), tt.txn)

			if got := len(errs); got != tt.wantErrCount {
				t.Errorf("validateAmounts() error count = %v, want %v", got, tt.wantErrCount)
				for i, err := range errs {
					t.Logf("  error[%d]: %v", i, err)
				}
			}
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
			name: "within tolerance balanced",
			txn: ast.NewTransaction(date, "Test",
				ast.WithPostings(
					ast.NewPosting(expenses, ast.WithAmount("50.001", "USD")),
					ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
				),
			),
			wantBalanced: true, // Within 0.005 tolerance
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newValidator(nil) // calculateBalance doesn't need accounts
			result, errs := v.calculateBalance(context.Background(), tt.txn)

			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}

			if result.isBalanced != tt.wantBalanced {
				t.Errorf("IsBalanced = %v, want %v", result.isBalanced, tt.wantBalanced)
				if !result.isBalanced {
					t.Logf("Residuals: %v", result.residuals)
				}
			}

			if got := len(result.inferredAmounts); got != tt.wantInferred {
				t.Errorf("inferred amounts count = %v, want %v", got, tt.wantInferred)
			}

			// Check residuals if specified
			for currency, expected := range tt.wantResiduals {
				if got, exists := result.residuals[currency]; !exists || got != expected {
					t.Errorf("residual[%s] = %v, want %v", currency, got, expected)
				}
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

			if got := len(pc.withAmounts); got != tt.wantWithAmounts {
				t.Errorf("WithAmounts count = %v, want %v", got, tt.wantWithAmounts)
			}

			if got := len(pc.withoutAmounts); got != tt.wantWithoutAmounts {
				t.Errorf("WithoutAmounts count = %v, want %v", got, tt.wantWithoutAmounts)
			}

			if got := len(pc.withEmptyCosts); got != tt.wantWithEmptyCosts {
				t.Errorf("WithEmptyCosts count = %v, want %v", got, tt.wantWithEmptyCosts)
			}

			if got := len(pc.withExplicitCost); got != tt.wantWithExplicitCost {
				t.Errorf("WithExplicitCost count = %v, want %v", got, tt.wantWithExplicitCost)
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
			v := newValidator(accounts)
			errs, result := v.validateTransaction(context.Background(), tt.txn)

			if got := len(errs); got != tt.wantErrCount {
				t.Errorf("validateTransaction() error count = %v, want %v", got, tt.wantErrCount)
				for i, err := range errs {
					t.Logf("  error[%d]: %v", i, err)
				}
			}

			if tt.wantBalanceResult {
				if result == nil {
					t.Errorf("expected BalanceResult, got nil")
				}
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

	v := newValidator(accounts)

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
