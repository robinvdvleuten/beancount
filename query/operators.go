package query

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// opSignature is one typed signature of a binary operator: its operand
// types, its result type, and its evaluation on two non-NULL values. bind,
// when set, prepares the evaluation for a constant right operand once.
type opSignature struct {
	l, r   dtype
	result dtype
	eval   func(l, r any) any
	bind   func(r any) func(l any) any
}

// operatorDef is a binary operator beanquery type-checks: the name its
// errors give it and its signatures, which an operand pair must match
// exactly.
type operatorDef struct {
	name       string
	signatures []opSignature
}

func (d *operatorDef) match(l, r dtype) *opSignature {
	for i := range d.signatures {
		if sig := &d.signatures[i]; sig.l == l && sig.r == r {
			return sig
		}
	}
	return nil
}

// operators is the registry of typed binary operators, beanquery's
// OPERATORS (query_compile.py) for the operators BQL parses. AND, OR, IN
// and NOT IN take operands of any type and are in untypedOperators
// instead; BETWEEN, with three operands, is compileBetween's.
var operators = func() map[bql.TokenType]*operatorDef {
	ops := map[bql.TokenType]*operatorDef{
		bql.TILDE: {name: "match", signatures: []opSignature{
			{tString, tString, tBool, func(l, r any) any {
				return matcher(r.(string))(l.(string))
			}, func(r any) func(l any) any {
				match := matcher(r.(string))
				return func(l any) any { return match(l.(string)) }
			}},
		}},
		bql.NOTTILDE: {name: "notmatch", signatures: []opSignature{
			{tString, tString, tBool, func(l, r any) any {
				return !matcher(r.(string))(l.(string))
			}, func(r any) func(l any) any {
				match := matcher(r.(string))
				return func(l any) any { return !match(l.(string)) }
			}},
		}},
		// Like beanquery's, ?~ searches the right operand for the left
		// one, case-sensitively.
		bql.QTILDE: {name: "matches", signatures: []opSignature{
			{l: tString, r: tString, result: tBool, eval: func(l, r any) any {
				return mustCompilePattern("", l.(string)).MatchString(r.(string))
			}},
		}},
	}

	// Comparisons take two numbers, two dates or two strings.
	comparable := [][2]dtype{
		{tInt, tInt}, {tDecimal, tInt}, {tInt, tDecimal}, {tDecimal, tDecimal},
		{tDate, tDate}, {tString, tString},
	}
	for op, cmp := range map[bql.TokenType]struct {
		name string
		test func(int) bool
	}{
		bql.EQ:  {"equal", func(c int) bool { return c == 0 }},
		bql.NE:  {"notequal", func(c int) bool { return c != 0 }},
		bql.GT:  {"greater", func(c int) bool { return c > 0 }},
		bql.GTE: {"greatereq", func(c int) bool { return c >= 0 }},
		bql.LT:  {"less", func(c int) bool { return c < 0 }},
		bql.LTE: {"lesseq", func(c int) bool { return c <= 0 }},
	} {
		def := &operatorDef{name: cmp.name}
		for _, types := range comparable {
			def.signatures = append(def.signatures, opSignature{l: types[0], r: types[1], result: tBool, eval: func(l, r any) any {
				return cmp.test(compareValues(l, r))
			}})
		}
		ops[op] = def
	}

	// Arithmetic takes two numbers: an integer when both are integers,
	// except for division, and a decimal otherwise.
	for op, arith := range map[bql.TokenType]struct {
		name    string
		integer func(l, r int64) int64
		dec     func(l, r decimal.Decimal) any
	}{
		bql.PLUS:     {"add", addInt, func(l, r decimal.Decimal) any { return pydecimal.Add(l, r) }},
		bql.MINUS:    {"sub", subInt, func(l, r decimal.Decimal) any { return pydecimal.Sub(l, r) }},
		bql.ASTERISK: {"mul", mulInt, func(l, r decimal.Decimal) any { return pydecimal.Mul(l, r) }},
		bql.SLASH:    {"div", nil, divide},
	} {
		dec := func(l, r any) any {
			ld, _ := asDecimal(l)
			rd, _ := asDecimal(r)
			return arith.dec(ld, rd)
		}
		def := &operatorDef{name: arith.name, signatures: []opSignature{
			{l: tDecimal, r: tDecimal, result: tDecimal, eval: dec},
			{l: tDecimal, r: tInt, result: tDecimal, eval: dec},
			{l: tInt, r: tDecimal, result: tDecimal, eval: dec},
		}}
		if arith.integer != nil {
			def.signatures = append(def.signatures, opSignature{l: tInt, r: tInt, result: tInt, eval: func(l, r any) any {
				return arith.integer(l.(int64), r.(int64))
			}})
		} else {
			def.signatures = append(def.signatures, opSignature{l: tInt, r: tInt, result: tDecimal, eval: dec})
		}
		ops[op] = def
	}

	// Strings concatenate, and dates move by a number of days.
	ops[bql.PLUS].signatures = append(ops[bql.PLUS].signatures,
		opSignature{l: tString, r: tString, result: tString, eval: func(l, r any) any { return l.(string) + r.(string) }},
		opSignature{l: tDate, r: tInt, result: tDate, eval: func(l, r any) any { return addDays(l.(*ast.Date), r.(int64)) }},
		opSignature{l: tInt, r: tDate, result: tDate, eval: func(l, r any) any { return addDays(r.(*ast.Date), l.(int64)) }},
	)
	// Intervals move dates and add up, like dateutil's relativedelta.
	// Python subtracts no date from an interval, which beanquery types as
	// a date anyway, and types the difference of two intervals as a
	// date, which it then fails to print; here it is an interval
	// (KNOWN_GAPS.md).
	ops[bql.PLUS].signatures = append(ops[bql.PLUS].signatures,
		opSignature{l: tDate, r: tInterval, result: tDate, eval: func(l, r any) any { return r.(*intervalValue).addTo(l.(*ast.Date)) }},
		opSignature{l: tInterval, r: tDate, result: tDate, eval: func(l, r any) any { return l.(*intervalValue).addTo(r.(*ast.Date)) }},
		opSignature{l: tInterval, r: tInterval, result: tInterval, eval: func(l, r any) any { return l.(*intervalValue).add(r.(*intervalValue)) }},
	)
	ops[bql.MINUS].signatures = append(ops[bql.MINUS].signatures,
		opSignature{l: tDate, r: tInterval, result: tDate, eval: func(l, r any) any { return r.(*intervalValue).neg().addTo(l.(*ast.Date)) }},
		opSignature{l: tInterval, r: tDate, result: tDate, eval: func(l, r any) any {
			fail("unsupported operand type(s) for -: 'relativedelta' and 'datetime.date'")
			return nil
		}},
		opSignature{l: tInterval, r: tInterval, result: tInterval, eval: func(l, r any) any { return l.(*intervalValue).add(r.(*intervalValue).neg()) }},
	)
	ops[bql.MINUS].signatures = append(ops[bql.MINUS].signatures,
		opSignature{l: tDate, r: tInt, result: tDate, eval: func(l, r any) any { return addDays(l.(*ast.Date), -r.(int64)) }},
		opSignature{l: tDate, r: tDate, result: tInt, eval: func(l, r any) any {
			return daysBetween(r.(*ast.Date), l.(*ast.Date))
		}},
	)

	// Modulo takes two numbers, like Python's %: an integer's remainder
	// takes the divisor's sign and a decimal's the dividend's. A zero
	// divisor gives NULL.
	modDecimal := func(l, r any) any {
		ld, _ := asDecimal(l)
		rd, _ := asDecimal(r)
		if rd.IsZero() {
			return nil
		}
		return pydecimal.Rem(ld, rd)
	}
	ops[bql.PERCENT] = &operatorDef{name: "mod", signatures: []opSignature{
		{l: tInt, r: tInt, result: tInt, eval: func(l, r any) any {
			x, y := l.(int64), r.(int64)
			if y == 0 {
				return nil
			}
			m := x % y
			if m != 0 && (m < 0) != (y < 0) {
				m += y
			}
			return m
		}},
		{l: tDecimal, r: tInt, result: tDecimal, eval: modDecimal},
		{l: tInt, r: tDecimal, result: tDecimal, eval: modDecimal},
		{l: tDecimal, r: tDecimal, result: tDecimal, eval: modDecimal},
	}}
	return ops
}()

