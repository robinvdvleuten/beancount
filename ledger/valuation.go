package ledger

import (
	"fmt"
	"regexp"
	"time"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// Valuation is how a report states a balance: in Units, At cost, At market
// value, or Converted to a currency. The zero value is At cost, the
// default.
type Valuation struct {
	kind     valuationKind
	currency string // the target currency of Converted to
}

type valuationKind int

const (
	valuationAtCost valuationKind = iota
	valuationUnits
	valuationAtMarket
	valuationConverted
)

var (
	// ValuationAtCost states a position with a cost at its units times its
	// per-unit cost, like bean-query's cost().
	ValuationAtCost = Valuation{kind: valuationAtCost}
	// ValuationUnits states each position as its units.
	ValuationUnits = Valuation{kind: valuationUnits}
	// ValuationAtMarket states a position with a cost at the price of its
	// units in its cost currency, like bean-query's value().
	ValuationAtMarket = Valuation{kind: valuationAtMarket}
)

// ValuationConvertedTo states each position in currency, like bean-query's
// convert().
func ValuationConvertedTo(currency string) Valuation {
	return Valuation{kind: valuationConverted, currency: currency}
}

// currencyPattern matches a currency as beancount v3's lexer does,
// including a futures contract such as /ESZ24.
var currencyPattern = regexp.MustCompile(`^(?:[A-Z][A-Z0-9'._-]*[A-Z0-9]|/[A-Z0-9'._-]*[A-Z](?:[A-Z0-9'._-]*[A-Z0-9])?)$`)

// ParseValuation parses a Valuation as the web API spells it: units, cost,
// market, or the currency to convert to.
func ParseValuation(s string) (Valuation, error) {
	switch s {
	case "units":
		return ValuationUnits, nil
	case "cost":
		return ValuationAtCost, nil
	case "market":
		return ValuationAtMarket, nil
	}
	if !currencyPattern.MatchString(s) {
		return Valuation{}, fmt.Errorf("invalid valuation %q: expected units, cost, market or a currency", s)
	}
	return ValuationConvertedTo(s), nil
}

// String spells v as ParseValuation parses it.
func (v Valuation) String() string {
	switch v.kind {
	case valuationUnits:
		return "units"
	case valuationAtMarket:
		return "market"
	case valuationConverted:
		return v.currency
	default:
		return "cost"
	}
}

// Position is units of a currency held at an optional per-unit cost: what
// a Valuation values.
type Position struct {
	Number   decimal.Decimal
	Currency string
	Cost     *BookedCost // nil without cost
}

// AtCost returns p's units times its per-unit cost, or its units when it
// has no cost, like beancount's get_cost.
func (p Position) AtCost() CurrencyAmount {
	if p.Cost == nil {
		return p.units()
	}
	return CurrencyAmount{Currency: p.Cost.Currency, Amount: pydecimal.Mul(p.Number, p.Cost.Number)}
}

func (p Position) units() CurrencyAmount {
	return CurrencyAmount{Currency: p.Currency, Amount: p.Number}
}

// ConvertAmount converts number of from into to at the latest price on or
// before date, or returns it unchanged when there is none, like
// beancount's convert_amount without a via currency.
func (l *Ledger) ConvertAmount(number decimal.Decimal, from, to string, date *ast.Date) CurrencyAmount {
	if from != to {
		if rate, ok := l.GetPrice(date, from, to); ok {
			return CurrencyAmount{Currency: to, Amount: pydecimal.Mul(number, rate)}
		}
	}
	return CurrencyAmount{Currency: from, Amount: number}
}

// MarketValue returns p at the price of its units in its cost currency on
// date, or its units when it has no cost or there is no such price, like
// beancount's get_value.
func (l *Ledger) MarketValue(p Position, date *ast.Date) CurrencyAmount {
	if p.Cost == nil {
		return p.units()
	}
	return l.ConvertAmount(p.Number, p.Currency, p.Cost.Currency, date)
}

// Convert returns p in currency on date like beancount's convert_position:
// at its units' price in currency, or else through its cost currency,
// multiplying the units' price in that currency by its price in currency.
// Without either it returns p's units. Like GetPrice, it never chains
// prices through any other currency.
func (l *Ledger) Convert(p Position, currency string, date *ast.Date) CurrencyAmount {
	if p.Currency == currency {
		return p.units()
	}
	if rate, ok := l.GetPrice(date, p.Currency, currency); ok {
		return CurrencyAmount{Currency: currency, Amount: pydecimal.Mul(p.Number, rate)}
	}
	if p.Cost != nil && p.Cost.Currency != currency {
		if toCost, ok := l.GetPrice(date, p.Currency, p.Cost.Currency); ok {
			if toTarget, ok := l.GetPrice(date, p.Cost.Currency, currency); ok {
				return CurrencyAmount{Currency: currency, Amount: pydecimal.Mul(pydecimal.Mul(p.Number, toCost), toTarget)}
			}
		}
	}
	return p.units()
}

// Value returns p stated under v on date.
func (l *Ledger) Value(p Position, v Valuation, date *ast.Date) CurrencyAmount {
	switch v.kind {
	case valuationUnits:
		return p.units()
	case valuationAtMarket:
		return l.MarketValue(p, date)
	case valuationConverted:
		return l.Convert(p, v.currency, date)
	default:
		return p.AtCost()
	}
}

// today returns the current date, the valuation date of a report without
// an end date.
func today() *ast.Date {
	now := time.Now()
	return &ast.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)}
}

