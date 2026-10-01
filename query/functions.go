package query

import (
	"fmt"
	"regexp"
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

// funcOverload is one typed signature of a simple function. tAny parameters
// match any argument type but the * of count(*); other parameters need that
// exact type, so like bean-query an object-typed value or NULL only fits a
// tAny parameter.
type funcOverload struct {
	params []dtype
	result dtype
	call   func(row *evalRow, args []any) any
}

// funcDef is a simple function with one or more overloads, tried in order.
type funcDef struct {
	overloads []funcOverload
}

// matchOverload selects the first overload compatible with the argument
// types.
func (d *funcDef) matchOverload(argTypes []dtype) *funcOverload {
	for i := range d.overloads {
		o := &d.overloads[i]
		if len(o.params) != len(argTypes) {
			continue
		}
		ok := true
		for j, param := range o.params {
			if param != argTypes[j] && (param != tAny || argTypes[j] == tAsterisk) {
				ok = false
				break
			}
		}
		if ok {
			return o
		}
	}
	return nil
}

// functions is the registry of simple functions, shared by the targets and
// filter environments, matching the official bean-query environment.
var functions = map[string]*funcDef{
	"abs": {overloads: []funcOverload{
		{[]dtype{tDecimal}, tDecimal, func(_ *evalRow, args []any) any {
			return args[0].(decimal.Decimal).Abs()
		}},
		{[]dtype{tInt}, tInt, func(_ *evalRow, args []any) any {
			v := args[0].(int64)
			if v < 0 {
				return -v
			}
			return v
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
		{[]dtype{tInt}, tInt, func(_ *evalRow, args []any) any {
			return -args[0].(int64)
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
	"ymonth": {overloads: []funcOverload{
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
			return &ast.Date{Time: time.Date(int(args[0].(int64)), time.Month(args[1].(int64)), int(args[2].(int64)), 0, 0, 0, 0, time.UTC)}
		}},
		{[]dtype{tString}, tDate, func(_ *evalRow, args []any) any {
			date := &ast.Date{}
			if err := date.Capture([]string{args[0].(string)}); err != nil {
				return nil
			}
			return date
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
			a, b := args[0].(*ast.Date), args[1].(*ast.Date)
			return int64(a.Sub(b.Time).Hours() / 24)
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
	"has_account": {overloads: []funcOverload{
		// Like bean-query, a case-insensitive search through every
		// account the entry references, not only transaction postings.
		{[]dtype{tString}, tBool, func(row *evalRow, args []any) any {
			entry, ok := row.Entry.(ast.WithAccounts)
			if !ok {
				return false
			}
			re, err := regexp.Compile("(?i)" + args[0].(string))
			if err != nil {
				return false
			}
			for _, account := range entry.Accounts() {
				if re.MatchString(string(account)) {
					return true
				}
			}
			return false
		}},
	}},

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
	"length": {overloads: []funcOverload{
		{[]dtype{tString}, tInt, func(_ *evalRow, args []any) any {
			// Python's len() counts code points.
			return int64(utf8.RuneCountInString(args[0].(string)))
		}},
		{[]dtype{tSet}, tInt, func(_ *evalRow, args []any) any {
			return int64(len(args[0].(setValue)))
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
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return nil
			}
			if match := re.FindString(args[1].(string)); match != "" {
				return match
			}
			return nil
		}},
	}},
	"grepn": {overloads: []funcOverload{
		{[]dtype{tString, tString, tInt}, tString, func(_ *evalRow, args []any) any {
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return nil
			}
			groups := re.FindStringSubmatch(args[1].(string))
			n := int(args[2].(int64))
			if groups == nil || n < 0 || n >= len(groups) {
				return nil
			}
			return groups[n]
		}},
	}},
	"subst": {overloads: []funcOverload{
		{[]dtype{tString, tString, tString}, tString, func(_ *evalRow, args []any) any {
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return nil
			}
			return re.ReplaceAllString(args[2].(string), args[1].(string))
		}},
	}},
	"findfirst": {overloads: []funcOverload{
		{[]dtype{tString, tSet}, tString, func(_ *evalRow, args []any) any {
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return nil
			}
			for _, elem := range args[1].(setValue).Sorted() {
				if re.MatchString(elem) {
					return elem
				}
			}
			return nil
		}},
	}},
	"joinstr": {overloads: []funcOverload{
		{[]dtype{tSet}, tString, func(_ *evalRow, args []any) any {
			return strings.Join(args[0].(setValue).Sorted(), ",")
		}},
	}},

	// Metadata functions.
	"meta": {overloads: []funcOverload{
		{[]dtype{tString}, tAny, func(row *evalRow, args []any) any {
			if row.Posting == nil {
				return nil
			}
			return metaLookup(row.Posting.Metadata, args[0].(string))
		}},
	}},
	"entry_meta": {overloads: []funcOverload{
		{[]dtype{tString}, tAny, func(row *evalRow, args []any) any {
			if row.Txn == nil {
				return nil
			}
			return metaLookup(row.Txn.Metadata, args[0].(string))
		}},
	}},
	"any_meta": {overloads: []funcOverload{
		{[]dtype{tString}, tAny, func(row *evalRow, args []any) any {
			key := args[0].(string)
			if row.Posting != nil {
				if v := metaLookup(row.Posting.Metadata, key); v != nil {
					return v
				}
			}
			if row.Txn != nil {
				return metaLookup(row.Txn.Metadata, key)
			}
			return nil
		}},
	}},
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

// accountInvertsSign reports whether an account's usual balance is negative
// (liabilities, equity, income), for the possign function.
func accountInvertsSign(ctx *Context, account string) bool {
	typ, ok := accountType(ctx, account)
	if !ok {
		return false
	}
	switch typ {
	case ast.AccountTypeLiabilities, ast.AccountTypeEquity, ast.AccountTypeIncome:
		return true
	}
	return false
}

// metaLookup finds a metadata key and converts its value to a query value.
func metaLookup(metadata []*ast.Metadata, key string) any {
	for _, md := range metadata {
		if md.Key == key {
			return metaValue(md.Value)
		}
	}
	return nil
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
