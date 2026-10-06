package query

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// strValue is beanquery's str(): TRUE or FALSE for a boolean, and Python's
// str() otherwise. Rendering an untyped column prints Python's True.
func strValue(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "TRUE"
		}
		return "FALSE"
	}
	return objectString(v)
}

// castIntArg and castDecimalArg are beanquery's int() and decimal() of
// their one argument.
func castIntArg(_ *evalRow, args []any) any     { return castInt(args[0]) }
func castDecimalArg(_ *evalRow, args []any) any { return castDecimal(args[0]) }
func castDateArg(_ *evalRow, args []any) any    { return castDate(args[0]) }

// dateFromYMD is beanquery's date(year, month, day): Python's
// datetime.date, NULL for a year, month or day out of its range rather than
// normalized. Like Python, which reads each as a C int in turn, a number
// outside a C int's range fails the query.
func dateFromYMD(year, month, day int64) any {
	for _, n := range []int64{year, month, day} {
		if n > math.MaxInt32 {
			fail("signed integer is greater than maximum")
		}
		if n < math.MinInt32 {
			fail("signed integer is less than minimum")
		}
	}
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 {
		return nil
	}
	t := time.Date(int(year), time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
	if t.Day() != int(day) {
		return nil
	}
	return &ast.Date{Time: t}
}

// funcOverload is one typed signature of a simple function. tAny parameters
// match any argument type but the * of count(*), and tObject parameters an
// object-typed value or NULL; other parameters need that exact type, or
// one of its bases (matchOverload), so like bean-query an object-typed
// value or NULL only fits a tAny or tObject parameter.
type funcOverload struct {
	params []dtype
	result dtype
	call   func(row *evalRow, args []any) any
}

// funcDef is a simple function with one or more overloads, tried in order.
type funcDef struct {
	overloads []funcOverload
}

// matchOverload selects the overload for the argument types as
// beanquery's types.function_lookup does: for each combination of the
// argument types' bases (lookupSignature), the first overload that takes
// it. It returns the overload and the combination, the type each argument
// is taken as (asBase).
func (d *funcDef) matchOverload(argTypes []dtype) (*funcOverload, []dtype) {
	var overload *funcOverload
	sig := lookupSignature(argTypes, func(sig []dtype) bool {
		for i := range d.overloads {
			if o := &d.overloads[i]; o.accepts(sig) {
				overload = o
				return true
			}
		}
		return false
	})
	return overload, sig
}

// accepts reports whether the overload takes arguments of types argTypes.
func (o *funcOverload) accepts(argTypes []dtype) bool {
	if len(o.params) != len(argTypes) {
		return false
	}
	for j, param := range o.params {
		if !paramAccepts(param, argTypes[j]) {
			return false
		}
	}
	return true
}

// paramAccepts reports whether a parameter of type param takes an argument
// of type arg.
func paramAccepts(param, arg dtype) bool {
	switch param {
	case arg:
		return true
	case tAny:
		return arg != tAsterisk
	case tObject:
		return arg == tAny || arg == tNull
	}
	return false
}

// functions is the registry of simple functions, shared by the targets and
// filter environments, matching the official bean-query environment.
var functions = map[string]*funcDef{
	"abs": {overloads: []funcOverload{
		{[]dtype{tDecimal}, tDecimal, func(_ *evalRow, args []any) any {
			return args[0].(decimal.Decimal).Abs()
		}},
		{[]dtype{tPosition}, tPosition, func(_ *evalRow, args []any) any {
			p := args[0].(*positionValue)
			return &positionValue{Units: amountValue{Number: p.Units.Number.Abs(), Currency: p.Units.Currency}, Cost: p.Cost}
		}},
		{[]dtype{tInventory}, tInventory, func(_ *evalRow, args []any) any {
			inv := args[0].(*inventoryValue)
			result := newInventory()
			for _, p := range inv.Positions() {
				result.AddPosition(&positionValue{Units: amountValue{Number: p.Units.Number.Abs(), Currency: p.Units.Currency}, Cost: p.Cost})
			}
			return result
		}},
	}},

	"neg": {overloads: []funcOverload{
		{[]dtype{tDecimal}, tDecimal, func(_ *evalRow, args []any) any {
			return args[0].(decimal.Decimal).Neg()
		}},
		{[]dtype{tAmount}, tAmount, func(_ *evalRow, args []any) any {
			a := args[0].(*amountValue)
			return &amountValue{Number: a.Number.Neg(), Currency: a.Currency}
		}},
		{[]dtype{tPosition}, tPosition, func(_ *evalRow, args []any) any {
			p := args[0].(*positionValue)
			return &positionValue{Units: amountValue{Number: p.Units.Number.Neg(), Currency: p.Units.Currency}, Cost: p.Cost}
		}},
		{[]dtype{tInventory}, tInventory, func(_ *evalRow, args []any) any {
			return args[0].(*inventoryValue).Neg()
		}},
	}},

	// Date functions.
	"year": {overloads: []funcOverload{
		{[]dtype{tDate}, tInt, func(_ *evalRow, args []any) any {
			return int64(args[0].(*ast.Date).Year())
		}},
	}},
	"month": {overloads: []funcOverload{
		{[]dtype{tDate}, tInt, func(_ *evalRow, args []any) any {
			return int64(args[0].(*ast.Date).Month())
		}},
	}},
	"day": {overloads: []funcOverload{
		{[]dtype{tDate}, tInt, func(_ *evalRow, args []any) any {
			return int64(args[0].(*ast.Date).Day())
		}},
	}},
	"quarter": {overloads: []funcOverload{
		{[]dtype{tDate}, tString, func(_ *evalRow, args []any) any {
			d := args[0].(*ast.Date)
			return fmt.Sprintf("%04d-Q%d", d.Year(), (int(d.Month())+2)/3)
		}},
	}},
	"weekday": {overloads: []funcOverload{
		{[]dtype{tDate}, tString, func(_ *evalRow, args []any) any {
			return args[0].(*ast.Date).Format("Mon")
		}},
	}},
	"yearmonth": {overloads: []funcOverload{
		{[]dtype{tDate}, tDate, func(_ *evalRow, args []any) any {
			d := args[0].(*ast.Date)
			return &ast.Date{Time: time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)}
		}},
	}},
	"today": {overloads: []funcOverload{
		{[]dtype{}, tDate, func(_ *evalRow, _ []any) any {
			now := time.Now()
			return &ast.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)}
		}},
	}},
	"date": {overloads: []funcOverload{
		{[]dtype{tInt, tInt, tInt}, tDate, func(_ *evalRow, args []any) any {
			return dateFromYMD(args[0].(int64), args[1].(int64), args[2].(int64))
		}},
		{[]dtype{tDate}, tDate, castDateArg},
		{[]dtype{tString}, tDate, castDateArg},
		{[]dtype{tObject}, tDate, castDateArg},
	}},
	"date_trunc": {overloads: []funcOverload{
		{[]dtype{tString, tDate}, tDate, func(_ *evalRow, args []any) any {
			return dateTrunc(args[0].(string), args[1].(*ast.Date))
		}},
	}},
	"date_part": {overloads: []funcOverload{
		{[]dtype{tString, tDate}, tInt, func(_ *evalRow, args []any) any {
			return datePart(args[0].(string), args[1].(*ast.Date))
		}},
	}},
	"interval": {overloads: []funcOverload{
		{[]dtype{tString}, tInterval, func(_ *evalRow, args []any) any {
			return parseInterval(args[0].(string))
		}},
	}},
	"date_bin": {overloads: []funcOverload{
		{[]dtype{tInterval, tDate, tDate}, tDate, func(_ *evalRow, args []any) any {
			return dateBin(args[0].(*intervalValue), args[1].(*ast.Date), args[2].(*ast.Date))
		}},
		{[]dtype{tString, tDate, tDate}, tDate, func(_ *evalRow, args []any) any {
			stride, ok := parseInterval(args[0].(string)).(*intervalValue)
			if !ok {
				// beanquery bins by the None interval() gives.
				fail("'NoneType' object has no attribute 'months'")
			}
			return dateBin(stride, args[1].(*ast.Date), args[2].(*ast.Date))
		}},
	}},
	"parse_date": {overloads: []funcOverload{
		{[]dtype{tString}, tDate, func(_ *evalRow, args []any) any {
			return parseDate(args[0].(string))
		}},
		{[]dtype{tString, tString}, tDate, func(_ *evalRow, args []any) any {
			return strptime(args[0].(string), args[1].(string))
		}},
	}},
	"date_add": {overloads: []funcOverload{
		{[]dtype{tDate, tInt}, tDate, func(_ *evalRow, args []any) any {
			d := args[0].(*ast.Date)
			return &ast.Date{Time: d.AddDate(0, 0, int(args[1].(int64)))}
		}},
	}},
	"date_diff": {overloads: []funcOverload{
		{[]dtype{tDate, tDate}, tInt, func(_ *evalRow, args []any) any {
			return daysBetween(args[1].(*ast.Date), args[0].(*ast.Date))
		}},
	}},

	// Account functions.
	"parent": {overloads: []funcOverload{
		{[]dtype{tString}, tString, func(_ *evalRow, args []any) any {
			account := args[0].(string)
			if idx := strings.LastIndex(account, ":"); idx >= 0 {
				return account[:idx]
			}
			return ""
		}},
	}},
	"leaf": {overloads: []funcOverload{
		{[]dtype{tString}, tString, func(_ *evalRow, args []any) any {
			account := args[0].(string)
			if idx := strings.LastIndex(account, ":"); idx >= 0 {
				return account[idx+1:]
			}
			return account
		}},
	}},
	"root": {overloads: []funcOverload{
		{[]dtype{tString}, tString, func(_ *evalRow, args []any) any {
			return strings.SplitN(args[0].(string), ":", 2)[0]
		}},
		{[]dtype{tString, tInt}, tString, func(_ *evalRow, args []any) any {
			parts := strings.Split(args[0].(string), ":")
			n := min(int(args[1].(int64)), len(parts))
			return strings.Join(parts[:n], ":")
		}},
	}},
	"account_sortkey": {overloads: []funcOverload{
		{[]dtype{tString}, tString, func(row *evalRow, args []any) any {
			return accountSortKey(row.Ctx, args[0].(string))
		}},
	}},
	"open_date": {overloads: []funcOverload{
		{[]dtype{tString}, tDate, func(row *evalRow, args []any) any {
			if account, ok := row.Ctx.Ledger.GetAccount(args[0].(string)); ok && account.OpenDate != nil {
				return account.OpenDate
			}
			return nil
		}},
	}},
	"close_date": {overloads: []funcOverload{
		{[]dtype{tString}, tDate, func(row *evalRow, args []any) any {
			if account, ok := row.Ctx.Ledger.GetAccount(args[0].(string)); ok && account.CloseDate != nil {
				return account.CloseDate
			}
			return nil
		}},
	}},
	// Like beanquery's, a stub that only types the call, which compiles
	// as columnRewrites rewrites it.
	"has_account": {overloads: []funcOverload{{params: []dtype{tString}, result: tBool}}},

	// Amount, position, and inventory functions.
	"number": {overloads: []funcOverload{
		{[]dtype{tAmount}, tDecimal, func(_ *evalRow, args []any) any {
			return args[0].(*amountValue).Number
		}},
	}},
	"currency": {overloads: []funcOverload{
		{[]dtype{tAmount}, tString, func(_ *evalRow, args []any) any {
			return args[0].(*amountValue).Currency
		}},
	}},
	"commodity": {overloads: []funcOverload{
		{[]dtype{tAmount}, tString, func(_ *evalRow, args []any) any {
			return args[0].(*amountValue).Currency
		}},
	}},
	"units": {overloads: []funcOverload{
		{[]dtype{tPosition}, tAmount, func(_ *evalRow, args []any) any {
			p := args[0].(*positionValue)
			return &amountValue{Number: p.Units.Number, Currency: p.Units.Currency}
		}},
		{[]dtype{tInventory}, tInventory, func(_ *evalRow, args []any) any {
			inv := args[0].(*inventoryValue)
			result := newInventory()
			for _, p := range inv.Positions() {
				result.AddAmount(&amountValue{Number: p.Units.Number, Currency: p.Units.Currency})
			}
			return result
		}},
	}},
	"cost": {overloads: []funcOverload{
		{[]dtype{tPosition}, tAmount, func(_ *evalRow, args []any) any {
			return positionCost(args[0].(*positionValue))
		}},
		{[]dtype{tInventory}, tInventory, func(_ *evalRow, args []any) any {
			inv := args[0].(*inventoryValue)
			result := newInventory()
			for _, p := range inv.Positions() {
				result.AddAmount(positionCost(p))
			}
			return result
		}},
	}},
	"only": {overloads: []funcOverload{
		{[]dtype{tString, tInventory}, tAmount, func(_ *evalRow, args []any) any {
			currency := args[0].(string)
			total := decimal.Decimal{}
			for _, p := range args[1].(*inventoryValue).Positions() {
				if p.Units.Currency == currency {
					total = pydecimal.Add(total, p.Units.Number)
				}
			}
			return &amountValue{Number: total, Currency: currency}
		}},
	}},
	"filter_currency": {overloads: []funcOverload{
		{[]dtype{tPosition, tString}, tPosition, func(_ *evalRow, args []any) any {
			p := args[0].(*positionValue)
			if p.Units.Currency == args[1].(string) {
				return p
			}
			return nil
		}},
		{[]dtype{tInventory, tString}, tInventory, func(_ *evalRow, args []any) any {
			currency := args[1].(string)
			result := newInventory()
			for _, p := range args[0].(*inventoryValue).Positions() {
				if p.Units.Currency == currency {
					result.AddPosition(p)
				}
			}
			return result
		}},
	}},
	"getprice": {overloads: []funcOverload{
		{[]dtype{tString, tString}, tDecimal, func(row *evalRow, args []any) any {
			return getPrice(row, args[0].(string), args[1].(string), nil)
		}},
		{[]dtype{tString, tString, tDate}, tDecimal, func(row *evalRow, args []any) any {
			return getPrice(row, args[0].(string), args[1].(string), args[2].(*ast.Date))
		}},
	}},
	"convert": {overloads: []funcOverload{
		{[]dtype{tAmount, tString}, tAmount, func(row *evalRow, args []any) any {
			return convertAmount(row, args[0].(*amountValue), args[1].(string), nil)
		}},
		{[]dtype{tAmount, tString, tDate}, tAmount, func(row *evalRow, args []any) any {
			return convertAmount(row, args[0].(*amountValue), args[1].(string), args[2].(*ast.Date))
		}},
		{[]dtype{tPosition, tString}, tAmount, func(row *evalRow, args []any) any {
			p := args[0].(*positionValue)
			return convertPosition(row, p, args[1].(string), nil)
		}},
		{[]dtype{tPosition, tString, tDate}, tAmount, func(row *evalRow, args []any) any {
			p := args[0].(*positionValue)
			return convertPosition(row, p, args[1].(string), args[2].(*ast.Date))
		}},
		{[]dtype{tInventory, tString}, tInventory, func(row *evalRow, args []any) any {
			return convertInventory(row, args[0].(*inventoryValue), args[1].(string), nil)
		}},
		{[]dtype{tInventory, tString, tDate}, tInventory, func(row *evalRow, args []any) any {
			return convertInventory(row, args[0].(*inventoryValue), args[1].(string), args[2].(*ast.Date))
		}},
	}},
	"value": {overloads: []funcOverload{
		{[]dtype{tPosition}, tAmount, func(row *evalRow, args []any) any {
			return marketValue(row, args[0].(*positionValue), nil)
		}},
		{[]dtype{tPosition, tDate}, tAmount, func(row *evalRow, args []any) any {
			return marketValue(row, args[0].(*positionValue), args[1].(*ast.Date))
		}},
		{[]dtype{tInventory}, tInventory, func(row *evalRow, args []any) any {
			return inventoryMarketValue(row, args[0].(*inventoryValue), nil)
		}},
		{[]dtype{tInventory, tDate}, tInventory, func(row *evalRow, args []any) any {
			return inventoryMarketValue(row, args[0].(*inventoryValue), args[1].(*ast.Date))
		}},
	}},
	"possign": {overloads: []funcOverload{
		{[]dtype{tDecimal, tString}, tDecimal, func(row *evalRow, args []any) any {
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return args[0].(decimal.Decimal).Neg()
			}
			return args[0]
		}},
		{[]dtype{tAmount, tString}, tAmount, func(row *evalRow, args []any) any {
			a := args[0].(*amountValue)
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return &amountValue{Number: a.Number.Neg(), Currency: a.Currency}
			}
			return a
		}},
		{[]dtype{tPosition, tString}, tPosition, func(row *evalRow, args []any) any {
			p := args[0].(*positionValue)
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return &positionValue{Units: amountValue{Number: p.Units.Number.Neg(), Currency: p.Units.Currency}, Cost: p.Cost}
			}
			return p
		}},
		{[]dtype{tInventory, tString}, tInventory, func(row *evalRow, args []any) any {
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return args[0].(*inventoryValue).Neg()
			}
			return args[0]
		}},
	}},
	"safediv": {overloads: []funcOverload{
		{[]dtype{tDecimal, tDecimal}, tDecimal, func(_ *evalRow, args []any) any {
			return safeDiv(args[0].(decimal.Decimal), args[1].(decimal.Decimal))
		}},
		{[]dtype{tDecimal, tInt}, tDecimal, func(_ *evalRow, args []any) any {
			return safeDiv(args[0].(decimal.Decimal), decimal.NewFromInt(args[1].(int64)))
		}},
	}},

	// String functions.
	"str": {overloads: []funcOverload{
		{[]dtype{tAny}, tString, func(_ *evalRow, args []any) any {
			return strValue(args[0])
		}},
	}},
	"repr": {overloads: []funcOverload{
		{[]dtype{tAny}, tString, func(_ *evalRow, args []any) any {
			return reprValue(args[0])
		}},
	}},
	"int": {overloads: []funcOverload{
		{[]dtype{tInt}, tInt, castIntArg},
		{[]dtype{tBool}, tInt, castIntArg},
		{[]dtype{tDecimal}, tInt, castIntArg},
		{[]dtype{tString}, tInt, castIntArg},
		{[]dtype{tObject}, tInt, castIntArg},
	}},
	"decimal": {overloads: []funcOverload{
		{[]dtype{tDecimal}, tDecimal, castDecimalArg},
		{[]dtype{tInt}, tDecimal, castDecimalArg},
		{[]dtype{tBool}, tDecimal, castDecimalArg},
		{[]dtype{tString}, tDecimal, castDecimalArg},
		{[]dtype{tObject}, tDecimal, castDecimalArg},
	}},
	"bool": {overloads: []funcOverload{
		{[]dtype{tAny}, tBool, func(_ *evalRow, args []any) any {
			return castBool(args[0])
		}},
	}},
	"round": {overloads: []funcOverload{
		{[]dtype{tDecimal}, tDecimal, func(_ *evalRow, args []any) any {
			return roundDecimal(args[0].(decimal.Decimal), 0)
		}},
		{[]dtype{tDecimal, tInt}, tDecimal, func(_ *evalRow, args []any) any {
			return roundDecimal(args[0].(decimal.Decimal), args[1].(int64))
		}},
		{[]dtype{tInt}, tInt, func(_ *evalRow, args []any) any {
			return args[0].(int64)
		}},
		{[]dtype{tInt, tInt}, tInt, func(_ *evalRow, args []any) any {
			return roundInt(args[0].(int64), args[1].(int64))
		}},
	}},
	"substr": {overloads: []funcOverload{
		{[]dtype{tString, tInt, tInt}, tString, func(_ *evalRow, args []any) any {
			return substr(args[0].(string), args[1].(int64), args[2].(int64))
		}},
	}},
	"splitcomp": {overloads: []funcOverload{
		{[]dtype{tString, tString, tInt}, tString, func(_ *evalRow, args []any) any {
			return splitComponent(args[0].(string), args[1].(string), args[2].(int64))
		}},
	}},
	"empty": {overloads: []funcOverload{
		{[]dtype{tInventory}, tBool, func(_ *evalRow, args []any) any {
			return args[0].(*inventoryValue).IsEmpty()
		}},
	}},
	"length": {overloads: []funcOverload{
		{[]dtype{tString}, tInt, func(_ *evalRow, args []any) any {
			// Python's len() counts code points.
			return int64(utf8.RuneCountInString(args[0].(string)))
		}},
		{[]dtype{tSet}, tInt, func(_ *evalRow, args []any) any {
			elems, _ := stringElements(args[0])
			return int64(len(elems))
		}},
		{[]dtype{tList}, tInt, func(_ *evalRow, args []any) any {
			return int64(len(args[0].(listValue)))
		}},
	}},
	"maxwidth": {overloads: []funcOverload{
		{[]dtype{tString, tInt}, tString, func(_ *evalRow, args []any) any {
			return shorten(args[0].(string), int(args[1].(int64)))
		}},
	}},
	"upper": {overloads: []funcOverload{
		{[]dtype{tString}, tString, func(_ *evalRow, args []any) any {
			return strings.ToUpper(args[0].(string))
		}},
	}},
	"lower": {overloads: []funcOverload{
		{[]dtype{tString}, tString, func(_ *evalRow, args []any) any {
			return strings.ToLower(args[0].(string))
		}},
	}},
	"grep": {overloads: []funcOverload{
		{[]dtype{tString, tString}, tString, func(_ *evalRow, args []any) any {
			re := mustCompilePattern("", args[0].(string))
			if match := re.FindString(args[1].(string)); match != "" {
				return match
			}
			return nil
		}},
	}},
	"grepn": {overloads: []funcOverload{
		{[]dtype{tString, tString, tInt}, tString, func(_ *evalRow, args []any) any {
			re := mustCompilePattern("", args[0].(string))
			s := args[1].(string)
			loc := re.FindStringSubmatchIndex(s)
			if loc == nil {
				return nil
			}
			// Like Python's match.group(n), a group the pattern does not
			// have fails the statement, while one that did not take part
			// in the match is NULL.
			n := args[2].(int64)
			if n < 0 || n >= int64(len(loc)/2) {
				fail("no such group")
			}
			if loc[2*n] < 0 {
				return nil
			}
			return s[loc[2*n]:loc[2*n+1]]
		}},
	}},
	"subst": {overloads: []funcOverload{
		{[]dtype{tString, tString, tString}, tString, func(_ *evalRow, args []any) any {
			re := mustCompilePattern("", args[0].(string))
			return pySub(re, args[1].(string), args[2].(string))
		}},
	}},
	"findfirst": {overloads: []funcOverload{
		{[]dtype{tString, tSet}, tString, func(_ *evalRow, args []any) any {
			re := mustCompilePattern("", args[0].(string))
			elems, _ := stringElements(args[1])
			for _, elem := range elems {
				// beanquery uses re.match, which anchors the whole pattern
				// at the start: the leftmost match starts there exactly
				// when one can.
				if loc := re.FindStringIndex(elem); loc != nil && loc[0] == 0 {
					return elem
				}
			}
			return nil
		}},
	}},
	"joinstr": {overloads: []funcOverload{
		{[]dtype{tSet}, tString, func(_ *evalRow, args []any) any {
			elems, _ := stringElements(args[0])
			return strings.Join(elems, ",")
		}},
	}},

	// Metadata functions. Like beanquery's, stubs that only type the
	// call, which compiles as columnRewrites rewrites it.
	"meta":       {overloads: []funcOverload{{params: []dtype{tString}, result: tAny}}},
	"entry_meta": {overloads: []funcOverload{{params: []dtype{tString}, result: tAny}}},
	"any_meta":   {overloads: []funcOverload{{params: []dtype{tString}, result: tAny}}},
}

