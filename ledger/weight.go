package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// Weight represents the contribution of a posting to the transaction balance
// A posting can contribute multiple weights (e.g., commodity + cost currency)
type weight struct {
	Amount   decimal.Decimal
	Currency string
}

// weightSet is a collection of weights from a single posting
type weightSet []weight

// calculateWeights calculates all weights contributed by a posting
// This handles cost basis and price annotations
func calculateWeights(posting *ast.Posting) (weightSet, error) {
	if posting.Amount == nil {
		// No amount specified - this will be inferred (not implemented yet)
		return weightSet{}, nil
	}

	// Parse the main amount
	amount, err := ParseAmount(posting.Amount)
	if err != nil {
		return nil, err
	}

	currency := posting.Amount.Currency

	// Check for cost specification. A cost spec without an amount (empty {},
	// or date/label-only like {2020-02-01}) has its cost resolved from booked
	// lots or inferred, so it contributes no weight here.
	hasExplicitCost := posting.Cost != nil && posting.Cost.Amount != nil
	hasEmptyCost := posting.Cost != nil && posting.Cost.Amount == nil
	hasPrice := posting.Price != nil

	var weights weightSet

	if hasEmptyCost {
		// Amount-less cost spec - cost will be inferred/calculated to balance the transaction
		// Return empty weights; cost inference happens in processTransaction()
		return weightSet{}, nil

	} else if hasExplicitCost {
		// Cost: {X CURR} or {X CURR} @ Y CURR2 or {{X CURR}} (total cost)
		// When there's a cost, ONLY the cost contributes to balance!
		// The price (if present) is just informational (market value)
		costAmount, err := ParseAmount(posting.Cost.Amount)
		if err != nil {
			return nil, err
		}

		costCurrency := posting.Cost.Amount.Currency

		// Like beancount, a total cost {{X CURR}} or compound cost
		// {X # Y CURR} becomes a per-unit cost first, and the weight is the
		// units times that (possibly rounded) per-unit cost.
		var totalCost decimal.Decimal
		if posting.Cost.IsTotal {
			totalCost = perUnitWeight(amount, costAmount)
		} else {
			if posting.Cost.Total != nil {
				additionalTotal, err := ParseAmount(posting.Cost.Total)
				if err != nil {
					return nil, err
				}
				if !amount.IsZero() {
					costAmount = costAmount.Add(pydecimal.Quo(additionalTotal, amount.Abs()))
				}
			}
			totalCost = amount.Mul(costAmount)
		}

		weights = weightSet{
			{Amount: totalCost, Currency: costCurrency},
		}

	} else if hasPrice {
		// Price only: @ or @@
		// When there's only a price, use it for balance
		priceAmount, err := ParseAmount(posting.Price)
		if err != nil {
			return nil, err
		}

		priceCurrency := posting.Price.Currency

		priceWeight := amount.Mul(priceAmount)
		if posting.PriceTotal {
			priceWeight = perUnitWeight(amount, priceAmount)
		}

		weights = weightSet{
			{Amount: priceWeight, Currency: priceCurrency},
		}

	} else {
		// No cost or price: just the commodity amount
		weights = weightSet{
			{Amount: amount, Currency: currency},
		}
	}

	return weights, nil
}

// perUnitWeight returns the weight of units bought at a total, derived like
// beancount from the per-unit number total / |units|: 3 units at a total of
// 10 weigh 9.999999999999999999999999999. Zero units weigh the signless total.
func perUnitWeight(units, total decimal.Decimal) decimal.Decimal {
	if units.IsZero() {
		return total
	}
	return units.Mul(pydecimal.Quo(total, units.Abs()))
}

// balanceWeights accumulates weights from multiple postings
// Returns a map of currency -> total amount
// NOTE: Caller must call putBalanceMap() when done with the returned map
func balanceWeights(allWeights []weightSet) map[string]decimal.Decimal {
	balance := getBalanceMap()

	for _, weights := range allWeights {
		for _, weight := range weights {
			current := balance[weight.Currency]
			balance[weight.Currency] = current.Add(weight.Amount)
		}
	}

	return balance
}
