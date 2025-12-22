package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Helper to add price and panic on error (for tests with valid inputs)
func addPriceMust(pg *PriceGraph, date *ast.Date, from, to string, rate decimal.Decimal) {
	err := pg.AddPrice(date, from, to, rate)
	if err != nil {
		panic(err)
	}
}

func TestNewPriceGraph(t *testing.T) {
	pg := NewPriceGraph()
	assert.NotZero(t, pg)
	assert.Equal(t, len(pg.sortedDates), 0)
}

func TestAddPriceBasic(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	err := pg.AddPrice(date, "USD", "EUR", mustParseDec("0.92"))
	assert.NoError(t, err)

	// Verify forward edge exists
	dateKey := date.String()
	assert.True(t, pg.pricesByDate[dateKey]["USD"]["EUR"].Equal(mustParseDec("0.92")))

	// Verify inverse edge was created
	expectedInverse := decimal.NewFromInt(1).Div(mustParseDec("0.92"))
	assert.True(t, pg.pricesByDate[dateKey]["EUR"]["USD"].Equal(expectedInverse))
}

func TestAddPriceKeepsSorted(t *testing.T) {
	pg := NewPriceGraph()

	date1 := newTestDate("2024-01-15")
	date2 := newTestDate("2024-01-10")
	date3 := newTestDate("2024-01-20")

	addPriceMust(pg, date1, "USD", "EUR", mustParseDec("0.92"))
	addPriceMust(pg, date2, "USD", "EUR", mustParseDec("0.91"))
	addPriceMust(pg, date3, "USD", "EUR", mustParseDec("0.93"))

	assert.Equal(t, len(pg.sortedDates), 3)
	assert.True(t, pg.sortedDates[0].String() == date2.String())
	assert.True(t, pg.sortedDates[1].String() == date1.String())
	assert.True(t, pg.sortedDates[2].String() == date3.String())
}

func TestAddPriceZeroRateError(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	err := pg.AddPrice(date, "USD", "EUR", decimal.Zero)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-zero")
}

func TestLookupPriceSameCurrency(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	rate, found := pg.LookupPrice(date, "USD", "USD")
	assert.True(t, found)
	assert.True(t, rate.Equal(decimal.NewFromInt(1)))

	// Should work even if no prices have been added
	pg2 := NewPriceGraph()
	rate2, found2 := pg2.LookupPrice(date, "EUR", "EUR")
	assert.True(t, found2)
	assert.True(t, rate2.Equal(decimal.NewFromInt(1)))
}

func TestLookupPriceExactMatch(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))

	rate, found := pg.LookupPrice(date, "USD", "EUR")
	assert.True(t, found)
	assert.True(t, rate.Equal(mustParseDec("0.92")))
}

func TestLookupPriceForwardFill(t *testing.T) {
	pg := NewPriceGraph()

	date1 := newTestDate("2024-01-10")
	date2 := newTestDate("2024-01-15")
	dateLookup := newTestDate("2024-01-18")

	addPriceMust(pg, date1, "USD", "EUR", mustParseDec("0.90"))
	addPriceMust(pg, date2, "USD", "EUR", mustParseDec("0.92"))

	// Lookup on a date after both prices should use the most recent (date2)
	rate, found := pg.LookupPrice(dateLookup, "USD", "EUR")
	assert.True(t, found)
	assert.True(t, rate.Equal(mustParseDec("0.92")))
}

func TestLookupPriceBeforeFirstPrice(t *testing.T) {
	pg := NewPriceGraph()

	date := newTestDate("2024-01-15")
	dateBefore := newTestDate("2024-01-10")

	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))

	rate, found := pg.LookupPrice(dateBefore, "USD", "EUR")
	assert.False(t, found)
	assert.True(t, rate.IsZero())
}

