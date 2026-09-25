package ledger

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// Pads fill balance assertions like beancount's ops/pad.py: a pad pads each
// currency at most once, at the first balance assertion for that currency
// after it, with a padding transaction dated at the pad. The padding takes
// effect at that assertion, so later directives see the padded balance.

// padState tracks an account's latest pad and the currencies whose first
// balance assertion after it was seen. A pad counts as used only if it
// inserted padding.
type padState struct {
	pad      *ast.Pad
	consumed map[string]bool
	used     bool
}

// pads is the Ledger's record of the pads it has processed.
type pads struct {
	latest     map[string]*padState // account -> its latest pad
	superseded []*ast.Pad           // replaced pads that inserted no padding
	padding    []*ast.Transaction   // padding transactions, in the order inserted
}

func newPads() *pads {
	return &pads{latest: make(map[string]*padState)}
}

// add makes pad its account's latest. Like beancount, a pad it replaces is
// unused if it never inserted padding.
func (p *pads) add(pad *ast.Pad) {
	account := string(pad.Account)
	if previous, ok := p.latest[account]; ok && !previous.used {
		p.superseded = append(p.superseded, previous.pad)
	}
	p.latest[account] = &padState{pad: pad, consumed: make(map[string]bool)}
}

// active returns the pad that fills a balance assertion of account in
// currency, or nil when there is none or its currency was consumed.
func (p *pads) active(account, currency string) *ast.Pad {
	if state, ok := p.latest[account]; ok && !state.consumed[currency] {
		return state.pad
	}
	return nil
}

// consume records a balance assertion of account in currency: the first
// one after a pad consumes that currency, and uses the pad when it inserted
// padding.
func (p *pads) consume(account, currency string, padding *ast.Transaction) {
	state, ok := p.latest[account]
	if !ok || state.consumed[currency] {
		return
	}
	state.consumed[currency] = true
	if padding != nil {
		state.used = true
		p.padding = append(p.padding, padding)
	}
}

// unusedPads returns the pads that inserted no padding, in source order.
func (p *pads) unusedPads() []*ast.Pad {
	unused := slices.Clone(p.superseded)
	for _, state := range p.latest {
		if !state.used {
			unused = append(unused, state.pad)
		}
	}
	slices.SortFunc(unused, func(a, b *ast.Pad) int {
		return cmp.Or(strings.Compare(a.Position().Filename, b.Position().Filename), cmp.Compare(a.Position().Offset, b.Position().Offset))
	})
	return unused
}

// applyPadding books and applies a padding transaction when its balance
// assertion is applied.
func (l *Ledger) applyPadding(ctx context.Context, padding *ast.Transaction) {
	if l.bookTransaction(padding) {
		l.processDirective(ctx, padding)
	}
}

// createPaddingTransaction creates the transaction pad inserts to fill
// balance with difference. Like beancount's, it is dated and positioned at
// the pad, so its errors are reported on the pad's line, and its flag and
// narration match beancount's:
//
//	2020-01-01 P "(Padding inserted for Balance of 1000.00 USD for difference 1000.00 USD)"
//	  Assets:Checking         1000.00 USD
//	  Equity:Opening-Balances -1000.00 USD
func createPaddingTransaction(pad *ast.Pad, balance *ast.Balance, difference string) *ast.Transaction {
	currency := balance.Amount.Currency
	var narration strings.Builder
	narration.WriteString("(Padding inserted for Balance of ")
	narration.WriteString(balance.Amount.Value)
	narration.WriteString(" ")
	narration.WriteString(currency)
	narration.WriteString(" for difference ")
	narration.WriteString(difference)
	narration.WriteString(" ")
	narration.WriteString(currency)
	narration.WriteString(")")

	negated, ok := strings.CutPrefix(difference, "-")
	if !ok {
		negated = "-" + difference
	}

	txn := ast.NewTransaction(pad.Date(), narration.String(),
		ast.WithFlag("P"),
		ast.WithPostings(
			ast.NewPosting(balance.Account, ast.WithAmount(difference, currency)),
			ast.NewPosting(pad.AccountPad, ast.WithAmount(negated, currency)),
		),
	)
	txn.SetPosition(pad.Position())
	return txn
}
