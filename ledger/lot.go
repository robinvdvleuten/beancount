package ledger

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

// LotSpec uniquely identifies a lot by its cost basis
type LotSpec struct {
	Cost         *decimal.Decimal // Cost per unit (nil if no cost basis)
	CostCurrency string           // Currency of the cost
	Date         *parser.Date     // Optional acquisition date
	Label        string           // Optional label
}

// IsEmpty returns true if this is an empty cost specification {}
func (ls *LotSpec) IsEmpty() bool {
	return ls.Cost == nil && ls.Date == nil && ls.Label == ""
}

// IsMerge returns true if this represents a merge cost {*}
// Note: This is handled separately in parser.Cost.IsMergeCost()
func (ls *LotSpec) IsMerge() bool {
	return false // Merge is handled at parser level
}

// Equal checks if two lot specs are equal
func (ls *LotSpec) Equal(other *LotSpec) bool {
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
func (ls *LotSpec) String() string {
	if ls == nil || ls.IsEmpty() {
		return "{}"
	}

	result := "{"
	parts := make([]string, 0, 3)

	if ls.Cost != nil {
		parts = append(parts, fmt.Sprintf("%s %s", ls.Cost.String(), ls.CostCurrency))
	}

	if ls.Date != nil {
		parts = append(parts, ls.Date.Format("2006-01-02"))
	}

	if ls.Label != "" {
		parts = append(parts, fmt.Sprintf("\"%s\"", ls.Label))
	}

	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}

	result += "}"
	return result
}

// Lot represents a specific lot of a commodity with cost basis
type Lot struct {
	Commodity string
	Amount    decimal.Decimal
	Spec      *LotSpec
}

// NewLot creates a new lot
func NewLot(commodity string, amount decimal.Decimal, spec *LotSpec) *Lot {
	return &Lot{
		Commodity: commodity,
		Amount:    amount,
		Spec:      spec,
	}
}

// String returns a string representation of the lot
func (l *Lot) String() string {
	if l.Spec == nil || l.Spec.IsEmpty() {
		return fmt.Sprintf("%s %s", l.Amount.String(), l.Commodity)
	}
	return fmt.Sprintf("%s %s %s", l.Amount.String(), l.Commodity, l.Spec.String())
}

// ParseLotSpec creates a LotSpec from parser.Cost
func ParseLotSpec(cost *parser.Cost) (*LotSpec, error) {
	if cost == nil {
		return nil, nil
	}

	// Empty cost {}
	if cost.IsEmpty() {
		return &LotSpec{}, nil
	}

	// Merge cost {*} - return special marker
	// This will be handled separately in booking logic
	if cost.IsMergeCost() {
		return nil, fmt.Errorf("merge cost {*} not yet implemented")
	}

	spec := &LotSpec{
		Date:  cost.Date,
		Label: cost.Label,
	}

	// Parse cost amount
	if cost.Amount != nil {
		amount, err := ParseAmount(cost.Amount)
		if err != nil {
			return nil, fmt.Errorf("invalid cost amount: %w", err)
		}
		spec.Cost = &amount
		spec.CostCurrency = cost.Amount.Currency
	}

	return spec, nil
}
