package pydecimal

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

func TestQuo(t *testing.T) {
	// Expected values are Python decimal results.
	for _, tt := range [][3]string{
		{"10", "4", "2.5"},
		{"10.00", "2", "5.00"},
		{"20.00", "10", "2.00"},
		{"20.00", "10.00", "2"},
		{"1.00", "4", "0.25"},
		{"7.5", "2.5", "3"},
		{"-3.30", "1.1", "-3.0"},
		{"100", "8", "12.5"},
		{"0.00", "5", "0.00"},
	} {
		got := Quo(decimal.RequireFromString(tt[0]), decimal.RequireFromString(tt[1]))
		assert.Equal(t, tt[2], got.StringFixed(max(-got.Exponent(), 0)), tt[0]+" / "+tt[1])
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"0.0100": "0.01", "0.03096": "0.03096", "20": "20", "0.00": "0"} {
		got := Normalize(decimal.RequireFromString(in))
		assert.Equal(t, want, got.StringFixed(max(-got.Exponent(), 0)), in)
	}
}
