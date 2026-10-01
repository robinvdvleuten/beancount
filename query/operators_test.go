package query

import (
	"math"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// TestCastDecimal pins castDecimal to what Python's Decimal() reads,
// probed with Python 3.11 ("" where it fails).
func TestCastDecimal(t *testing.T) {
	for input, want := range map[string]string{
		"1_000": "1000",
		"1__0":  "10",
		"_1":    "1",
		"1.0_0": "1.00",
		".5":    "0.5",
		"5.":    "5",
		"1.e5":  "100000",
		" 7 ":   "7",
		"\t7\n": "7",
		"+1":    "1",
		"-0.50": "-0.50",
		"１２":    "12",
		"1,000": "",
		"0x10":  "",
		"1e":    "",
		"e5":    "",
		// Python's NaN and Infinity have no decimal here.
		"NaN": "",
		"Inf": "",
	} {
		got := castDecimal(input)
		if want == "" {
			assert.Zero(t, got, input)
			continue
		}
		d, ok := got.(decimal.Decimal)
		assert.True(t, ok, input)
		assert.Equal(t, want, decimalLiteral(d), input)
	}
}

// TestCastDate pins castDate to what Python's strptime(s, '%Y-%m-%d')
// reads, probed with Python 3.11 ("" where it fails).
func TestCastDate(t *testing.T) {
	for input, want := range map[string]string{
		"2023-2-1":    "2023-02-01",
		"2023-02-1":   "2023-02-01",
		"2023-2- 1":   "2023-02-01",
		"2023-12-31":  "2023-12-31",
		"２０２３-01-01":  "2023-01-01",
		"2023-02-01 ": "",
		"23-02-01":    "",
		"02023-02-01": "",
		"2023-02-30":  "",
		"0000-01-01":  "",
		"2023-1-1x":   "",
		"2023-13-01":  "",
	} {
		got := castDate(input)
		if want == "" {
			assert.Zero(t, got, input)
			continue
		}
		date, ok := got.(*ast.Date)
		assert.True(t, ok, input)
		assert.Equal(t, want, date.String(), input)
	}
}

func TestIntegerOverflowFails(t *testing.T) {
	for name, op := range map[string]func(){
		"add": func() { addInt(math.MaxInt64, 1) },
		"sub": func() { subInt(math.MinInt64, 1) },
		"neg": func() { subInt(0, math.MinInt64) },
		"mul": func() { mulInt(100000000000, 100000000000) },
		"min": func() { mulInt(-1, math.MinInt64) },
	} {
		func() {
			defer func() {
				assert.Equal(t, any(evalError{message: "integer overflow"}), recover(), name)
			}()
			op()
		}()
	}
	assert.Equal(t, int64(math.MinInt64), addInt(math.MinInt64+1, -1))
	assert.Equal(t, int64(math.MinInt64), mulInt(math.MinInt64/2, 2))
	assert.Equal(t, int64(math.MaxInt64), subInt(-1, math.MinInt64))
}
