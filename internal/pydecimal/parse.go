package pydecimal

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/shopspring/decimal"
)

// ErrConversionSyntax is Python's decimal.ConversionSyntax: the string is
// not a number.
var ErrConversionSyntax = errors.New("[<class 'decimal.ConversionSyntax'>]")

// finiteNumberRegex is the syntax of a finite number in Python's Decimal
// constructor, once its digits are ASCII and its underscores gone.
var finiteNumberRegex = regexp.MustCompile(`^([+-]?)(?:([0-9]+)(?:\.([0-9]*))?|\.([0-9]+))(?:[eE]([+-]?[0-9]+))?$`)

// NewFromString reads a number as Python's Decimal(str) reads a finite one:
// surrounding whitespace is stripped, underscores are dropped wherever they
// are, and a digit may be any Unicode decimal digit ("٣" is 3). Like
// shopspring, it has no infinity or NaN, so it rejects them, which Python
// takes.
func NewFromString(s string) (decimal.Decimal, error) {
	s = strings.TrimFunc(s, isPythonSpace)
	var ascii strings.Builder
	ascii.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '_':
		case r < 0x80:
			ascii.WriteRune(r)
		case unicode.Is(unicode.Nd, r):
			ascii.WriteByte('0' + digitValue(r))
		default:
			return decimal.Decimal{}, ErrConversionSyntax
		}
	}
	match := finiteNumberRegex.FindStringSubmatch(ascii.String())
	if match == nil {
		return decimal.Decimal{}, ErrConversionSyntax
	}
	sign, whole, fraction, exponent := match[1], match[2], match[3], match[5]
	if whole == "" {
		whole, fraction = "0", match[4]
	}
	canonical := sign + whole
	if fraction != "" {
		canonical += "." + fraction
	}
	if exponent != "" {
		canonical += "e" + exponent
	}
	d, err := decimal.NewFromString(canonical)
	if err != nil {
		return decimal.Decimal{}, ErrConversionSyntax
	}
	return d, nil
}

// isPythonSpace reports whether str.strip() strips r: Go's white space and
// the information separators Python counts as white space too.
func isPythonSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// digitValue is the value of a Unicode decimal digit. Unicode encodes each
// script's digits as a contiguous run from zero to nine, and adjacent runs
// (the mathematical digits) each start at zero, so the value is the
// distance from the start of the run, modulo ten.
func digitValue(r rune) byte {
	start := r
	for unicode.Is(unicode.Nd, start-1) {
		start--
	}
	return byte((r - start) % 10)
}
