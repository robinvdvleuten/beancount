package query

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// funcOverload is one typed signature of a simple function. TAny parameters
// match any argument type.
type funcOverload struct {
	params []DType
	result DType
	call   func(row *Row, args []any) any
}

// funcDef is a simple function with one or more overloads, tried in order.
type funcDef struct {
	overloads []funcOverload
}

// matchOverload selects the first overload compatible with the argument
// types. Arguments of unknown type (TAny) match any parameter.
func (d *funcDef) matchOverload(argTypes []DType) *funcOverload {
	for i := range d.overloads {
		o := &d.overloads[i]
		if len(o.params) != len(argTypes) {
			continue
		}
		ok := true
		for j, param := range o.params {
			if param != TAny && argTypes[j] != TAny && param != argTypes[j] {
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
		{[]DType{TDecimal}, TDecimal, func(_ *Row, args []any) any {
			return args[0].(decimal.Decimal).Abs()
		}},
		{[]DType{TInt}, TInt, func(_ *Row, args []any) any {
			v := args[0].(int64)
			if v < 0 {
				return -v
			}
			return v
		}},
		{[]DType{TPosition}, TPosition, func(_ *Row, args []any) any {
			p := args[0].(*Position)
			return &Position{Units: Amount{Number: p.Units.Number.Abs(), Currency: p.Units.Currency}, Cost: p.Cost}
		}},
		{[]DType{TInventory}, TInventory, func(_ *Row, args []any) any {
			inv := args[0].(*Inventory)
			result := NewInventory()
			for _, p := range inv.Positions() {
				result.AddPosition(&Position{Units: Amount{Number: p.Units.Number.Abs(), Currency: p.Units.Currency}, Cost: p.Cost})
			}
			return result
		}},
	}},

	"neg": {overloads: []funcOverload{
		{[]DType{TDecimal}, TDecimal, func(_ *Row, args []any) any {
			return args[0].(decimal.Decimal).Neg()
		}},
		{[]DType{TInt}, TInt, func(_ *Row, args []any) any {
			return -args[0].(int64)
		}},
		{[]DType{TAmount}, TAmount, func(_ *Row, args []any) any {
			a := args[0].(*Amount)
			return &Amount{Number: a.Number.Neg(), Currency: a.Currency}
		}},
		{[]DType{TPosition}, TPosition, func(_ *Row, args []any) any {
			p := args[0].(*Position)
			return &Position{Units: Amount{Number: p.Units.Number.Neg(), Currency: p.Units.Currency}, Cost: p.Cost}
		}},
		{[]DType{TInventory}, TInventory, func(_ *Row, args []any) any {
			return args[0].(*Inventory).Neg()
		}},
	}},

	// Date functions.
	"year": {overloads: []funcOverload{
		{[]DType{TDate}, TInt, func(_ *Row, args []any) any {
			return int64(args[0].(*ast.Date).Year())
		}},
	}},
	"month": {overloads: []funcOverload{
		{[]DType{TDate}, TInt, func(_ *Row, args []any) any {
			return int64(args[0].(*ast.Date).Month())
		}},
	}},
	"day": {overloads: []funcOverload{
		{[]DType{TDate}, TInt, func(_ *Row, args []any) any {
			return int64(args[0].(*ast.Date).Day())
		}},
	}},
	"quarter": {overloads: []funcOverload{
		{[]DType{TDate}, TString, func(_ *Row, args []any) any {
			d := args[0].(*ast.Date)
			return fmt.Sprintf("%04d-Q%d", d.Year(), (int(d.Month())+2)/3)
		}},
	}},
	"weekday": {overloads: []funcOverload{
		{[]DType{TDate}, TString, func(_ *Row, args []any) any {
			return args[0].(*ast.Date).Format("Mon")
		}},
	}},
	"ymonth": {overloads: []funcOverload{
		{[]DType{TDate}, TDate, func(_ *Row, args []any) any {
			d := args[0].(*ast.Date)
			return &ast.Date{Time: time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)}
		}},
	}},
	"today": {overloads: []funcOverload{
		{[]DType{}, TDate, func(_ *Row, _ []any) any {
			now := time.Now()
			return &ast.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)}
		}},
	}},
	"date": {overloads: []funcOverload{
		{[]DType{TInt, TInt, TInt}, TDate, func(_ *Row, args []any) any {
			return &ast.Date{Time: time.Date(int(args[0].(int64)), time.Month(args[1].(int64)), int(args[2].(int64)), 0, 0, 0, 0, time.UTC)}
		}},
		{[]DType{TString}, TDate, func(_ *Row, args []any) any {
			date := &ast.Date{}
			if err := date.Capture([]string{args[0].(string)}); err != nil {
				return nil
			}
			return date
		}},
	}},
	"date_add": {overloads: []funcOverload{
		{[]DType{TDate, TInt}, TDate, func(_ *Row, args []any) any {
			d := args[0].(*ast.Date)
			return &ast.Date{Time: d.AddDate(0, 0, int(args[1].(int64)))}
		}},
	}},
	"date_diff": {overloads: []funcOverload{
		{[]DType{TDate, TDate}, TInt, func(_ *Row, args []any) any {
			a, b := args[0].(*ast.Date), args[1].(*ast.Date)
			return int64(a.Sub(b.Time).Hours() / 24)
		}},
	}},

	// Account functions.
	"parent": {overloads: []funcOverload{
		{[]DType{TString}, TString, func(_ *Row, args []any) any {
			account := args[0].(string)
			if idx := strings.LastIndex(account, ":"); idx >= 0 {
				return account[:idx]
			}
			return ""
		}},
	}},
	"leaf": {overloads: []funcOverload{
		{[]DType{TString}, TString, func(_ *Row, args []any) any {
			account := args[0].(string)
			if idx := strings.LastIndex(account, ":"); idx >= 0 {
				return account[idx+1:]
			}
			return account
		}},
	}},
	"root": {overloads: []funcOverload{
		{[]DType{TString, TInt}, TString, func(_ *Row, args []any) any {
			parts := strings.Split(args[0].(string), ":")
			n := min(int(args[1].(int64)), len(parts))
			return strings.Join(parts[:n], ":")
		}},
	}},
	"account_sortkey": {overloads: []funcOverload{
		{[]DType{TString}, TString, func(row *Row, args []any) any {
			return accountSortKey(row.Ctx, args[0].(string))
		}},
	}},
	"open_date": {overloads: []funcOverload{
		{[]DType{TString}, TDate, func(row *Row, args []any) any {
			if account, ok := row.Ctx.Ledger.GetAccount(args[0].(string)); ok && account.OpenDate != nil {
				return account.OpenDate
			}
			return nil
		}},
	}},
	"close_date": {overloads: []funcOverload{
		{[]DType{TString}, TDate, func(row *Row, args []any) any {
			if account, ok := row.Ctx.Ledger.GetAccount(args[0].(string)); ok && account.CloseDate != nil {
				return account.CloseDate
			}
			return nil
		}},
	}},
	"has_account": {overloads: []funcOverload{
		{[]DType{TString}, TBool, func(row *Row, args []any) any {
			txn, ok := row.Entry.(*ast.Transaction)
			if !ok {
				return false
			}
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return false
			}
			for _, posting := range txn.Postings {
				if re.MatchString(string(posting.Account)) {
					return true
				}
			}
			return false
		}},
	}},

	// Amount, position, and inventory functions.
	"number": {overloads: []funcOverload{
		{[]DType{TAmount}, TDecimal, func(_ *Row, args []any) any {
			return args[0].(*Amount).Number
		}},
	}},
	"currency": {overloads: []funcOverload{
		{[]DType{TAmount}, TString, func(_ *Row, args []any) any {
			return args[0].(*Amount).Currency
		}},
	}},
	"commodity": {overloads: []funcOverload{
		{[]DType{TAmount}, TString, func(_ *Row, args []any) any {
			return args[0].(*Amount).Currency
		}},
	}},
	"units": {overloads: []funcOverload{
		{[]DType{TPosition}, TAmount, func(_ *Row, args []any) any {
			p := args[0].(*Position)
			return &Amount{Number: p.Units.Number, Currency: p.Units.Currency}
		}},
		{[]DType{TInventory}, TInventory, func(_ *Row, args []any) any {
			inv := args[0].(*Inventory)
			result := NewInventory()
			for _, p := range inv.Positions() {
				result.AddAmount(&Amount{Number: p.Units.Number, Currency: p.Units.Currency})
			}
			return result
		}},
	}},
	"cost": {overloads: []funcOverload{
		{[]DType{TPosition}, TAmount, func(_ *Row, args []any) any {
			return positionCost(args[0].(*Position))
		}},
		{[]DType{TInventory}, TInventory, func(_ *Row, args []any) any {
			inv := args[0].(*Inventory)
			result := NewInventory()
			for _, p := range inv.Positions() {
				result.AddAmount(positionCost(p))
			}
			return result
		}},
	}},
	"only": {overloads: []funcOverload{
		{[]DType{TString, TInventory}, TAmount, func(_ *Row, args []any) any {
			currency := args[0].(string)
			total := decimal.Decimal{}
			for _, p := range args[1].(*Inventory).Positions() {
				if p.Units.Currency == currency {
					total = total.Add(p.Units.Number)
				}
			}
			return &Amount{Number: total, Currency: currency}
		}},
	}},
	"filter_currency": {overloads: []funcOverload{
		{[]DType{TPosition, TString}, TPosition, func(_ *Row, args []any) any {
			p := args[0].(*Position)
			if p.Units.Currency == args[1].(string) {
				return p
			}
			return nil
		}},
		{[]DType{TInventory, TString}, TInventory, func(_ *Row, args []any) any {
			currency := args[1].(string)
			result := NewInventory()
			for _, p := range args[0].(*Inventory).Positions() {
				if p.Units.Currency == currency {
					result.AddPosition(p)
				}
			}
			return result
		}},
	}},
	"getprice": {overloads: []funcOverload{
		{[]DType{TString, TString}, TDecimal, func(row *Row, args []any) any {
			return getPrice(row, args[0].(string), args[1].(string), nil)
		}},
		{[]DType{TString, TString, TDate}, TDecimal, func(row *Row, args []any) any {
			return getPrice(row, args[0].(string), args[1].(string), args[2].(*ast.Date))
		}},
	}},
	"convert": {overloads: []funcOverload{
		{[]DType{TAmount, TString}, TAmount, func(row *Row, args []any) any {
			return convertAmount(row, args[0].(*Amount), args[1].(string), nil)
		}},
		{[]DType{TAmount, TString, TDate}, TAmount, func(row *Row, args []any) any {
			return convertAmount(row, args[0].(*Amount), args[1].(string), args[2].(*ast.Date))
		}},
		{[]DType{TPosition, TString}, TAmount, func(row *Row, args []any) any {
			p := args[0].(*Position)
			return convertAmount(row, &p.Units, args[1].(string), nil)
		}},
		{[]DType{TPosition, TString, TDate}, TAmount, func(row *Row, args []any) any {
			p := args[0].(*Position)
			return convertAmount(row, &p.Units, args[1].(string), args[2].(*ast.Date))
		}},
		{[]DType{TInventory, TString}, TInventory, func(row *Row, args []any) any {
			return convertInventory(row, args[0].(*Inventory), args[1].(string), nil)
		}},
		{[]DType{TInventory, TString, TDate}, TInventory, func(row *Row, args []any) any {
			return convertInventory(row, args[0].(*Inventory), args[1].(string), args[2].(*ast.Date))
		}},
	}},
	"value": {overloads: []funcOverload{
		{[]DType{TPosition}, TAmount, func(row *Row, args []any) any {
			return positionValue(row, args[0].(*Position), nil)
		}},
		{[]DType{TPosition, TDate}, TAmount, func(row *Row, args []any) any {
			return positionValue(row, args[0].(*Position), args[1].(*ast.Date))
		}},
		{[]DType{TInventory}, TInventory, func(row *Row, args []any) any {
			return inventoryValue(row, args[0].(*Inventory), nil)
		}},
		{[]DType{TInventory, TDate}, TInventory, func(row *Row, args []any) any {
			return inventoryValue(row, args[0].(*Inventory), args[1].(*ast.Date))
		}},
	}},
	"possign": {overloads: []funcOverload{
		{[]DType{TDecimal, TString}, TDecimal, func(row *Row, args []any) any {
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return args[0].(decimal.Decimal).Neg()
			}
			return args[0]
		}},
		{[]DType{TAmount, TString}, TAmount, func(row *Row, args []any) any {
			a := args[0].(*Amount)
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return &Amount{Number: a.Number.Neg(), Currency: a.Currency}
			}
			return a
		}},
		{[]DType{TPosition, TString}, TPosition, func(row *Row, args []any) any {
			p := args[0].(*Position)
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return &Position{Units: Amount{Number: p.Units.Number.Neg(), Currency: p.Units.Currency}, Cost: p.Cost}
			}
			return p
		}},
		{[]DType{TInventory, TString}, TInventory, func(row *Row, args []any) any {
			if accountInvertsSign(row.Ctx, args[1].(string)) {
				return args[0].(*Inventory).Neg()
			}
			return args[0]
		}},
	}},
	"safediv": {overloads: []funcOverload{
		{[]DType{TDecimal, TDecimal}, TDecimal, func(_ *Row, args []any) any {
			return safeDiv(args[0].(decimal.Decimal), args[1].(decimal.Decimal))
		}},
		{[]DType{TDecimal, TInt}, TDecimal, func(_ *Row, args []any) any {
			return safeDiv(args[0].(decimal.Decimal), decimal.NewFromInt(args[1].(int64)))
		}},
	}},

	// String functions.
	"str": {overloads: []funcOverload{
		{[]DType{TAny}, TString, func(_ *Row, args []any) any {
			return valueString(args[0])
		}},
	}},
	"length": {overloads: []funcOverload{
		{[]DType{TString}, TInt, func(_ *Row, args []any) any {
			return int64(len(args[0].(string)))
		}},
		{[]DType{TSet}, TInt, func(_ *Row, args []any) any {
			return int64(len(args[0].(Set)))
		}},
	}},
	"maxwidth": {overloads: []funcOverload{
		{[]DType{TString, TInt}, TString, func(_ *Row, args []any) any {
			return shorten(args[0].(string), int(args[1].(int64)))
		}},
	}},
	"upper": {overloads: []funcOverload{
		{[]DType{TString}, TString, func(_ *Row, args []any) any {
			return strings.ToUpper(args[0].(string))
		}},
	}},
	"lower": {overloads: []funcOverload{
		{[]DType{TString}, TString, func(_ *Row, args []any) any {
			return strings.ToLower(args[0].(string))
		}},
	}},
	"grep": {overloads: []funcOverload{
		{[]DType{TString, TString}, TString, func(_ *Row, args []any) any {
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
		{[]DType{TString, TString, TInt}, TString, func(_ *Row, args []any) any {
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
		{[]DType{TString, TString, TString}, TString, func(_ *Row, args []any) any {
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return nil
			}
			return re.ReplaceAllString(args[2].(string), args[1].(string))
		}},
	}},
	"findfirst": {overloads: []funcOverload{
		{[]DType{TString, TSet}, TString, func(_ *Row, args []any) any {
			re, err := regexp.Compile(args[0].(string))
			if err != nil {
				return nil
			}
			for _, elem := range args[1].(Set).Sorted() {
				if re.MatchString(elem) {
					return elem
				}
			}
			return nil
		}},
	}},
	"joinstr": {overloads: []funcOverload{
		{[]DType{TSet}, TString, func(_ *Row, args []any) any {
			return strings.Join(args[0].(Set).Sorted(), ",")
		}},
	}},
	"coalesce": {overloads: []funcOverload{
		{[]DType{TAny, TAny}, TAny, func(_ *Row, args []any) any {
			if args[0] != nil {
				return args[0]
			}
			return args[1]
		}},
	}},

	// Metadata functions.
	"meta": {overloads: []funcOverload{
		{[]DType{TString}, TAny, func(row *Row, args []any) any {
			if row.Posting == nil {
				return nil
			}
			return metaLookup(row.Posting.Metadata, args[0].(string))
		}},
	}},
	"entry_meta": {overloads: []funcOverload{
		{[]DType{TString}, TAny, func(row *Row, args []any) any {
			if row.Txn == nil {
				return nil
			}
			return metaLookup(row.Txn.Metadata, args[0].(string))
		}},
	}},
	"any_meta": {overloads: []funcOverload{
		{[]DType{TString}, TAny, func(row *Row, args []any) any {
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

// positionCost returns a position's total cost as an amount, or its units
// when no cost basis is attached.
func positionCost(p *Position) *Amount {
	if p.Cost == nil {
		return &Amount{Number: p.Units.Number, Currency: p.Units.Currency}
	}
	return &Amount{Number: p.Units.Number.Mul(p.Cost.Number), Currency: p.Cost.Currency}
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

func getPrice(row *Row, from, to string, date *ast.Date) any {
	if rate, ok := priceLookup(row.Ctx, priceDate(date), from, to); ok {
		return rate
	}
	return nil
}

// convertAmount converts an amount to the given currency, returning it
// unmodified when no conversion rate is available (official behavior).
func convertAmount(row *Row, a *Amount, currency string, date *ast.Date) any {
	if a.Currency == currency {
		return a
	}
	if rate, ok := priceLookup(row.Ctx, priceDate(date), a.Currency, currency); ok {
		return &Amount{Number: a.Number.Mul(rate), Currency: currency}
	}
	return a
}

func convertInventory(row *Row, inv *Inventory, currency string, date *ast.Date) any {
	result := NewInventory()
	for _, p := range inv.Positions() {
		converted := convertAmount(row, &p.Units, currency, date).(*Amount)
		result.AddAmount(converted)
	}
	return result
}

// positionValue converts a position to its cost currency at market value.
// Positions without a cost basis are returned as their units.
func positionValue(row *Row, p *Position, date *ast.Date) any {
	if p.Cost == nil {
		return &Amount{Number: p.Units.Number, Currency: p.Units.Currency}
	}
	return convertAmount(row, &p.Units, p.Cost.Currency, date)
}

func inventoryValue(row *Row, inv *Inventory, date *ast.Date) any {
	result := NewInventory()
	for _, p := range inv.Positions() {
		result.AddAmount(positionValue(row, p, date).(*Amount))
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
	return a.Div(b)
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
		return &Amount{Number: number, Currency: v.Amount.Currency}
	case v.Boolean != nil:
		return *v.Boolean
	}
	return nil
}
