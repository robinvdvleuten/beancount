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

// Pads fill balance assertions like beancount's ops/pad.py, which runs once
// the ledger is booked and before any assertion is checked. planPads does
// the same: it reads the booked ledger from start to end and decides every
// padding, each dated at its pad, so the padding takes effect at the pad
// and an assertion dated between a pad and the assertion it fills already
// sees it.
//
// A pad's account is padded for its whole subtree: the first assertion in a
// currency after the pad, on the account or any account under it, is
// measured against what the subtree holds, and the difference beyond its
// tolerance is posted to the pad's account. Each account with a pad is
// worked out on its own, from the ledger's postings and its own paddings,
// as in beancount.

// pads is the plan: the padding each pad inserts and the errors of padding
// a currency held at cost, by the assertion that is padded.
type pads struct {
	all      []*ast.Pad // every pad, in the ledger's order
	planned  map[*ast.Pad][]*ast.Transaction
	costErrs map[*ast.Balance][]error
	padding  []*ast.Transaction // the paddings applied so far, in order
}

func newPads() *pads {
	return &pads{planned: map[*ast.Pad][]*ast.Transaction{}, costErrs: map[*ast.Balance][]error{}}
}

// paddedAccount is an account with a pad as the plan follows it: what its
// subtree holds, its latest pad, and the currencies an assertion has
// reached since that pad.
type paddedAccount struct {
	held   *inventory
	active *ast.Pad
	padded map[string]bool
}

// planPads decides the paddings of the booked directives, given each
// applied transaction's booking and a balance assertion's tolerance.
func planPads(directives []ast.Directive, booked map[*ast.Transaction]*bookedTransaction, tolerance func(*ast.Balance) (decimal.Decimal, error)) *pads {
	p := newPads()
	accounts := map[string]*paddedAccount{}
	for _, directive := range directives {
		if pad, ok := directive.(*ast.Pad); ok {
			p.all = append(p.all, pad)
			accounts[string(pad.Account)] = &paddedAccount{held: newInventory()}
		}
	}
	if len(accounts) == 0 {
		return p
	}

	// within returns the padded accounts account is under or is itself,
	// the outermost first, as beancount goes through them in name order.
	within := func(account ast.Account) []*paddedAccount {
		var found []*paddedAccount
		name := string(account)
		for end := 0; end <= len(name); end++ {
			if end == len(name) || name[end] == ':' {
				if padded, ok := accounts[name[:end]]; ok {
					found = append(found, padded)
				}
			}
		}
		return found
	}

	for _, directive := range directives {
		switch d := directive.(type) {
		case *ast.Transaction:
			applied := booked[d]
			if applied == nil {
				continue
			}
			for _, bp := range applied.postings {
				for _, account := range within(bp.posting.Account) {
					for _, position := range bp.positions {
						account.held.addLot(bp.commodity, position.Units, position.lotSpec())
					}
				}
			}
		case *ast.Pad:
			account := accounts[string(d.Account)]
			account.active, account.padded = d, map[string]bool{}
		case *ast.Balance:
			expected, err := ParseAmount(d.Amount)
			if err != nil {
				continue
			}
			allowed, err := tolerance(d)
			if err != nil {
				continue
			}
			currency := d.Amount.Currency
			for _, account := range within(d.Account) {
				if account.active == nil || account.padded[currency] {
					continue
				}
				account.padded[currency] = true
				difference := pydecimal.Sub(expected, account.held.get(currency))
				if difference.Abs().LessThanOrEqual(allowed) {
					continue
				}
				// Padding a currency held at cost is an error for each
				// such lot, and the padding, without cost, still applies.
				for range account.held.countAtCost(currency) {
					p.costErrs[d] = append(p.costErrs[d], newPadCostError(d, account.active, account.held))
				}
				// Like beancount, the padding is the difference as the
				// subtraction leaves it, with its own exponent.
				padding := createPaddingTransaction(account.active, d, formatInferredNumber(difference))
				p.planned[account.active] = append(p.planned[account.active], padding)
				account.held.addLot(currency, difference, nil)
			}
		}
	}
	return p
}

// unusedPads returns the pads that insert no padding, in source order.
func (p *pads) unusedPads() []*ast.Pad {
	var unused []*ast.Pad
	for _, pad := range p.all {
		if len(p.planned[pad]) == 0 {
			unused = append(unused, pad)
		}
	}
	slices.SortFunc(unused, func(a, b *ast.Pad) int {
		return cmp.Or(strings.Compare(a.Position().Filename, b.Position().Filename), cmp.Compare(a.Position().Offset, b.Position().Offset))
	})
	return unused
}

// applyPadding applies a padding transaction when its pad is applied. Like
// beancount's pad plugin, which inserts padding after
// booking, the padding is not booked: each posting books its units as they
// are, without cost, which the Ledger publishes like any booked position.
func (l *Ledger) applyPadding(ctx context.Context, padding *ast.Transaction) {
	booked := &bookedTransaction{postings: make([]bookedPosting, len(padding.Postings))}
	for i, posting := range padding.Postings {
		positions := []BookedPosition{{Units: mustParseAmount(posting.Amount)}}
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
	padded := ast.NewPosting(pad.Account, ast.WithAmount(difference, currency))
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
