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

// compileFrom compiles a FROM clause, whose expression sees the entry
// environment. A statement without one compiles to nil.
func (c *compiler) compileFrom(from *bql.From) (*compiledFrom, error) {
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
		env := c.env
		c.env = fromEnv
		expr, err := c.compileExpr(from.Expr)
		c.env = env
		if err != nil {
			return nil, err
		}
		compiled.Expr = expr
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