// addInt, subInt and mulInt are integer arithmetic. Python's integers do
// not overflow and ours do, so one that would fails the statement rather
// than wrap around (KNOWN_GAPS.md).
func addInt(l, r int64) int64 {
	sum := l + r
	if (l >= 0) == (r >= 0) && (sum >= 0) != (l >= 0) {
		failIntegerOverflow()
	}
	return sum
}

func subInt(l, r int64) int64 {
	difference := l - r
	if (l >= 0) != (r >= 0) && (difference >= 0) != (l >= 0) {
		failIntegerOverflow()
	}
	return difference
}

func mulInt(l, r int64) int64 {
	product := l * r
	if l != 0 && (product/l != r || l == -1 && r == math.MinInt64) {
		failIntegerOverflow()
	}
	return product
}

func failIntegerOverflow() {
	fail("integer overflow")
}

// betweenGroups are the types BETWEEN compares with each other, beanquery's
// _comparable: its three operands must all be in one group.
var betweenGroups = [][]dtype{{tInt, tDecimal}, {tDate}, {tString}}

// cBetween is beanquery's EvalBetween, lower <= x <= upper, NULL when any
// operand is.
type cBetween struct{ x, lower, upper cexpr }

func (c *cBetween) typ() dtype { return tBool }

func (c *cBetween) eval(row *evalRow) any {
	x := c.x.eval(row)
	if x == nil {
		return nil
	}
	lower := c.lower.eval(row)
	if lower == nil {
		return nil
	}
	upper := c.upper.eval(row)
	if upper == nil {
		return nil
	}
	return compareValues(lower, x) <= 0 && compareValues(x, upper) <= 0
}

