package query

import (
	"context"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// table is an executed query: a header of visible columns and the result
// rows, with one value per column.
type table struct {
	Columns []tableColumn
	Rows    [][]any
	// Display holds the ledger's per-currency display precision, which the
	// renderers size number columns with; nil renders numbers as written.
	Display *ledger.DisplayContext
}

// tableColumn describes one output column for the renderers.
type tableColumn struct {
	Name string
	Type dtype
}

// execute runs a compiled query over the context's processed directives.
func execute(ctx context.Context, qctx *Context, compiled *compiledSelect) (*table, error) {
	timer := telemetry.FromContext(ctx).Start("query.execute")
	defer timer.End()

	rows, err := generateRows(ctx, qctx, compiled)
	if err != nil {
		return nil, err
	}

	var output [][]any
	if compiled.HasAgg || len(compiled.GroupBy) > 0 {
		output = executeGrouped(rows, compiled)
	} else {
		output = make([][]any, 0, len(rows))
		for _, row := range rows {
			output = append(output, evalTargets(row, compiled))
		}
	}

	output = orderRows(output, compiled)
	output = projectVisible(output, compiled)
	if compiled.Distinct {
		output = distinctRows(output)
	}
	if compiled.Limit != nil && int64(len(output)) > *compiled.Limit {
		output = output[:*compiled.Limit]
	}

	result := &table{Rows: output, Display: qctx.Ledger.DisplayContext()}
	for _, target := range compiled.Targets {
		if !target.Hidden {
			result.Columns = append(result.Columns, tableColumn{Name: target.Name, Type: target.Type})
		}
	}
	return result, nil
}

// generateRows applies the FROM filter to the directive stream and flattens
// the surviving transactions into posting rows, one per booked position (see
// postingPositions). The balance column is a single running inventory over
// the rows that survive WHERE, matching the official executor.
func generateRows(ctx context.Context, qctx *Context, compiled *compiledSelect) ([]*evalRow, error) {
	qctx, entries, err := compiled.From.entries(ctx, qctx)
	if err != nil {
		return nil, err
	}

	var rows []*evalRow
	running := newInventory()

	for i, entry := range entries {
		if i%1024 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		txn, ok := entry.(*ast.Transaction)
		if !ok {
			continue
		}

		for _, posting := range txn.Postings {
			positions := postingPositions(qctx, posting)
			if len(positions) == 0 {
				positions = []*positionValue{nil}
			}

			for _, position := range positions {
				row := &evalRow{Ctx: qctx, Entry: entry, Txn: txn, Posting: posting, Position: position}
				if compiled.Where != nil && !truthy(compiled.Where.eval(row)) {
					continue
				}
				if compiled.UsesBalance {
					if position != nil {
						running.AddPosition(position)
					}
					row.Balance = running.Copy()
				}
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// evalTargets evaluates every target (visible and hidden) for a row.
func evalTargets(row *evalRow, compiled *compiledSelect) []any {
	values := make([]any, len(compiled.Targets))
	for i, target := range compiled.Targets {
		values[i] = target.expr.eval(row)
	}
	return values
}

// group accumulates aggregate state for one distinct set of group keys.
type group struct {
	rep  *evalRow // representative row for evaluating group-key targets
	accs []accumulator
}

// executeGrouped hash-aggregates rows by the GROUP-BY targets and evaluates
// the full target list once per group, in first-seen order.
func executeGrouped(rows []*evalRow, compiled *compiledSelect) [][]any {
	groups := make(map[string]*group)
	var order []string

	for _, row := range rows {
		var key strings.Builder
		for _, idx := range compiled.GroupBy {
			key.WriteString(valueString(compiled.Targets[idx].expr.eval(row)))
			key.WriteByte('\x00')
		}
		k := key.String()

		g, ok := groups[k]
		if !ok {
			g = &group{rep: row, accs: make([]accumulator, len(compiled.Aggs))}
			for i, agg := range compiled.Aggs {
				g.accs[i] = agg.def.new(agg.arg.typ())
			}
			groups[k] = g
			order = append(order, k)
		}
		for i, agg := range compiled.Aggs {
			g.accs[i].update(agg.arg.eval(row))
		}
	}

	output := make([][]any, 0, len(order))
	for _, k := range order {
		g := groups[k]
		g.rep.AggValues = make([]any, len(compiled.Aggs))
		for i, acc := range g.accs {
			g.rep.AggValues[i] = acc.finalize()
		}
		output = append(output, evalTargets(g.rep, compiled))
	}
	return output
}

// orderRows sorts rows by the ORDER-BY target values. The sort is stable so
// ties keep their natural (ledger) order, and a single direction applies to
// the whole key list, matching the official grammar.
func orderRows(output [][]any, compiled *compiledSelect) [][]any {
	if len(compiled.OrderBy) == 0 {
		return output
	}
	slices.SortStableFunc(output, func(a, b []any) int {
		for _, idx := range compiled.OrderBy {
			if cmp := compareValues(a[idx], b[idx]); cmp != 0 {
				if compiled.OrderDesc {
					return -cmp
				}
				return cmp
			}
		}
		return 0
	})
	return output
}

// projectVisible strips hidden (group/order key) columns from the output.
func projectVisible(output [][]any, compiled *compiledSelect) [][]any {
	visible := make([]int, 0, len(compiled.Targets))
	for i, target := range compiled.Targets {
		if !target.Hidden {
			visible = append(visible, i)
		}
	}
	if len(visible) == len(compiled.Targets) {
		return output
	}
	projected := make([][]any, len(output))
	for i, row := range output {
		values := make([]any, len(visible))
		for j, idx := range visible {
			values[j] = row[idx]
		}
		projected[i] = values
	}
	return projected
}

// distinctRows removes duplicate rows, keeping first occurrences.
func distinctRows(output [][]any) [][]any {
	seen := make(map[string]bool, len(output))
	result := output[:0]
	for _, row := range output {
		var key strings.Builder
		for _, v := range row {
			key.WriteString(valueString(v))
			key.WriteByte('\x00')
		}
		k := key.String()
		if !seen[k] {
			seen[k] = true
			result = append(result, row)
		}
	}
	return result
}
