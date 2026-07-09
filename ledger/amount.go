package ledger

import (
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// ParseAmount converts a ast.Amount to a decimal.Decimal.
// Supports both plain numbers ("100.50") and arithmetic expressions ("(5 + 3)").
// Expressions must be wrapped in parentheses and support +, -, *, / operators.
func ParseAmount(amount *ast.Amount) (decimal.Decimal, error) {
	if amount == nil {
		return decimal.Zero, fmt.Errorf("amount is nil")
	}

	// Check if it's an expression (starts with '(')
	if strings.HasPrefix(amount.Value, "(") {
		return EvaluateExpression(amount.Value)
	}

	// Plain number - parse directly
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
// Default: no configured default tolerances, 0.5 multiplier
func NewToleranceConfig() *ToleranceConfig {
	return &ToleranceConfig{
		defaults:      make(map[string]decimal.Decimal),
		multiplier:    decimal.NewFromFloat(0.5),
		inferFromCost: false,
	}
}

// InferTolerance calculates tolerance from amount precision.
// Algorithm (matching beancount's interpolate.infer_tolerances):
//  1. For each amount with fractional precision (negative exponent, zero
//     amounts included), compute 10^exp * multiplier.
//  2. Use the maximum of those tolerances (the coarsest precision wins),
//     also taking the currency-specific configured default into account.
//  3. If no amount has fractional precision, fall back to the default
//     tolerance for the currency.
func InferTolerance(amounts []decimal.Decimal, currency string, config *ToleranceConfig) decimal.Decimal {
	if config == nil {
		config = NewToleranceConfig()
	}

	inferred := decimal.Zero
	foundAny := false

	for _, amount := range amounts {
		exp := amount.Exponent()
		if exp >= 0 {
			continue // Integer precision does not contribute
		}

		tolerance := decimal.New(1, exp).Mul(config.multiplier)
		if !foundAny || tolerance.GreaterThan(inferred) {
			inferred = tolerance
			foundAny = true
		}
	}

	if !foundAny {
		return config.GetDefaultTolerance(currency)
	}

	// The currency-specific configured default participates in the maximum.
	if def, ok := config.defaults[currency]; ok && def.GreaterThan(inferred) {
		return def
	}

	return inferred
}

// GetDefaultTolerance returns the default tolerance for a currency
// Checks currency-specific default first, then wildcard "*"
func (c *ToleranceConfig) GetDefaultTolerance(currency string) decimal.Decimal {
	if c == nil {
		return decimal.Zero
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
	return decimal.Zero
}

// AmountEqual checks if two amounts are equal within tolerance
func AmountEqual(a, b decimal.Decimal, tolerance decimal.Decimal) bool {
	diff := a.Sub(b).Abs()
	return diff.LessThanOrEqual(tolerance)
}
