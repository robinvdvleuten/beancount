package ledger

import (
	"sync"

	"github.com/shopspring/decimal"
)

// Pools for commonly allocated objects to reduce GC pressure

var (
	// balanceMapPool provides pooled maps for BalanceWeights calculations
	balanceMapPool = sync.Pool{
		New: func() any {
			return make(map[string]decimal.Decimal, 4) // typical transaction has 2-4 currencies
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
