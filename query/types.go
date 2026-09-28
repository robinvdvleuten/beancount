// Package query implements the Beancount Query Language (BQL) engine. Run
// takes one statement's text and writes what the official bean-query tool
// writes for it, through the same pipeline: parse (bql package) → compile →
// execute → render.
package query

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// dtype identifies the static type of a compiled expression. It drives
// function overload resolution at compile time and column formatting in the
// renderers. Runtime values are Go values: bool, int64, decimal.Decimal,
// string, *ast.Date, setValue, *amountValue, *positionValue and
// *inventoryValue; NULL is nil.
type dtype uint8

const (
	tAny dtype = iota // unknown or polymorphic (renders via str)
	tBool
	tInt
	tDecimal
	tString
	tDate
	tSet
	tAmount
	tPosition
	tInventory
)

var dtypeNames = map[dtype]string{
	tAny:       "object",
	tBool:      "bool",
	tInt:       "int",
	tDecimal:   "Decimal",
	tString:    "str",
	tDate:      "date",
	tSet:       "set",
	tAmount:    "Amount",
	tPosition:  "Position",
	tInventory: "Inventory",
}

func (t dtype) String() string {
	if name, ok := dtypeNames[t]; ok {
		return name
	}
	return "object"
}

// amountValue is a number with a currency, the query-engine counterpart of a
// beancount amount.
type amountValue struct {
	Number   decimal.Decimal
	Currency string
}

// costValue is the per-unit cost basis attached to a position.
type costValue struct {
	Number   decimal.Decimal
	Currency string
	Date     *ast.Date
	Label    string
}

// positionValue is an amount of units held at an optional cost.
type positionValue struct {
	Units amountValue
	Cost  *costValue
}

// costKey returns a stable identity for grouping positions by (currency, cost).
func (p *positionValue) costKey() string {
	if p.Cost == nil {
		return p.Units.Currency
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s",
		p.Units.Currency, p.Cost.Currency, p.Cost.Number.String(), p.Cost.Date.String(), p.Cost.Label)
}

// inventoryValue is a collection of positions keyed by currency and cost basis.
// Summing amounts or positions in aggregate functions produces an inventoryValue.
// Like beancount's inventory (a Python dict), positions keep the order they
// were first added in, and a position that sums to zero is dropped.
type inventoryValue struct {
	positions []*positionValue
	index     map[string]int // costKey -> index into positions
}

// newInventory creates an empty inventory.
func newInventory() *inventoryValue {
	return &inventoryValue{index: make(map[string]int)}
}

// AddAmount adds a cost-less amount to the inventory.
func (inv *inventoryValue) AddAmount(a *amountValue) {
	inv.AddPosition(&positionValue{Units: *a})
}

// AddPosition merges a position into the inventory, summing units for
// positions with the same currency and cost basis. Positions that sum to
// zero are removed, matching official inventories.
func (inv *inventoryValue) AddPosition(p *positionValue) {
	key := p.costKey()
	if i, ok := inv.index[key]; ok {
		existing := inv.positions[i]
		existing.Units.Number = pydecimal.Add(existing.Units.Number, p.Units.Number)
		if existing.Units.Number.IsZero() {
			inv.remove(i)
		}
		return
	}
	if p.Units.Number.IsZero() {
		return
	}
	inv.index[key] = len(inv.positions)
	inv.positions = append(inv.positions, &positionValue{Units: p.Units, Cost: p.Cost})
}

// remove drops the position at i, keeping the order of the others.
func (inv *inventoryValue) remove(i int) {
	delete(inv.index, inv.positions[i].costKey())
	inv.positions = append(inv.positions[:i], inv.positions[i+1:]...)
	for j := i; j < len(inv.positions); j++ {
		inv.index[inv.positions[j].costKey()] = j
	}
}

// AddInventory merges another inventory into this one.
func (inv *inventoryValue) AddInventory(other *inventoryValue) {
	for _, p := range other.positions {
		inv.AddPosition(p)
	}
}

// IsEmpty reports whether the inventory has no non-zero positions.
func (inv *inventoryValue) IsEmpty() bool {
	return len(inv.positions) == 0
}

// Positions returns the inventory positions in the order they were first
// added, as beancount's inventory iterates them.
func (inv *inventoryValue) Positions() []*positionValue {
	return append([]*positionValue(nil), inv.positions...)
}

// sortedPositions returns the positions in the order Python's sorted()
// gives a beancount inventory (Position.sortkey): by currency rank, cost
// number, cost currency, then units, ties keeping their insertion order.
func (inv *inventoryValue) sortedPositions() []*positionValue {
	costOf := func(p *positionValue) (decimal.Decimal, string) {
		if p.Cost == nil {
			return decimal.Zero, ""
		}
		return p.Cost.Number, p.Cost.Currency
	}

	positions := inv.Positions()
	sort.SliceStable(positions, func(i, j int) bool {
		a, b := positions[i], positions[j]
		if ra, rb := ledger.CurrencyRank(a.Units.Currency), ledger.CurrencyRank(b.Units.Currency); ra != rb {
			return ra < rb
		}
		an, ac := costOf(a)
		bn, bc := costOf(b)
		if c := an.Cmp(bn); c != 0 {
			return c < 0
		}
		if ac != bc {
			return ac < bc
		}
		return a.Units.Number.LessThan(b.Units.Number)
	})
	return positions
}

// Copy returns a deep copy of the inventory.
func (inv *inventoryValue) Copy() *inventoryValue {
	copied := newInventory()
	for _, p := range inv.positions {
		copied.AddPosition(p)
	}
	return copied
}