func TestLookupPriceBidirectional(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))

	// Forward direction
	rate1, found1 := pg.LookupPrice(date, "USD", "EUR")
	assert.True(t, found1)
	assert.True(t, rate1.Equal(mustParseDec("0.92")))

	// Inverse direction should use the automatically created inverse edge
	rate2, found2 := pg.LookupPrice(date, "EUR", "USD")
	assert.True(t, found2)
	expected := decimal.NewFromInt(1).Div(mustParseDec("0.92"))
	assert.True(t, rate2.Equal(expected))
}

func TestLookupPriceMultipleCurrencies(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))
	addPriceMust(pg, date, "USD", "GBP", mustParseDec("0.79"))
	addPriceMust(pg, date, "EUR", "CHF", mustParseDec("1.05"))

	// All forward directions
	rate1, found1 := pg.LookupPrice(date, "USD", "EUR")
	assert.True(t, found1)
	assert.True(t, rate1.Equal(mustParseDec("0.92")))

	rate2, found2 := pg.LookupPrice(date, "USD", "GBP")
	assert.True(t, found2)
	assert.True(t, rate2.Equal(mustParseDec("0.79")))

	rate3, found3 := pg.LookupPrice(date, "EUR", "CHF")
	assert.True(t, found3)
	assert.True(t, rate3.Equal(mustParseDec("1.05")))

	// All inverse directions
	rate4, found4 := pg.LookupPrice(date, "EUR", "USD")
	assert.True(t, found4)
	expected4 := decimal.NewFromInt(1).Div(mustParseDec("0.92"))
	assert.True(t, rate4.Equal(expected4))

	rate5, found5 := pg.LookupPrice(date, "GBP", "USD")
	assert.True(t, found5)
	expected5 := decimal.NewFromInt(1).Div(mustParseDec("0.79"))
	assert.True(t, rate5.Equal(expected5))

	rate6, found6 := pg.LookupPrice(date, "CHF", "EUR")
	assert.True(t, found6)
	expected6 := decimal.NewFromInt(1).Div(mustParseDec("1.05"))
	assert.True(t, rate6.Equal(expected6))
}

func TestLookupPriceNonexistentPair(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))

	// Pair that was never added
	rate, found := pg.LookupPrice(date, "USD", "GBP")
	assert.False(t, found)
	assert.True(t, rate.IsZero())
}

func TestForwardFillMultipleDates(t *testing.T) {
	pg := NewPriceGraph()

	date1 := newTestDate("2024-01-05")
	date2 := newTestDate("2024-01-10")
	date3 := newTestDate("2024-01-15")
	date4 := newTestDate("2024-01-20")
	date5 := newTestDate("2024-01-25")

	addPriceMust(pg, date1, "USD", "EUR", mustParseDec("0.90"))
	addPriceMust(pg, date3, "USD", "EUR", mustParseDec("0.92"))
	addPriceMust(pg, date5, "USD", "EUR", mustParseDec("0.94"))

	// Date between 1 and 3 should use price from date1
	rate1, found1 := pg.LookupPrice(date2, "USD", "EUR")
	assert.True(t, found1)
	assert.True(t, rate1.Equal(mustParseDec("0.90")))

	// Date on date3 should use price from date3
	rate2, found2 := pg.LookupPrice(date3, "USD", "EUR")
	assert.True(t, found2)
	assert.True(t, rate2.Equal(mustParseDec("0.92")))

	// Date between 3 and 5 should use price from date3
	rate3, found3 := pg.LookupPrice(date4, "USD", "EUR")
	assert.True(t, found3)
	assert.True(t, rate3.Equal(mustParseDec("0.92")))

	// Date after all prices should use the most recent (date5)
	date6 := newTestDate("2024-01-30")
	rate4, found4 := pg.LookupPrice(date6, "USD", "EUR")
	assert.True(t, found4)
	assert.True(t, rate4.Equal(mustParseDec("0.94")))
}