// ledgerPosition returns p as the ledger values it.
func ledgerPosition(p *positionValue) ledger.Position {
	position := ledger.Position{Number: p.Units.Number, Currency: p.Units.Currency}
	if p.Cost != nil {
		position.Cost = &ledger.BookedCost{Number: p.Cost.Number, Currency: p.Cost.Currency, Date: p.Cost.Date, Label: p.Cost.Label}
	}
	return position
}

func fromLedgerAmount(a ledger.CurrencyAmount) *amountValue {
	return &amountValue{Number: a.Amount, Currency: a.Currency}
}

// positionCost returns a position's total cost as an amount, or its units
// when no cost basis is attached (ledger.Position.AtCost).
func positionCost(p *positionValue) *amountValue {
	return fromLedgerAmount(ledgerPosition(p).AtCost())
}

// priceDate defaults a missing conversion date to today, matching the
// official functions that value at the current date.
func priceDate(date *ast.Date) *ast.Date {
	if date != nil {
		return date
	}
	now := time.Now()
	return &ast.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)}
}

func getPrice(row *evalRow, from, to string, date *ast.Date) any {
	if rate, ok := priceLookup(row.Ctx, priceDate(date), from, to); ok {
		return rate
	}
	return nil
}

// valuer returns the ledger that values positions, or nil without one, when
// every position keeps its units.
func valuer(row *evalRow) *ledger.Ledger {
	if row.Ctx == nil {
		return nil
	}
	return row.Ctx.Ledger
}

