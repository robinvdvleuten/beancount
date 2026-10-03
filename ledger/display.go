package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// DisplayContext records how many fractional digits the ledger's source
// amounts use per currency, like beancount's display context: beanquery
// renders amounts at each currency's most common precision. Only amounts
// written in the source count, not interpolated ones, and a currency's
// display_precision option fixes its precision whatever they use.
type DisplayContext struct {
	fractional map[string]map[int32]int // currency -> fractional digits -> count
	fixed      map[string]int32         // currency -> fractional digits
}

func newDisplayContext() *DisplayContext {
	return &DisplayContext{fractional: make(map[string]map[int32]int), fixed: make(map[string]int32)}
}

// precision returns the fractional digits display_precision fixes for
// currency, or else the most common number of them among the source
// amounts in currency, preferring more digits on a tie, and false when
// neither gives currency one.
func (dc *DisplayContext) precision(currency string) (int32, bool) {
	if digits, ok := dc.fixed[currency]; ok {
		return digits, true
	}
	counts, ok := dc.fractional[currency]
	if !ok {
		return 0, false
	}
	return MostCommonDigits(counts), true
}

// MostCommonDigits returns the most frequent number of fractional digits
// in counts (digits -> count), preferring more digits on a tie, like
// beancount's Distribution.mode, which picks a display context's
// precision.
func MostCommonDigits(counts map[int32]int) int32 {
	var digits int32
	best := 0
	for d, count := range counts {
		if count > best || (count == best && d > digits) {
			digits, best = d, count
		}
	}
	return digits
}

// Quantize rounds number half-to-even to currency's precision, leaving it
// unchanged for a currency without source amounts.
func (dc *DisplayContext) Quantize(number decimal.Decimal, currency string) decimal.Decimal {
	digits, ok := dc.precision(currency)
	if !ok {
		return number
	}
	return number.RoundBank(digits)
}

// fixPrecisions applies the display_precision options, each mapping a
// currency to an example number whose exponent fixes its fractional
// digits, like beancount's set_fixed_precision.
func (dc *DisplayContext) fixPrecisions(examples map[string]decimal.Decimal) {
	for currency, example := range examples {
		dc.fixed[currency] = -example.Exponent()
	}
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
