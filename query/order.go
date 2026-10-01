package query

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// compareValues orders two values like Python's < on beanquery's values,
// returning -1 or 1 when one is smaller, and 0 when they are equal or
// neither is smaller (sets compare by inclusion, so two sets can be
// incomparable). NULL sorts before everything, as beanquery's NullType
// does. Values of types Python cannot order, such as a string and a
// number in one untyped column, compare by their string forms, where
// beanquery fails with a TypeError (KNOWN_GAPS.md).
func compareValues(l, r any) int {
	c, _ := pyCompare(l, r)
	return c
}

// pyCompare is compareValues, also reporting whether the values are equal
// like Python's ==. A sort key of several values decides at its first
// unequal one, as Python compares tuples, even when neither is smaller.
func pyCompare(l, r any) (c int, equal bool) {
	switch {
	case l == nil && r == nil:
		return 0, true
	case l == nil:
		return -1, false
	case r == nil:
		return 1, false
	}
	if order, ok := pyOrder(l, r); ok {
		return order.cmp, order.equal
	}
	return strings.Compare(valueString(l), valueString(r)), false
}

// pyEqual is Python's == on two values, NULL equal only to NULL.
func pyEqual(l, r any) bool {
	_, equal := pyCompare(l, r)
	return equal
}

// ordering is how two values of one type compare in Python.
type ordering struct {
	cmp   int
	equal bool
}

// pyOrder compares two non-NULL values the way Python does, ok false when
// Python cannot order them.
func pyOrder(l, r any) (ordering, bool) {
	if ld, ok := asNumber(l); ok {
		if rd, ok := asNumber(r); ok {
			c := ld.Cmp(rd)
			return ordering{c, c == 0}, true
		}
		return ordering{}, false
	}
	switch lv := l.(type) {
	case string:
		if rv, ok := r.(string); ok {
			c := strings.Compare(lv, rv)
			return ordering{c, c == 0}, true
		}
	case *ast.Date:
		if rv, ok := r.(*ast.Date); ok {
			c := lv.Compare(rv.Time)
			return ordering{c, c == 0}, true
		}
	case *amountValue:
		if rv, ok := r.(*amountValue); ok {
			return ordering{lv.cmp(rv), lv.equal(rv)}, true
		}
	case *positionValue:
		if rv, ok := r.(*positionValue); ok {
			return ordering{lv.cmp(rv), lv.equal(rv)}, true
		}
	case *inventoryValue:
		if rv, ok := r.(*inventoryValue); ok {
			return lv.order(rv), true
		}
	case setValue:
		if rv, ok := r.(setValue); ok {
			return lv.order(rv), true
		}
	case listValue:
		if rv, ok := r.(listValue); ok {
			return lv.order(rv), true
		}
	}
	return ordering{}, false
}

// order compares two lists as Python does: at their first unequal
// values, else by length.
func (a listValue) order(b listValue) ordering {
	for i := range min(len(a), len(b)) {
		if c, equal := pyCompare(a[i], b[i]); !equal {
			return ordering{c, false}
		}
	}
	c := len(a) - len(b)
	return ordering{max(min(c, 1), -1), c == 0}
}

// asNumber coerces a value Python treats as a number, a boolean included,
// to a decimal.
func asNumber(v any) (decimal.Decimal, bool) {
	if b, ok := v.(bool); ok {
		if b {
			return decimal.NewFromInt(1), true
		}
		return decimal.Zero, true
	}
	return asDecimal(v)
}

// cmp is beancount's Amount ordering, amount.sortkey: by currency,
// then number.
func (a *amountValue) cmp(b *amountValue) int {
	if c := strings.Compare(a.Currency, b.Currency); c != 0 {
		return c
	}
	return a.Number.Cmp(b.Number)
}

func (a *amountValue) equal(b *amountValue) bool {
	return a.Currency == b.Currency && a.Number.Equal(b.Number)
}

// cmp is beancount's Position ordering, Position.sortkey: by the
// currency's rank (CURRENCY_ORDER, then the length of any other name),
// cost number, cost currency and units number, a position without cost
// counting as cost 0 in no currency. Positions with equal keys are
// neither smaller.
func (a *positionValue) cmp(b *positionValue) int {
	if ra, rb := ledger.CurrencyRank(a.Units.Currency), ledger.CurrencyRank(b.Units.Currency); ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	an, ac := a.sortCost()
	bn, bc := b.sortCost()
	if c := an.Cmp(bn); c != 0 {
		return c
	}
	if c := strings.Compare(ac, bc); c != 0 {
		return c
	}
	return a.Units.Number.Cmp(b.Units.Number)
}

