// Package query implements the Beancount Query Language (BQL) engine. Run
// takes one statement's text and writes what the official bean-query tool
// writes for it, through the same pipeline: parse (bql package) → compile →
// execute → render.
package query

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
	"github.com/shopspring/decimal"
)

// dtype identifies the static type of a compiled expression. It drives
// function overload and operator resolution at compile time and column
// formatting in the renderers, where tNull renders like tAny. Runtime
// values are Go values: bool, int64, decimal.Decimal, string, *ast.Date,
// setValue, listValue, *amountValue, *costValue, *positionValue,
// *inventoryValue, *intervalValue, *dictValue and *transactionValue; NULL
// is nil.
// Only numberify's output holds a negativeZero, for the renderers.
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
	tNull     // the NULL literal, Python's NoneType
	tAsterisk // the * of count(*)
	tList     // a list constant, (1, 2)
	tCost     // a position's cost, position.cost
	tInterval // interval()'s relative time interval, dateutil's relativedelta
	tDict     // a meta column, a dict
	tMetadata // a Transaction's meta, beanquery's Metadata dict
	// tTransaction is the entry column, beancount's Transaction.
	tTransaction
	// tAccountSet is the accounts columns, typed typing.Set[str] in
	// beanquery, which renders like a set but is no set to its functions
	// and operators.
	tAccountSet
	// tObject is a function parameter type only, beanquery's object: unlike
	// tAny, its Any, it takes an untyped (tAny) argument or NULL alone.
	tObject
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
	tNull:      "NoneType",
	tAsterisk:  "*",
	tList:      "list",
	tCost:      "cost",
	tInterval:  "relativedelta",
	tDict:      "dict",
	tMetadata:  "metadata",
	// beanquery names a types.Structure by its name.
	tTransaction: "transaction",
	tAccountSet:  "set[str]",
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
// gives a beancount inventory: by Position.sortkey (positionValue.cmp), ties
// keeping their insertion order.
func (inv *inventoryValue) sortedPositions() []*positionValue {
	positions := inv.Positions()
	slices.SortStableFunc(positions, (*positionValue).cmp)
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

// listValue is a list constant's values, Python's list: literal values
// in the order written.
type listValue []any

// String renders the list like Python's str() of a list: each value's
// repr() between brackets.
func (l listValue) String() string {
	parts := make([]string, len(l))
	for i, v := range l {
		parts[i] = pyValueRepr(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// contains is Python's in on a list: whether a value equals v.
func (l listValue) contains(v any) bool {
	for _, elem := range l {
		if pyEqual(elem, v) {
			return true
		}
	}
	return false
}

// stringElements returns the elements of a set-typed value as strings: a
// set's sorted, and a list's in order. other_accounts holds a listValue of
// account names: beanquery types the column as a set but holds a sorted
// list, which orders element by element and prints as a list.
func stringElements(v any) ([]string, bool) {
	switch val := v.(type) {
	case setValue:
		return val.Sorted(), true
	case listValue:
		elems := make([]string, 0, len(val))
		for _, elem := range val {
			s, ok := elem.(string)
			if !ok {
				return nil, false
			}
			elems = append(elems, s)
		}
		return elems, true
	}
	return nil, false
}

// pyValueRepr renders a literal's value like Python's repr().
func pyValueRepr(v any) string {
	switch val := v.(type) {
	case nil:
		return "None"
	case bool:
		if val {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(val, 10)
	case decimal.Decimal:
		return "Decimal(" + pyrepr.String(pydecimal.String(val)) + ")"
	case string:
		return pyrepr.String(val)
	case *ast.Date:
		return fmt.Sprintf("datetime.date(%d, %d, %d)", val.Year(), val.Month(), val.Day())
	case listValue:
		return val.String()
	case setValue:
		return valueString(val)
	case *amountValue:
		return amountRepr(val)
	case *dictValue:
		return val.String()
	case *costValue:
		var label any
		if val.Label != "" {
			label = val.Label
		}
		var date any
		if val.Date != nil {
			date = val.Date
		}
		return fmt.Sprintf("Cost(number=%s, currency=%s, date=%s, label=%s)",
			pyValueRepr(val.Number), pyValueRepr(val.Currency), pyValueRepr(date), pyValueRepr(label))
	}
	return valueString(v)
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
	case listValue:
		return len(val) > 0
	case *amountValue:
		return val != nil
	case *positionValue:
		return val != nil
	case *inventoryValue:
		return val != nil && len(val.positions) > 0
	case *dictValue:
		return len(val.keys) > 0
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
	case listValue:
		return val.String()
	case *costValue:
		return pyValueRepr(val)
	case *intervalValue:
		return val.String()
	case *dictValue:
		return val.String()
	case *transactionValue:
		return val.String()
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

// typeBases is beanquery's types._bases for t: the types an operand of
// type t fits when a function or operator is looked up, t itself first.
// A bool also fits an int, as Python's bool subclasses int.
func typeBases(t dtype) []dtype {
	if t == tBool {
		return []dtype{tBool, tInt}
	}
	return []dtype{t}
}

// lookupSignature is beanquery's types.function_lookup: it tries each
// combination of the argument types' bases in itertools.product order (the
// last argument varying fastest) and returns the first combination match
// accepts, or nil when none does.
func lookupSignature(argTypes []dtype, match func(sig []dtype) bool) []dtype {
	sig := make([]dtype, len(argTypes))
	var try func(i int) bool
	try = func(i int) bool {
		if i == len(argTypes) {
			return match(sig)
		}
		for _, base := range typeBases(argTypes[i]) {
			sig[i] = base
			if try(i + 1) {
				return true
			}
		}
		return false
	}
	if try(0) {
		return sig
	}
	return nil
}

// asBase adapts an operand looked up as base, one of its type's bases, so
// that it evaluates to a value of that type: a bool taken as an int is
// its int value, as Python's True is 1.
func asBase(x cexpr, base dtype) cexpr {
	if x.typ() == tBool && base == tInt {
		return &cBoolAsInt{x: x}
	}
	return x
}

// cBoolAsInt is a bool operand taken as an int.
type cBoolAsInt struct {
	x cexpr
}

func (c *cBoolAsInt) typ() dtype { return tInt }

func (c *cBoolAsInt) eval(row *evalRow) any {
	switch c.x.eval(row) {
	case true:
		return int64(1)
	case false:
		return int64(0)
	}
	return nil
}