// unarySignature is one typed signature of a unary operator. A NULL
// operand gives NULL, unless the operator is nullSafe.
type unarySignature struct {
	x, result dtype
	nullSafe  bool
	eval      func(x any) any
}

// unaryOperatorDef is a unary operator beanquery type-checks: the name its
// errors give it and its signatures. tAny matches every operand.
type unaryOperatorDef struct {
	name       string
	pyClass    string // beanquery's parser node class, as its errors quote it
	signatures []unarySignature
}

// match finds the signature for an operand of type x. Like beanquery,
// which looks unary operators up like functions, a boolean also matches
// an integer signature: Python's bool subclasses int.
func (d *unaryOperatorDef) match(x dtype) *unarySignature {
	candidates := []dtype{x}
	if x == tBool {
		candidates = append(candidates, tInt)
	}
	for _, t := range candidates {
		for i := range d.signatures {
			if sig := &d.signatures[i]; sig.x == t || sig.x == tAny && x != tAsterisk {
				return sig
			}
		}
	}
	return nil
}

// unaryOperators is the registry of unary operators, beanquery's OPERATORS
// for Not and Neg.
var unaryOperators = map[bql.TokenType]*unaryOperatorDef{
	bql.NOT: {name: "not", pyClass: "Not", signatures: []unarySignature{
		{x: tAny, result: tBool, nullSafe: true, eval: func(x any) any { return !truthy(x) }},
	}},
	bql.MINUS: {name: "neg", pyClass: "Neg", signatures: []unarySignature{
		{x: tInt, result: tInt, eval: func(x any) any {
			if b, ok := x.(bool); ok {
				if b {
					return int64(-1)
				}
				return int64(0)
			}
			return subInt(0, x.(int64))
		}},
		{x: tDecimal, result: tDecimal, eval: func(x any) any { return x.(decimal.Decimal).Neg() }},
	}},
}

