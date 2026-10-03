package ledger

import (
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Helpers of interpolation (interpolation.go), the part of Booking that
// weighs a Currency group's postings, interpolates its missing number, and
// checks the residual against the transaction's tolerances.

// postingClassification groups postings by their characteristics
// This makes the processing logic clearer and prevents misclassification
type postingClassification struct {
	withAmounts       []*ast.Posting
	withoutAmounts    []*ast.Posting
	incompleteAmounts []*ast.Posting // amount present but missing number or currency
	incompletePrices  []*ast.Posting // complete amount, price annotation missing number or currency
	withEmptyCosts    []*ast.Posting
	withExplicitCost  []*ast.Posting
}

// classifyPostings categorizes postings for different processing paths
func classifyPostings(postings []*ast.Posting) postingClassification {
	var pc postingClassification
	for _, posting := range postings {
		switch {
		case posting.Amount == nil:
			pc.withoutAmounts = append(pc.withoutAmounts, posting)
		case isIncompleteAmount(posting.Amount):
			pc.incompleteAmounts = append(pc.incompleteAmounts, posting)
		default:
			pc.withAmounts = append(pc.withAmounts, posting)

			if posting.Price != nil && isIncompleteAmount(posting.Price) {
				pc.incompletePrices = append(pc.incompletePrices, posting)
			}

			// Cost specs without a number (empty {}, currency-only {USD}
			// or date/label-only) need their cost resolved from booked
			// lots or inferred.
			if posting.Cost != nil {
				if !posting.Cost.HasNumber() {
					pc.withEmptyCosts = append(pc.withEmptyCosts, posting)
				} else {
					pc.withExplicitCost = append(pc.withExplicitCost, posting)
				}
			}
		}
	}
	return pc
}

// unitsWeightTerms returns how a posting whose units number is missing
// weighs: in its cost currency at a per-unit cost (plus a compound total),
// in its price currency at a price, or in its own currency. ok is false
// when the units cannot be solved for, as with a total-only or empty cost.
func unitsWeightTerms(posting *ast.Posting) (currency string, perUnit, total decimal.Decimal, ok bool) {
	if cost := posting.Cost; cost != nil {
		if cost.IsTotal || cost.Amount == nil || cost.Amount.Value == "" {
			return "", decimal.Zero, decimal.Zero, false
		}
		perUnit, err := ParseAmount(cost.Amount)
		if err != nil || perUnit.IsZero() {
			return "", decimal.Zero, decimal.Zero, false
		}
		if cost.Total != nil {
			if total, err = ParseAmount(cost.Total); err != nil {
				return "", decimal.Zero, decimal.Zero, false
			}
		}
		return cost.Amount.Currency, perUnit, total, true
	}
	if price := posting.Price; price != nil && price.Value != "" && price.Currency != "" && !posting.PriceTotal {
		perUnit, err := ParseAmount(price)
		if err != nil || perUnit.IsZero() {
			return "", decimal.Zero, decimal.Zero, false
		}
		return price.Currency, perUnit, decimal.Zero, true
	}
	return posting.Amount.Currency, decimal.Zero, decimal.Zero, true
}

// residualCurrencies returns the currencies with a non-zero residual, in the
// order their weights first appear in the transaction (beancount orders its
// currency groups by first posting), then any others sorted.
func residualCurrencies(allWeights []weightSet, balance map[string]decimal.Decimal) []string {
	var currencies []string
	seen := make(map[string]bool, len(balance))
	add := func(currency string) {
		if !seen[currency] && !balance[currency].IsZero() {
			currencies = append(currencies, currency)
		}
		seen[currency] = true
	}
	for _, weights := range allWeights {
		for _, w := range weights {
			add(w.currency)
		}
	}
	rest := make([]string, 0, len(balance))
	for currency := range balance {
		if !seen[currency] {
			rest = append(rest, currency)
		}
	}
	slices.Sort(rest)
	for _, currency := range rest {
		add(currency)
	}
	return currencies
}

func costCurrency(cost *ast.Cost) string {
	if cost.Amount != nil && cost.Amount.Currency != "" {
		return cost.Amount.Currency
	}
	if cost.Total != nil {
		return cost.Total.Currency
	}
	return ""
}
