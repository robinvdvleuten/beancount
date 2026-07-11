package query

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// Context carries the processed ledger data a query executes against.
type Context struct {
	Ledger *ledger.Ledger
	Config *config.Config
}

// Row is the evaluation context for one data row. In the FROM (entry)
// environment only Entry is set; in the posting environment Txn and Posting
// identify the flattened posting row. Balance is the running per-account
// inventory maintained by the executor. AggValues holds finalized aggregate
// results while group targets are evaluated.
type Row struct {
	Ctx       *Context
	Entry     ast.Directive
	Txn       *ast.Transaction
	Posting   *ast.Posting
	Balance   *Inventory
	AggValues []any
	// CostDate is the effective cost-basis date for this posting: lot
	// reductions inherit the matched lot's date (official booking behavior),
	// everything else gets the transaction date.
	CostDate *ast.Date
}

// costDate returns the effective cost date, defaulting to the entry date.
func (row *Row) costDate() *ast.Date {
	if row.CostDate != nil {
		return row.CostDate
	}
	return row.Entry.Date()
}

// columnDef declares a column available in an environment: its result type
// and how to evaluate it for a row.
type columnDef struct {
	typ  DType
	eval func(row *Row) any
}

// wildcardColumns is the column list SELECT * expands to, matching the
// official targets environment.
var wildcardColumns = []string{"date", "flag", "payee", "narration", "position"}

// entryColumns is the FROM environment: columns on directives.
var entryColumns = map[string]*columnDef{
	"date":        {TDate, func(row *Row) any { return row.Entry.Date() }},
	"year":        {TInt, func(row *Row) any { return int64(row.Entry.Date().Year()) }},
	"month":       {TInt, func(row *Row) any { return int64(row.Entry.Date().Month()) }},
	"day":         {TInt, func(row *Row) any { return int64(row.Entry.Date().Day()) }},
	"type":        {TString, func(row *Row) any { return string(row.Entry.Kind()) }},
	"filename":    {TString, func(row *Row) any { return row.Entry.Position().Filename }},
	"lineno":      {TInt, func(row *Row) any { return int64(row.Entry.Position().Line) }},
	"id":          {TString, func(row *Row) any { return entryID(row.Entry) }},
	"flag":        {TString, txnColumn(func(txn *ast.Transaction) any { return txn.Flag })},
	"payee":       {TString, txnColumn(func(txn *ast.Transaction) any { return txn.Payee.String() })},
	"narration":   {TString, txnColumn(func(txn *ast.Transaction) any { return txn.Narration.String() })},
	"description": {TString, txnColumn(descriptionValue)},
	"tags":        {TSet, txnColumn(func(txn *ast.Transaction) any { return tagSet(txn) })},
	"links":       {TSet, txnColumn(func(txn *ast.Transaction) any { return linkSet(txn) })},
}

// postingColumns is the targets/WHERE environment: columns on posting rows.
var postingColumns = map[string]*columnDef{
	"account":       {TString, func(row *Row) any { return string(row.Posting.Account) }},
	"position":      {TPosition, func(row *Row) any { return postingPosition(row.Posting, row.costDate()) }},
	"change":        {TPosition, func(row *Row) any { return postingPosition(row.Posting, row.costDate()) }},
	"balance":       {TInventory, func(row *Row) any { return row.Balance }},
	"number":        {TDecimal, func(row *Row) any { return postingNumber(row.Posting) }},
	"currency":      {TString, func(row *Row) any { return postingCurrency(row.Posting) }},
	"cost_number":   {TDecimal, costColumn(func(c *Cost) any { return c.Number })},
	"cost_currency": {TString, costColumn(func(c *Cost) any { return c.Currency })},
	"cost_date":     {TDate, costColumn(func(c *Cost) any { return c.Date })},
	"cost_label":    {TString, costColumn(func(c *Cost) any { return c.Label })},
	"price":         {TAmount, func(row *Row) any { return postingPrice(row.Posting) }},
	"weight":        {TAmount, func(row *Row) any { return postingWeight(row.Posting, row.Entry.Date()) }},
	"posting_flag":  {TString, func(row *Row) any { return row.Posting.Flag }},
	"other_accounts": {TSet, func(row *Row) any {
		others := make(Set)
		for _, p := range row.Txn.Postings {
			if p.Account != row.Posting.Account {
				others[string(p.Account)] = struct{}{}
			}
		}
		return others
	}},
	"location": {TString, func(row *Row) any {
		pos := row.Posting.Position()
		return fmt.Sprintf("%s:%d:", pos.Filename, pos.Line)
	}},

	// Transaction-level context, shared with the entry environment.
	"date":        {TDate, func(row *Row) any { return row.Entry.Date() }},
	"year":        {TInt, func(row *Row) any { return int64(row.Entry.Date().Year()) }},
	"month":       {TInt, func(row *Row) any { return int64(row.Entry.Date().Month()) }},
	"day":         {TInt, func(row *Row) any { return int64(row.Entry.Date().Day()) }},
	"type":        {TString, func(row *Row) any { return string(row.Entry.Kind()) }},
	"filename":    {TString, func(row *Row) any { return row.Entry.Position().Filename }},
	"lineno":      {TInt, func(row *Row) any { return int64(row.Entry.Position().Line) }},
	"id":          {TString, func(row *Row) any { return entryID(row.Entry) }},
	"flag":        {TString, func(row *Row) any { return row.Txn.Flag }},
	"payee":       {TString, func(row *Row) any { return row.Txn.Payee.String() }},
	"narration":   {TString, func(row *Row) any { return row.Txn.Narration.String() }},
	"description": {TString, func(row *Row) any { return descriptionValue(row.Txn) }},
	"tags":        {TSet, func(row *Row) any { return tagSet(row.Txn) }},
	"links":       {TSet, func(row *Row) any { return linkSet(row.Txn) }},
}

