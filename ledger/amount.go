package ledger

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
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

// formatInferredNumber renders a number Processing writes into the AST
// (an interpolated amount, an implicit price) preserving its decimal scale
// (decimal.String trims trailing zeros, so a -4.50 residual would otherwise
// display as -4.5, diverging from official beancount).
func formatInferredNumber(d decimal.Decimal) string {
	return d.StringFixed(max(-d.Exponent(), 0))
}

// AmountEqual checks if two amounts are equal within tolerance
func AmountEqual(a, b decimal.Decimal, tolerance decimal.Decimal) bool {
	diff := pydecimal.Sub(a, b).Abs()
	return diff.LessThanOrEqual(tolerance)
}