// matcher is ~ against pattern: Python's re.search with re.IGNORECASE. A
// pattern that does not compile fails the statement once it is used.
func matcher(pattern string) func(s string) bool {
	re, err := compilePattern("(?i)", pattern)
	return func(s string) bool {
		if err != nil {
			fail("%s", err)
		}
		return re.MatchString(s)
	}
}

// compilePattern compiles pattern, behind flags, as Python's re module
// would, RE2's syntax standing in for Python's: a pattern that does not
// compile is an error, as it is a re.error in beanquery, and so is one
// that uses Python syntax RE2 lacks, such as a lookaround or a
// backreference (KNOWN_GAPS.md).
func compilePattern(flags, pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		reason := err.Error()
		var syntaxErr *syntax.Error
		if errors.As(err, &syntaxErr) {
			reason = string(syntaxErr.Code)
		}
		return nil, fmt.Errorf("invalid regular expression %s: %s", pyrepr.String(pattern), reason)
	}
	return re, nil
}

// mustCompilePattern is compilePattern, failing the statement for a
// pattern that does not compile.
func mustCompilePattern(flags, pattern string) *regexp.Regexp {
	re, err := compilePattern(flags, pattern)
	if err != nil {
		fail("%s", err)
	}
	return re
}

// untypedOperators builds AND, OR and IN, which take operands of any type,
// so beanquery checks no signature for them.
var untypedOperators = map[bql.TokenType]func(l, r cexpr) cexpr{
	bql.AND:   func(l, r cexpr) cexpr { return &cAnd{l: l, r: r} },
	bql.OR:    func(l, r cexpr) cexpr { return &cOr{l: l, r: r} },
	bql.IN:    func(l, r cexpr) cexpr { return &cIn{l: l, r: r} },
	bql.NOTIN: func(l, r cexpr) cexpr { return &cNotIn{in: cIn{l: l, r: r}} },
}

// cAnd is beanquery's EvalAnd: NULL at the first NULL operand, FALSE at the
// first false one, and TRUE otherwise, so NULL AND FALSE is NULL.
type cAnd struct{ l, r cexpr }

func (c *cAnd) typ() dtype { return tBool }

func (c *cAnd) eval(row *evalRow) any {
	for _, operand := range []cexpr{c.l, c.r} {
		v := operand.eval(row)
		if v == nil {
			return nil
		}
		if !truthy(v) {
			return false
		}
	}
	return true
}

// cOr is beanquery's EvalOr: TRUE at the first true operand, else NULL when
// an operand was NULL, and FALSE otherwise.
type cOr struct{ l, r cexpr }

func (c *cOr) typ() dtype { return tBool }

func (c *cOr) eval(row *evalRow) any {
	var result any = false
	for _, operand := range []cexpr{c.l, c.r} {
		v := operand.eval(row)
		if v == nil {
			result = nil
		} else if truthy(v) {
			return true
		}
	}
	return result
}

// cIn is Python's in, NULL when either operand is: a value equal to a
// list's element by Python's ==, membership of a set, or a case-sensitive
// substring of a string. A list is unhashable, so testing it against a set
// fails the statement, as Python's TypeError fails beanquery's. Any other
// left operand that is not a string is never in a set or a string, where
// 1 IN 'abc' is a TypeError in beanquery (KNOWN_GAPS.md).
type cIn struct{ l, r cexpr }

func (c *cIn) typ() dtype { return tBool }

func (c *cIn) eval(row *evalRow) any {
	l := c.l.eval(row)
	if l == nil {
		return nil
	}
	r := c.r.eval(row)
	if r == nil {
		return nil
	}
	if list, ok := r.(listValue); ok {
		return list.contains(l)
	}
	if _, isList := l.(listValue); isList {
		if _, isSet := r.(setValue); isSet {
			fail("unhashable type: 'list'")
		}
	}
	elem, ok := l.(string)
	if !ok {
		return false
	}
	switch container := r.(type) {
	case setValue:
		return container.Contains(elem)
	case string:
		return strings.Contains(container, elem)
	}
	return false
}

