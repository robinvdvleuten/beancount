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
		// A zero quotient keeps the ideal exponent, above zero too.
		{"0", "-10.5", "0E+1"},
		{"0", "8.00", "0E+2"},
		{"0.00", "8.00", "0"},
	} {
		got := Quo(decimal.RequireFromString(tt[0]), decimal.RequireFromString(tt[1]))
		want := decimal.RequireFromString(tt[2])
		assert.Equal(t, want.Coefficient().String(), got.Coefficient().String(), tt[0]+" / "+tt[1])
		assert.Equal(t, want.Exponent(), got.Exponent(), tt[0]+" / "+tt[1])
	}
}

func TestZero(t *testing.T) {
	// Python's Decimal() has exponent 0; shopspring's Zero has 1.
	assert.Equal(t, int32(0), Zero.Exponent())
	assert.Equal(t, int32(1), decimal.Zero.Exponent())
	assert.Equal(t, "0", String(Mul(decimal.RequireFromString("8.00"), Quo(Zero, decimal.RequireFromString("8.00")))))
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
		{"7.5 % 2", Rem(decimal.RequireFromString("7.5"), decimal.NewFromInt(2)), "1.5"},
		{"-7.5 % 2", Rem(decimal.RequireFromString("-7.5"), decimal.NewFromInt(2)), "-1.5"},
		{"-7 % 2.0", Rem(decimal.NewFromInt(-7), decimal.RequireFromString("2.0")), "-1.0"},
		{"7 % -3", Rem(decimal.NewFromInt(7), decimal.NewFromInt(-3)), "1"},
		{"6.00 % 3", Rem(decimal.RequireFromString("6.00"), decimal.NewFromInt(3)), "0.00"},
		{"carry adds a digit", Add(decimal.RequireFromString("9999999999999999999999999999"), decimal.RequireFromString("0.5")), "1.000000000000000000000000000E+28"},
	} {
		want := decimal.RequireFromString(tt.want)
		assert.Equal(t, want.Coefficient().String(), tt.got.Coefficient().String(), tt.name)
		assert.Equal(t, want.Exponent(), tt.got.Exponent(), tt.name)
	}
}

func TestString(t *testing.T) {
	// Expected values are Python's str(Decimal).
	for _, tt := range []struct {
		in   decimal.Decimal
		want string
	}{
		{decimal.New(1, -6), "0.000001"},
		{decimal.New(1, -7), "1E-7"},
		{decimal.New(12, -8), "1.2E-7"},
		{decimal.New(-1, -7), "-1E-7"},
		{decimal.New(0, -7), "0E-7"},
		{decimal.New(0, -6), "0.000000"},
		{decimal.RequireFromString("3.333333333333333333333333333E-11"), "3.333333333333333333333333333E-11"},
		{decimal.New(250, -2), "2.50"},
		{decimal.New(-5, -1), "-0.5"},
		{decimal.New(200, 0), "200"},
		{decimal.New(0, 0), "0"},
		{decimal.New(2, 1), "2E+1"},
		{decimal.New(0, 2), "0E+2"},
		{decimal.New(123, 3), "1.23E+5"},
	} {
		assert.Equal(t, tt.want, String(tt.in), tt.want)
	}
}
