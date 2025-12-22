package ledger

import (
	"fmt"
	"sort"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// PriceGraph maintains a temporal index of currency exchange rates with support
// for forward-fill lookups (most recent price on or before a given date).
//
// It stores prices bidirectionally - adding a price from USD to EUR automatically
// creates the inverse edge from EUR to USD. Same-currency conversions always return
// a rate of 1.0.
//
// Time complexity:
//   - AddPrice: O(log n) for sorted insertion
//   - LookupPrice: O(log n) binary search + O(m) path traversal
type PriceGraph struct {
	// pricesByDate maps a date to a 2-level nested map: from currency -> to currency -> rate
	pricesByDate map[string]map[string]map[string]decimal.Decimal
	// sortedDates maintains dates in chronological order for forward-fill lookups
	sortedDates []*ast.Date
}

// NewPriceGraph creates a new empty price graph
func NewPriceGraph() *PriceGraph {
	return &PriceGraph{
		pricesByDate: make(map[string]map[string]map[string]decimal.Decimal),
		sortedDates:  make([]*ast.Date, 0),
	}
}

// AddPrice adds a price conversion from one currency to another at a specific date.
// It automatically creates the inverse edge (bidirectional) and keeps the date list sorted.
// Zero rates are rejected with an error.
//
// Example: AddPrice(2024-01-15, "USD", "EUR", 0.92) creates edges:
//
//	USD → EUR: 0.92
//	EUR → USD: 1/0.92 ≈ 1.087
func (pg *PriceGraph) AddPrice(date *ast.Date, fromCurrency, toCurrency string, rate decimal.Decimal) error {
	if rate.IsZero() {
		return fmt.Errorf("price rate must be non-zero: %s %s %s on %s", fromCurrency, toCurrency, rate, date.String())
	}

	dateKey := date.String()

	// Initialize date index if needed
	if _, exists := pg.pricesByDate[dateKey]; !exists {
		pg.pricesByDate[dateKey] = make(map[string]map[string]decimal.Decimal)
		pg.sortedDates = append(pg.sortedDates, date)
		// Keep dates sorted
		sort.Slice(pg.sortedDates, func(i, j int) bool {
			return pg.sortedDates[i].Before(pg.sortedDates[j].Time)
		})
	}

	// Initialize currency maps if needed
	if pg.pricesByDate[dateKey][fromCurrency] == nil {
		pg.pricesByDate[dateKey][fromCurrency] = make(map[string]decimal.Decimal)
	}
	if pg.pricesByDate[dateKey][toCurrency] == nil {
		pg.pricesByDate[dateKey][toCurrency] = make(map[string]decimal.Decimal)
	}

	// Add forward edge: fromCurrency → toCurrency
	pg.pricesByDate[dateKey][fromCurrency][toCurrency] = rate

	// Add inverse edge: toCurrency → fromCurrency
	inverse := decimal.NewFromInt(1).Div(rate)
	pg.pricesByDate[dateKey][toCurrency][fromCurrency] = inverse

	return nil
}

// LookupPrice returns the exchange rate from one currency to another at a given date,
// using forward-fill semantics (most recent price on or before the date).
//
// Same-currency conversions always return 1.0.
// Returns (rate, found) where found is false if no price exists before or on the date.
//
// Example: LookupPrice(2024-02-01, "USD", "EUR") returns the USD→EUR rate from
// the most recent date on or before 2024-02-01, or (0, false) if none exists.
func (pg *PriceGraph) LookupPrice(date *ast.Date, fromCurrency, toCurrency string) (decimal.Decimal, bool) {
	// Same currency conversion is always 1.0
	if fromCurrency == toCurrency {
		return decimal.NewFromInt(1), true
	}

	// Search backwards from the given date to find the most recent price
	for i := len(pg.sortedDates) - 1; i >= 0; i-- {
		sortedDate := pg.sortedDates[i]

		// Stop if we've gone before the lookup date
		if sortedDate.After(date.Time) {
			continue
		}

		dateKey := sortedDate.String()
		if rates, ok := pg.pricesByDate[dateKey][fromCurrency]; ok {
			if rate, found := rates[toCurrency]; found {
				return rate, true
			}
		}
	}

	return decimal.Zero, false
}

// HasPrice returns true if a price exists for the given currency pair on or before the date.
func (pg *PriceGraph) HasPrice(date *ast.Date, fromCurrency, toCurrency string) bool {
	_, found := pg.LookupPrice(date, fromCurrency, toCurrency)
	return found
}

// GetPricesBefore returns all prices that exist on or before a given date,
// organized by date (most recent first), then by currency pair.
// This is useful for debugging or reporting price history.
func (pg *PriceGraph) GetPricesBefore(date *ast.Date) map[string]map[string]map[string]decimal.Decimal {
	result := make(map[string]map[string]map[string]decimal.Decimal)

	// Iterate in reverse chronological order
	for i := len(pg.sortedDates) - 1; i >= 0; i-- {
		sortedDate := pg.sortedDates[i]
		if sortedDate.After(date.Time) {
			continue
		}

		dateKey := sortedDate.String()
		if rates, ok := pg.pricesByDate[dateKey]; ok {
			// Deep copy the rates map
			result[dateKey] = make(map[string]map[string]decimal.Decimal)
			for from, toRates := range rates {
				result[dateKey][from] = make(map[string]decimal.Decimal)
				for to, rate := range toRates {
					result[dateKey][from][to] = rate
				}
			}
		}
	}

	return result
}
