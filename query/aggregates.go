package query

import (
	"github.com/shopspring/decimal"
)

// accumulator collects one aggregate function's values over the rows of a
// group and produces the final value.
type accumulator interface {
	update(v any)
	finalize() any
}

// aggDef declares an aggregate function: the result type it produces for a
// given argument type (ok=false when the argument type is unsupported) and a
// factory for per-group accumulators.
type aggDef struct {
	resultType func(arg DType) (DType, bool)
	new        func(arg DType) accumulator
}

// aggregates is the registry of aggregate functions, matching the official
// bean-query environment.
var aggregates = map[string]*aggDef{
	"count": {
		resultType: func(DType) (DType, bool) { return TInt, true },
		new:        func(DType) accumulator { return &countAcc{} },
	},
	"first": {
		resultType: func(arg DType) (DType, bool) { return arg, true },
		new:        func(DType) accumulator { return &firstAcc{} },
	},
	"last": {
		resultType: func(arg DType) (DType, bool) { return arg, true },
		new:        func(DType) accumulator { return &lastAcc{} },
	},
	"min": {
		resultType: func(arg DType) (DType, bool) { return arg, true },
		new:        func(DType) accumulator { return &minMaxAcc{keepMin: true} },
	},
	"max": {
		resultType: func(arg DType) (DType, bool) { return arg, true },
		new:        func(DType) accumulator { return &minMaxAcc{} },
	},
	"sum": {
		resultType: func(arg DType) (DType, bool) {
			switch arg {
			case TInt:
				return TInt, true
			case TDecimal, TAny:
				return TDecimal, true
			case TAmount, TPosition, TInventory:
				return TInventory, true
			}
			return TAny, false
		},
		new: func(arg DType) accumulator {
			switch arg {
			case TInt:
				return &sumIntAcc{}
			case TAmount, TPosition, TInventory:
				return &sumInventoryAcc{inv: NewInventory()}
			default:
				return &sumDecimalAcc{}
			}
		},
	},
}

// countAcc counts rows, including NULL values, matching Python's len().
type countAcc struct {
	n int64
}

func (a *countAcc) update(any)    { a.n++ }
func (a *countAcc) finalize() any { return a.n }

type firstAcc struct {
	value any
	seen  bool
}

func (a *firstAcc) update(v any) {
	if !a.seen {
		a.value, a.seen = v, true
	}
}
func (a *firstAcc) finalize() any { return a.value }

type lastAcc struct {
	value any
}

func (a *lastAcc) update(v any)  { a.value = v }
func (a *lastAcc) finalize() any { return a.value }

type minMaxAcc struct {
	keepMin bool
	value   any
	seen    bool
}

func (a *minMaxAcc) update(v any) {
	if v == nil {
		return
	}
	if !a.seen {
		a.value, a.seen = v, true
		return
	}
	cmp := compareValues(v, a.value)
	if (a.keepMin && cmp < 0) || (!a.keepMin && cmp > 0) {
		a.value = v
	}
}
func (a *minMaxAcc) finalize() any { return a.value }

type sumIntAcc struct {
	total int64
}

func (a *sumIntAcc) update(v any) {
	if n, ok := v.(int64); ok {
		a.total += n
	}
}
func (a *sumIntAcc) finalize() any { return a.total }

type sumDecimalAcc struct {
	total decimal.Decimal
}

func (a *sumDecimalAcc) update(v any) {
	if d, ok := asDecimal(v); ok {
		a.total = a.total.Add(d)
	}
}
func (a *sumDecimalAcc) finalize() any { return a.total }

// sumInventoryAcc sums amounts, positions, or inventories into an Inventory,
// matching the official SUM aggregates.
type sumInventoryAcc struct {
	inv *Inventory
}

func (a *sumInventoryAcc) update(v any) {
	switch val := v.(type) {
	case *Amount:
		a.inv.AddAmount(val)
	case *Position:
		a.inv.AddPosition(val)
	case *Inventory:
		a.inv.AddInventory(val)
	}
}
func (a *sumInventoryAcc) finalize() any { return a.inv }
