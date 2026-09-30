package query

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// numberify splits every amount, position and inventory column into one
// number column per currency, named "column (CUR)", like beanquery's
// numberify_results. A column's currencies come in order of how many rows
// hold them, then of currency, both descending, and a column holding no
// currency disappears. Numbers are quantized with the ledger's display
// context; a cell whose value has no number in the currency, or whose
// inventory's units of it quantize to zero, is NULL.
func numberify(result *table) *table {
	type converter struct {
		source   int
		split    *numberifier // nil copies the source column
		currency string
	}

	converters := make([]converter, 0, len(result.Columns))
	columns := make([]tableColumn, 0, len(result.Columns))
	for i, col := range result.Columns {
		split, ok := numberifiers[col.Type]
		if !ok {
			converters = append(converters, converter{source: i})
			columns = append(columns, col)
			continue
		}
		counts := make(map[string]int)
		for _, row := range result.Rows {
			if row[i] == nil {
				continue
			}
			for _, currency := range split.currencies(row[i]) {
				counts[currency]++
			}
		}
		currencies := make([]string, 0, len(counts))
		for currency := range counts {
			currencies = append(currencies, currency)
		}
		slices.SortFunc(currencies, func(a, b string) int {
			if c := cmp.Compare(counts[b], counts[a]); c != 0 {
				return c
			}
			return cmp.Compare(b, a)
		})
		for _, currency := range currencies {
			converters = append(converters, converter{source: i, split: &split, currency: currency})
			columns = append(columns, tableColumn{Name: fmt.Sprintf("%s (%s)", col.Name, currency), Type: tDecimal})
		}
	}

	rows := make([][]any, len(result.Rows))
	for r, row := range result.Rows {
		values := make([]any, len(converters))
		for i, conv := range converters {
			value := row[conv.source]
			switch {
			case conv.split == nil:
				values[i] = value
			case value != nil:
				values[i] = conv.split.number(value, conv.currency, result.Display)
			}
		}
		rows[r] = values
	}
	return &table{Columns: columns, Rows: rows, Display: result.Display}
}

// numberifier splits one column type, like a converter factory of
// beanquery's CONVERTING_TYPES: currencies lists the currencies a non-NULL
// value counts for, and number gives its number in one of them, quantized,
// or nil for NULL.
type numberifier struct {
	currencies func(v any) []string
	number     func(v any, currency string, display *ledger.DisplayContext) any
}

// numberifiers maps each column type numberify splits to its numberifier.
// Like beancount's Amount, whose truth is its number's, a zero amount
// counts for no currency and converts to NULL; a position always counts;
// an inventory's units of a currency are summed and are NULL when they
// quantize to zero.
var numberifiers = map[dtype]numberifier{
	tAmount: {
		currencies: func(v any) []string {
			if a := v.(*amountValue); a.Currency != "" && !a.Number.IsZero() {
				return []string{a.Currency}
			}
			return nil
		},
		number: func(v any, currency string, display *ledger.DisplayContext) any {
			if a := v.(*amountValue); a.Currency == currency && !a.Number.IsZero() {
				return quantizedCell(display, a.Number, currency)
			}
			return nil
		},
	},
	tPosition: {
		currencies: func(v any) []string {
			if p := v.(*positionValue); p.Units.Currency != "" {
				return []string{p.Units.Currency}
			}
			return nil
		},
		number: func(v any, currency string, display *ledger.DisplayContext) any {
			if p := v.(*positionValue); p.Units.Currency == currency {
				return quantizedCell(display, p.Units.Number, currency)
			}
			return nil
		},
	},
	tInventory: {
		currencies: func(v any) []string {
			var currencies []string
			for _, p := range v.(*inventoryValue).Positions() {
				if !slices.Contains(currencies, p.Units.Currency) {
					currencies = append(currencies, p.Units.Currency)
				}
			}
			return currencies
		},
		number: func(v any, currency string, display *ledger.DisplayContext) any {
			total := decimal.Decimal{}
			for _, p := range v.(*inventoryValue).Positions() {
				if p.Units.Currency == currency {
					total = pydecimal.Add(total, p.Units.Number)
				}
			}
			if total = quantize(display, total, currency); total.IsZero() {
				return nil
			}
			return total
		},
	},
}

// negativeZero is a numberified number that rounds to zero from below.
// Python keeps its sign (-0.00), which a decimal cannot hold, so the number
// renderer adds it back.
type negativeZero struct{ decimal.Decimal }

// quantizedCell is number quantized to currency's display precision, a
// negativeZero when a negative number rounds to zero.
func quantizedCell(display *ledger.DisplayContext, number decimal.Decimal, currency string) any {
	quantized := quantize(display, number, currency)
	if quantized.IsZero() && number.Sign() < 0 {
		return negativeZero{quantized}
	}
	return quantized
}

// quantize rounds number to currency's display precision, when there is a
// display context.
func quantize(display *ledger.DisplayContext, number decimal.Decimal, currency string) decimal.Decimal {
	if display == nil {
		return number
	}
	return display.Quantize(number, currency)
}
