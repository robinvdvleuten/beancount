package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

func mustParseDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func newTestDate(dateStr string) *ast.Date {
	date, err := ast.NewDate(dateStr)
	if err != nil {
		panic(err)
	}
	return date
}

func TestPriceIndexRate(t *testing.T) {
	type price struct{ date, from, rate, to string }
	tests := []struct {
		name   string
		prices []price
		date   string
		from   string
		to     string
		want   string // empty when no rate is found
	}{
		{
			name:   "price on the date",
			prices: []price{{"2024-01-15", "USD", "1.08", "CAD"}},
			date:   "2024-01-15", from: "USD", to: "CAD", want: "1.08",
		},
		{
			name: "forward fill from the latest earlier price",
			prices: []price{
				{"2024-01-10", "USD", "1.05", "CAD"},
				{"2024-01-15", "USD", "1.08", "CAD"},
				{"2024-01-20", "USD", "1.10", "CAD"},
			},
			date: "2024-01-18", from: "USD", to: "CAD", want: "1.08",
		},
		{
			name:   "before the first price",
			prices: []price{{"2024-01-15", "USD", "1.08", "CAD"}},
			date:   "2024-01-10", from: "USD", to: "CAD",
		},
		{
			name:   "inverse",
			prices: []price{{"2024-01-15", "EUR", "2", "USD"}},
			date:   "2024-01-15", from: "USD", to: "EUR", want: "0.5",
		},
		{
			name: "a later inverse wins over an earlier direct price",
			prices: []price{
				{"2024-01-10", "EUR", "2", "USD"},
				{"2024-01-15", "USD", "0.4", "EUR"},
			},
			date: "2024-01-20", from: "EUR", to: "USD", want: "2.5",
		},
		{
			name:   "a zero price converts to zero",
			prices: []price{{"2020-01-01", "HOOL", "0", "USD"}},
			date:   "2020-01-03", from: "HOOL", to: "USD", want: "0",
		},
		{
			name:   "a zero price has no inverse",
			prices: []price{{"2020-01-01", "HOOL", "0", "USD"}},
			date:   "2020-01-03", from: "USD", to: "HOOL",
		},
		{
			name: "a zero price's inverse falls back to an earlier price",
			prices: []price{
				{"2019-12-01", "GOOG", "4", "USD"},
				{"2020-01-02", "GOOG", "0", "USD"},
			},
			date: "2020-01-03", from: "USD", to: "GOOG", want: "0.25",
		},
		{
			// A known gap: bean-query keeps the day's last price.
			name: "the first price of a day wins",
			prices: []price{
				{"2024-01-15", "ACME", "20", "USD"},
				{"2024-01-15", "ACME", "21", "USD"},
			},
			date: "2024-01-15", from: "ACME", to: "USD", want: "20",
		},
		{
			name: "the first price of a day wins over a later inverse",
			prices: []price{
				{"2024-01-15", "EUR", "2", "USD"},
				{"2024-01-15", "USD", "0.4", "EUR"},
			},
			date: "2024-01-15", from: "USD", to: "EUR", want: "0.5",
		},
		{
			name: "no chaining through a third currency",
			prices: []price{
				{"2020-01-01", "EUR", "2", "USD"},
				{"2020-01-01", "HOOL", "5", "USD"},
			},
			date: "2020-01-02", from: "EUR", to: "HOOL",
		},
		{
			name: "unknown pair",
			date: "2020-01-02", from: "EUR", to: "USD",
		},
		{
			name: "same currency",
			date: "2020-01-02", from: "USD", to: "USD", want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var index priceIndex
			for _, p := range tt.prices {
				index.add(newTestDate(p.date), p.from, p.to, mustParseDec(p.rate))
			}
			rate, ok := index.rate(newTestDate(tt.date), tt.from, tt.to)
			if tt.want == "" {
				assert.False(t, ok, "rate: %s", rate)
				assert.True(t, rate.IsZero())
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tt.want, rate.String())
		})
	}
}

func TestPriceIndexAddsOutOfDateOrder(t *testing.T) {
	var index priceIndex
	index.add(newTestDate("2024-01-20"), "USD", "CAD", mustParseDec("1.10"))
	index.add(newTestDate("2024-01-10"), "USD", "CAD", mustParseDec("1.05"))
	index.add(newTestDate("2024-01-15"), "USD", "CAD", mustParseDec("1.08"))

	rate, ok := index.rate(newTestDate("2024-01-18"), "USD", "CAD")
	assert.True(t, ok)
	assert.Equal(t, "1.08", rate.String())
}
