// Package query implements the Beancount Query Language (BQL) engine: it
// compiles statements parsed by the bql package against a processed ledger
// and executes them into result tables. The pipeline mirrors the official
// bean-query tool: parse (bql package) → compile → execute → render.
package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// DType identifies the static type of a compiled expression. It drives
// function overload resolution at compile time and column formatting in the
// renderers. Runtime values are Go values: bool, int64, decimal.Decimal,
// string, *ast.Date, Set, *Amount, *Position, and *Inventory; NULL is nil.
type DType uint8

const (
	TAny DType = iota // unknown or polymorphic (renders via str)
	TBool
	TInt
	TDecimal
	TString
	TDate
	TSet
	TAmount
	TPosition
	TInventory
)

var dtypeNames = map[DType]string{
	TAny:       "object",
	TBool:      "bool",
	TInt:       "int",
	TDecimal:   "Decimal",
	TString:    "str",
	TDate:      "date",
	TSet:       "set",
	TAmount:    "Amount",
	TPosition:  "Position",
	TInventory: "Inventory",
}

func (t DType) String() string {
	if name, ok := dtypeNames[t]; ok {
		return name
	}
	return "object"
}

// Amount is a number with a currency, the query-engine counterpart of a
// beancount amount.
type Amount struct {
	Number   decimal.Decimal
	Currency string
}

// Cost is the per-unit cost basis attached to a position.
type Cost struct {
	Number   decimal.Decimal
	Currency string
	Date     *ast.Date
	Label    string
}

// Position is an amount of units held at an optional cost.
type Position struct {
	Units Amount
	Cost  *Cost
}

// costKey returns a stable identity for grouping positions by (currency, cost).
func (p *Position) costKey() string {
	if p.Cost == nil {
		return p.Units.Currency
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s",
		p.Units.Currency, p.Cost.Currency, p.Cost.Number.String(), p.Cost.Date.String(), p.Cost.Label)
}

// Inventory is a collection of positions keyed by currency and cost basis.
// Summing amounts or positions in aggregate functions produces an Inventory.
type Inventory struct {
	positions map[string]*Position
}

// NewInventory creates an empty inventory.
func NewInventory() *Inventory {
	return &Inventory{positions: make(map[string]*Position)}
}

// AddAmount adds a cost-less amount to the inventory.
func (inv *Inventory) AddAmount(a *Amount) {
	inv.AddPosition(&Position{Units: *a})
}

// AddPosition merges a position into the inventory, summing units for
// positions with the same currency and cost basis. Positions that sum to
// zero are removed, matching official inventories.
func (inv *Inventory) AddPosition(p *Position) {
	key := p.costKey()
	if existing, ok := inv.positions[key]; ok {
		existing.Units.Number = existing.Units.Number.Add(p.Units.Number)
		if existing.Units.Number.IsZero() {
			delete(inv.positions, key)
		}
		return
	}
	if p.Units.Number.IsZero() {
		return
	}
	inv.positions[key] = &Position{Units: p.Units, Cost: p.Cost}
}

// AddInventory merges another inventory into this one.
func (inv *Inventory) AddInventory(other *Inventory) {
	for _, p := range other.positions {
		inv.AddPosition(p)
	}
}

// IsEmpty reports whether the inventory has no non-zero positions.
func (inv *Inventory) IsEmpty() bool {
	for _, p := range inv.positions {
		if !p.Units.Number.IsZero() {
			return false
		}
	}
	return true
}

