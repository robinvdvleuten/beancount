package query

import (
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
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
	resultType func(arg dtype) (dtype, bool)
	new        func(arg dtype) accumulator
}

// aggregates is the registry of aggregate functions, matching the official
// bean-query environment.
var aggregates = map[string]*aggDef{
	"count": {
		resultType: func(dtype) (dtype, bool) { return tInt, true },
		new:        func(arg dtype) accumulator { return &countAcc{rows: arg == tAsterisk} },
	},
	"first": {
		resultType: func(arg dtype) (dtype, bool) { return arg, arg != tAsterisk },
		new:        func(dtype) accumulator { return &firstAcc{} },
	},
	"last": {
		resultType: func(arg dtype) (dtype, bool) { return arg, arg != tAsterisk },
		new:        func(dtype) accumulator { return &lastAcc{} },
	},
	"min": {
		resultType: func(arg dtype) (dtype, bool) { return arg, arg != tAsterisk },
		new:        func(dtype) accumulator { return &minMaxAcc{keepMin: true} },
	},
	"max": {
		resultType: func(arg dtype) (dtype, bool) { return arg, arg != tAsterisk },
		new:        func(dtype) accumulator { return &minMaxAcc{} },
	},
	"sum": {
		resultType: func(arg dtype) (dtype, bool) {
			switch arg {
			case tInt:
				return tInt, true
			case tDecimal:
				return tDecimal, true
			case tAmount, tPosition, tInventory:
				return tInventory, true
			}
			return tAny, false
		},
		new: func(arg dtype) accumulator {
			switch arg {
			case tInt:
				return &sumIntAcc{}
			case tAmount, tPosition, tInventory:
				return &sumInventoryAcc{inv: newInventory()}
			default:
				return &sumDecimalAcc{}
			}
		},
	},
}

// countAcc is beanquery's CountArg, which counts the non-NULL values, or
// Count, which counts the rows of count(*).
type countAcc struct {
	rows bool // count(*), which counts NULLs too
	n    int64
}

func (a *countAcc) update(v any) {
	if v != nil || a.rows {
		a.n++
	}
}

func (a *countAcc) finalize() any { return a.n }

// firstAcc is beanquery's First, which stores a value while it holds none,
// so it keeps the first non-NULL value, and stops evaluating its argument
// once it has one (see done).
type firstAcc struct {
	value any
}

func (a *firstAcc) update(v any) {
	if a.value == nil {
		a.value = v
	}
}
func (a *firstAcc) done() bool    { return a.value != nil }
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
		a.total = pydecimal.Add(a.total, d)
	}
}
func (a *sumDecimalAcc) finalize() any { return a.total }

// sumInventoryAcc sums amounts, positions, or inventories into an
// inventoryValue, matching the official SUM aggregates.
type sumInventoryAcc struct {
	inv *inventoryValue
}

func (a *sumInventoryAcc) update(v any) {
	switch val := v.(type) {
	case *amountValue:
		a.inv.AddAmount(val)
	case *positionValue:
		// A posting without a position yields a NULL (typed nil) position,
		// which adds nothing.
		if val != nil {
			a.inv.AddPosition(val)
		}
	case *inventoryValue:
		a.inv.AddInventory(val)
	}
}
func (a *sumInventoryAcc) finalize() any { return a.inv }
