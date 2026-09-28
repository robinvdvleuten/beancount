package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// tolerances answers every tolerance question the ledger asks, from the
// tolerance options (inferred_tolerance_default,
// inferred_tolerance_multiplier, infer_tolerance_from_cost): a
// transaction's tolerance per currency as Booking sees it before booking
// (spec) and after (booked), and a balance assertion's tolerance. The
// Ledger builds one per Process.
type tolerances struct {
	options *sharedconfig.Tolerance
}

// newTolerances takes the tolerance options, the defaults when nil.
func newTolerances(options *sharedconfig.Tolerance) tolerances {
	if options == nil {
		options = sharedconfig.NewTolerance()
	}
	return tolerances{options: options}
}

// maxQuantumDigits mirrors beancount's MAX_TOLERANCE_DIGITS: a quantum with
// this many significant digits is not a neat, user-like step to round to.
const maxQuantumDigits = 5

// maxCostTolerance mirrors beancount's MAXIMUM_TOLERANCE, the cap on what one
// posting held at cost or price adds to a tolerance.
var maxCostTolerance = decimal.RequireFromString("0.5")

// ofNumber is the tolerance a number's precision implies: the multiplier
// times its last digit's unit (0.005 for 1.00 at 0.5). A whole number
// implies none. Every tolerance rule builds on it.
func (t tolerances) ofNumber(number decimal.Decimal) (decimal.Decimal, bool) {
	exp := number.Exponent()
	if exp >= 0 {
		return decimal.Zero, false
	}
	return pydecimal.Mul(decimal.New(1, exp), t.options.Multiplier), true
}

// spec returns a transaction's tolerances as beancount sees its postings
// before booking, when it rounds interpolated numbers: from the units
// numbers written in full and, under infer_tolerance_from_cost, the numbers
// cost specs state.
func (t tolerances) spec(postings []*ast.Posting) transactionTolerances {
	return transactionTolerances{
		tolerances: t,
		units:      statedUnits(postings),
		fromCost:   t.fromCost(specToleranceShares(postings)),
	}
}

// booked returns a transaction's tolerances as beancount sees its postings
// once booked, when it checks the residual: from every units number,
// interpolated ones included, and, under infer_tolerance_from_cost, costs
// per unit with inferred costs resolved and a reduction split per lot.
func (t tolerances) booked(postings []*ast.Posting, delta *TransactionDelta, reducedPositions map[*ast.Posting][]BookedPosition) transactionTolerances {
	return transactionTolerances{
		tolerances: t,
		units:      bookedUnits(postings, delta),
		fromCost:   t.fromCost(bookedToleranceShares(postings, delta, reducedPositions)),
	}
}

// balance returns a balance assertion's tolerance, like beancount's
// get_balance_tolerance: the one it states after ~, or else twice what the
// asserted number's precision implies, as user-provided balances may be
// rounded further off than the amounts within a single transaction.
func (t tolerances) balance(balance *ast.Balance) (decimal.Decimal, error) {
	if balance.Tolerance != nil {
		return ParseAmount(balance.Tolerance)
	}
	amount, err := ParseAmount(balance.Amount)
	if err != nil {
		return decimal.Zero, err
	}
	tolerance, ok := t.ofNumber(amount)
	if !ok {
		return decimal.Zero, nil
	}
	return pydecimal.Mul(tolerance, decimal.NewFromInt(2)), nil
}

// inferred returns the tolerance for currency inferred from its units
// numbers, like beancount's infer_tolerances: the coarsest precision wins,
// and the currency's configured default joins the maximum. Without a
// fractional number, it is the configured default, falling back to "*".
func (t tolerances) inferred(currency string, numbers []decimal.Decimal) decimal.Decimal {
	inferred := decimal.Zero
	found := false
	for _, number := range numbers {
		if tolerance, ok := t.ofNumber(number); ok && (!found || tolerance.GreaterThan(inferred)) {
			inferred = tolerance
			found = true
		}
	}
	if !found {
		return t.options.GetDefault(currency)
	}
	if def, ok := t.options.Defaults[currency]; ok && def.GreaterThan(inferred) {
		return def
	}
	return inferred
}

