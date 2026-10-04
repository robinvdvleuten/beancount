package ledger

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// lotSpec uniquely identifies a lot by its cost basis
type lotSpec struct {
	cost         *decimal.Decimal // Cost per unit (nil if no cost basis)
	costCurrency string           // Currency of the cost
	date         *ast.Date        // Optional acquisition date
	label        string           // Optional label
}

// isEmpty returns true if this is an empty cost specification {}
func (ls *lotSpec) isEmpty() bool {
	return ls.cost == nil && ls.costCurrency == "" && ls.date == nil && ls.label == ""
}

// equal checks if two lot specs are equal
func (ls *lotSpec) equal(other *lotSpec) bool {
	if ls == nil && other == nil {
		return true
	}
	if ls == nil || other == nil {
		return false
	}

	// Compare cost
	if (ls.cost == nil) != (other.cost == nil) {
		return false
	}
	if ls.cost != nil && !ls.cost.Equal(*other.cost) {
		return false
	}

	// Compare cost currency
	if ls.costCurrency != other.costCurrency {
		return false
	}

	// Compare date
	if (ls.date == nil) != (other.date == nil) {
		return false
	}
	if ls.date != nil && !ls.date.Equal(other.date.Time) {
		return false
	}

	// Compare label
	if ls.label != other.label {
		return false
	}

	return true
}

// String returns a string representation of the lot spec
func (ls *lotSpec) String() string {
	if ls == nil {
		return "{}"
	}

	if ls.isEmpty() {
		return "{}"
	}

	parts := make([]string, 0, 3)

	if ls.cost != nil {
		parts = append(parts, fmt.Sprintf("%s %s", formatInferredNumber(*ls.cost), ls.costCurrency))
	}

	if ls.date != nil {
		parts = append(parts, ls.date.String())
	}

	if ls.label != "" {
		parts = append(parts, fmt.Sprintf("\"%s\"", ls.label))
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

// lotKey identifies a lot among its commodity's lots: two specs have the
// same key exactly when lotSpec.equal says they are equal. A lot without a
// spec has the empty key, which no spec has, not even an empty {}. It is one
// string, built once per lot, so the index a clone copies stays small.
type lotKey string

// key returns the spec's lot key: its parts length-prefixed, the cost
// number normalized so 100.0 and 100.00 are one lot, and the date as the
// instant time.Time.Equal compares.
func (ls *lotSpec) key() lotKey {
	if ls == nil {
		return ""
	}
	var cost, date string
	if ls.cost != nil {
		cost = pydecimal.Normalize(*ls.cost).String()
	}
	if ls.date != nil {
		date = strconv.FormatInt(ls.date.Unix(), 10) + "." + strconv.Itoa(ls.date.Nanosecond())
	}
	b := []byte{'{'}
	for _, part := range [...]string{cost, ls.costCurrency, date, ls.label} {
		b = strconv.AppendInt(b, int64(len(part)), 10)
		b = append(b, ':')
		b = append(b, part...)
	}
	return lotKey(b)
}

// Lot represents a specific lot of a commodity with cost basis
type lot struct {
	commodity string
	amount    decimal.Decimal
	spec      *lotSpec
	key       lotKey // spec's key, which the inventory indexes the lot by
}

// String returns a string representation of the lot
func (l *lot) String() string {
	if l.spec == nil || l.spec.isEmpty() {
		return fmt.Sprintf("%s %s", formatInferredNumber(l.amount), l.commodity)
	}
	return fmt.Sprintf("%s %s %s", formatInferredNumber(l.amount), l.commodity, l.spec.String())
}

// sortKey is the lot's Position.sortkey.
func (l *lot) sortKey() SortKey {
	key := SortKey{Currency: l.commodity, Number: l.amount}
	if l.spec != nil && l.spec.cost != nil {
		key.CostNumber, key.CostCurrency = *l.spec.cost, l.spec.costCurrency
	}
	return key
}

// parseLotSpec creates a lotSpec from ast.Cost
func parseLotSpec(cost *ast.Cost) (*lotSpec, error) {
	if cost == nil {
		return nil, nil
	}

	// A merge marker, which beancount reports and then ignores: {*} books
	// like {}, {100 USD, *} like {100 USD}.
	if cost.IsMergeCost() {
		unmerged := *cost
		unmerged.IsMerge = false
		cost = &unmerged
	}

	if cost.IsEmpty() {
		return &lotSpec{}, nil
	}

	spec := &lotSpec{
		date:  cost.Date,
		label: cost.Label,
	}

	// Parse cost amount; a currency-only cost {USD} leaves cost nil.
	if cost.Amount != nil {
		spec.costCurrency = cost.Amount.Currency
	}
	if cost.HasNumber() {
		amount, err := ParseAmount(cost.Amount)
		if err != nil {
			return nil, fmt.Errorf("invalid cost amount: %w", err)
		}
		spec.cost = &amount
	}

	return spec, nil
}

// normalizeLotSpecForPosting converts total cost {{}} to per-unit cost for inventory operations.
// This is called during applyTransaction to ensure inventory uses correct per-unit costs.
func normalizeLotSpecForPosting(lotSpec *lotSpec, posting *ast.Posting) error {
	if lotSpec == nil || lotSpec.cost == nil {
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
		perUnitCost := pydecimal.Quo(*lotSpec.cost, quantity.Abs())
		lotSpec.cost = &perUnitCost
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
		perUnitCost := compoundCostNumber(*lotSpec.cost, total, quantity)
		lotSpec.cost = &perUnitCost
	}

	return nil
}

// compoundCostNumber is the per-unit cost of a compound cost on units, as
// beancount's compute_cost_number computes it, whose rounding this keeps:
// (total + per-unit × |units|) / |units|.
func compoundCostNumber(perUnit, total, units decimal.Decimal) decimal.Decimal {
	return pydecimal.Quo(pydecimal.Add(total, pydecimal.Mul(perUnit, units.Abs())), units.Abs())
}

// postingLotSpec returns the lot spec a posting's cost names, per unit: its
// cost number, a total cost ({{...}}) spread over the units, or a compound
// cost's per-unit part plus its total spread over the units. It is nil
// without a cost.
func postingLotSpec(posting *ast.Posting) (*lotSpec, error) {
	spec, err := parseLotSpec(posting.Cost)
	if err != nil {
		return nil, err
	}
	if err := normalizeLotSpecForPosting(spec, posting); err != nil {
		return nil, err
	}
	return spec, nil
}

// perUnitCost returns the per-unit cost of a posting's cost spec
// (postingLotSpec): what Booking weighs the posting at before it books a
// lot, and what validation checks for a negative cost. ok is false when the
// posting states no cost number.
func perUnitCost(posting *ast.Posting) (number decimal.Decimal, currency string, ok bool) {
	spec, err := postingLotSpec(posting)
	if err != nil || spec == nil || spec.cost == nil {
		return decimal.Zero, "", false
	}
	return *spec.cost, spec.costCurrency, true
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
