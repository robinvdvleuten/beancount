package pydecimal

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestNewFromString(t *testing.T) {
	// Expected values are str(Decimal(s)) in Python.
	for _, tt := range [][2]string{
		{"10", "10"},
		{"1_0", "10"},
		{"_1", "1"},
		{"1__0", "10"},
		{"1._5", "1.5"},
		{"1.5_0", "1.50"},
		{"1e1_0", "1E+10"},
		{"1e-_2", "0.01"},
		{"٣", "3"},
		{"٣.١", "3.1"},
		{"๓", "3"},
		{"１", "1"},
		{"\U0001D7D7", "9"},
		{"\U0001D7D8", "0"},
		{"\t10\n", "10"},
		{" 10", "10"},
		{"　 1", "1"},
		{"1 \x1c", "1"},
		{"+.5", "0.5"},
		{"-5.", "-5"},
		{"1E+2", "1E+2"},
		{"0.010", "0.010"},
	} {
		got, err := NewFromString(tt[0])
		assert.NoError(t, err, tt[0])
		assert.Equal(t, tt[1], String(got), tt[0])
	}
}

func TestNewFromStringRejects(t *testing.T) {
	// Python rejects all but the infinities and NaNs, which have no
	// shopspring value.
	for _, s := range []string{"", ".", "_", "__", "_e5", "1e", "abc", "1,0", "1 0", "1 0", "inf", "NaN", "-Infinity"} {
		_, err := NewFromString(s)
		assert.Equal(t, ErrConversionSyntax, err, s)
	}
}