// positionKey identifies the lot a position changes, for summing an
// account's positions like an inventory does.
type positionKey struct {
	currency     string
	costNumber   string
	costCurrency string
	costDate     string
	costLabel    string
}

func keyOf(p Position) positionKey {
	key := positionKey{currency: p.Currency}
	if p.Cost != nil {
		key.costNumber = pydecimal.Normalize(p.Cost.Number).String()
		key.costCurrency = p.Cost.Currency
		key.costLabel = p.Cost.Label
		if p.Cost.Date != nil {
			key.costDate = p.Cost.Date.String()
		}
	}
	return key
}

// positionsBetween returns the positions account's postings dated within
// [start, end] inclusive add up to, a nil bound leaving that side open,
// summed per lot in the order the lots first appear. A posting holds the
// positions Booking recorded for it (BookedPositions), or else its units.
func (l *Ledger) positionsBetween(account *Account, start, end *ast.Date) []Position {
	var positions []Position
	index := make(map[positionKey]int)
	add := func(p Position) {
		key := keyOf(p)
		if i, ok := index[key]; ok {
			positions[i].Number = pydecimal.Add(positions[i].Number, p.Number)
			return
		}
		index[key] = len(positions)
		positions = append(positions, p)
	}
	for _, posting := range account.Postings {
		date := posting.Transaction.Date()
		if start != nil && date.Before(start.Time) || end != nil && date.After(end.Time) {
			continue
		}
		amount := posting.Posting.Amount
		if amount == nil {
			continue
		}
		booked := l.BookedPositions(posting.Posting)
		if len(booked) == 0 {
			number, err := ParseAmount(amount)
			if err != nil {
				continue
			}
			add(Position{Number: number, Currency: amount.Currency})
			continue
		}
		for _, position := range booked {
			add(Position{Number: position.Units, Currency: amount.Currency, Cost: position.Cost})
		}
	}
	return positions
}

// valuedBalance returns account's balance over [start, end] stated under
// v on date: each lot's summed position valued, then summed per currency.
// Like a beancount inventory, it holds no currency that sums to zero.
func (l *Ledger) valuedBalance(account *Account, start, end *ast.Date, v Valuation, date *ast.Date) *Balance {
	sums := make(map[string]decimal.Decimal)
	for _, position := range l.positionsBetween(account, start, end) {
		valued := l.Value(position, v, date)
		sums[valued.Currency] = pydecimal.Add(sums[valued.Currency], valued.Amount)
	}
	for currency, sum := range sums {
		if sum.IsZero() {
			delete(sums, currency)
		}
	}
	return NewBalanceFromMap(sums)
}
