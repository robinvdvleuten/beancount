package query

import (
	"context"
	"errors"
	"io"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
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

// ExecutePrint renders the directives passing the FROM filter as beancount
// text. Like beancount's print_entries, a blank line precedes every
// transaction and commodity, and every directive of another kind than the
// one printed before it; postings are indented by two spaces. The source's
// comments are left out, as beancount's parser discards them.
func ExecutePrint(ctx context.Context, qctx *Context, tree *ast.AST, compiled *CompiledPrint, w io.Writer) error {
	entries := []ast.Directive(tree.Directives)
	if compiled.From != nil {
		entries = applyFromTransforms(qctx, entries, compiled.From)
	}

	f := formatter.New(formatter.WithParsedNumbers(), formatter.WithIndentation(2), formatter.WithPreserveComments(false))
	differences := balanceDifferences(qctx)
	var previous ast.DirectiveKind
	first := true
	for _, entry := range entries {
		if compiled.From != nil && compiled.From.Expr != nil {
			row := &Row{Ctx: qctx, Entry: entry}
			if !truthy(compiled.From.Expr.eval(row)) {
				continue
			}
		}
		kind := entry.Kind()
		if kind == ast.KindTransaction || kind == ast.KindCommodity || !first && kind != previous {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		previous, first = kind, false
		switch d := entry.(type) {
		case *ast.Transaction:
			entry = printedTransaction(qctx, d)
		case *ast.Balance:
			if difference, ok := differences[d]; ok {
				entry = printedFailedBalance(d, difference)
			}
		}
		single := &ast.AST{Directives: ast.Directives{entry}}
		if err := f.Format(ctx, single, nil, w); err != nil {
			return err
		}
	}
	return nil
}

// balanceDifferences maps each balance assertion that failed to its
// account's actual amount less the expected one.
func balanceDifferences(qctx *Context) map[*ast.Balance]decimal.Decimal {
	differences := make(map[*ast.Balance]decimal.Decimal)
	for _, err := range qctx.Ledger.Diagnostics() {
		var mismatch *ledger.BalanceMismatchError
		if errors.As(err, &mismatch) {
			if balance, ok := mismatch.Directive().(*ast.Balance); ok {
				differences[balance] = mismatch.Difference
			}
		}
	}
	return differences
}

// printedFailedBalance returns a copy of a failed balance assertion that
// carries its difference as a comment, like beancount's printer. The
// formatter writes one space before a comment; bean-query writes three.
func printedFailedBalance(balance *ast.Balance, difference decimal.Decimal) *ast.Balance {
	printed := *balance
	printed.SetComment(&ast.Comment{Content: "  ; Diff: " + numberString(difference) + " " + balance.Amount.Currency})
	return &printed
}

// printedTransaction returns a copy of txn holding the postings beancount
// books it as, which bean-query's print renders: a reduction becomes one
// posting per lot it was booked against, a cost is its booked lot in full
// (per-unit number, currency, date and label) and a total price a per-unit
// one. The copy leaves out the source layout, so the formatter renders these
// postings, and a transaction whose postings were all dropped prints as its
// header.
func printedTransaction(qctx *Context, txn *ast.Transaction) *ast.Transaction {
	printed := *txn
	printed.BodyItems = nil
	printed.Postings = make([]*ast.Posting, 0, len(txn.Postings))
	for _, posting := range txn.Postings {
		if posting.Cost == nil && !posting.PriceTotal {
			printed.Postings = append(printed.Postings, posting)
			continue
		}
		price := postingPrice(posting)
		for _, position := range postingPositions(qctx, posting, txn.Date()) {
			booked := *posting
			booked.Amount = ast.NewAmount(numberString(position.Units.Number), position.Units.Currency)
			if cost := position.Cost; cost != nil {
				booked.Cost = ast.NewCostWithDate(ast.NewAmount(numberString(cost.Number), cost.Currency), cost.Date)
				booked.Cost.Label = cost.Label
			}
			if price, ok := price.(*Amount); ok {
				booked.Price = ast.NewAmount(numberString(price.Number), price.Currency)
				booked.PriceTotal = false
			}
			printed.Postings = append(printed.Postings, &booked)
		}
	}
	return &printed
}
