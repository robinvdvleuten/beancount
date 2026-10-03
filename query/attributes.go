package query

import "github.com/robinvdvleuten/beancount/ast"

// attributeDef is an attribute of a structured type: its type and how to
// read it from a non-NULL value.
type attributeDef struct {
	typ dtype
	get func(v any) any
}

// structures maps each structured type to its attributes, as beanquery's
// types.Structure subclasses (Position, Amount, Cost, Transaction and
// date) list their columns.
var structures = map[dtype]map[string]attributeDef{
	tTransaction: transactionAttributes,
	tOpen:        openAttributes,
	tClose:       closeAttributes,
	tPosition: {
		"units": {tAmount, func(v any) any {
			units := v.(*positionValue).Units
			return &units
		}},
		"cost": {tCost, func(v any) any {
			if cost := v.(*positionValue).Cost; cost != nil {
				return cost
			}
			return nil
		}},
	},
	tAmount: {
		"number":   {tDecimal, func(v any) any { return v.(*amountValue).Number }},
		"currency": {tString, func(v any) any { return v.(*amountValue).Currency }},
	},
	tCost: {
		"number":   {tDecimal, func(v any) any { return v.(*costValue).Number }},
		"currency": {tString, func(v any) any { return v.(*costValue).Currency }},
		"date": {tDate, func(v any) any {
			if date := v.(*costValue).Date; date != nil {
				return date
			}
			return nil
		}},
		"label": {tString, func(v any) any {
			if label := v.(*costValue).Label; label != "" {
				return label
			}
			return nil
		}},
	},
	tDate: {
		"year":  {tInt, func(v any) any { return int64(v.(*ast.Date).Year()) }},
		"month": {tInt, func(v any) any { return int64(v.(*ast.Date).Month()) }},
		"day":   {tInt, func(v any) any { return int64(v.(*ast.Date).Day()) }},
	},
}

// cAttribute reads an attribute of a structured value, NULL for NULL, like
// beanquery's EvalGetter.
type cAttribute struct {
	x    cexpr
	attr attributeDef
}

func (c *cAttribute) typ() dtype { return c.attr.typ }

func (c *cAttribute) eval(row *evalRow) any {
	v := c.x.eval(row)
	if v == nil {
		return nil
	}
	return c.attr.get(v)
}

// cSubscript reads a key of a dict, NULL for NULL or a missing key, like
// beanquery's EvalGetItem.
type cSubscript struct {
	x   cexpr
	key string
}

func (c *cSubscript) typ() dtype { return tAny }

func (c *cSubscript) eval(row *evalRow) any {
	d, ok := c.x.eval(row).(*dictValue)
	if !ok {
		return nil
	}
	return d.get(c.key)
}

// cGetItem is beanquery's getitem(): the value of a key, a string
// expression, in a dict, NULL for a NULL dict. A missing key, or a NULL
// one, is NULL, or the value of fallback when there is one.
type cGetItem struct {
	dict, key, fallback cexpr
}

func (c *cGetItem) typ() dtype { return tAny }

func (c *cGetItem) eval(row *evalRow) any {
	d, ok := c.dict.eval(row).(*dictValue)
	if !ok {
		return nil
	}
	if key, ok := c.key.eval(row).(string); ok {
		if value, found := d.lookup(key); found {
			return value
		}
	}
	if c.fallback != nil {
		return c.fallback.eval(row)
	}
	return nil
}

// cHasAccount is has_account(re) as beanquery rewrites it, '(?i)' + re ?~
// any(accounts): whether any account matches the pattern, ignoring case.
// It is NULL for a NULL pattern or accounts.
type cHasAccount struct {
	pattern, accounts cexpr
}

func (c *cHasAccount) typ() dtype { return tBool }

func (c *cHasAccount) eval(row *evalRow) any {
	pattern, ok := c.pattern.eval(row).(string)
	if !ok {
		return nil
	}
	accounts, ok := c.accounts.eval(row).(setValue)
	if !ok {
		return nil
	}
	re := mustCompilePattern("(?i)", pattern)
	for account := range accounts {
		if re.MatchString(account) {
			return true
		}
	}
	return false
}