func TestHasPrice(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))

	assert.True(t, pg.HasPrice(date, "USD", "EUR"))
	assert.True(t, pg.HasPrice(date, "EUR", "USD"))
	assert.False(t, pg.HasPrice(date, "USD", "GBP"))
	// Same currency always returns true
	assert.True(t, pg.HasPrice(date, "USD", "USD"))
}

func TestGetPricesBefore(t *testing.T) {
	pg := NewPriceGraph()

	date1 := newTestDate("2024-01-05")
	date2 := newTestDate("2024-01-10")
	date3 := newTestDate("2024-01-20")

	addPriceMust(pg, date1, "USD", "EUR", mustParseDec("0.90"))
	addPriceMust(pg, date2, "USD", "EUR", mustParseDec("0.91"))
	addPriceMust(pg, date3, "USD", "EUR", mustParseDec("0.93"))

	// Get all prices before date2
	pricesBefore := pg.GetPricesBefore(date2)

	// Should include date1 and date2, but not date3
	assert.True(t, len(pricesBefore) >= 2)
	assert.NotZero(t, pricesBefore[date1.String()])
	assert.NotZero(t, pricesBefore[date2.String()])
	assert.Zero(t, pricesBefore[date3.String()])
}

func TestAddPriceSameDate(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	// Add two different pairs on the same date
	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))
	addPriceMust(pg, date, "USD", "GBP", mustParseDec("0.79"))

	// Should have only one date in sortedDates
	assert.Equal(t, len(pg.sortedDates), 1)

	// Both prices should be accessible
	rate1, found1 := pg.LookupPrice(date, "USD", "EUR")
	assert.True(t, found1)
	assert.True(t, rate1.Equal(mustParseDec("0.92")))

	rate2, found2 := pg.LookupPrice(date, "USD", "GBP")
	assert.True(t, found2)
	assert.True(t, rate2.Equal(mustParseDec("0.79")))
}

func TestAddPriceReplaceExisting(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	// Add a price
	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.92"))

	// Replace with new price for same pair and date
	addPriceMust(pg, date, "USD", "EUR", mustParseDec("0.93"))

	// Should have the new price
	rate, found := pg.LookupPrice(date, "USD", "EUR")
	assert.True(t, found)
	assert.True(t, rate.Equal(mustParseDec("0.93")))

	// Inverse should also be updated
	rateInv, foundInv := pg.LookupPrice(date, "EUR", "USD")
	assert.True(t, foundInv)
	expected := decimal.NewFromInt(1).Div(mustParseDec("0.93"))
	assert.True(t, rateInv.Equal(expected))
}

func TestEdgeCaseSmallRates(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	// Very small rate (Bitcoin-like)
	smallRate := mustParseDec("0.00001234")
	addPriceMust(pg, date, "USD", "BTC", smallRate)

	rate, found := pg.LookupPrice(date, "USD", "BTC")
	assert.True(t, found)
	assert.True(t, rate.Equal(smallRate))

	// Inverse should be very large
	rateInv, foundInv := pg.LookupPrice(date, "BTC", "USD")
	assert.True(t, foundInv)
	expected := decimal.NewFromInt(1).Div(smallRate)
	assert.True(t, rateInv.Equal(expected))
}

func TestEdgeCaseLargeRates(t *testing.T) {
	pg := NewPriceGraph()
	date := newTestDate("2024-01-15")

	// Very large rate
	largeRate := mustParseDec("1000000")
	addPriceMust(pg, date, "USD", "VEF", largeRate)

	rate, found := pg.LookupPrice(date, "USD", "VEF")
	assert.True(t, found)
	assert.True(t, rate.Equal(largeRate))

	// Inverse should be very small
	rateInv, foundInv := pg.LookupPrice(date, "VEF", "USD")
	assert.True(t, foundInv)
	expected := decimal.NewFromInt(1).Div(largeRate)
	assert.True(t, rateInv.Equal(expected))
}
