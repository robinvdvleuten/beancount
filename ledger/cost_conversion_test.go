package ledger

import (
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

func TestValidateTotalCost(t *testing.T) {
	tests := []struct {
		name          string
		posting       *ast.Posting
		expectError   bool
		expectedValue string
	}{
		{
			name: "TotalCostBasic",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				},
			},
			expectError:   false,
			expectedValue: "1000.00",
		},
		{
			name: "TotalCostFractional",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "3.5", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "350.00", Currency: "USD"},
				},
			},
			expectError:   false,
			expectedValue: "350.00",
		},
		{
			name: "TotalCostNegativeQuantity",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "-5", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "500.00", Currency: "USD"},
				},
			},
			expectError:   false,
			expectedValue: "500.00",
		},
		{
			name: "TotalCostWithDate",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "5", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "500.00", Currency: "USD"},
					Date:    &ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
				},
			},
			expectError:   false,
			expectedValue: "500.00",
		},
		{
			name: "TotalCostWithLabel",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "8", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "800.00", Currency: "USD"},
					Label:   "lot-1",
				},
			},
			expectError:   false,
			expectedValue: "800.00",
		},
		{
			name: "PerUnitCostUnchanged",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: false,
					Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				},
			},
			expectError:   false,
			expectedValue: "100.00",
		},
		{
			name: "NoCostUnchanged",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost:    nil,
			},
			expectError:   false,
			expectedValue: "",
		},
		{
			name: "TotalCostMissingAmount",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  nil,
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				},
			},
			expectError: true,
		},
		{
			name: "TotalCostMissingCostAmount",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  nil,
				},
			},
			expectError: true,
		},
		{
			name: "TotalCostZeroQuantity",
			posting: &ast.Posting{
				Account: "Assets:Stock",
				Amount:  &ast.Amount{Value: "0", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				},
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
		txn := ast.NewTransaction(
			&ast.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			"Test transaction",
			ast.WithPostings(test.posting),
		)

			cfg := &Config{Tolerance: NewToleranceConfig()}
			v := newValidator(make(map[string]*Account), cfg)
			errs := v.validateCosts(txn)

			if test.expectError {
				assert.True(t, len(errs) > 0, "Expected error for test: %s", test.name)
				return
			}

			assert.Equal(t, 0, len(errs), "Expected no errors for test: %s", test.name)

			if test.posting.Cost == nil {
				assert.Equal(t, test.expectedValue, "", "Expected no cost")
				return
			}

			if test.posting.Cost.Amount == nil {
				assert.Equal(t, test.expectedValue, "", "Expected no cost amount")
				return
			}

			if test.posting.Cost != nil {
				assert.Equal(t, test.expectedValue, test.posting.Cost.Amount.Value,
					"Cost amount mismatch for test: %s", test.name)
				if strings.Contains(test.name, "TotalCost") {
					assert.True(t, test.posting.Cost.IsTotal,
						"IsTotal should remain true for total cost postings: %s", test.name)
				} else {
					assert.False(t, test.posting.Cost.IsTotal,
						"IsTotal should remain false for per-unit cost postings: %s", test.name)
				}
			}
		})
	}
}

func TestNormalizeLotSpecForPosting(t *testing.T) {
	tests := []struct {
		name         string
		lotSpec      *lotSpec
		posting      *ast.Posting
		expectError  bool
		expectedCost decimal.Decimal
	}{
		{
			name: "TotalCostConversion",
			lotSpec: &lotSpec{
				Cost:         &decimal.Decimal{},
				CostCurrency: "USD",
			},
			posting: &ast.Posting{
				Amount: &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "1000.00", Currency: "USD"},
				},
			},
			expectError:  false,
			expectedCost: decimal.RequireFromString("100"), // 1000 / 10 = 100
		},
		{
			name: "TotalCostFractionalConversion",
			lotSpec: &lotSpec{
				Cost:         &decimal.Decimal{},
				CostCurrency: "USD",
			},
			posting: &ast.Posting{
				Amount: &ast.Amount{Value: "3.5", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: true,
					Amount:  &ast.Amount{Value: "350.00", Currency: "USD"},
				},
			},
			expectError:  false,
			expectedCost: decimal.RequireFromString("100"), // 350 / 3.5 = 100
		},
		{
			name: "PerUnitCostUnchanged",
			lotSpec: &lotSpec{
				Cost:         &decimal.Decimal{},
				CostCurrency: "USD",
			},
			posting: &ast.Posting{
				Amount: &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: false,
					Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				},
			},
			expectError:  false,
			expectedCost: decimal.RequireFromString("100.00"), // unchanged
		},
		{
			name: "NoTotalCostUnchanged",
			lotSpec: &lotSpec{
				Cost:         &decimal.Decimal{},
				CostCurrency: "USD",
			},
			posting: &ast.Posting{
				Amount: &ast.Amount{Value: "10", Currency: "AAPL"},
				Cost: &ast.Cost{
					IsTotal: false,
					Amount:  &ast.Amount{Value: "100.00", Currency: "USD"},
				},
			},
			expectError:  false,
			expectedCost: decimal.RequireFromString("100.00"), // unchanged
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Initialize lotSpec.Cost from posting.Cost.Amount
			if test.posting.Cost != nil && test.posting.Cost.Amount != nil {
				cost, err := ParseAmount(test.posting.Cost.Amount)
				assert.NoError(t, err)
				test.lotSpec.Cost = &cost
				test.lotSpec.CostCurrency = test.posting.Cost.Amount.Currency
			}

			err := normalizeLotSpecForPosting(test.lotSpec, test.posting)

			if test.expectError {
				assert.Error(t, err, "Expected error for test: %s", test.name)
				return
			}

			assert.NoError(t, err, "Expected no error for test: %s", test.name)
			assert.True(t, test.expectedCost.Equal(*test.lotSpec.Cost),
				"Cost mismatch for test: %s\nExpected: %s\nActual: %s",
				test.name, test.expectedCost.String(), test.lotSpec.Cost.String())
		})
	}
}
