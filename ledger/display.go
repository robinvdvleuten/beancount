package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// DisplayContext records how many fractional digits the ledger's source
// amounts use per currency, like beancount's display context: bean-query
// renders numbers at each currency's most common precision. Only amounts
// written in the source count, not interpolated ones.
type DisplayContext struct {
	fractional map[string]map[int32]int // currency -> fractional digits -> count
}

func newDisplayContext() *DisplayContext {
	return &DisplayContext{fractional: make(map[string]map[int32]int)}
}

// Precision returns the most common number of fractional digits among the
// source amounts in currency, preferring more digits on a tie, and false
// when the source has no amount in that currency.
func (dc *DisplayContext) Precision(currency string) (int32, bool) {
	counts, ok := dc.fractional[currency]
	if !ok {
		return 0, false
	}
	var digits int32
	best := 0
	for d, count := range counts {
		if count > best || (count == best && d > digits) {
			digits, best = d, count
		}
	}
	return digits, true
}

// Quantize rounds number half-to-even to currency's precision, leaving it
// unchanged for a currency without source amounts.
func (dc *DisplayContext) Quantize(number decimal.Decimal, currency string) decimal.Decimal {
	digits, ok := dc.Precision(currency)
	if !ok {
		return number
	}
	return number.RoundBank(digits)
}

// update records a source amount. Amounts missing their number or currency
// do not count, matching beancount, which records them while parsing.
func (dc *DisplayContext) update(amount *ast.Amount) {
	if amount == nil || amount.Value == "" || amount.Currency == "" {
		return
	}
	number, err := decimal.NewFromString(amount.Value)
	if err != nil {
		return
	}
	counts, ok := dc.fractional[amount.Currency]
	if !ok {
		counts = make(map[int32]int)
		dc.fractional[amount.Currency] = counts
	}
	counts[max(-number.Exponent(), 0)]++
}

// updateFromDirective records the amounts beancount's parser feeds its
// display context: posting units, prices, and costs; balance (not its
// tolerance), price, and custom amounts; and amount-valued metadata.
func (dc *DisplayContext) updateFromDirective(d ast.Directive) {
	dc.updateFromMetadata(d.GetMetadata())

	switch d := d.(type) {
	case *ast.Transaction:
		for _, posting := range d.Postings {
			dc.update(posting.Amount)
			dc.update(posting.Price)
			if posting.Cost != nil {
				dc.update(posting.Cost.Amount)
				dc.update(posting.Cost.Total)
			}
			dc.updateFromMetadata(posting.Metadata)
		}
	case *ast.Balance:
		dc.update(d.Amount)
	case *ast.Price:
		dc.update(d.Amount)
	case *ast.Custom:
		for _, value := range d.Values {
			dc.update(value.Amount)
		}
	}
}

func (dc *DisplayContext) updateFromMetadata(metadata []*ast.Metadata) {
	for _, m := range metadata {
		if m.Value != nil {
			dc.update(m.Value.Amount)
		}
	}
}
