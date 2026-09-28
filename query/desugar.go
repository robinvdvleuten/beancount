package query

import (
	"context"
	"errors"
	"io"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/printer"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// Desugar rewrites the BALANCES and JOURNAL shortcut statements into the
// SELECT statements the official tool expands them to. Other statements are
// returned unchanged.
func Desugar(stmt bql.Statement) bql.Statement {
	switch node := stmt.(type) {
	case *bql.Balances:
		return desugarBalances(node)
	case *bql.Journal:
		return desugarJournal(node)
	}
	return stmt
}

// desugarBalances expands BALANCES [AT fn] [FROM ...] [WHERE ...] into
//
//	SELECT account, sum([fn(]position[)]) [FROM ...] [WHERE ...]
//	GROUP BY account ORDER BY account_sortkey(account)
func desugarBalances(b *bql.Balances) *bql.Select {
	account := &bql.Ident{Name: "account"}
	return &bql.Select{
		Targets: []bql.Target{
			{Expr: account},
			{Expr: call("sum", summarize(b.Summary, &bql.Ident{Name: "position"}))},
		},
		From:    b.From,
		Where:   b.Where,
		GroupBy: []bql.Expr{account},
		OrderBy: []bql.Expr{call("account_sortkey", account)},
	}
}

// desugarJournal expands JOURNAL [account] [AT fn] into
//
//	SELECT date, flag, maxwidth(payee, 48), maxwidth(narration, 80),
//	       account, [fn(]position[)], [fn(]balance[)]
//	[WHERE account ~ "<account>"]
func desugarJournal(j *bql.Journal) *bql.Select {
	sel := &bql.Select{
		Targets: []bql.Target{
			{Expr: &bql.Ident{Name: "date"}},
			{Expr: &bql.Ident{Name: "flag"}},
			{Expr: call("maxwidth", &bql.Ident{Name: "payee"}, &bql.Int{Value: 48})},
			{Expr: call("maxwidth", &bql.Ident{Name: "narration"}, &bql.Int{Value: 80})},
			{Expr: &bql.Ident{Name: "account"}},
			{Expr: summarize(j.Summary, &bql.Ident{Name: "position"})},
			{Expr: summarize(j.Summary, &bql.Ident{Name: "balance"})},
		},
		From: j.From,
	}
	if j.Account != "" {
		sel.Where = &bql.Binary{
			Op: bql.TILDE,
			L:  &bql.Ident{Name: "account"},
			R:  &bql.Str{Value: j.Account},
		}
	}
	return sel
}

func call(name string, args ...bql.Expr) *bql.Call {
	return &bql.Call{Func: name, Args: args}
}

// summarize wraps an expression in the AT summary function when present.
func summarize(summary string, expr bql.Expr) bql.Expr {
	if summary == "" {
		return expr
	}
	return call(summary, expr)
}

// CompiledPrint is a compiled PRINT statement: just its entry filter.
type CompiledPrint struct {
	From *CompiledFrom
}

// CompilePrint compiles a PRINT statement's FROM clause.
func CompilePrint(ctx *Context, p *bql.Print) (*CompiledPrint, error) {
	compiled := &CompiledPrint{}
	if p.From != nil {
		from := &CompiledFrom{
			OpenOn:  p.From.OpenOn,
			Close:   p.From.Close,
			CloseOn: p.From.CloseOn,
			Clear:   p.From.Clear,
		}
		if p.From.Expr != nil {
			c := &compiler{ctx: ctx, env: fromEnv}
			expr, err := c.compileExpr(p.From.Expr)
			if err != nil {
				return nil, err
			}
			from.Expr = expr
		}
		compiled.From = from
	}
	return compiled, nil
}

// ExecutePrint prints the directives passing the FROM filter as beancount
// text, like bean-query's print: a transaction's postings as booked, and a
// failed balance assertion with its difference.
func ExecutePrint(ctx context.Context, qctx *Context, tree *ast.AST, compiled *CompiledPrint, w io.Writer) error {
	entries := []ast.Directive(tree.Directives)
	if compiled.From != nil {
		qctx, entries = applyFromTransforms(qctx, entries, compiled.From)
	}
	if compiled.From != nil && compiled.From.Expr != nil {
		entries = slices.DeleteFunc(slices.Clone(entries), func(entry ast.Directive) bool {
			return !truthy(compiled.From.Expr.eval(&Row{Ctx: qctx, Entry: entry}))
		})
	}
	return printer.Print(ctx, w, entries,
		printer.WithBookedPositions(qctx.Ledger.BookedPositions),
		printer.WithBalanceDiffs(balanceDifferences(qctx)))
}

// balanceDifferences maps each balance assertion that failed to its
// account's actual amount less the expected one.
func balanceDifferences(qctx *Context) map[*ast.Balance]decimal.Decimal {
	differences := make(map[*ast.Balance]decimal.Decimal)
	for _, err := range qctx.Ledger.Diagnostics() {
		var mismatch *ledger.BalanceMismatchError
		if errors.As(err, &mismatch) {
			if balance, ok := mismatch.GetDirective().(*ast.Balance); ok {
				differences[balance] = mismatch.Difference
			}
		}
	}
	return differences
}
