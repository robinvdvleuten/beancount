package ledger

import (
	"fmt"

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

var (
	// defaultTolerance is cached to avoid repeated decimal.NewFromFloat calls
	defaultTolerance = decimal.NewFromFloat(0.005)
)

// GetTolerance returns the tolerance for a given currency
// Standard tolerance is 0.005 (half a cent) for most currencies
func GetTolerance(currency string) decimal.Decimal {
	// For now, use 0.005 for all currencies (half a cent)
	// In the future, this could consult a per-currency tolerance map
	return defaultTolerance
}

// AmountEqual checks if two amounts are equal within tolerance
func AmountEqual(a, b decimal.Decimal, tolerance decimal.Decimal) bool {
	diff := a.Sub(b).Abs()
	return diff.LessThanOrEqual(tolerance)
}
