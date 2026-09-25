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

func TestQuoInexact(t *testing.T) {
	// Expected values are Python decimal results in the default context.
	for _, tt := range [][3]string{
		{"1", "3", "0.3333333333333333333333333333"},
		{"2", "3", "0.6666666666666666666666666667"},
		{"1000000", "7", "142857.1428571428571428571429"},
		{"-1", "3", "-0.3333333333333333333333333333"},
		{"1", "-0.3", "-3.333333333333333333333333333"},
		{"0.1", "3", "0.03333333333333333333333333333"},
		{"1", "7E-30", "1.428571428571428571428571429E+29"},
		{"123456789012345678901234567890", "1", "1.234567890123456789012345679E+29"},
		{"99999999999999999999999999995", "1", "1.000000000000000000000000000E+29"},
	} {
		got := Quo(decimal.RequireFromString(tt[0]), decimal.RequireFromString(tt[1]))
		want := decimal.RequireFromString(tt[2])
		assert.Equal(t, want.Coefficient().String(), got.Coefficient().String(), tt[0]+" / "+tt[1])
		assert.Equal(t, want.Exponent(), got.Exponent(), tt[0]+" / "+tt[1])
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"0.0100": "0.01", "0.03096": "0.03096", "20": "20", "0.00": "0"} {
		got := Normalize(decimal.RequireFromString(in))
		assert.Equal(t, want, got.StringFixed(max(-got.Exponent(), 0)), in)
	}
}

func TestArithmetic(t *testing.T) {
	third := Quo(decimal.NewFromInt(1), decimal.NewFromInt(3))
	big := decimal.RequireFromString("1000000000000000000000000000")
	// Expected values are Python decimal results in the default context.
	for _, tt := range []struct {
		name string
		got  decimal.Decimal
		want string
	}{
		{"100 + 1/3", Add(decimal.NewFromInt(100), third), "100.3333333333333333333333333"},
		{"1/3 - 100", Sub(third, decimal.NewFromInt(100)), "-99.66666666666666666666666667"},
		{"10 * 1/3", Mul(decimal.NewFromInt(10), third), "3.333333333333333333333333333"},
		{"-10 * 1/3", Mul(decimal.NewFromInt(-10), third), "-3.333333333333333333333333333"},
		{"1.10 + 0", Add(decimal.RequireFromString("1.10"), decimal.Zero), "1.10"},
		{"1.5 * 2.0", Mul(decimal.RequireFromString("1.5"), decimal.RequireFromString("2.0")), "3.00"},
		{"10 - 10.00", Sub(decimal.NewFromInt(10), decimal.RequireFromString("10.00")), "0.00"},
		{"tie to even, down", Add(big, decimal.RequireFromString("0.5")), "1000000000000000000000000000"},
		{"tie to even, up", Add(big, decimal.RequireFromString("1.5")), "1000000000000000000000000002"},
		{"tie to even, stays", Add(big, decimal.RequireFromString("2.5")), "1000000000000000000000000002"},
		{"carry adds a digit", Add(decimal.RequireFromString("9999999999999999999999999999"), decimal.RequireFromString("0.5")), "1.000000000000000000000000000E+28"},
	} {
		want := decimal.RequireFromString(tt.want)
		assert.Equal(t, want.Coefficient().String(), tt.got.Coefficient().String(), tt.name)
		assert.Equal(t, want.Exponent(), tt.got.Exponent(), tt.name)
	}
}
