package query

import (
	"context"
	"errors"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/printer"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// compiledPrint is a compiled PRINT statement: the FROM clause choosing the
// directives it prints.
type compiledPrint struct {
	from *compiledFrom
}

func (c *compiler) compilePrint(p *bql.Print) (*compiledPrint, error) {
	from, err := c.compileFrom(p.From, entriesTable)
	if err != nil {
		return nil, err
	}
	return &compiledPrint{from: from}, nil
}

// run prints the directives the FROM clause reads as beancount text, like
// bean-query's print: a transaction's postings as booked, and a failed
// balance assertion with its difference.
func (p *compiledPrint) run(ctx context.Context, qctx *Context, out output) error {
	qctx, entries, err := p.from.entries(ctx, qctx)
	if err != nil {
		return err
	}
	return printer.Print(ctx, out.w, entries,
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