// Positions returns the inventory positions in a stable order: by units
// currency, then cost currency, number, date, and label.
func (inv *Inventory) Positions() []*Position {
	positions := make([]*Position, 0, len(inv.positions))
	for _, p := range inv.positions {
		positions = append(positions, p)
	}
	sort.Slice(positions, func(i, j int) bool {
		a, b := positions[i], positions[j]
		if a.Units.Currency != b.Units.Currency {
			return a.Units.Currency < b.Units.Currency
		}
		ac, bc := a.Cost, b.Cost
		switch {
		case ac == nil && bc == nil:
			return false
		case ac == nil:
			return true
		case bc == nil:
			return false
		}
		if ac.Currency != bc.Currency {
			return ac.Currency < bc.Currency
		}
		if !ac.Number.Equal(bc.Number) {
			return ac.Number.LessThan(bc.Number)
		}
		return ac.Date.String() < bc.Date.String()
	})
	return positions
}

// Copy returns a deep copy of the inventory.
func (inv *Inventory) Copy() *Inventory {
	copied := NewInventory()
	for key, p := range inv.positions {
		copied.positions[key] = &Position{Units: p.Units, Cost: p.Cost}
	}
	return copied
}

// matchLotDate returns the cost date of an existing lot that a reduction
// matches by currency and cost basis, or nil. Used to inherit lot dates the
// way official booking does.
func (inv *Inventory) matchLotDate(p *Position) *ast.Date {
	if p.Cost == nil {
		return nil
	}
	var best *ast.Date
	for _, lot := range inv.positions {
		if lot.Cost == nil || lot.Cost.Date == nil {
			continue
		}
		if lot.Units.Currency != p.Units.Currency ||
			lot.Cost.Currency != p.Cost.Currency ||
			!lot.Cost.Number.Equal(p.Cost.Number) {
			continue
		}
		if lot.Units.Number.Sign() == 0 || lot.Units.Number.Sign() == p.Units.Number.Sign() {
			continue
		}
		if best == nil || lot.Cost.Date.Before(best.Time) {
			best = lot.Cost.Date
		}
	}
	return best
}

// Neg returns a new inventory with all unit numbers negated.
func (inv *Inventory) Neg() *Inventory {
	negated := NewInventory()
	for key, p := range inv.positions {
		negated.positions[key] = &Position{
			Units: Amount{Number: p.Units.Number.Neg(), Currency: p.Units.Currency},
			Cost:  p.Cost,
		}
	}
	return negated
}

// Set is an unordered collection of strings, used for tags, links, and
// other-accounts values.
type Set map[string]struct{}

// NewSet builds a Set from the given elements.
func NewSet(elems ...string) Set {
	set := make(Set, len(elems))
	for _, elem := range elems {
		set[elem] = struct{}{}
	}
	return set
}

// Contains reports whether the set contains the given element.
func (s Set) Contains(elem string) bool {
	_, ok := s[elem]
	return ok
}

// Sorted returns the set elements in lexicographic order.
func (s Set) Sorted() []string {
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
	case Set:
		return len(val) > 0
	case *Amount:
		return val != nil
	case *Position:
		return val != nil
	case *Inventory:
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
	case Set:
		if len(val) == 0 {
			return "frozenset()"
		}
		elems := val.Sorted()
		quoted := make([]string, len(elems))
		for i, elem := range elems {
			quoted[i] = fmt.Sprintf("'%s'", elem)
		}
		return "frozenset({" + strings.Join(quoted, ", ") + "})"
	case *Amount:
		return fmt.Sprintf("%s %s", val.Number.String(), val.Currency)
	case *Position:
		return positionString(val)
	case *Inventory:
		positions := val.Positions()
		parts := make([]string, len(positions))
		for i, p := range positions {
			parts[i] = positionString(p)
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func positionString(p *Position) string {
	units := fmt.Sprintf("%s %s", p.Units.Number.String(), p.Units.Currency)
	if p.Cost == nil {
		return units
	}
	cost := fmt.Sprintf("%s %s", p.Cost.Number.String(), p.Cost.Currency)
	if p.Cost.Date != nil {
		cost += ", " + p.Cost.Date.String()
	}
	if p.Cost.Label != "" {
		cost += fmt.Sprintf(", \"%s\"", p.Cost.Label)
	}
	return fmt.Sprintf("%s {%s}", units, cost)
}
