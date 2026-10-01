package query

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/internal/pyrepr"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// pyIntPattern is the integers Python's int() reads from a string, once
// parsePyInt has normalized it: underscores only between digits.
var pyIntPattern = regexp.MustCompile(`^[+-]?\d+(_\d+)*$`)

// parsePyInt reads s as Python's int(s) does: white space around it,
// single underscores between digits and digits of any script. One beyond
// what an integer holds here fails the statement, where Python's integers
// grow.
func parsePyInt(s string) (int64, bool) {
	s = asciiDigits(strings.TrimFunc(s, bql.IsSpace))
	if !pyIntPattern.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
	if err != nil {
		failIntegerOverflow()
	}
	return n, true
}

// castInt is beanquery's int(): an integer, a boolean as 1 or 0, a decimal
// truncated toward zero, a string Python's int() reads, anything else NULL.
func castInt(v any) any {
	switch val := v.(type) {
	case int64:
		return val
	case bool:
		if val {
			return int64(1)
		}
		return int64(0)
	case decimal.Decimal:
		integer := val.Truncate(0).BigInt()
		if !integer.IsInt64() {
			failIntegerOverflow()
		}
		return integer.Int64()
	case string:
		if n, ok := parsePyInt(val); ok {
			return n
		}
	}
	return nil
}

// castBool is beanquery's bool(): Python's truth value. Like beancount's
// Inventory, an inventory has none, which fails the statement.
func castBool(v any) any {
	if _, ok := v.(*inventoryValue); ok {
		fail("Use explicit is_empty() method instead.")
	}
	return truthy(v)
}

// roundDecimal is Python's round(d, digits) of a decimal: d rounded half to
// even to digits places, its exponent -digits, so round(12.345, -1) is
// 1E+1. A result of more digits than Python's context holds fails the
// statement with Python's InvalidOperation.
func roundDecimal(d decimal.Decimal, digits int64) decimal.Decimal {
	if digits > 1<<20 || digits < -(1<<20) {
		fail("[<class 'decimal.InvalidOperation'>]")
	}
	rounded := d.RoundBank(int32(digits))
	if rounded.NumDigits() > 28 && !rounded.IsZero() {
		fail("[<class 'decimal.InvalidOperation'>]")
	}
	return rounded
}

// roundInt is Python's round(n, digits) of an integer: n itself for digits
// of zero or more, and otherwise n rounded half to even to a multiple of
// 10**-digits.
func roundInt(n, digits int64) int64 {
	if digits >= 0 {
		return n
	}
	if digits < -19 {
		return 0
	}
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(-digits), nil)
	q, r := new(big.Int).DivMod(big.NewInt(n), p, new(big.Int))
	twice := new(big.Int).Lsh(r, 1)
	if c := twice.Cmp(p); c > 0 || c == 0 && q.Bit(0) == 1 {
		q.Add(q, big.NewInt(1))
	}
	result := q.Mul(q, p)
	if !result.IsInt64() {
		failIntegerOverflow()
	}
	return result.Int64()
}

// substr is Python's s[start:end] in code points, negative indices counting
// from the end.
func substr(s string, start, end int64) string {
	runes := []rune(s)
	n := int64(len(runes))
	clamp := func(i int64) int64 {
		if i < 0 {
			i += n
		}
		return max(0, min(i, n))
	}
	start, end = clamp(start), clamp(end)
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

// splitComponent is Python's s.split(delim)[index], negative indices
// counting from the end. An empty delimiter or an index out of range fails
// the statement with Python's message.
func splitComponent(s, delim string, index int64) string {
	if delim == "" {
		fail("empty separator")
	}
	parts := strings.Split(s, delim)
	if index < 0 {
		index += int64(len(parts))
	}
	if index < 0 || index >= int64(len(parts)) {
		fail("list index out of range")
	}
	return parts[index]
}

// reprValue is Python's repr() of a value as beanquery holds it: a
// literal's, a list's or a cost's as pyValueRepr renders it, a set's
// elements sorted, and an amount, a position or an inventory as str()
// prints it.
func reprValue(v any) string {
	switch val := v.(type) {
	case setValue:
		if len(val) == 0 {
			return "frozenset()"
		}
		quoted := make([]string, 0, len(val))
		for _, elem := range val.Sorted() {
			quoted = append(quoted, pyrepr.String(elem))
		}
		return "frozenset({" + strings.Join(quoted, ", ") + "})"
	case *amountValue, *positionValue, *inventoryValue:
		return objectString(v)
	}
	return pyValueRepr(v)
}
