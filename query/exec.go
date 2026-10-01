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
// Like beanquery, it evaluates each row's targets, or updates its group,
// right after WHERE keeps the row, so the lazy balance column accumulates
// in the order beanquery's does.
func execute(ctx context.Context, qctx *Context, compiled *compiledSelect) (*table, error) {
	timer := telemetry.FromContext(ctx).Start("query.execute")
	defer timer.End()

	var output [][]any
	visit := func(row *evalRow) { output = append(output, evalTargets(row, compiled)) }
	var groups *grouper
	if compiled.HasAgg || len(compiled.GroupBy) > 0 {
		groups = newGrouper(compiled)
		visit = groups.add
	}
	last, err := scanRows(ctx, qctx, compiled, visit)
	if err != nil {
		return nil, err
	}
	if groups != nil {
		output = groups.rows(last)
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
	if compiled.Pivot != nil {
		result = result.pivot(compiled.Pivot[0], compiled.Pivot[1])
	}
	return result, nil
}

// scanRows applies the FROM transforms to the directive stream, flattens
// the transactions into posting rows, one per booked position (see
// postingPositions), and visits each row that FROM's filter expression and
// WHERE keep. Every row shares one running inventory for the lazy balance
// column. It returns last, the last row of the table: after FROM's
// transforms but before its filter expression and WHERE, which beanquery
// joins into one filter. It is nil for an empty table.
func scanRows(ctx context.Context, qctx *Context, compiled *compiledSelect, visit func(*evalRow)) (last *evalRow, err error) {
	qctx, entries := compiled.From.transformed(qctx)
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
		kept := compiled.From.keeps(qctx, entry)

		for _, posting := range txn.Postings {
			positions := postingPositions(qctx, posting)
			if len(positions) == 0 {
				positions = []*positionValue{nil}
			}

			for _, position := range positions {
				row := &evalRow{Ctx: qctx, Entry: entry, Txn: txn, Posting: posting, Position: position, running: running}
				last = row
				if !kept || compiled.Where != nil && !truthy(compiled.Where.eval(row)) {
					continue
				}
				visit(row)
			}
		}
	}
	return last, nil
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

// grouper hash-aggregates rows by the GROUP-BY targets, keeping groups in
// first-seen order.
type grouper struct {
	compiled *compiledSelect
	// keyOrder lists the group-key targets in target order, the order
	// beanquery evaluates them in.
	keyOrder []int
	groups   map[string]*group
	order    []string
}

func newGrouper(compiled *compiledSelect) *grouper {
	return &grouper{
		compiled: compiled,
		keyOrder: slices.Sorted(slices.Values(compiled.GroupBy)),
		groups:   make(map[string]*group),
	}
}

// add evaluates a row's group key, in target order like beanquery, and
// updates its group's aggregates.
func (gr *grouper) add(row *evalRow) {
	compiled := gr.compiled
	var key strings.Builder
	for _, idx := range gr.keyOrder {
		key.WriteString(valueString(compiled.Targets[idx].expr.eval(row)))
		key.WriteByte('\x00')
	}
	k := key.String()

	g, ok := gr.groups[k]
	if !ok {
		g = &group{rep: row, accs: make([]accumulator, len(compiled.Aggs))}
		for i, agg := range compiled.Aggs {
			g.accs[i] = agg.def.new(agg.arg.typ())
		}
		gr.groups[k] = g
		gr.order = append(gr.order, k)
	}
	for i, agg := range compiled.Aggs {
		// An accumulator that is done no longer evaluates its argument,
		// which matters for the lazy balance column.
		if acc, ok := g.accs[i].(interface{ done() bool }); ok && acc.done() {
			continue
		}
		g.accs[i].update(agg.arg.eval(row))
	}
}

// rows evaluates the full target list once per group, keeping the groups
// HAVING accepts. Like beanquery, a target outside the group key, an
// aggregate one such as HAVING's or an ORDER BY expression's, reads its
// columns from last, the table's last row; last is set whenever a group
// exists.
func (gr *grouper) rows(last *evalRow) [][]any {
	compiled := gr.compiled
	inGroup := make([]bool, len(compiled.Targets))
	for _, idx := range compiled.GroupBy {
		inGroup[idx] = true
	}
	var scanned evalRow
	if last != nil {
		scanned = *last
	}
	output := make([][]any, 0, len(gr.order))
	for _, k := range gr.order {
		g := gr.groups[k]
		g.rep.AggValues = make([]any, len(compiled.Aggs))
		for i, acc := range g.accs {
			g.rep.AggValues[i] = acc.finalize()
		}
		scanned.AggValues = g.rep.AggValues
		values := make([]any, len(compiled.Targets))
		for i, target := range compiled.Targets {
			if inGroup[i] {
				values[i] = target.expr.eval(g.rep)
			} else {
				values[i] = target.expr.eval(&scanned)
			}
		}
		if compiled.Having >= 0 && !truthy(values[compiled.Having]) {
			continue
		}
		output = append(output, values)
	}
	return output
}

// orderRows sorts rows like beanquery: one stable sort per run of ORDER BY
// terms sharing a direction, last run first, each comparing its terms'
// values as a Python tuple does: the first unequal value decides, even when
// neither of the two is smaller. One lexicographic sort over every term
// would differ there: a run that ties on values neither smaller keeps the
// order the later runs gave. NULL sorts below everything.
func orderRows(output [][]any, compiled *compiledSelect) [][]any {
	keys := compiled.OrderBy
	for end := len(keys); end > 0; {
		start := end - 1
		for start > 0 && keys[start-1].desc == keys[end-1].desc {
			start--
		}
		run, desc := keys[start:end], keys[end-1].desc
		slices.SortStableFunc(output, func(a, b []any) int {
			for _, key := range run {
				if c, equal := pyCompare(a[key.target], b[key.target]); !equal {
					if desc {
						return -c
					}
					return c
				}
			}
			return 0
		})
		end = start
	}
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
