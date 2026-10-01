package query

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

// TestPivot pins beanquery's EvalPivot layout without bean-query.
func TestPivot(t *testing.T) {
	str, num := tableColumn{Type: tString}, tableColumn{Type: tInt}
	named := func(c tableColumn, name string) tableColumn { c.Name = name; return c }

	for _, tt := range []struct {
		name    string
		columns []tableColumn
		rows    [][]any
		col1    int
		col2    int
		want    *table
	}{{
		name:    "one other column names a pivot column by its value",
		columns: []tableColumn{named(str, "a"), named(num, "y"), named(num, "n")},
		rows:    [][]any{{"x", int64(2024), int64(1)}, {"x", int64(2023), int64(2)}, {"w", int64(2023), int64(3)}},
		col1:    0, col2: 1,
		want: &table{
			Columns: []tableColumn{named(str, "a/y"), named(num, "2023"), named(num, "2024")},
			// Rows sort by the first column; a missing combination is NULL.
			Rows: [][]any{{"w", int64(3), nil}, {"x", int64(2), int64(1)}},
		},
	}, {
		name:    "several other columns name a pivot column value/name",
		columns: []tableColumn{named(str, "a"), named(num, "y"), named(num, "n"), named(str, "s")},
		rows:    [][]any{{"x", int64(2023), int64(1), "p"}},
		col1:    0, col2: 1,
		want: &table{
			Columns: []tableColumn{named(str, "a/y"), named(num, "2023/n"), named(str, "2023/s")},
			Rows:    [][]any{{"x", int64(1), "p"}},
		},
	}, {
		name:    "a later row overwrites an earlier one",
		columns: []tableColumn{named(num, "n"), named(str, "a"), named(num, "y")},
		rows:    [][]any{{int64(1), "x", int64(2023)}, {int64(2), "x", int64(2023)}},
		col1:    1, col2: 2,
		want: &table{
			Columns: []tableColumn{named(str, "a/y"), named(num, "2023")},
			Rows:    [][]any{{"x", int64(2)}},
		},
	}, {
		name:    "no other column leaves the first column alone",
		columns: []tableColumn{named(str, "a"), named(num, "y")},
		rows:    [][]any{{"x", int64(2023)}, {"x", int64(2024)}},
		col1:    0, col2: 1,
		want: &table{
			Columns: []tableColumn{named(str, "a/y")},
			Rows:    [][]any{{"x"}},
		},
	}, {
		name:    "a decimal value is named as Python prints it",
		columns: []tableColumn{named(str, "a"), {Name: "d", Type: tDecimal}, named(num, "n")},
		rows:    [][]any{{"x", decimal.RequireFromString("0.000000375"), int64(1)}},
		col1:    0, col2: 1,
		want: &table{
			Columns: []tableColumn{named(str, "a/d"), named(num, "3.75E-7")},
			Rows:    [][]any{{"x", int64(1)}},
		},
	}, {
		name:    "an empty result keeps the first column",
		columns: []tableColumn{named(str, "a"), named(num, "y"), named(num, "n")},
		col1:    0, col2: 1,
		want: &table{Columns: []tableColumn{named(str, "a/y")}},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			got := (&table{Columns: tt.columns, Rows: tt.rows}).pivot(tt.col1, tt.col2)
			assert.Equal(t, tt.want, got)
		})
	}
}
