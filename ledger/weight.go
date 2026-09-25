package ledger

import (
	"fmt"

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

	// Check for cost specification. A cost spec without a number (empty {},
	// currency-only {USD}, or date/label-only like {2020-02-01}) has its cost
	// resolved from booked lots or inferred, so it contributes no weight here.
	hasExplicitCost := posting.Cost.HasNumber()
	hasEmptyCost := posting.Cost != nil && !hasExplicitCost
	hasPrice := posting.Price != nil

	var weights weightSet

	if hasEmptyCost {
		// Amount-less cost spec - cost will be inferred/calculated to balance the transaction
		// Return empty weights; cost inference happens in processTransaction()
		return weightSet{}, nil

	} else if hasExplicitCost {
		// When there's a cost, only the cost contributes to balance; a
		// price is informational. Like beancount, a total or compound cost
		// becomes a per-unit cost first, and the weight is the units times
		// that (possibly rounded) per-unit cost.
		perUnit, costCurrency, ok := PerUnitCost(posting)
		if !ok {
			return nil, fmt.Errorf("invalid cost specification")
		}
		weights = weightSet{
			{Amount: pydecimal.Mul(amount, perUnit), Currency: costCurrency},
		}

	} else if hasPrice {
		// Price only: the units convert at the per-unit price.
		perUnit, priceCurrency, ok := PerUnitPrice(posting)
		if !ok {
			return nil, fmt.Errorf("invalid price specification")
		}
		weights = weightSet{
			{Amount: pydecimal.Mul(amount, perUnit), Currency: priceCurrency},
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
			balance[weight.Currency] = pydecimal.Add(current, weight.Amount)
		}
	}

	return balance
}
