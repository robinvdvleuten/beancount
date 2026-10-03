package ledger

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
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

// fill fills a balance assertion from its account's pad, given the
// account's inventory and the assertion's tolerance. With a pad whose
// currency the assertion is the first to reach, and an amount beyond the
// tolerance, the account is padded to the asserted amount: fill returns the
// padding transaction, dated at the pad, else nil. It also returns the
// amount the account holds once padded, which the assertion is checked
// against. fill only reads the pads; applying the assertion consumes its
// pad.
//
// The padding applies whatever the check finds, as in beancount, whose pad
// plugin inserts padding before any assertion is checked. Padding a
// currency the account holds at cost is an error for each such lot, and
// the padding, without cost, still applies, as in beancount's ops/pad.py.
func (p *pads) fill(balance *ast.Balance, inventory *Inventory, tolerance decimal.Decimal) (padding *ast.Transaction, held decimal.Decimal, errs []error) {
	currency := balance.Amount.Currency
	held = inventory.Get(currency)
	pad := p.active(string(balance.Account), currency)
	if pad == nil {
		return nil, held, nil
	}
	expected, _ := ParseAmount(balance.Amount)
	difference := pydecimal.Sub(expected, held)
	if difference.Abs().LessThanOrEqual(tolerance) {
		return nil, held, nil
	}
	for range inventory.countAtCost(currency) {
		errs = append(errs, NewPadCostError(balance, pad, inventory))
	}
	// Like beancount, the padding is the difference as the subtraction
	// leaves it, with its own exponent.
	padding = createPaddingTransaction(pad, balance, formatInferredNumber(difference))
	// Padding an account from itself posts both legs to it, so nothing
	// changes and the assertion fails, as in beancount.
	if pad.AccountPad != balance.Account {
		held = pydecimal.Add(held, difference)
	}
	return padding, held, errs
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

// applyPadding applies a padding transaction when its balance assertion is
// applied. Like beancount's pad plugin, which inserts padding after
// booking, the padding is not booked: each posting books its units as they
// are, without cost, which the Ledger publishes like any booked position.
func (l *Ledger) applyPadding(ctx context.Context, padding *ast.Transaction) {
	booked := &bookedTransaction{postings: make([]bookedPosting, len(padding.Postings))}
	for i, posting := range padding.Postings {
		positions := []BookedPosition{{Units: MustParseAmount(posting.Amount)}}
		booked.postings[i] = bookedPosting{posting: posting, commodity: posting.Amount.Currency, positions: positions}
	}
	l.publishBooking(padding, booked)
	l.processDirective(ctx, padding)
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

	// Like beancount's, the postings carry the balance assertion's
	// location and metadata, where the transaction carries the pad's.
	padded := ast.NewPosting(balance.Account, ast.WithAmount(difference, currency))
	source := ast.NewPosting(pad.AccountPad, ast.WithAmount(negated, currency))
	for _, posting := range []*ast.Posting{padded, source} {
		posting.SetPosition(balance.Position())
		posting.AddMetadata(balance.GetMetadata()...)
	}
	txn := ast.NewTransaction(pad.Date(), narration.String(),
		ast.WithFlag("P"),
		ast.WithPostings(padded, source),
	)
	txn.SetPosition(pad.Position())
	txn.AddMetadata(pad.GetMetadata()...)
	return txn
}