// cNotIn is Python's not in: the negation of in, NULL when either
// operand is.
type cNotIn struct{ in cIn }

func (c *cNotIn) typ() dtype { return tBool }

func (c *cNotIn) eval(row *evalRow) any {
	if in, ok := c.in.eval(row).(bool); ok {
		return !in
	}
	return nil
}

// divide divides like beanquery's operator, which gives NULL for a zero
// divisor.
func divide(l, r decimal.Decimal) any {
	if r.IsZero() {
		return nil
	}
	return pydecimal.Quo(l, r)
}

func addDays(d *ast.Date, days int64) *ast.Date {
	return &ast.Date{Time: d.AddDate(0, 0, int(days))}
}

// casts are the types beanquery casts an untyped (object) operand to, with
// the cast function it applies (types.MAP); each is NULL-strict.
var casts = map[dtype]func(v any) any{
	tBool:    func(v any) any { return truthy(v) },
	tDate:    castDate,
	tDecimal: castDecimal,
	tString:  func(v any) any { return strValue(v) },
}

// castDecimal is beanquery's decimal(): a number or a numeric string as a
// decimal, a boolean as 1 or 0, anything else NULL.
func castDecimal(v any) any {
	switch val := v.(type) {
	case bool:
		if val {
			return decimal.NewFromInt(1)
		}
		return decimal.Zero
	case string:
		if d, ok := parsePyDecimal(val); ok {
			return d
		}
		return nil
	}
	if d, ok := asDecimal(v); ok {
		return d
	}
	return nil
}

// pyDecimalPattern is the finite numbers Python's Decimal() reads, once
// parsePyDecimal has normalized the string.
var pyDecimalPattern = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// parsePyDecimal reads s as Python's Decimal(s) does: white space around
// it, underscores anywhere in it and digits of any script. Python's NaN
// and Infinity have no decimal here, so they read as no number
// (KNOWN_GAPS.md).
func parsePyDecimal(s string) (decimal.Decimal, bool) {
	s = asciiDigits(strings.ReplaceAll(strings.TrimFunc(s, bql.IsSpace), "_", ""))
	if !pyDecimalPattern.MatchString(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	return d, err == nil
}

// asciiDigits replaces every decimal digit of another script with its
// ASCII digit, as Python's int() and Decimal() read them.
func asciiDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII && unicode.IsDigit(r) {
			if digit, ok := digitValue(r); ok {
				return '0' + digit
			}
		}
		return r
	}, s)
}

// digitValue is the value of a Unicode decimal digit: each range of
// unicode.Nd holds whole runs of ten digits, from zero to nine.
func digitValue(r rune) (rune, bool) {
	for _, rng := range unicode.Nd.R16 {
		if lo, hi := rune(rng.Lo), rune(rng.Hi); r >= lo && r <= hi {
			return (r - lo) % 10, true
		}
	}
	for _, rng := range unicode.Nd.R32 {
		if lo, hi := rune(rng.Lo), rune(rng.Hi); r >= lo && r <= hi {
			return (r - lo) % 10, true
		}
	}
	return 0, false
}

// pyDatePattern is what Python's strptime(s, '%Y-%m-%d') matches: a
// four-digit year, a month and a day of one or two digits, the day's
// tens digit possibly a space.
var pyDatePattern = regexp.MustCompile(`^(\d{4})-(1[0-2]|0[1-9]|[1-9])-(3[01]|[12]\d|0[1-9]|[1-9]| [1-9])$`)

// castDate is beanquery's date(): a date, or a string strptime reads with
// '%Y-%m-%d' as one (2023-2-1 too), anything else NULL.
func castDate(v any) any {
	switch val := v.(type) {
	case *ast.Date:
		return val
	case string:
		match := pyDatePattern.FindStringSubmatch(asciiDigits(val))
		if match == nil {
			return nil
		}
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(strings.TrimSpace(match[3]))
		t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		if year < 1 || t.Day() != day {
			return nil
		}
		return &ast.Date{Time: t}
	}
	return nil
}
