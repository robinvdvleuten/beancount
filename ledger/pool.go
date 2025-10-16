package ledger

import (
	"sync"

	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

// Pools for commonly allocated objects to reduce GC pressure

var (
	// balanceMapPool provides pooled maps for BalanceWeights calculations
	balanceMapPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]decimal.Decimal, 4) // typical transaction has 2-4 currencies
		},
	}

	// inferredAmountsMapPool provides pooled maps for amount inference
	inferredAmountsMapPool = sync.Pool{
		New: func() interface{} {
			return make(map[*parser.Posting]*parser.Amount, 2)
		},
	}
)

// getBalanceMap retrieves a pooled balance map
func getBalanceMap() map[string]decimal.Decimal {
	return balanceMapPool.Get().(map[string]decimal.Decimal)
}

// putBalanceMap clears and returns a balance map to the pool
func putBalanceMap(m map[string]decimal.Decimal) {
	// Clear the map before returning to pool
	for k := range m {
		delete(m, k)
	}
	balanceMapPool.Put(m)
}

// getInferredAmountsMap retrieves a pooled inferred amounts map
func getInferredAmountsMap() map[*parser.Posting]*parser.Amount {
	return inferredAmountsMapPool.Get().(map[*parser.Posting]*parser.Amount)
}

// putInferredAmountsMap clears and returns an inferred amounts map to the pool
func putInferredAmountsMap(m map[*parser.Posting]*parser.Amount) {
	for k := range m {
		delete(m, k)
	}
	inferredAmountsMapPool.Put(m)
}
