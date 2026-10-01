package query

import (
	"context"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/query/bql"
)

// compiledFrom is the compiled FROM clause of a SELECT or PRINT statement:
// an entry-level filter plus summarization transforms.
type compiledFrom struct {
	Expr    cexpr
	OpenOn  *ast.Date
	Close   bool
	CloseOn *ast.Date
	Clear   bool
}

// compileFrom compiles a FROM clause, whose expression sees env, an entry
// environment. A statement without one compiles to nil.
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
	entries := []ast.Directive(qctx.AST.Directives)
	if from == nil {
		return qctx, entries, nil
	}

	qctx, entries = applyFromTransforms(qctx, entries, from)
	if from.Expr == nil {
		return qctx, entries, nil
	}

	kept := make([]ast.Directive, 0, len(entries))
	for i, entry := range entries {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		if truthy(from.Expr.eval(&evalRow{Ctx: qctx, Entry: entry})) {
			kept = append(kept, entry)
		}
	}
	return qctx, kept, nil
}
