package query

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

// TestCompareValues pins how values order, as Python orders beancount's.
func TestCompareValues(t *testing.T) {
	amount := func(number, currency string) *amountValue {
		return &amountValue{Number: decimal.RequireFromString(number), Currency: currency}
	}
	position := func(number, currency string, cost *costValue) *positionValue {
		return &positionValue{Units: *amount(number, currency), Cost: cost}
	}
	cost := func(number, currency string) *costValue {
		return &costValue{Number: decimal.RequireFromString(number), Currency: currency}
	}
	inventory := func(positions ...*positionValue) *inventoryValue {
		inv := newInventory()
		for _, p := range positions {
			inv.AddPosition(p)
		}
		return inv
	}

	for _, tt := range []struct {
		name  string
		l, r  any
		cmp   int
		equal bool
	}{
		{"NULL first", nil, int64(1), -1, false},
		{"amounts by currency first", amount("9", "EUR"), amount("1", "USD"), -1, false},
		{"amounts then by number", amount("1", "USD"), amount("2", "USD"), -1, false},
		{"positions by currency rank", position("9", "USD", nil), position("1", "EUR", nil), -1, false},
		{"a ranked currency before any other", position("1", "CHF", nil), position("1", "ARS", nil), -1, false},
		{"other currencies by name length", position("9", "BRL", nil), position("1", "XAUG", nil), -1, false},
		{"same length is no order", position("1", "BRL", nil), position("1", "ARS", nil), 0, false},
		{"no cost before a cost", position("5", "HOOL", nil), position("1", "HOOL", cost("90", "USD")), -1, false},
		{"cost number before cost currency", position("1", "HOOL", cost("90", "USD")), position("1", "HOOL", cost("100", "EUR")), -1, false},
		{"equal positions", position("1.0", "USD", nil), position("1", "USD", nil), 0, true},
		{"inventories by sorted positions", inventory(position("1", "EUR", nil)), inventory(position("2", "USD", nil)), 1, false},
		{"a shorter inventory first", inventory(position("1", "USD", nil)), inventory(position("1", "USD", nil), position("1", "EUR", nil)), -1, false},
		{"a subset first", setValue{"a": {}}, setValue{"a": {}, "b": {}}, -1, false},
		{"sets neither including", setValue{"a": {}}, setValue{"b": {}}, 0, false},
		{"mixed types by string form", "x", int64(2), 1, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmp, equal := pyCompare(tt.l, tt.r)
			assert.Equal(t, tt.cmp, cmp)
			assert.Equal(t, tt.equal, equal)
		})
	}

	// > on a position is the named tuple's: units by number first.
	assert.True(t, pyGreater(position("300", "JPY", nil), position("10", "USD", nil)))
	assert.True(t, compareValues(position("300", "JPY", nil), position("10", "USD", nil)) > 0)
	assert.False(t, pyGreater(position("1", "USD", nil), position("2", "EUR", nil)))
}
