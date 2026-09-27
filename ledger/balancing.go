package ledger

import (
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// Balancing is the part of Booking that weighs a Currency group's postings,
// interpolates its missing numbers, and checks the residual against the
// transaction's tolerances (booker.calculateBalance). These are its helpers.

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

// statedUnits collects the units numbers written in full (number and
// currency) per currency, which beancount infers a transaction's tolerances
// from before interpolating.
func statedUnits(postings []*ast.Posting) map[string][]decimal.Decimal {
	stated := make(map[string][]decimal.Decimal)
	for _, posting := range postings {
		if posting.Amount == nil || posting.Amount.Value == "" || posting.Amount.Currency == "" {
			continue
		}
		if number, err := ParseAmount(posting.Amount); err == nil {
			stated[posting.Amount.Currency] = append(stated[posting.Amount.Currency], number)
		}
	}
	return stated
}

// maxQuantumDigits mirrors beancount's MAX_TOLERANCE_DIGITS: a quantum with
// this many significant digits is not a neat, user-like step to round to.
const maxQuantumDigits = 5

// maxCostTolerance mirrors beancount's MAXIMUM_TOLERANCE, the cap on what one
// posting held at cost or price adds to a tolerance.
var maxCostTolerance = decimal.RequireFromString("0.5")

// transactionTolerance returns a transaction's tolerance for currency: the
// one inferred from its units amounts, widened by what postings at cost or
// price add under infer_tolerance_from_cost.
func (b *booker) transactionTolerance(currency string, amounts []decimal.Decimal, costTolerances map[string]decimal.Decimal) decimal.Decimal {
	tolerance := InferTolerance(amounts, currency, b.config.Tolerance)
	if fromCost, ok := costTolerances[currency]; ok && fromCost.GreaterThan(tolerance) {
		return fromCost
	}
	return tolerance
}

// toleranceShare is what one posting, as beancount sees it at some stage,
// can add to a transaction's tolerances under infer_tolerance_from_cost.
type toleranceShare struct {
	units        decimal.Decimal
	hasCost      bool
	costNumbers  []decimal.Decimal // numbers the cost states; none widens by the cap
	costCurrency string
	price        *priceAmount
}

// costTolerances returns what postings held at cost or price add to a
// transaction's tolerances when infer_tolerance_from_cost is set, like
// beancount's infer_tolerances(use_cost=True). A share whose units have
// fractional digits, with units tolerance t, adds min(t × n, 0.5) to its
// cost currency (n the smallest cost number, or just 0.5 without one) and
// to its price currency (n the per-unit price). Contributions sum per
// currency.
func (b *booker) costTolerances(shares []toleranceShare) map[string]decimal.Decimal {
	if !b.config.Tolerance.InferFromCost {
		return nil
	}
	tolerances := make(map[string]decimal.Decimal)
	for _, share := range shares {
		if share.units.Exponent() >= 0 {
			continue
		}
		unitsTolerance := pydecimal.Mul(decimal.New(1, share.units.Exponent()), b.config.Tolerance.Multiplier)
		if share.hasCost && share.costCurrency != "" {
			contribution := maxCostTolerance
			for _, n := range share.costNumbers {
				contribution = decimal.Min(contribution, pydecimal.Mul(unitsTolerance, n))
			}
			tolerances[share.costCurrency] = pydecimal.Add(tolerances[share.costCurrency], contribution)
		}
		if share.price != nil {
			contribution := decimal.Min(maxCostTolerance, pydecimal.Mul(unitsTolerance, share.price.number))
			tolerances[share.price.currency] = pydecimal.Add(tolerances[share.price.currency], contribution)
		}
	}
	return tolerances
}

// statedUnitsNumber returns a posting's units number when the source states
// it with its currency; interpolated amounts never contribute tolerance.
func statedUnitsNumber(posting *ast.Posting) (decimal.Decimal, bool) {
	if posting.Inferred || posting.Amount == nil || posting.Amount.Value == "" || posting.Amount.Currency == "" {
		return decimal.Decimal{}, false
	}
	units, err := ParseAmount(posting.Amount)
	return units, err == nil
}

// specToleranceShares describes postings as beancount sees them before
// booking, when it rounds interpolated amounts: a cost spec contributes the
// numbers it states (per-unit, or total for {{...}}, and a compound total).
func specToleranceShares(postings []*ast.Posting) []toleranceShare {
	var shares []toleranceShare
	for _, posting := range postings {
		units, ok := statedUnitsNumber(posting)
		if !ok {
			continue
		}
		share := toleranceShare{units: units, price: perUnitPrice(posting)}
		if posting.Cost != nil {
			share.hasCost = true
			share.costCurrency = costCurrency(posting.Cost)
			for _, amount := range []*ast.Amount{posting.Cost.Amount, posting.Cost.Total} {
				if amount == nil || amount.Value == "" {
					continue
				}
				if n, err := ParseAmount(amount); err == nil {
					share.costNumbers = append(share.costNumbers, n)
				}
			}
		}
		shares = append(shares, share)
	}
	return shares
}

// bookedToleranceShares describes postings as beancount books them, when it
// checks the balance: a cost is its per-unit number, with inferred costs
// resolved, and a reduction against lots becomes one share per lot, like
// the booked postings beancount replaces it with.
func bookedToleranceShares(postings []*ast.Posting, delta *TransactionDelta, bookedLots map[*ast.Posting][]BookedLot) []toleranceShare {
	var shares []toleranceShare
	for _, posting := range postings {
		units, ok := statedUnitsNumber(posting)
		if !ok {
			continue
		}
		price := perUnitPrice(posting)

		if lots, ok := bookedLots[posting]; ok {
			for _, lot := range lots {
				shares = append(shares, toleranceShare{
					units:        lot.Units,
					hasCost:      true,
					costNumbers:  []decimal.Decimal{*lot.Cost},
					costCurrency: lot.CostCurrency,
					price:        price,
				})
			}
			continue
		}

		share := toleranceShare{units: units, price: price}
		if cost := delta.costFor(posting); cost != nil {
			share.hasCost = true
			if number, currency, ok := PerUnitCost(&ast.Posting{Amount: posting.Amount, Cost: cost}); ok {
				share.costNumbers = []decimal.Decimal{number}
				share.costCurrency = currency
			}
		}
		shares = append(shares, share)
	}
	return shares
}

type priceAmount struct {
	number   decimal.Decimal
	currency string
}

// perUnitPrice returns a posting's stated price per unit (PerUnitPrice),
// or nil when it has none or leaves its currency to interpolation.
func perUnitPrice(posting *ast.Posting) *priceAmount {
	number, currency, ok := PerUnitPrice(posting)
	if !ok || currency == "" {
		return nil
	}
	return &priceAmount{number: number, currency: currency}
}

// roundInterpolated rounds an interpolated units number like beancount's
// quantize_with_tolerance: half-to-even to twice the transaction's tolerance
// for its currency, when that step is a neat number. Without a tolerance
// the number is left as computed, so 10.00 USD - 3.333 USD books -6.67 USD
// while a price conversion keeps all its digits.
func roundInterpolated(number, tolerance decimal.Decimal) decimal.Decimal {
	if !tolerance.IsPositive() {
		return number
	}
	// Normalize the quantum (strip trailing zeros), as beancount does.
	quantum := pydecimal.Normalize(pydecimal.Mul(tolerance, decimal.NewFromInt(2)))
	if len(quantum.Coefficient().String()) >= maxQuantumDigits {
		return number
	}
	return number.RoundBank(-quantum.Exponent())
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
			add(w.Currency)
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
