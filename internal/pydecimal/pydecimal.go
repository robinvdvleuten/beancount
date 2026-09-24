// Package pydecimal reproduces the Python decimal semantics beancount relies
// on where they differ from shopspring/decimal, so parsed and interpolated
// numbers keep the precision beancount gives them.
package pydecimal

import (
	"math/big"

	"github.com/shopspring/decimal"
)

// Quo divides like Python's decimal: an exact quotient takes the ideal
// exponent exp(a) - exp(b), or as few more fractional digits as it needs
// (10.00 / 2 = 5.00, 10 / 4 = 2.5, 20.00 / 10 = 2.00). An inexact quotient
// keeps shopspring's division precision (#392 tracks Python's 28
// significant digits).
func Quo(a, b decimal.Decimal) decimal.Decimal {
	q := a.Div(b)
	if !q.Mul(b).Equal(a) {
		return q
	}
	return reduce(q, a.Exponent()-b.Exponent())
}

// Normalize strips trailing zeros, like Python's Decimal.normalize.
func Normalize(d decimal.Decimal) decimal.Decimal {
	return reduce(d, 1<<30)
}

// reduce drops trailing zeros from d's coefficient until its exponent
// reaches limit; zero takes the limit exponent (or 0 when normalizing).
func reduce(d decimal.Decimal, limit int32) decimal.Decimal {
	coefficient, exponent := d.Coefficient(), d.Exponent()
	if coefficient.Sign() == 0 {
		return decimal.New(0, min(limit, 0))
	}
	ten, remainder := big.NewInt(10), new(big.Int)
	for exponent < limit {
		quotient, rem := new(big.Int).QuoRem(coefficient, ten, remainder)
		if rem.Sign() != 0 {
			break
		}
		coefficient, exponent = quotient, exponent+1
	}
	return decimal.NewFromBigInt(coefficient, exponent)
}
