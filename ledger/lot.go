package ledger

import (
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// LotSpec uniquely identifies a lot by its cost basis
type lotSpec struct {
	Cost         *decimal.Decimal // Cost per unit (nil if no cost basis)
	CostCurrency string           // Currency of the cost
	Date         *ast.Date        // Optional acquisition date
	Label        string           // Optional label
}

// IsEmpty returns true if this is an empty cost specification {}
func (ls *lotSpec) IsEmpty() bool {
	return ls.Cost == nil && ls.CostCurrency == "" && ls.Date == nil && ls.Label == ""
}

// Equal checks if two lot specs are equal
func (ls *lotSpec) Equal(other *lotSpec) bool {
	if ls == nil && other == nil {
		return true
	}
	if ls == nil || other == nil {
		return false
	}

	// Compare cost
	if (ls.Cost == nil) != (other.Cost == nil) {
		return false
	}
	if ls.Cost != nil && !ls.Cost.Equal(*other.Cost) {
		return false
	}

	// Compare cost currency
	if ls.CostCurrency != other.CostCurrency {
		return false
	}

	// Compare date
	if (ls.Date == nil) != (other.Date == nil) {
		return false
	}
	if ls.Date != nil && !ls.Date.Equal(other.Date.Time) {
		return false
	}

	// Compare label
	if ls.Label != other.Label {
		return false
	}

	return true
}

// String returns a string representation of the lot spec
func (ls *lotSpec) String() string {
	if ls == nil {
		return "{}"
	}

	if ls.IsEmpty() {
		return "{}"
	}

	parts := make([]string, 0, 3)

	if ls.Cost != nil {
		parts = append(parts, fmt.Sprintf("%s %s", ls.Cost.String(), ls.CostCurrency))
	}

	if ls.Date != nil {
		parts = append(parts, ls.Date.String())
	}

	if ls.Label != "" {
		parts = append(parts, fmt.Sprintf("\"%s\"", ls.Label))
	}

	var buf strings.Builder
	buf.WriteByte('{')
	for i, part := range parts {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(part)
	}
	buf.WriteByte('}')
	return buf.String()
}

// Lot represents a specific lot of a commodity with cost basis
type lot struct {
	Commodity string
	Amount    decimal.Decimal
	Spec      *lotSpec
}

// newLot creates a new lot
func newLot(commodity string, amount decimal.Decimal, spec *lotSpec) *lot {
	return &lot{
		Commodity: commodity,
		Amount:    amount,
		Spec:      spec,
	}
}

// String returns a string representation of the lot
func (l *lot) String() string {
	if l.Spec == nil || l.Spec.IsEmpty() {
		return fmt.Sprintf("%s %s", l.Amount.String(), l.Commodity)
	}
	return fmt.Sprintf("%s %s %s", l.Amount.String(), l.Commodity, l.Spec.String())
}

// ParseLotSpec creates a LotSpec from ast.Cost
func ParseLotSpec(cost *ast.Cost) (*lotSpec, error) {
	if cost == nil {
		return nil, nil
	}

	// Empty cost {}, and a merge cost {*}, which beancount v2 reports and
	// then books like {}.
	if cost.IsEmpty() || cost.IsMergeCost() {
		return &lotSpec{}, nil
	}

	spec := &lotSpec{
		Date:  cost.Date,
		Label: cost.Label,
	}

	// Parse cost amount; a currency-only cost {USD} leaves Cost nil.
	if cost.Amount != nil {
		spec.CostCurrency = cost.Amount.Currency
	}
	if cost.HasNumber() {
		amount, err := ParseAmount(cost.Amount)
		if err != nil {
			return nil, fmt.Errorf("invalid cost amount: %w", err)
		}
		spec.Cost = &amount
	}

	return spec, nil
}

// normalizeLotSpecForPosting converts total cost {{}} to per-unit cost for inventory operations.
// This is called during applyTransaction to ensure inventory uses correct per-unit costs.
func normalizeLotSpecForPosting(lotSpec *lotSpec, posting *ast.Posting) error {
	if lotSpec == nil || lotSpec.Cost == nil {
		return nil
	}

	// Check if this posting has total cost syntax
	if posting.Cost != nil && posting.Cost.IsTotal {
		// Convert total cost to per-unit cost for inventory operations
		if posting.Amount == nil {
			return fmt.Errorf("total cost requires a quantity")
		}

		quantity, err := ParseAmount(posting.Amount)
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}

		if quantity.IsZero() {
			return fmt.Errorf("cannot use total cost with zero quantity")
		}

		// Calculate per-unit cost: total ÷ quantity
		perUnitCost := pydecimal.Quo(*lotSpec.Cost, quantity.Abs())
		lotSpec.Cost = &perUnitCost
	} else if posting.Cost != nil && posting.Cost.Total != nil {
		if posting.Amount == nil {
			return fmt.Errorf("compound cost requires a quantity")
		}
		quantity, err := ParseAmount(posting.Amount)
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		if quantity.IsZero() {
			return fmt.Errorf("cannot use compound cost with zero quantity")
		}
		total, err := ParseAmount(posting.Cost.Total)
		if err != nil {
			return fmt.Errorf("invalid compound total: %w", err)
		}
		perUnitCost := pydecimal.Add(*lotSpec.Cost, pydecimal.Quo(total, quantity.Abs()))
		lotSpec.Cost = &perUnitCost
	}

	return nil
}

// PerUnitCost returns the per-unit cost a posting is booked at: its cost
// number, a total cost ({{...}}) spread over the units, or a compound cost's
// per-unit part plus its total spread over the units. ok is false when the
// posting states no cost number.
func PerUnitCost(posting *ast.Posting) (number decimal.Decimal, currency string, ok bool) {
	spec, err := ParseLotSpec(posting.Cost)
	if err != nil || spec == nil || spec.Cost == nil {
		return decimal.Zero, "", false
	}
	if normalizeLotSpecForPosting(spec, posting) != nil {
		return decimal.Zero, "", false
	}
	return *spec.Cost, spec.CostCurrency, true
}

// PerUnitPrice returns the per-unit price a posting converts at, as
// beancount's parser computes it: the price number made positive, and for a
// total price (@@) that total spread over the units, zero for zero units.
// The currency is empty when the source leaves it to interpolation. ok is
// false without a price number, and for a total price on a posting without
// units, which beancount drops.
func PerUnitPrice(posting *ast.Posting) (number decimal.Decimal, currency string, ok bool) {
	if posting.Price == nil || posting.Price.Value == "" {
		return decimal.Zero, "", false
	}
	number, err := ParseAmount(posting.Price)
	if err != nil {
		return decimal.Zero, "", false
	}
	number = number.Abs()
	if posting.PriceTotal {
		if posting.Amount == nil || posting.Amount.Value == "" {
			return decimal.Zero, "", false
		}
		units, err := ParseAmount(posting.Amount)
		if err != nil {
			return decimal.Zero, "", false
		}
		if units.IsZero() {
			number = decimal.Zero
		} else {
			number = pydecimal.Quo(number, units.Abs())
		}
	}
	return number, posting.Price.Currency, true
}