// fromCost returns what postings held at cost or price add to a
// transaction's tolerances when infer_tolerance_from_cost is set, like
// beancount's infer_tolerances(use_cost=True). A share whose units have
// fractional digits, with units tolerance t, adds min(t × n, 0.5) to its
// cost currency (n the smallest cost number, or just 0.5 without one) and
// to its price currency (n the per-unit price). Contributions sum per
// currency.
func (t tolerances) fromCost(shares []toleranceShare) map[string]decimal.Decimal {
	if !t.options.InferFromCost {
		return nil
	}
	fromCost := make(map[string]decimal.Decimal)
	for _, share := range shares {
		unitsTolerance, ok := t.ofNumber(share.units)
		if !ok {
			continue
		}
		if share.hasCost && share.costCurrency != "" {
			contribution := maxCostTolerance
			for _, n := range share.costNumbers {
				contribution = decimal.Min(contribution, pydecimal.Mul(unitsTolerance, n))
			}
			fromCost[share.costCurrency] = pydecimal.Add(fromCost[share.costCurrency], contribution)
		}
		if share.price != nil {
			contribution := decimal.Min(maxCostTolerance, pydecimal.Mul(unitsTolerance, share.price.number))
			fromCost[share.price.currency] = pydecimal.Add(fromCost[share.price.currency], contribution)
		}
	}
	return fromCost
}

// transactionTolerances is one transaction's tolerance per currency at one
// stage of Booking.
type transactionTolerances struct {
	tolerances tolerances
	units      map[string][]decimal.Decimal
	fromCost   map[string]decimal.Decimal
}

// of returns the transaction's tolerance for currency: the one inferred
// from its units numbers, widened by what postings at cost or price add
// under infer_tolerance_from_cost.
func (tt transactionTolerances) of(currency string) decimal.Decimal {
	tolerance := tt.tolerances.inferred(currency, tt.units[currency])
	if fromCost, ok := tt.fromCost[currency]; ok && fromCost.GreaterThan(tolerance) {
		return fromCost
	}
	return tolerance
}

// round rounds an interpolated number in currency like beancount's
// quantize_with_tolerance: half-to-even to twice the transaction's tolerance
// for the currency, when that step is a neat number. Without a tolerance
// the number is left as computed, so 10.00 USD - 3.333 USD books -6.67 USD
// while a price conversion keeps all its digits.
func (tt transactionTolerances) round(currency string, number decimal.Decimal) decimal.Decimal {
	tolerance := tt.of(currency)
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

// bookedUnits collects every units number per currency once booked,
// interpolated ones included.
func bookedUnits(postings []*ast.Posting, delta *TransactionDelta) map[string][]decimal.Decimal {
	booked := make(map[string][]decimal.Decimal)
	for _, posting := range postings {
		amount := delta.amountFor(posting)
		if amount == nil {
			continue
		}
		if number, err := ParseAmount(amount); err == nil {
			booked[amount.Currency] = append(booked[amount.Currency], number)
		}
	}
	return booked
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
func bookedToleranceShares(postings []*ast.Posting, delta *TransactionDelta, reducedPositions map[*ast.Posting][]BookedPosition) []toleranceShare {
	var shares []toleranceShare
	for _, posting := range postings {
		units, ok := statedUnitsNumber(posting)
		if !ok {
			continue
		}
		price := perUnitPrice(posting)

		if positions, ok := reducedPositions[posting]; ok {
			for _, position := range positions {
				shares = append(shares, toleranceShare{
					units:        position.Units,
					hasCost:      true,
					costNumbers:  []decimal.Decimal{position.Cost.Number},
					costCurrency: position.Cost.Currency,
					price:        price,
				})
			}
			continue
		}

		share := toleranceShare{units: units, price: price}
		if cost := delta.costFor(posting); cost != nil {
			share.hasCost = true
			if number, currency, ok := perUnitCost(&ast.Posting{Amount: posting.Amount, Cost: cost}); ok {
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
