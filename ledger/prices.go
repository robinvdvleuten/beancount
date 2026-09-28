package ledger

import (
	"slices"
	"sort"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// priceIndex holds the ledger's prices by currency pair and answers the rate
// between two currencies on a date. Like beancount's get_price, a pair
// converts through its own prices or the inverses of the prices quoted the
// other way, never through a third currency.
type priceIndex struct {
	// Each pair's rates, by date and, within a date, in the order they were
	// added: a price quoted from→to and the inverse of one quoted to→from
	rates map[currencyPair][]datedRate
}

type currencyPair struct{ from, to string }

type datedRate struct {
	date *ast.Date
	rate decimal.Decimal
}

// add records a price of one unit of from in to. Like beancount, which
// filters out zero prices for zero-cost postings such as gifted options, a
// zero price has no inverse: the inverse falls back to an earlier price.
func (p *priceIndex) add(date *ast.Date, from, to string, rate decimal.Decimal) {
	if p.rates == nil {
		p.rates = make(map[currencyPair][]datedRate)
	}
	p.insert(currencyPair{from, to}, datedRate{date, rate})
	if !rate.IsZero() {
		p.insert(currencyPair{to, from}, datedRate{date, pydecimal.Quo(decimal.NewFromInt(1), rate)})
	}
}

// insert adds a rate after the pair's rates on or before its date.
func (p *priceIndex) insert(pair currencyPair, rate datedRate) {
	rates := p.rates[pair]
	i := sort.Search(len(rates), func(i int) bool { return rates[i].date.After(rate.date.Time) })
	p.rates[pair] = slices.Insert(rates, i, rate)
}

// rate returns the rate from one currency to another on a date, forward
// filled from the latest date on or before it that has one; when that date
// has several, the first added wins. A currency converts to itself at 1.
func (p *priceIndex) rate(date *ast.Date, from, to string) (decimal.Decimal, bool) {
	if from == to {
		return decimal.NewFromInt(1), true
	}
	rates := p.rates[currencyPair{from, to}]
	after := sort.Search(len(rates), func(i int) bool { return rates[i].date.After(date.Time) })
	if after == 0 {
		return decimal.Zero, false
	}
	day := rates[after-1].date
	first := sort.Search(after, func(i int) bool { return !rates[i].date.Before(day.Time) })
	return rates[first].rate, true
}