// Neg returns a new inventory with all unit numbers negated.
func (inv *inventoryValue) Neg() *inventoryValue {
	negated := newInventory()
	for _, p := range inv.positions {
		negated.AddPosition(&positionValue{
			Units: amountValue{Number: p.Units.Number.Neg(), Currency: p.Units.Currency},
			Cost:  p.Cost,
		})
	}
	return negated
}

// setValue is an unordered collection of strings, used for tags, links, and
// other-accounts values.
type setValue map[string]struct{}

// Contains reports whether the set contains the given element.
func (s setValue) Contains(elem string) bool {
	_, ok := s[elem]
	return ok
}

// Sorted returns the set elements in lexicographic order.
func (s setValue) Sorted() []string {
	elems := make([]string, 0, len(s))
	for elem := range s {
		elems = append(elems, elem)
	}
	sort.Strings(elems)
	return elems
}

// truthy converts a value to a boolean following Python truthiness, which is
// what the official implementation applies in logical contexts: NULL, zero,
// empty strings and empty collections are false.
func truthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case int64:
		return val != 0
	case decimal.Decimal:
		return !val.IsZero()
	case string:
		return val != ""
	case *ast.Date:
		return !val.IsZero()
	case setValue:
		return len(val) > 0
	case *amountValue:
		return val != nil
	case *positionValue:
		return val != nil
	case *inventoryValue:
		return val != nil && len(val.positions) > 0
	default:
		return v != nil
	}
}

// asDecimal coerces numeric values (int64, decimal) to a decimal.
func asDecimal(v any) (decimal.Decimal, bool) {
	switch val := v.(type) {
	case int64:
		return decimal.NewFromInt(val), true
	case decimal.Decimal:
		return val, true
	}
	return decimal.Decimal{}, false
}

// valuesEqual is Python's == on two non-NULL values: numbers equal by value,
// a boolean counting as the integer 1 or 0 (bool subclasses int), and values
// of any other differing types never equal, so year = '2023' is false.
func valuesEqual(l, r any) bool {
	ln, lok := asNumber(l)
	rn, rok := asNumber(r)
	if lok || rok {
		return lok && rok && ln.Equal(rn)
	}
	if reflect.TypeOf(l) != reflect.TypeOf(r) {
		return false
	}
	return compareValues(l, r) == 0
}

// asNumber coerces a value Python treats as a number to a decimal: an
// integer, a decimal, or a boolean.
func asNumber(v any) (decimal.Decimal, bool) {
	if b, ok := v.(bool); ok {
		if b {
			return decimal.NewFromInt(1), true
		}
		return decimal.Zero, true
	}
	return asDecimal(v)
}

// compareValues orders two values of compatible types, returning -1, 0, or 1.
// NULL sorts before everything. Values of incompatible types compare by their
// string forms as a last resort, so sorting never fails at runtime.
func compareValues(l, r any) int {
	if l == nil && r == nil {
		return 0
	}
	if l == nil {
		return -1
	}
	if r == nil {
		return 1
	}

	if ld, ok := asDecimal(l); ok {
		if rd, ok := asDecimal(r); ok {
			return ld.Cmp(rd)
		}
	}

	switch lv := l.(type) {
	case string:
		if rv, ok := r.(string); ok {
			return strings.Compare(lv, rv)
		}
	case *ast.Date:
		if rv, ok := r.(*ast.Date); ok {
			switch {
			case lv.Before(rv.Time):
				return -1
			case lv.After(rv.Time):
				return 1
			default:
				return 0
			}
		}
	case bool:
		if rv, ok := r.(bool); ok {
			switch {
			case !lv && rv:
				return -1
			case lv && !rv:
				return 1
			default:
				return 0
			}
		}
	}

	return strings.Compare(valueString(l), valueString(r))
}

// valueString renders a value the way the official str() function does,
// following Python conventions for booleans and sets.
func valueString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case bool:
		if val {
			return "True"
		}
		return "False"
	case string:
		return val
	case int64:
		return fmt.Sprintf("%d", val)
	case decimal.Decimal:
		return val.String()
	case *ast.Date:
		return val.String()
	case setValue:
		if len(val) == 0 {
			return "frozenset()"
		}
		elems := val.Sorted()
		quoted := make([]string, len(elems))
		for i, elem := range elems {
			quoted[i] = fmt.Sprintf("'%s'", elem)
		}
		return "frozenset({" + strings.Join(quoted, ", ") + "})"
	case *amountValue:
		return fmt.Sprintf("%s %s", val.Number.String(), val.Currency)
	case *positionValue:
		return positionString(val, decimal.Decimal.String)
	case *inventoryValue:
		return inventoryString(val, decimal.Decimal.String)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// positionString renders a position as "units {cost, date, "label"}",
// spelling its numbers with number.
func positionString(p *positionValue, number func(decimal.Decimal) string) string {
	units := number(p.Units.Number) + " " + p.Units.Currency
	if p.Cost == nil {
		return units
	}
	cost := number(p.Cost.Number) + " " + p.Cost.Currency
	if p.Cost.Date != nil {
		cost += ", " + p.Cost.Date.String()
	}
	if p.Cost.Label != "" {
		cost += fmt.Sprintf(", \"%s\"", p.Cost.Label)
	}
	return fmt.Sprintf("%s {%s}", units, cost)
}

// inventoryString joins an inventory's positions with ", ", sorted like
// beancount's str() of an inventory.
func inventoryString(inv *inventoryValue, number func(decimal.Decimal) string) string {
	positions := inv.sortedPositions()
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = positionString(p, number)
	}
	return strings.Join(parts, ", ")
}
