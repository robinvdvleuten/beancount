package query

import (
	"fmt"
	"slices"
)

// pyType is a Python type bean-query checks a function argument against,
// with the query types that pass its issubclass() check.
type pyType struct {
	repr    string
	accepts []dtype // nil accepts every type, like object
}

func (t pyType) admits(arg dtype) bool {
	return t.accepts == nil || slices.Contains(t.accepts, arg)
}

var (
	pyObject    = pyType{repr: "<class 'object'>"}
	pyStr       = pyType{"<class 'str'>", []dtype{tString}}
	pyInt       = pyType{"<class 'int'>", []dtype{tInt, tBool}} // bool subclasses int
	pyDate      = pyType{"<class 'datetime.date'>", []dtype{tDate}}
	pySet       = pyType{"<class 'set'>", []dtype{tSet}}
	pyDict      = pyType{"<class 'dict'>", []dtype{}}
	pyAmount    = pyType{"<class 'beancount.core.amount.Amount'>", []dtype{tAmount}}
	pyInventory = pyType{"<class 'beancount.core.inventory.Inventory'>", []dtype{tInventory}}
	pyNumber    = pyType{"(<class 'int'>, <class 'float'>, <class 'decimal.Decimal'>)", []dtype{tInt, tBool, tDecimal}}
	pySized     = pyType{"(<class 'list'>, <class 'set'>, <class 'str'>)", []dtype{tSet, tString}}
)

// fallbackClass is the class bean-query instantiates when no signature of a
// function matches its argument types: it looks the function up by name
// alone and lets the class's constructor reject the arguments.
type fallbackClass struct {
	name   string
	params []pyType
}

// fallbackClasses lists bean-query's by-name registrations (query_env.py).
// Functions without one fail as "Invalid function" instead.
var fallbackClasses = map[string]*fallbackClass{
	"account_sortkey": {"AccountSortKey", []pyType{pyStr}},
	"any_meta":        {"AnyMeta", []pyType{pyStr}},
	"close_date":      {"CloseDate", []pyType{pyStr}},
	"coalesce":        {"Coalesce", []pyType{pyObject, pyObject}},
	"commodity":       {"Currency", []pyType{pyAmount}},
	"commodity_meta":  {"CurrencyMeta", []pyType{pyStr}},
	"currency":        {"Currency", []pyType{pyAmount}},
	"currency_meta":   {"CurrencyMeta", []pyType{pyStr}},
	"date_add":        {"DateAdd", []pyType{pyDate, pyInt}},
	"date_diff":       {"DateDiff", []pyType{pyDate, pyDate}},
	"day":             {"Day", []pyType{pyDate}},
	"entry_meta":      {"EntryMeta", []pyType{pyStr}},
	"findfirst":       {"FindFirst", []pyType{pyStr, pySet}},
	"getitem":         {"GetItemStr", []pyType{pyDict, pyStr}},
	"grep":            {"Grep", []pyType{pyStr, pyStr}},
	"grepn":           {"GrepN", []pyType{pyStr, pyStr, pyInt}},
	"has_account":     {"MatchAccount", []pyType{pyStr}},
	"joinstr":         {"JoinStr", []pyType{pySet}},
	"leaf":            {"Leaf", []pyType{pyStr}},
	"length":          {"Length", []pyType{pySized}},
	"lower":           {"Lower", []pyType{pyStr}},
	"maxwidth":        {"MaxWidth", []pyType{pyStr, pyInt}},
	"meta":            {"Meta", []pyType{pyStr}},
	"month":           {"Month", []pyType{pyDate}},
	"number":          {"Number", []pyType{pyAmount}},
	"only":            {"OnlyInventory", []pyType{pyStr, pyInventory}},
	"open_date":       {"OpenDate", []pyType{pyStr}},
	"open_meta":       {"OpenMeta", []pyType{pyStr}},
	"parent":          {"Parent", []pyType{pyStr}},
	"quarter":         {"Quarter", []pyType{pyDate}},
	"root":            {"Root", []pyType{pyStr, pyInt}},
	"str":             {"Str", []pyType{pyObject}},
	"subst":           {"Subst", []pyType{pyStr, pyStr, pyStr}},
	"today":           {"Today", nil},
	"upper":           {"Upper", []pyType{pyStr}},
	"weekday":         {"Weekday", []pyType{pyDate}},
	"year":            {"Year", []pyType{pyDate}},
	"ymonth":          {"YearMonth", []pyType{pyDate}},

	// Aggregates, registered in the targets environment only.
	"count": {"Count", []pyType{pyObject}},
	"first": {"First", []pyType{pyObject}},
	"last":  {"Last", []pyType{pyObject}},
	"max":   {"Max", []pyType{pyObject}},
	"min":   {"Min", []pyType{pyObject}},
	"sum":   {"Sum", []pyType{pyNumber}},
}

// rejection returns the error the class's constructor raises for args, or
// "" when it would accept them.
func (f *fallbackClass) rejection(args []cexpr) string {
	if len(args) != len(f.params) {
		return fmt.Sprintf("Invalid number of arguments for %s: found %d expected %d.", f.name, len(args), len(f.params))
	}
	for i, arg := range args {
		if !f.params[i].admits(arg.typ()) {
			return fmt.Sprintf("Invalid type for argument %d of %s: found %s expected %s.", i, f.name, pyTypeRepr(arg), f.params[i].repr)
		}
	}
	return ""
}

var dtypeReprs = map[dtype]string{
	tAny:       "<class 'object'>",
	tBool:      "<class 'bool'>",
	tInt:       "<class 'int'>",
	tDecimal:   "<class 'decimal.Decimal'>",
	tString:    "<class 'str'>",
	tDate:      "<class 'datetime.date'>",
	tSet:       "<class 'set'>",
	tAmount:    "<class 'beancount.core.amount.Amount'>",
	tPosition:  "<class 'beancount.core.position.Position'>",
	tInventory: "<class 'beancount.core.inventory.Inventory'>",
}

// pyTypeRepr renders an argument's type like Python's repr() of its class.
func pyTypeRepr(arg cexpr) string {
	if isNullLiteral(arg) {
		return "<class 'NoneType'>"
	}
	return dtypeReprs[arg.typ()]
}

// isNullLiteral reports whether e is the NULL constant, whose Python type
// is NoneType rather than object.
func isNullLiteral(e cexpr) bool {
	lit, ok := e.(*cLiteral)
	return ok && lit.v == nil
}
