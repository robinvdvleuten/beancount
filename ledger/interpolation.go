package ledger

import (
	"fmt"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// Interpolation completes one Currency group, like beancount's
// interpolate_group: it weighs the group's postings (weight.go), fills in
// the one number the group may leave out, rounded to the transaction's
// tolerances (tolerance.go), and reports the residual the booked group
// leaves. It writes nothing: the booker commits what it returns.

// interpolatedGroup is a Currency group with its numbers complete.
type interpolatedGroup struct {
	// postings are the group's postings, in the group's order.
	postings []interpolatedPosting
	// residuals holds what the group's weights leave beyond the booked
	// tolerances, in the order their currencies first appear; it is empty
	// when the group balances.
	residuals []residual
	// errs are what interpolation reports on a group it still books, as
	// beancount's interpolate_group keeps a group it cannot complete.
	errs []error
}

// interpolatedPosting is what Booking commits for one posting of a group.
type interpolatedPosting struct {
	posting *ast.Posting
	// amount, cost and price are the posting's units, cost and price where
	// interpolation completed them with a number or a currency, nil where
	// they stay as written. The amount-less posting's amount is the first
	// of its amounts.
	amount *ast.Amount
	cost   *ast.Cost
	price  *ast.Amount
	// leftOut is set when the posting's missing units interpolate to a zero
	// weight, or its missing cost is on zero units: beancount leaves such a
	// posting out of the booked transaction.
	leftOut bool
	// amounts are the amounts the group books the amount-less posting at,
	// one per currency with a non-zero residual; none when the group leaves
	// it nothing.
	amounts []*ast.Amount
}

// interpolate completes group's missing numbers, rounded to specTolerances,
// given the positions its reductions booked. The errors make it a Dropped
// group: an amount that does not parse, more than one missing number, or a
// missing number the group's residual does not determine, reported as a
// transaction that does not balance.
func interpolate(txn *ast.Transaction, group currencyGroup, reductions map[*ast.Posting][]BookedPosition, specTolerances transactionTolerances) (*interpolatedGroup, []error) {
	var errs []error
	pc := classifyPostings(group.postings)

	// Calculate weights for postings with amounts
	var allWeights []weightSet
	// Positions that reductions with an amount-less cost spec booked, one
	// per lot. Every other amount-less cost spec is an augmentation's, whose
	// cost is inferred from the residual.
	reducedPositions := make(map[*ast.Posting][]BookedPosition)
	for _, posting := range pc.withAmounts {
		// A partial price annotation leaves the weight of a posting without
		// cost unknown; it is resolved from the residual during
		// interpolation below. Held at cost, the posting weighs at its cost.
		if posting.Cost == nil && posting.Price != nil && isIncompleteAmount(posting.Price) {
			continue
		}

		weights, err := calculateWeights(posting)
		if err != nil {
			errs = append(errs, newInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
			continue
		}

		// Check if this is a cost spec without an amount (returns empty weights)
		if len(weights) == 0 && posting.Cost != nil && !posting.Cost.HasNumber() {
			// Reductions resolve their weight from the booked lots' cost basis,
			// matching beancount, which books lots before interpolation. The
			// spec's date/label (if any) narrows which lots are booked.
			// Augmentations, NONE's included, are handled in cost inference
			// below.
			if positions, ok := reductions[posting]; ok {
				var weights weightSet
				for _, position := range positions {
					weights = append(weights, weight{
						amount:   pydecimal.Mul(position.Units, position.Cost.Number),
						currency: position.Cost.Currency,
					})
				}
				allWeights = append(allWeights, weights)
				reducedPositions[posting] = positions
			}
		} else {
			allWeights = append(allWeights, weights)
		}
	}

	if len(errs) > 0 {
		return nil, errs
	}
	// Beancount interpolates at most one missing number per group. This is
	// the one place that rule is decided: past it, at most one of the steps
	// below finds a number to complete.
	if first := tooManyMissing(group, reducedPositions); first != nil {
		return nil, []error{newInterpolationError(txn, first,
			fmt.Sprintf("Too many missing numbers for currency group '%s'", group.currency))}
	}

	// Balance the weights
	balance := balanceWeights(allWeights)
	defer putBalanceMap(balance)

	amounts := make(map[*ast.Posting]*ast.Amount)
	costs := make(map[*ast.Posting]*ast.Cost)
	prices := make(map[*ast.Posting]*ast.Amount)
	var leftOut []*ast.Posting

	// Booking has resolved every units and price currency
	// (resolveCurrencies), so an incomplete amount or price leaves out its
	// number, the group's one missing number.
	var currencyOnlyAmount, valuelessPrice *ast.Posting
	if len(pc.incompleteAmounts) > 0 {
		currencyOnlyAmount = pc.incompleteAmounts[0]
	}
	if len(pc.incompletePrices) > 0 {
		valuelessPrice = pc.incompletePrices[0]
	}

	// The posting without an amount absorbs the residual of every weight
	// currency, so it is booked once per currency with a non-zero residual,
	// in the order the currencies first appear.
	var autoPosting *ast.Posting
	var autoAmounts []*ast.Amount
	if len(pc.withoutAmounts) == 1 {
		autoPosting = pc.withoutAmounts[0]

		for _, currency := range residualCurrencies(allWeights, balance) {
			needed := specTolerances.round(currency, balance[currency].Neg())
			autoAmounts = append(autoAmounts, &ast.Amount{
				Value:    formatInferredNumber(needed),
				Currency: currency,
			})
			balance[currency] = pydecimal.Add(balance[currency], needed)
		}

		if len(autoAmounts) > 0 {
			amounts[autoPosting] = autoAmounts[0]
		}
	}

	// Like beancount's interpolate_group, units cannot be solved for at a
	// zero per-unit cost, as total braces have: the group is still booked,
	// without the posting, and its residual reported.
	var kept []error
	if posting := currencyOnlyAmount; posting != nil && zeroPerUnitCost(posting.Cost) {
		kept = append(kept, newInterpolationError(txn, posting, "Cannot infer per-unit cost only from total"))
		leftOut = append(leftOut, posting)
		currencyOnlyAmount = nil
	}

	// Complete a currency-only amount: the missing number is the residual
	// of the currency it balances in. Held at a per-unit cost or at a price,
	// the units are the residual of that currency divided by it (less a
	// compound cost's total part), like beancount's interpolate_group.
	if posting := currencyOnlyAmount; posting != nil {
		currency := posting.Amount.Currency
		weightCurrency, perUnit, total, ok := unitsWeightTerms(posting)
		if !ok {
			// Past tooManyMissing and zeroPerUnitCost, only a zero price
			// in another currency than the units is left, at which
			// beancount fails an assertion.
			return nil, []error{newInterpolationError(txn, posting, "Cannot infer units at a zero price")}
		}

		weight := balance[weightCurrency].Neg()
		if weight.IsZero() {
			leftOut = append(leftOut, posting)
		}
		// Without a cost or a per-unit price, perUnit is zero and the units
		// weigh as themselves, even where the cost or price is in the
		// units' own currency.
		needed := weight
		if !perUnit.IsZero() {
			needed = pydecimal.Quo(pydecimal.Sub(weight, total), perUnit)
		}
		needed = specTolerances.round(currency, needed)
		amounts[posting] = &ast.Amount{
			Value:    formatInferredNumber(needed),
			Currency: currency,
		}
		switch {
		case weight.IsZero():
			// Left out, the posting weighs nothing, though at a compound
			// cost its units are not zero.
		case perUnit.IsZero() && posting.Cost == nil && posting.Price != nil && posting.Price.Value != "" && !posting.PriceTotal:
			// At a zero price in their own currency the units are the
			// residual and weigh nothing, so the residual stays.
		case perUnit.IsZero():
			balance[currency] = pydecimal.Add(balance[currency], needed)
		case posting.Cost != nil && posting.Cost.Total != nil && !needed.IsZero():
			// Like beancount's, the units weigh at the compound's per-unit
			// cost, which spreads its total over their absolute value: for
			// negative units, that leaves twice the total as a residual.
			perUnit = compoundCostNumber(perUnit, total, needed)
			balance[weightCurrency] = pydecimal.Add(balance[weightCurrency], pydecimal.Mul(needed, perUnit))
		default:
			balance[weightCurrency] = pydecimal.Add(balance[weightCurrency], pydecimal.Add(pydecimal.Mul(needed, perUnit), total))
		}
	}

	// Complete a value-less price annotation (bare @ or currency-only): like
	// beancount's interpolate_group, the price is the residual's weight per
	// unit with its sign dropped, and the posting then weighs its units at
	// that price, which leaves a residual when the signs disagree.
	// Like beancount's, a price is never inferred for units held at cost:
	// the group is still booked, the posting weighing at its cost and its
	// price left without a number, and its residual reported.
	if posting := valuelessPrice; posting != nil && posting.Cost != nil {
		kept = append(kept, newInterpolationError(txn, posting, "Cannot infer price for postings with units held at cost"))
		valuelessPrice = nil
	}
	if posting := valuelessPrice; posting != nil {
		units, err := ParseAmount(posting.Amount)
		if err != nil {
			return nil, []error{newInvalidAmountError(txn, posting.Account, posting.Amount.Value, err)}
		}

		currency := posting.Price.Currency
		weight := balance[currency].Neg()
		if weight.IsZero() {
			// beancount's plain ZERO, as for a cost below.
			weight = pydecimal.Zero
		}
		priceNumber := weight.Abs()
		if !units.IsZero() {
			perUnit := pydecimal.Quo(weight, units).Abs()
			if !posting.PriceTotal {
				priceNumber = perUnit
			}
			weight = pydecimal.Mul(units, perUnit)
		}
		// The price is written as Python writes the quotient: 110.00
		// over 2 is 55.00, and over 100.00 it is 1.1.
		prices[posting] = &ast.Amount{Value: pydecimal.String(priceNumber), Currency: currency}
		balance[currency] = pydecimal.Add(balance[currency], weight)
	}

	// Infer the cost an augmentation's cost spec leaves out; a reduction's
	// comes from its lots.
	for _, posting := range pc.withEmptyCosts {
		amount, err := ParseAmount(posting.Amount)
		if err != nil {
			continue
		}

		// Like beancount's interpolate_group, zero units leave the cost
		// undefined, so the posting is left out of the booked transaction.
		if amount.IsZero() {
			leftOut = append(leftOut, posting)
			continue
		}
		if _, reduced := reducedPositions[posting]; reduced {
			continue
		}

		if len(balance) > 1 {
			// Multiple currencies - ambiguous
			nonZero := func(string) decimal.Decimal { return decimal.Zero }
			return nil, []error{newTransactionNotBalancedError(txn, residualsBeyond(allWeights, balance, nonZero))}
		}
		// The group's residual, zero when nothing else in the group
		// weighs, as for {USD} next to postings in other currencies.
		currency := group.currency
		residual := balance[currency]
		// The weight the posting must carry, and the number that gives
		// it, like beancount's COST_PER and COST_TOTAL; the weight is
		// then the units at the lot's (rounded) per-unit cost.
		needed := residual.Neg()
		if needed.IsZero() {
			// beancount's residual inventory is empty then, and its
			// weight the plain ZERO, whatever exponent the sum left.
			needed = pydecimal.Zero
		}
		cost := posting.Cost
		completed := *cost
		completed.Inferred = true
		var perUnit decimal.Decimal
		switch {
		case cost.Total != nil && cost.Total.Value == "":
			// {5 # USD}: the total is what the per-unit part leaves.
			per, _ := ParseAmount(cost.Amount)
			total := pydecimal.Sub(needed, pydecimal.Mul(per, amount))
			completed.Total = &ast.Amount{Value: formatInferredNumber(total), Currency: currency}
			perUnit = compoundCostNumber(per, total, amount)
		case cost.Total != nil:
			// {# 5 USD}: the per-unit number is what the total leaves.
			total, _ := ParseAmount(cost.Total)
			per := pydecimal.Quo(pydecimal.Sub(needed, total), amount)
			completed.Amount = &ast.Amount{Value: formatInferredNumber(per), Currency: currency}
			perUnit = compoundCostNumber(per, total, amount)
		case cost.IsTotal:
			// {{USD}} completes its total, beancount's number_total,
			// which is the weight less the per-unit part total braces
			// have, ZERO: that is what sets the exponent.
			total := pydecimal.Sub(needed, pydecimal.Mul(pydecimal.Zero, amount))
			completed.Amount = &ast.Amount{Value: formatInferredNumber(total), Currency: currency}
			perUnit = compoundCostNumber(pydecimal.Zero, total, amount)
		default:
			perUnit = pydecimal.Quo(needed, amount)
			completed.Amount = &ast.Amount{Value: formatInferredNumber(perUnit), Currency: currency}
		}
		costs[posting] = &completed
		balance[currency] = pydecimal.Add(residual, pydecimal.Mul(amount, perUnit))
	}

	// Check if balanced after inference, within the tolerances of the
	// booked postings.
	// Like beancount's infer_tolerances, which skips the postings marked
	// __automatic__, a posting with a number left to interpolation adds
	// nothing to the tolerances the residual is checked against, in any
	// Currency group of its transaction.
	stated := make([]*ast.Posting, 0, len(txn.Postings))
	for _, posting := range txn.Postings {
		if _, reduced := reducedPositions[posting]; reduced || !leavesNumberOut(posting) {
			stated = append(stated, posting)
		}
	}
	bookedTolerances := specTolerances.tolerances.booked(stated, amounts, costs, reducedPositions)
	residuals := residualsBeyond(allWeights, balance, bookedTolerances.of)

	interpolated := &interpolatedGroup{
		postings:  make([]interpolatedPosting, len(group.postings)),
		residuals: residuals,
		errs:      kept,
	}
	for i, posting := range group.postings {
		interpolated.postings[i] = interpolatedPosting{
			posting: posting,
			amount:  amounts[posting],
			cost:    costs[posting],
			price:   prices[posting],
			leftOut: slices.Contains(leftOut, posting),
		}
		if posting == autoPosting {
			interpolated.postings[i].amounts = autoAmounts
		}
	}
	return interpolated, nil
}

// tooManyMissing returns the first posting of a Currency group with a missing
// number when the group has more than one, which beancount cannot
// interpolate: a missing units number (or no amount at all), a missing cost
// number (a compound's two count twice) other than on a reduction booked
// against lots, or a missing price number, once per lot for such a
// reduction.
func tooManyMissing(group currencyGroup, reducedPositions map[*ast.Posting][]BookedPosition) *ast.Posting {
	var first *ast.Posting
	missing := 0
	for _, posting := range group.postings {
		n := 0
		if posting.Amount == nil || posting.Amount.Value == "" {
			n++
		}
		if _, reduced := reducedPositions[posting]; posting.Cost != nil && !reduced {
			n += missingCostNumbers(posting.Cost)
		}
		if posting.Price != nil && posting.Price.Value == "" {
			// A reduction is one posting per lot it is booked against,
			// each with the price left out.
			n += max(1, len(reducedPositions[posting]))
		}
		if n > 0 && first == nil {
			first = posting
		}
		missing += n
	}
	if missing > 1 {
		return first
	}
	return nil
}

// zeroPerUnitCost reports whether a cost's per-unit number is zero, like
// beancount's number_per of total braces ({{100 USD}}) or {0 # 100 USD}.
func zeroPerUnitCost(cost *ast.Cost) bool {
	if cost == nil {
		return false
	}
	if cost.IsTotal {
		return true
	}
	if cost.Amount == nil || cost.Amount.Value == "" {
		return false
	}
	number, err := ParseAmount(cost.Amount)
	return err == nil && number.IsZero()
}

// missingCostNumbers counts the numbers a cost leaves to Booking, like
// beancount's COST_PER and COST_TOTAL: its per-unit number (a total cost's
// number, for total braces), and a compound's total.
func missingCostNumbers(cost *ast.Cost) int {
	n := 0
	if cost.Amount == nil || cost.Amount.Value == "" {
		n++
	}
	if cost.Total != nil && cost.Total.Value == "" {
		n++
	}
	return n
}

// leavesNumberOut reports whether a posting leaves a number to
// interpolation: its units, its price, or the number of its cost. A
// reduction's cost comes from its lots, which the caller tells apart.
func leavesNumberOut(posting *ast.Posting) bool {
	if posting.Amount == nil || posting.Amount.Value == "" {
		return true
	}
	if posting.Price != nil && posting.Price.Value == "" {
		return true
	}
	cost := posting.Cost
	if cost == nil {
		return false
	}
	if cost.Amount == nil || cost.Amount.Value == "" {
		// {}, {USD}, {2020-01-01} and {# 5 USD} leave the per-unit number
		// out; {{}} and its kind are per-unit costs without one too.
		return true
	}
	return cost.Total != nil && cost.Total.Value == ""
}