// convertAmount converts an amount to the given currency, returning it
// unmodified when no conversion rate is available (ledger.ConvertAmount).
func convertAmount(row *evalRow, a *amountValue, currency string, date *ast.Date) any {
	l := valuer(row)
	if l == nil || a.Currency == currency {
		return a
	}
	return fromLedgerAmount(l.ConvertAmount(a.Number, a.Currency, currency, priceDate(date)))
}

// convertPosition converts a position like beancount's convert_position
// (ledger.Convert).
func convertPosition(row *evalRow, p *positionValue, currency string, date *ast.Date) any {
	l := valuer(row)
	if l == nil {
		return &p.Units
	}
	return fromLedgerAmount(l.Convert(ledgerPosition(p), currency, priceDate(date)))
}

func convertInventory(row *evalRow, inv *inventoryValue, currency string, date *ast.Date) any {
	result := newInventory()
	for _, p := range inv.Positions() {
		result.AddAmount(convertPosition(row, p, currency, date).(*amountValue))
	}
	return result
}

// marketValue converts a position to its cost currency at market value
// (ledger.MarketValue). Positions without a cost basis are returned as
// their units.
func marketValue(row *evalRow, p *positionValue, date *ast.Date) any {
	l := valuer(row)
	if l == nil {
		return &amountValue{Number: p.Units.Number, Currency: p.Units.Currency}
	}
	return fromLedgerAmount(l.MarketValue(ledgerPosition(p), priceDate(date)))
}

