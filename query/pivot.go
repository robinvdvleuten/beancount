package query

import (
	"slices"

	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// pivot reshapes the result like beanquery's EvalPivot (query_compile.py):
// one row per value of column col1, in that column's sort order, and, for
// each distinct value of column col2 in sort order, the other columns in
// their order. The first column is named col1/col2; a pivot column is
// named after its col2 value, as Python's str() prints it, followed by
// /name when there are several other columns. A missing combination is
// NULL, and a later row of the same combination overwrites an earlier one.
// With no other column, the result is the col1 column alone.
func (t *table) pivot(col1, col2 int) *table {
	var others []int
	for i := range t.Columns {
		if i != col1 && i != col2 {
			others = append(others, i)
		}
	}

	// Python dedupes the keys with == and sorts them with <.
	var keys []any
	for _, row := range t.Rows {
		if !slices.ContainsFunc(keys, func(key any) bool { return pyEqual(key, row[col2]) }) {
			keys = append(keys, row[col2])
		}
	}
	slices.SortStableFunc(keys, compareValues)

	columns := []tableColumn{{
		Name: t.Columns[col1].Name + "/" + t.Columns[col2].Name,
		Type: t.Columns[col1].Type,
	}}
	for _, key := range keys {
		for _, other := range others {
			name := pyStr(key)
			if len(others) > 1 {
				name += "/" + t.Columns[other].Name
			}
			columns = append(columns, tableColumn{Name: name, Type: t.Columns[other].Type})
		}
	}

	rows := slices.Clone(t.Rows)
	slices.SortStableFunc(rows, func(a, b []any) int { return compareValues(a[col1], b[col1]) })
	var pivoted [][]any
	for _, row := range rows {
		n := len(pivoted)
		if n == 0 || !pyEqual(pivoted[n-1][0], row[col1]) {
			out := make([]any, len(columns))
			out[0] = row[col1]
			pivoted = append(pivoted, out)
			n++
		}
		index := slices.IndexFunc(keys, func(key any) bool { return pyEqual(key, row[col2]) })
		for j, other := range others {
			pivoted[n-1][1+index*len(others)+j] = row[other]
		}
	}
	return &table{Columns: columns, Rows: pivoted, Display: t.Display}
}

// pyStr renders a value like Python's str(), which beanquery names pivot
// columns with: a decimal in its scientific form where Python uses one
// (3.75E-7).
func pyStr(v any) string {
	if d, ok := v.(decimal.Decimal); ok {
		return pydecimal.String(d)
	}
	return objectString(v)
}
