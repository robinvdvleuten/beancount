package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func TestDisplayContext(t *testing.T) {
	// Precisions pinned against beancount 2.3.6's display context for the
	// same ledger: every source amount counts except number-only amounts and
	// balance tolerances; ties prefer more digits.
	tree, err := parser.ParseString(context.Background(), `
2020-01-01 open Assets:A
  meta: 1.123 MET
2020-01-01 open Assets:B

2020-01-02 * "units, price"
  Assets:A  1.1 UNI @ 2.22 PRI
  Assets:B

2020-01-03 * "costs"
  Assets:A  1 CPU {3.333 CPC}
  Assets:A  1 CTU {{4.4444 CTC}}
  Assets:A  2 CMU {5.5 # 6.66 CMC}
  Assets:B

2020-01-04 balance Assets:A 1.10 UNI
2020-01-04 balance Assets:A 1.1 ~ 0.001 TOL
2020-01-05 price PPP 7.7777 PPC
2020-01-06 custom "budget" 8.88888 CUS

2020-01-07 * "number only"
  Assets:A  3.3
  Assets:B  -3.3 NOC
`)
	assert.NoError(t, err)
	l := New()
	_ = l.Process(context.Background(), tree) // Balance failures are irrelevant here.
	dc := l.DisplayContext()

	for currency, want := range map[string]int32{
		"MET": 3, "UNI": 2, "PRI": 2, "CPU": 0, "CPC": 3, "CTC": 4, "CMC": 2,
		"TOL": 1, "PPC": 4, "CUS": 5, "NOC": 1,
	} {
		got, ok := dc.Precision(currency)
		assert.True(t, ok, currency)
		assert.Equal(t, want, got, currency)
	}
	_, ok := dc.Precision("NOPE")
	assert.False(t, ok)

	assert.Equal(t, "2", dc.Quantize(decimal.RequireFromString("1.5"), "CPU").String())
	assert.Equal(t, "1.5", dc.Quantize(decimal.RequireFromString("1.5"), "NOPE").String())
}

func TestDisplayContextFixedPrecision(t *testing.T) {
	// Like beancount's set_fixed_precision, display_precision fixes its
	// currency's precision whatever the source amounts use, and the other
	// currencies keep inferring theirs.
	tree, err := parser.ParseString(context.Background(), `
option "display_precision" "USD:0.001"
option "display_precision" "JPY:1"

2020-01-01 open Assets:A
2020-01-01 open Assets:B

2020-01-02 * "amounts"
  Assets:A  1.5 USD
  Assets:A  1.50 USD
  Assets:A  1234.5678 JPY
  Assets:A  1.25 EUR
  Assets:B
`)
	assert.NoError(t, err)
	l := New()
	assert.NoError(t, l.Process(context.Background(), tree))
	dc := l.DisplayContext()

	for currency, want := range map[string]int32{"USD": 3, "JPY": 0, "EUR": 2} {
		got, ok := dc.Precision(currency)
		assert.True(t, ok, currency)
		assert.Equal(t, want, got, currency)
	}
	assert.Equal(t, "1.500", pydecimal.String(dc.Quantize(decimal.RequireFromString("1.5"), "USD")))
	assert.Equal(t, "1235", pydecimal.String(dc.Quantize(decimal.RequireFromString("1234.5678"), "JPY")))
}
