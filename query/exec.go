package query

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// Result is an executed query: a header of visible columns and the result
// rows, with one value per column.
type Result struct {
	Columns []ResultColumn
	Rows    [][]any
}

// ResultColumn describes one output column for the renderers.
type ResultColumn struct {
	Name string
	Type DType
}

// Execute runs a compiled query over the processed directive stream. The
// tree must have been processed by the ledger so posting amounts and costs
// are interpolated.
func Execute(ctx context.Context, qctx *Context, tree *ast.AST, compiled *Compiled) (*Result, error) {
	timer := telemetry.FromContext(ctx).Start("query.execute")
	defer timer.End()

	if len(compiled.PivotBy) > 0 {
		// Official bean-query 2.x parses but rejects PIVOT BY; match its
		// error message exactly, punctuation included.
		return nil, errors.New("The PIVOT BY clause is not supported yet.") //nolint:staticcheck // official message parity
	}

	rows, err := generateRows(ctx, qctx, tree, compiled)
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

	result := &Result{Rows: output}
	for _, target := range compiled.Targets {
		if !target.Hidden {
			result.Columns = append(result.Columns, ResultColumn{Name: target.Name, Type: target.Type})
		}
	}
	return result, nil
}

// generateRows applies the FROM filter to the directive stream and flattens
// the surviving transactions into posting rows. Per-account inventories are
// tracked for booking-style lot-date inheritance; the balance column is a
// single running inventory over the rows that survive WHERE, matching the
// official executor.
func generateRows(ctx context.Context, qctx *Context, tree *ast.AST, compiled *Compiled) ([]*Row, error) {
	entries := []ast.Directive(tree.Directives)
	if compiled.From != nil {
		entries = applyFromTransforms(qctx, entries, compiled.From)
	}

	var rows []*Row
	accounts := make(map[string]*Inventory)
	running := NewInventory()

	for i, entry := range entries {
		if i%1024 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		entryRow := &Row{Ctx: qctx, Entry: entry}
		if compiled.From != nil && compiled.From.Expr != nil {
			if !truthy(compiled.From.Expr.eval(entryRow)) {
				continue
			}
		}

		txn, ok := entry.(*ast.Transaction)
		if !ok {
			continue
		}

		for _, posting := range txn.Postings {
			row := &Row{Ctx: qctx, Entry: entry, Txn: txn, Posting: posting}

			account := string(posting.Account)
			inventory, ok := accounts[account]
			if !ok {
				inventory = NewInventory()
				accounts[account] = inventory
			}
			position := postingPosition(posting, entry.Date())
			if position != nil {
				// Reductions without an explicit cost date inherit the
				// matched lot's date, like official booking, so lots merge
				// when summed.
				if position.Cost != nil && (posting.Cost == nil || posting.Cost.Date == nil) {
					if inherited := inventory.matchLotDate(position); inherited != nil {
						row.CostDate = inherited
						position.Cost.Date = inherited
					}
				}
				inventory.AddPosition(position)
			}

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
	return rows, nil
}

// evalTargets evaluates every target (visible and hidden) for a row.
func evalTargets(row *Row, compiled *Compiled) []any {
	values := make([]any, len(compiled.Targets))
	for i, target := range compiled.Targets {
		values[i] = target.expr.eval(row)
	}
	return values
}

// group accumulates aggregate state for one distinct set of group keys.
type group struct {
	rep  *Row // representative row for evaluating group-key targets
	accs []accumulator
}

// executeGrouped hash-aggregates rows by the GROUP-BY targets and evaluates
// the full target list once per group, in first-seen order.
func executeGrouped(rows []*Row, compiled *Compiled) [][]any {
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
func orderRows(output [][]any, compiled *Compiled) [][]any {
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
func projectVisible(output [][]any, compiled *Compiled) [][]any {
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
