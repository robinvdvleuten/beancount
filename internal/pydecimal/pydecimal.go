// Package pydecimal reproduces the Python decimal semantics beancount relies
// on where they differ from shopspring/decimal, so parsed and interpolated
// numbers keep the precision beancount gives them.
package pydecimal

import (
	"math/big"

	"github.com/shopspring/decimal"
)

// Quo divides like Python's decimal in its default context: an exact
// quotient takes the ideal exponent exp(a) - exp(b), or as few more
// fractional digits as it needs (10.00 / 2 = 5.00, 10 / 4 = 2.5); an inexact
// one rounds half-even to 28 significant digits (1 / 3 =
// 0.3333333333333333333333333333). It is the project's only division, as
// Add, Sub and Mul are its only sum, difference and product.
func Quo(a, b decimal.Decimal) decimal.Decimal {
	ideal := a.Exponent() - b.Exponent()
	if a.IsZero() {
		return decimal.New(0, min(ideal, 0))
	}
	// Python's _divide: compute prec+1 digits, fold a remainder into a
	// sticky last digit, then round the whole to prec digits.
	ca, cb := new(big.Int).Abs(a.Coefficient()), new(big.Int).Abs(b.Coefficient())
	shift := len(cb.String()) - len(ca.String()) + precision + 1
	exponent := ideal - int32(shift)
	if shift >= 0 {
		ca.Mul(ca, pow10(shift))
	} else {
		cb.Mul(cb, pow10(-shift))
	}
	coefficient, remainder := new(big.Int).QuoRem(ca, cb, new(big.Int))
	if remainder.Sign() != 0 {
		if new(big.Int).Rem(coefficient, big.NewInt(5)).Sign() == 0 {
			coefficient.Add(coefficient, big.NewInt(1))
		}
	} else {
		reduced := reduce(decimal.NewFromBigInt(coefficient, exponent), ideal)
		coefficient, exponent = reduced.Coefficient(), reduced.Exponent()
	}
	coefficient, exponent = roundHalfEven(coefficient, exponent)
	if a.Sign() != b.Sign() {
		coefficient.Neg(coefficient)
	}
	return decimal.NewFromBigInt(coefficient, exponent)
}

// Add adds like Python's decimal in its default context: the exact sum,
// rounded half-even to 28 significant digits.
func Add(a, b decimal.Decimal) decimal.Decimal {
	return round(a.Add(b))
}

// Sub subtracts like Python's decimal in its default context: the exact
// difference, rounded half-even to 28 significant digits.
func Sub(a, b decimal.Decimal) decimal.Decimal {
	return round(a.Sub(b))
}

// Mul multiplies like Python's decimal in its default context: the exact
// product, rounded half-even to 28 significant digits.
func Mul(a, b decimal.Decimal) decimal.Decimal {
	return round(a.Mul(b))
}

// round rounds an exact result half-even to precision significant digits.
// A result that fits, the only kind most ledgers produce, is returned as is.
func round(d decimal.Decimal) decimal.Decimal {
	if d.NumDigits() <= precision {
		return d
	}
	coefficient, exponent := roundHalfEven(new(big.Int).Abs(d.Coefficient()), d.Exponent())
	if d.Sign() < 0 {
		coefficient.Neg(coefficient)
	}
	return decimal.NewFromBigInt(coefficient, exponent)
}

// precision is Python's default decimal context precision.
const precision = 28

// roundHalfEven rounds a non-negative coefficient to precision digits.
func roundHalfEven(coefficient *big.Int, exponent int32) (*big.Int, int32) {
	drop := len(coefficient.String()) - precision
	if drop <= 0 {
		return coefficient, exponent
	}
	divisor := pow10(drop)
	quotient, remainder := new(big.Int).QuoRem(coefficient, divisor, new(big.Int))
	switch remainder.Lsh(remainder, 1).Cmp(divisor) {
	case 1:
		quotient.Add(quotient, big.NewInt(1))
	case 0:
		if quotient.Bit(0) == 1 {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	exponent += int32(drop)
	if len(quotient.String()) > precision {
		quotient.Quo(quotient, big.NewInt(10))
		exponent++
	}
	return quotient, exponent
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
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