// environment names a column set for compilation and error messages.
type environment struct {
	columns map[string]*columnDef
	context string
}

var (
	targetsEnv = &environment{columns: postingColumns, context: "targets/column context"}
	filterEnv  = &environment{columns: entryColumns, context: "filter context"}
)

// txnColumn wraps a transaction accessor into an entry-environment column
// that yields NULL for non-transaction directives.
func txnColumn(eval func(txn *ast.Transaction) any) func(row *Row) any {
	return func(row *Row) any {
		if txn, ok := row.Entry.(*ast.Transaction); ok {
			return eval(txn)
		}
		return nil
	}
}

// costColumn wraps a cost accessor into a column that yields NULL for
// postings without a cost basis.
func costColumn(eval func(c *Cost) any) func(row *Row) any {
	return func(row *Row) any {
		position := postingPosition(row.Posting, row.costDate())
		if position == nil || position.Cost == nil {
			return nil
		}
		return eval(position.Cost)
	}
}

func descriptionValue(txn *ast.Transaction) any {
	payee := txn.Payee.String()
	narration := txn.Narration.String()
	if payee != "" && narration != "" {
		return payee + " | " + narration
	}
	if payee != "" {
		return payee
	}
	return narration
}

func tagSet(txn *ast.Transaction) Set {
	set := make(Set, len(txn.Tags))
	for _, tag := range txn.Tags {
		set[string(tag)] = struct{}{}
	}
	return set
}

func linkSet(txn *ast.Transaction) Set {
	set := make(Set, len(txn.Links))
	for _, link := range txn.Links {
		set[string(link)] = struct{}{}
	}
	return set
}

// entryID returns a unique, stable id for a directive. The official
// implementation hashes the full directive contents; we hash the source
// location, which is equally unique and stable but yields different digests
// (documented in testdata/compliance/KNOWN_GAPS.md).
func entryID(entry ast.Directive) string {
	pos := entry.Position()
	sum := md5.Sum(fmt.Appendf(nil, "%s:%d", pos.Filename, pos.Line))
	return hex.EncodeToString(sum[:])
}

// postingPosition converts an AST posting into a query Position with a
// normalized per-unit cost. The ledger resolves interpolated amounts and
// inferred costs in place, but total costs ({{...}}) keep their original
// form in the AST, so per-unit normalization happens here. Cost bases
// without an explicit date are stamped with the transaction date, matching
// official booking (the date shows in cost_date but not in rendered
// positions).
func postingPosition(posting *ast.Posting, entryDate *ast.Date) *Position {
	if posting.Amount == nil {
		return nil
	}
	number, err := ledger.ParseAmount(posting.Amount)
	if err != nil {
		return nil
	}
	position := &Position{Units: Amount{Number: number, Currency: posting.Amount.Currency}}

	if posting.Cost != nil {
		cost := &Cost{Date: posting.Cost.Date, Label: posting.Cost.Label}
		if cost.Date == nil {
			cost.Date = entryDate
		}
		costAmount := posting.Cost.Amount
		if costAmount == nil {
			costAmount = posting.Cost.Total
		}
		if costAmount != nil {
			costNumber, err := ledger.ParseAmount(costAmount)
			if err != nil {
				return nil
			}
			if posting.Cost.IsTotal || posting.Cost.Total != nil {
				if !number.IsZero() {
					costNumber = costNumber.Div(number.Abs())
				}
			}
			cost.Number = costNumber
			cost.Currency = costAmount.Currency
		}
		position.Cost = cost
	}
	return position
}

func postingNumber(posting *ast.Posting) any {
	if posting.Amount == nil {
		return nil
	}
	number, err := ledger.ParseAmount(posting.Amount)
	if err != nil {
		return nil
	}
	return number
}

func postingCurrency(posting *ast.Posting) any {
	if posting.Amount == nil {
		return nil
	}
	return posting.Amount.Currency
}

// postingPrice returns the per-unit price attached to a posting. Total
// prices (@@) are normalized to per-unit, matching the official booking
// behavior.
func postingPrice(posting *ast.Posting) any {
	if posting.Price == nil {
		return nil
	}
	number, err := ledger.ParseAmount(posting.Price)
	if err != nil {
		return nil
	}
	if posting.PriceTotal {
		units, err := ledger.ParseAmount(posting.Amount)
		if err != nil || units.IsZero() {
			return nil
		}
		number = number.Div(units.Abs())
	}
	return &Amount{Number: number, Currency: posting.Price.Currency}
}

// postingWeight computes the booking weight of a posting: units at cost if a
// cost basis is attached, converted at price if a price is attached, and the
// plain units otherwise.
func postingWeight(posting *ast.Posting, entryDate *ast.Date) any {
	position := postingPosition(posting, entryDate)
	if position == nil {
		return nil
	}
	if position.Cost != nil {
		return &Amount{
			Number:   position.Units.Number.Mul(position.Cost.Number),
			Currency: position.Cost.Currency,
		}
	}
	if price, ok := postingPrice(posting).(*Amount); ok && price != nil {
		return &Amount{
			Number:   position.Units.Number.Mul(price.Number),
			Currency: price.Currency,
		}
	}
	return &position.Units
}

// priceLookup fetches a conversion rate from the ledger price graph.
func priceLookup(ctx *Context, date *ast.Date, from, to string) (decimal.Decimal, bool) {
	if ctx == nil || ctx.Ledger == nil {
		return decimal.Decimal{}, false
	}
	return ctx.Ledger.GetPrice(date, from, to)
}
