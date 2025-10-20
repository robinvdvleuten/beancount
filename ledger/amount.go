package ledger

import (
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// ParseAmount converts a ast.Amount to a decimal.Decimal
func ParseAmount(amount *ast.Amount) (decimal.Decimal, error) {
	if amount == nil {
		return decimal.Zero, fmt.Errorf("amount is nil")
	}

	d, err := decimal.NewFromString(amount.Value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid amount value %q: %w", amount.Value, err)
	}

	return d, nil
}

// MustParseAmount converts a ast.Amount to a decimal.Decimal and panics on error
// Use only in tests or when you're certain the amount is valid
func MustParseAmount(amount *ast.Amount) decimal.Decimal {
	d, err := ParseAmount(amount)
	if err != nil {
		panic(err)
	}
	return d
}

// ToleranceConfig holds configuration for tolerance inference
type ToleranceConfig struct {
	// defaults maps currency to default tolerance (supports "*" wildcard)
	defaults map[string]decimal.Decimal
	// multiplier is applied to inferred tolerance (default 0.5)
	multiplier decimal.Decimal
	// inferFromCost includes costs/prices in tolerance inference
	inferFromCost bool
}

// NewToleranceConfig creates a default tolerance configuration
// Default: 0.005 tolerance for all currencies, 0.5 multiplier
func NewToleranceConfig() *ToleranceConfig {
	return &ToleranceConfig{
		defaults: map[string]decimal.Decimal{
			"*": decimal.NewFromFloat(0.005),
		},
		multiplier:    decimal.NewFromFloat(0.5),
		inferFromCost: false,
	}
}

// ParseToleranceConfig creates a ToleranceConfig from ledger options
// Supports:
//   - option "inferred_tolerance_default" "*:0.005"
//   - option "inferred_tolerance_default" "USD:0.003"
//   - option "tolerance_multiplier" "0.6"
//   - option "infer_tolerance_from_cost" "TRUE"
func ParseToleranceConfig(options map[string][]string) (*ToleranceConfig, error) {
	config := NewToleranceConfig()

	// Parse tolerance_multiplier (use first value if multiple)
	if vals := options["tolerance_multiplier"]; len(vals) > 0 {
		multiplier, err := decimal.NewFromString(vals[0])
		if err != nil {
			return nil, fmt.Errorf("invalid tolerance_multiplier %q: %w", vals[0], err)
		}
		config.multiplier = multiplier
	}

	// Parse inferred_tolerance_default (can appear multiple times for per-currency tolerances)
	// Format: "CURRENCY:TOLERANCE" or "*:TOLERANCE"
	if vals := options["inferred_tolerance_default"]; len(vals) > 0 {
		for _, val := range vals {
			parts := strings.SplitN(val, ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid inferred_tolerance_default format %q, expected CURRENCY:TOLERANCE", val)
			}

			currency := strings.TrimSpace(parts[0])
			toleranceStr := strings.TrimSpace(parts[1])

			tolerance, err := decimal.NewFromString(toleranceStr)
			if err != nil {
				return nil, fmt.Errorf("invalid tolerance value in %q: %w", val, err)
			}

			config.defaults[currency] = tolerance
		}
	}

	// Parse infer_tolerance_from_cost (use first value if multiple)
	if vals := options["infer_tolerance_from_cost"]; len(vals) > 0 {
		config.inferFromCost = strings.ToUpper(vals[0]) == "TRUE"
	}

	return config, nil
}

// InferTolerance calculates tolerance from amount precision
// Algorithm:
//  1. Find the smallest exponent across all amounts
//  2. Calculate tolerance = 10^minExp * multiplier
//  3. If no amounts, use default tolerance for currency
func InferTolerance(amounts []decimal.Decimal, currency string, config *ToleranceConfig) decimal.Decimal {
	if config == nil {
		config = NewToleranceConfig()
	}

	// If no amounts provided, return default tolerance
	if len(amounts) == 0 {
		return config.GetDefaultTolerance(currency)
	}

	// Find minimum exponent (most precise)
	minExp := int32(0)
	foundAny := false

	for _, amount := range amounts {
		if amount.IsZero() {
			continue // Skip zero amounts
		}

		exp := amount.Exponent()
		if !foundAny || exp < minExp {
			minExp = exp
			foundAny = true
		}
	}

	// If all amounts were zero, use default
	if !foundAny {
		return config.GetDefaultTolerance(currency)
	}

	// Calculate tolerance: 10^minExp * multiplier
	// For example: minExp = -5 gives 10^-5 = 0.00001
	tolerance := decimal.New(1, minExp).Mul(config.multiplier)

	return tolerance
}

// GetDefaultTolerance returns the default tolerance for a currency
// Checks currency-specific default first, then wildcard "*"
func (c *ToleranceConfig) GetDefaultTolerance(currency string) decimal.Decimal {
	if c == nil {
		return decimal.NewFromFloat(0.005)
	}

	// Check currency-specific default
	if tolerance, ok := c.defaults[currency]; ok {
		return tolerance
	}

	// Fall back to wildcard
	if tolerance, ok := c.defaults["*"]; ok {
		return tolerance
	}

	// Final fallback
	return decimal.NewFromFloat(0.005)
}

// AmountEqual checks if two amounts are equal within tolerance
func AmountEqual(a, b decimal.Decimal, tolerance decimal.Decimal) bool {
	diff := a.Sub(b).Abs()
	return diff.LessThanOrEqual(tolerance)
}
