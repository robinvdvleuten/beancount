package parser

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// The errors beancount's grammar raises as it reads a posting, in its
// cost_spec and posting rules, in its words. Each keeps its directive, and,
// raised as soon as the rule is read, stays reported when a later syntax
// error drops the transaction. Booking fixes up what they point at.

// reportCost reports what beancount's cost_spec rule does once a posting's
// cost spec is read: a merge marker, which beancount does not support yet,
// each component the spec repeats, ignored as the parser keeps the first,
// and a compound inside total braces, whose per-unit number is ignored.
func (p *Parser) reportCost(posting *ast.Posting) {
	cost := posting.Cost
	if cost.IsMerge {
		p.reportPosting(posting, "Cost merging is not supported yet")
	}
	for _, duplicate := range cost.Duplicates {
		switch {
		case duplicate.Amount != nil:
			p.reportPosting(posting, "Duplicate cost: '%s'.", compoundAmountText(duplicate))
		case duplicate.Date != nil:
			p.reportPosting(posting, "Duplicate date: '%s'.", duplicate.Date.Format("2006-01-02"))
		case duplicate.IsMerge:
			p.reportPosting(posting, "Duplicate merge-cost spec")
		default:
			p.reportPosting(posting, "Duplicate label: '%s'.", duplicate.Label)
		}
	}
	if cost.IsTotal && cost.Amount != nil && cost.Total != nil {
		p.reportPosting(posting, "Per-unit cost may not be specified using total cost syntax: '%s'; ignoring per-unit cost",
			compoundAmountText(cost))
	}
}

// reportPrice reports what beancount's posting rule does once the posting's
// line is read: a negative price, made positive; a total price on a posting
// without units, dropped; and otherwise a price in another currency than
// the cost.
func (p *Parser) reportPrice(posting *ast.Posting) {
	price := posting.Price
	if price == nil {
		return
	}
	if number, err := decimal.NewFromString(price.Value); err == nil && number.IsNegative() {
		p.reportPosting(posting, "Negative prices are not allowed: %s %s", price.Value, price.Currency)
	}
	if posting.PriceTotal && (posting.Amount == nil || posting.Amount.Value == "") {
		p.reportPosting(posting, "Total price on a posting without units: %s %s", price.Value, price.Currency)
		return
	}
	if cost := posting.Cost.Currency(); cost != "" && price.Currency != "" && cost != price.Currency {
		p.reportPosting(posting, "Cost and price currencies must match: %s != %s", cost, price.Currency)
	}
}

// reportPosting records an error on the posting's line that keeps its
// directive.
func (p *Parser) reportPosting(posting *ast.Posting, format string, args ...any) {
	pos := posting.Position()
	p.errs = append(p.errs, newErrorfWithSource(pos, p.calculateSourceRange(pos), format, args...).kept())
}

// compoundAmountText is a cost's amount as it is written: its per-unit
// number, its total after a #, and its currency, each when present. It is
// not CompoundAmount's Python repr, which bean-check prints here.
func compoundAmountText(cost *ast.Cost) string {
	var parts []string
	if cost.Amount.Value != "" {
		parts = append(parts, cost.Amount.Value)
	}
	if cost.Total != nil {
		parts = append(parts, "#", cost.Total.Value)
	}
	if cost.Amount.Currency != "" {
		parts = append(parts, cost.Amount.Currency)
	}
	return strings.Join(parts, " ")
}