func inventoryMarketValue(row *evalRow, inv *inventoryValue, date *ast.Date) any {
	result := newInventory()
	for _, p := range inv.Positions() {
		result.AddAmount(marketValue(row, p, date).(*amountValue))
	}
	return result
}

// shorten emulates Python's textwrap.shorten, which the official maxwidth
// function uses: whitespace collapses, whole words are kept while they fit,
// and truncation appends the "[...]" placeholder.
func shorten(s string, width int) string {
	words := strings.Fields(s)
	collapsed := strings.Join(words, " ")
	if len(collapsed) <= width {
		return collapsed
	}
	const placeholder = " [...]"
	var out string
	for _, word := range words {
		candidate := out
		if candidate != "" {
			candidate += " "
		}
		candidate += word
		if len(candidate)+len(placeholder) > width {
			break
		}
		out = candidate
	}
	if out == "" {
		return strings.TrimSpace(placeholder)
	}
	return out + placeholder
}

func safeDiv(a, b decimal.Decimal) decimal.Decimal {
	if b.IsZero() {
		return decimal.Decimal{}
	}
	return pydecimal.Quo(a, b)
}

// accountOrder is the canonical account type ordering used for sort keys and
// sign correction.
var accountOrder = []ast.AccountType{
	ast.AccountTypeAssets,
	ast.AccountTypeLiabilities,
	ast.AccountTypeEquity,
	ast.AccountTypeIncome,
	ast.AccountTypeExpenses,
}

