package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
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

	// Check for cost specification
	hasExplicitCost := posting.Cost != nil && !posting.Cost.IsEmpty() && !posting.Cost.IsMergeCost()
	hasEmptyCost := posting.Cost != nil && posting.Cost.IsEmpty()
	hasMergeCost := posting.Cost != nil && posting.Cost.IsMergeCost()
	hasPrice := posting.Price != nil

	var weights weightSet

	if hasEmptyCost || hasMergeCost {
		// Empty cost spec {} or merge cost {*} - cost will be inferred/calculated to balance the transaction
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

		var totalCost decimal.Decimal
		if posting.Cost.IsTotal {
			// Total cost {{X CURR}} - use the amount directly
			totalCost = costAmount
		} else {
			// Per-unit cost {X CURR} - multiply by quantity
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

		var priceWeight decimal.Decimal
		if posting.PriceTotal {
			// @@ total price with sign
			if amount.IsNegative() {
				priceWeight = priceAmount.Neg()
			} else {
				priceWeight = priceAmount
			}
		} else {
			// @ per-unit price
			priceWeight = amount.Mul(priceAmount)
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