// equal is Position's ==: equal units and equal costs.
func (a *positionValue) equal(b *positionValue) bool {
	if !a.Units.equal(&b.Units) {
		return false
	}
	if a.Cost == nil || b.Cost == nil {
		return a.Cost == nil && b.Cost == nil
	}
	return a.Cost.Number.Equal(b.Cost.Number) && a.Cost.Currency == b.Cost.Currency &&
		a.Cost.Label == b.Cost.Label && a.Cost.Date.String() == b.Cost.Date.String()
}

// order is Inventory's < (sorted(self) < sorted(other)): the
// sorted positions compared as Python lists, at the first unequal pair,
// a shorter list being smaller when one is the other's start. Two
// inventories are equal when they hold equal positions, as dicts compare.
func (a *inventoryValue) order(b *inventoryValue) ordering {
	equal := len(a.positions) == len(b.positions)
	for _, pa := range a.positions {
		if !equal {
			break
		}
		i, ok := b.index[pa.costKey()]
		equal = ok && pa.equal(b.positions[i])
	}
	if equal {
		return ordering{0, true}
	}
	sa, sb := a.sortedPositions(), b.sortedPositions()
	for i := range min(len(sa), len(sb)) {
		if !sa[i].equal(sb[i]) {
			return ordering{sa[i].cmp(sb[i]), false}
		}
	}
	switch {
	case len(sa) < len(sb):
		return ordering{-1, false}
	case len(sa) > len(sb):
		return ordering{1, false}
	}
	return ordering{0, false}
}

// order is Python's set comparison: a proper subset is smaller, and two
// sets that neither include are neither smaller.
func (a setValue) order(b setValue) ordering {
	switch {
	case len(a) == len(b) && a.subsetOf(b):
		return ordering{0, true}
	case len(a) < len(b) && a.subsetOf(b):
		return ordering{-1, false}
	case len(b) < len(a) && b.subsetOf(a):
		return ordering{1, false}
	}
	return ordering{0, false}
}

func (s setValue) subsetOf(other setValue) bool {
	for elem := range s {
		if !other.Contains(elem) {
			return false
		}
	}
	return true
}

// sortCost is the cost number and currency Position.sortkey uses: 0 and
// no currency for a position without cost.
func (p *positionValue) sortCost() (decimal.Decimal, string) {
	if p.Cost == nil {
		return decimal.Zero, ""
	}
	return p.Cost.Number, p.Cost.Currency
}

// pyGreater is Python's > on two non-NULL values. beancount's Amount and
// Position define only <, so their > is the one they inherit as named
// tuples: an amount by number then currency, a position by units
// then cost (number, currency, date, label), a position without cost
// counting as smaller. Other values use the reverse of <.
func pyGreater(l, r any) bool {
	switch lv := l.(type) {
	case *amountValue:
		if rv, ok := r.(*amountValue); ok {
			return amountTupleCmp(lv, rv) > 0
		}
	case *positionValue:
		if rv, ok := r.(*positionValue); ok {
			if c := amountTupleCmp(&lv.Units, &rv.Units); c != 0 {
				return c > 0
			}
			return costTupleCmp(lv.Cost, rv.Cost) > 0
		}
	}
	return compareValues(l, r) > 0
}

// amountTupleCmp orders amounts as the tuples (number, currency).
func amountTupleCmp(a, b *amountValue) int {
	if c := a.Number.Cmp(b.Number); c != 0 {
		return c
	}
	return strings.Compare(a.Currency, b.Currency)
}

// costTupleCmp orders costs as the tuples (number, currency, date, label).
func costTupleCmp(a, b *costValue) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	if c := a.Number.Cmp(b.Number); c != 0 {
		return c
	}
	if c := strings.Compare(a.Currency, b.Currency); c != 0 {
		return c
	}
	if c := strings.Compare(a.Date.String(), b.Date.String()); c != 0 {
		return c
	}
	return strings.Compare(a.Label, b.Label)
}
