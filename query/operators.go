package query

import (
	"regexp"
	"strings"
	"time"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
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
// OPERATORS (query_compile.py) for the operators BQL parses. AND, OR and IN
// take operands of any type and are in untypedOperators instead.
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
		bql.PLUS:     {"add", func(l, r int64) int64 { return l + r }, func(l, r decimal.Decimal) any { return pydecimal.Add(l, r) }},
		bql.MINUS:    {"sub", func(l, r int64) int64 { return l - r }, func(l, r decimal.Decimal) any { return pydecimal.Sub(l, r) }},
		bql.ASTERISK: {"mul", func(l, r int64) int64 { return l * r }, func(l, r decimal.Decimal) any { return pydecimal.Mul(l, r) }},
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
	ops[bql.MINUS].signatures = append(ops[bql.MINUS].signatures,
		opSignature{l: tDate, r: tInt, result: tDate, eval: func(l, r any) any { return addDays(l.(*ast.Date), -r.(int64)) }},
		opSignature{l: tDate, r: tDate, result: tInt, eval: func(l, r any) any {
			return int64(l.(*ast.Date).Sub(r.(*ast.Date).Time) / (24 * time.Hour))
		}},
	)
	return ops
}()

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
			return -x.(int64)
		}},
		{x: tDecimal, result: tDecimal, eval: func(x any) any { return x.(decimal.Decimal).Neg() }},
	}},
}

// matcher is ~ against pattern: Python's re.search with re.IGNORECASE,
// RE2's syntax standing in for Python's (#589), and false for a pattern
// that does not compile.
func matcher(pattern string) func(s string) bool {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return func(string) bool { return false }
	}
	return re.MatchString
}

// untypedOperators builds AND, OR and IN, which take operands of any type,
// so beanquery checks no signature for them.
var untypedOperators = map[bql.TokenType]func(l, r cexpr) cexpr{
	bql.AND: func(l, r cexpr) cexpr { return &cAnd{l: l, r: r} },
	bql.OR:  func(l, r cexpr) cexpr { return &cOr{l: l, r: r} },
	bql.IN:  func(l, r cexpr) cexpr { return &cIn{l: l, r: r} },
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

// cIn is Python's in, NULL when either operand is: membership of a set, or
// a case-sensitive substring of a string. A left operand that is not a
// string is never in either; in beanquery, 1 IN 'abc' is a TypeError
// (#589).
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
// the cast function it applies (types.MAP); each is NULL-strict. Python's
// parsing of decimals and dates accepts more than ours (#589).
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
		d, err := decimal.NewFromString(strings.TrimSpace(val))
		if err != nil {
			return nil
		}
		return d
	}
	if d, ok := asDecimal(v); ok {
		return d
	}
	return nil
}

// castDate is beanquery's date(): a date, or a string spelled YYYY-MM-DD
// as one, anything else NULL.
func castDate(v any) any {
	switch val := v.(type) {
	case *ast.Date:
		return val
	case string:
		t, err := time.Parse("2006-01-02", val)
		if err != nil {
			return nil
		}
		return &ast.Date{Time: t}
	}
	return nil
}
