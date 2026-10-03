package query

import (
	"context"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/query/bql"
)

// compiledFrom is the compiled FROM clause of a SELECT or PRINT statement:
// a filter expression plus summarization transforms. PRINT keeps the
// entries its expression is true for (entries); a SELECT joins its
// expression, compiled over postings, into WHERE, as beanquery does, and
// reads only the transforms here.
type compiledFrom struct {
	Expr    cexpr
	OpenOn  *ast.Date
	Close   bool
	CloseOn *ast.Date
	Clear   bool
}

// compileSelectFrom compiles SELECT's FROM clause, like beanquery's
// _compile_from: it returns the Table the statement reads and, for a FROM
// filter over the postings table, the compiled filter. A Table reference
// names a Table, and so does a FROM filter that is a lone name of a Table
// but not of a postings column; OPEN, CLOSE and CLEAR after that name are
// ignored, as in beanquery.
func (c *compiler) compileSelectFrom(from *bql.From) (*environment, *compiledFrom, error) {
	if from == nil {
		return postingsTable, nil, nil
	}
	if from.Table != nil {
		table, err := lookupTable(from.Table, from.Table.Name)
		if err != nil {
			return nil, nil, err
		}
		if table == nil {
			return nil, nil, compileErrorf(from.Table, `table "%s" does not exist`, from.Table.Name)
		}
		return table, nil, nil
	}
	if ident, ok := from.Expr.(*bql.Ident); ok && postingsTable.columns[ident.Name] == nil {
		table, err := lookupTable(ident, ident.Name)
		if err != nil || table != nil {
			return table, nil, err
		}
	}
	compiled, err := c.compileFrom(from, postingsTable)
	return postingsTable, compiled, err
}

// lookupTable returns the Table named name, nil when there is none, and an
// error, at node, for one of beanquery's Tables not built yet.
func lookupTable(node bql.Node, name string) (*environment, error) {
	if unbuiltTables[name] {
		return nil, compileErrorf(node, `table "%s" is not supported`, name)
	}
	return tables[name], nil
}

// compileFrom compiles a FROM clause, whose expression sees env: the
// postings table for a SELECT, the entries table for PRINT. A statement
// without one compiles to nil.
func (c *compiler) compileFrom(from *bql.From, env *environment) (*compiledFrom, error) {
	if from == nil {
		return nil, nil
	}
	compiled := &compiledFrom{
		OpenOn:  from.OpenOn,
		Close:   from.Close,
		CloseOn: from.CloseOn,
		Clear:   from.Clear,
	}
	if from.Expr != nil {
		outer := c.env
		c.env = env
		defer func() { c.env = outer }()
		expr, err := c.compileExpr(from.Expr)
		if err != nil {
			return nil, err
		}
		if c.isAggregate(from.Expr) {
			return nil, statementErrorf("aggregates are not allowed in FROM clause")
		}
		compiled.Expr = expr
	}
	if from.OpenOn != nil && from.CloseOn != nil && from.OpenOn.After(from.CloseOn.Time) {
		return nil, statementErrorf("CLOSE date must follow OPEN date")
	}
	return compiled, nil
}

// entries returns the directives a statement reads through this FROM
// clause: the ledger's, summarized by its transforms and then kept by its
// filter expression. The returned context is the one to evaluate them in; it
// knows the positions of the postings summarization creates. A nil clause
// reads every directive.
func (from *compiledFrom) entries(ctx context.Context, qctx *Context) (*Context, []ast.Directive, error) {
	qctx, entries := from.transformed(qctx)
	if from == nil || from.Expr == nil {
		return qctx, entries, nil
	}

	kept := make([]ast.Directive, 0, len(entries))
	for i, entry := range entries {
		if err := checkCancelled(ctx, i); err != nil {
			return nil, nil, err
		}
		if truthy(from.Expr.eval(&evalRow{Ctx: qctx, Entry: entry})) {
			kept = append(kept, entry)
		}
	}
	return qctx, kept, nil
}

// transformed returns the ledger's directives summarized by the clause's
// transforms, and the context to evaluate them in.
func (from *compiledFrom) transformed(qctx *Context) (*Context, []ast.Directive) {
	entries := []ast.Directive(qctx.AST.Directives)
	if from == nil {
		return qctx, entries
	}
	return applyFromTransforms(qctx, entries, from)
}