func accountType(ctx *Context, account string) (ast.AccountType, bool) {
	root := account
	if idx := strings.Index(account, ":"); idx >= 0 {
		root = account[:idx]
	}
	if ctx != nil && ctx.Config != nil {
		return ctx.Config.GetAccountTypeFromName(root)
	}
	return 0, false
}

// accountSortKey renders a sortable key placing accounts in canonical type
// order (Assets, Liabilities, Equity, Income, Expenses).
func accountSortKey(ctx *Context, account string) string {
	typ, ok := accountType(ctx, account)
	index := len(accountOrder)
	if ok {
		for i, t := range accountOrder {
			if t == typ {
				index = i
				break
			}
		}
	}
	return fmt.Sprintf("%d-%s", index, account)
}

// accountInvertsSign reports whether an account's usual balance is negative,
// for the possign function: like beancount's get_account_sign, that of every
// account but an asset or an expense, one under no account type included.
func accountInvertsSign(ctx *Context, account string) bool {
	typ, ok := accountType(ctx, account)
	return !ok || typ != ast.AccountTypeAssets && typ != ast.AccountTypeExpenses
}

func metaValue(v *ast.MetadataValue) any {
	if v == nil {
		return nil
	}
	switch {
	case v.StringValue != nil:
		return v.StringValue.String()
	case v.Date != nil:
		return v.Date
	case v.Account != nil:
		return string(*v.Account)
	case v.Currency != nil:
		return *v.Currency
	case v.Tag != nil:
		return string(*v.Tag)
	case v.Link != nil:
		return string(*v.Link)
	case v.Number != nil:
		if number, err := decimal.NewFromString(*v.Number); err == nil {
			return number
		}
		return nil
	case v.Amount != nil:
		number, err := ledger.ParseAmount(v.Amount)
		if err != nil {
			return nil
		}
		return &amountValue{Number: number, Currency: v.Amount.Currency}
	case v.Boolean != nil:
		return *v.Boolean
	}
	return nil
}
