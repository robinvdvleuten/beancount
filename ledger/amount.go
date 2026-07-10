package ledger

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/shopspring/decimal"
)

// ParseAmount converts a ast.Amount to a decimal.Decimal.
// Arithmetic expressions are evaluated by the parser before reaching the ledger.
func ParseAmount(amount *ast.Amount) (decimal.Decimal, error) {
	if amount == nil {
		return decimal.Zero, fmt.Errorf("amount is nil")
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

// ToleranceConfig aliases the shared tolerance configuration.
type ToleranceConfig = sharedconfig.Tolerance

// NewToleranceConfig creates a default tolerance configuration
// Default: no configured default tolerances, 0.5 multiplier
func NewToleranceConfig() *ToleranceConfig {
	return sharedconfig.NewTolerance()
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

		tolerance := decimal.New(1, exp).Mul(config.Multiplier)
		if !foundAny || tolerance.GreaterThan(inferred) {
			inferred = tolerance
			foundAny = true
		}
	}

	if !foundAny {
		return config.GetDefault(currency)
	}

	// The currency-specific configured default participates in the maximum.
	if def, ok := config.Defaults[currency]; ok && def.GreaterThan(inferred) {
		return def
	}

	return inferred
}

// AmountEqual checks if two amounts are equal within tolerance
func AmountEqual(a, b decimal.Decimal, tolerance decimal.Decimal) bool {
	diff := a.Sub(b).Abs()
	return diff.LessThanOrEqual(tolerance)
}
