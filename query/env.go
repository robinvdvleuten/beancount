package query

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// Context carries the processed ledger data a query executes against: the
// Ledger, the options, and the AST the Ledger processed.
type Context struct {
	Ledger *ledger.Ledger
	Config *config.Config
	// AST holds the directives as the Ledger left them, with postings
	// booked and interpolated.
	AST *ast.AST
	// summarized holds the position of each posting that FROM's
	// summarization creates, which the ledger never booked.
	summarized map[*ast.Posting]*positionValue
}

// evalRow is the evaluation context for one data row. In the FROM (entry)
// environment only Entry is set; in the posting environment Txn and Posting
// identify the flattened posting row. Balance is the running per-account
// inventory maintained by the executor. AggValues holds finalized aggregate
// results while group targets are evaluated.
type evalRow struct {
	Ctx       *Context
	Entry     ast.Directive
	Txn       *ast.Transaction
	Posting   *ast.Posting
	Balance   *inventoryValue
	AggValues []any
	// Position is what this row books: the posting's own position, or for a
	// reduction the share booked against one lot. Nil for postings without
	// an amount.
	Position *positionValue
}

// columnDef declares a column available in an environment: its result type
// and how to evaluate it for a row.
type columnDef struct {
	typ  dtype
	eval func(row *evalRow) any
}

// wildcardColumns is the column list SELECT * expands to, matching the
// official targets environment.
var wildcardColumns = []string{"date", "flag", "payee", "narration", "position"}

// entryColumns is the FROM environment: columns on directives.
var entryColumns = map[string]*columnDef{
	"date":        {tDate, func(row *evalRow) any { return row.Entry.Date() }},
	"year":        {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Year()) }},
	"month":       {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Month()) }},
	"day":         {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Day()) }},
	"type":        {tString, func(row *evalRow) any { return string(row.Entry.Kind()) }},
	"filename":    {tString, func(row *evalRow) any { return row.Entry.Position().Filename }},
	"lineno":      {tInt, func(row *evalRow) any { return int64(row.Entry.Position().Line) }},
	"id":          {tString, func(row *evalRow) any { return entryID(row.Entry) }},
	"flag":        {tString, txnColumn(func(txn *ast.Transaction) any { return txn.Flag })},
	"payee":       {tString, txnColumn(func(txn *ast.Transaction) any { return txn.Payee.String() })},
	"narration":   {tString, txnColumn(func(txn *ast.Transaction) any { return txn.Narration.String() })},
	"description": {tString, txnColumn(descriptionValue)},
	"tags":        {tSet, txnColumn(func(txn *ast.Transaction) any { return tagSet(txn) })},
	"links":       {tSet, txnColumn(func(txn *ast.Transaction) any { return linkSet(txn) })},
}

// postingColumns is the targets/WHERE environment: columns on posting rows.
var postingColumns = map[string]*columnDef{
	"account":       {tString, func(row *evalRow) any { return string(row.Posting.Account) }},
	"position":      {tPosition, func(row *evalRow) any { return row.Position }},
	"change":        {tPosition, func(row *evalRow) any { return row.Position }},
	"balance":       {tInventory, func(row *evalRow) any { return row.Balance }},
	"number":        {tDecimal, unitsColumn(func(units amountValue) any { return units.Number })},
	"currency":      {tString, unitsColumn(func(units amountValue) any { return units.Currency })},
	"cost_number":   {tDecimal, costColumn(func(c *costValue) any { return c.Number })},
	"cost_currency": {tString, costColumn(func(c *costValue) any { return c.Currency })},
	"cost_date":     {tDate, costColumn(func(c *costValue) any { return c.Date })},
	"cost_label":    {tString, costColumn(func(c *costValue) any { return c.Label })},
	"price":         {tAmount, func(row *evalRow) any { return postingPrice(row.Posting) }},
	"weight":        {tAmount, func(row *evalRow) any { return postingWeight(row.Posting, row.Position) }},
	"posting_flag":  {tString, func(row *evalRow) any { return row.Posting.Flag }},
	"other_accounts": {tSet, func(row *evalRow) any {
		others := make(setValue)
		for _, p := range row.Txn.Postings {
			if p.Account != row.Posting.Account {
				others[string(p.Account)] = struct{}{}
			}
		}
		return others
	}},
	"location": {tString, func(row *evalRow) any {
		pos := row.Posting.Position()
		return fmt.Sprintf("%s:%d:", pos.Filename, pos.Line)
	}},

	// Transaction-level context, shared with the entry environment.
	"date":        {tDate, func(row *evalRow) any { return row.Entry.Date() }},
	"year":        {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Year()) }},
	"month":       {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Month()) }},
	"day":         {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Day()) }},
	"type":        {tString, func(row *evalRow) any { return string(row.Entry.Kind()) }},
	"filename":    {tString, func(row *evalRow) any { return row.Entry.Position().Filename }},
	"lineno":      {tInt, func(row *evalRow) any { return int64(row.Entry.Position().Line) }},
	"id":          {tString, func(row *evalRow) any { return entryID(row.Entry) }},
	"flag":        {tString, func(row *evalRow) any { return row.Txn.Flag }},
	"payee":       {tString, func(row *evalRow) any { return row.Txn.Payee.String() }},
	"narration":   {tString, func(row *evalRow) any { return row.Txn.Narration.String() }},
	"description": {tString, func(row *evalRow) any { return descriptionValue(row.Txn) }},
	"tags":        {tSet, func(row *evalRow) any { return tagSet(row.Txn) }},
	"links":       {tSet, func(row *evalRow) any { return linkSet(row.Txn) }},
}

// environment is what one clause compiles against, like one of
// beanquery's tables: its columns, the table name its errors quote, and
// whether it registers the entry filters.
type environment struct {
	columns map[string]*columnDef
	table   string
	// withEntryFilters registers has_account (FROM only).
	withEntryFilters bool
}

var (
	// targetsEnv compiles SELECT targets, WHERE, GROUP BY, ORDER BY and
	// PIVOT BY.
	targetsEnv = &environment{columns: postingColumns, table: "postings"}
	// fromEnv compiles a SELECT's FROM clause, over entries; beanquery's
	// errors name the postings table there.
	fromEnv = &environment{columns: entryColumns, table: "postings", withEntryFilters: true}
	// printFromEnv compiles PRINT's FROM clause, over entries.
	printFromEnv = &environment{columns: entryColumns, table: "entries", withEntryFilters: true}
)

// entryFilters are the functions only the FROM environment registers.
var entryFilters = map[string]bool{"has_account": true}

// function returns the simple function registered under name, if any.
func (e *environment) function(name string) *funcDef {
	if entryFilters[name] && !e.withEntryFilters {
		return nil
	}
	return functions[name]
}

// txnColumn wraps a transaction accessor into an entry-environment column
// that yields NULL for non-transaction directives.
func txnColumn(eval func(txn *ast.Transaction) any) func(row *evalRow) any {
	return func(row *evalRow) any {
		if txn, ok := row.Entry.(*ast.Transaction); ok {
			return eval(txn)
		}
		return nil
	}
}

// unitsColumn wraps a units accessor into a column that yields NULL for
// postings without an amount.
func unitsColumn(eval func(units amountValue) any) func(row *evalRow) any {
	return func(row *evalRow) any {
		if row.Position == nil {
			return nil
		}
		return eval(row.Position.Units)
	}
}

// costColumn wraps a cost accessor into a column that yields NULL for
// postings without a cost basis.
func costColumn(eval func(c *costValue) any) func(row *evalRow) any {
	return func(row *evalRow) any {
		if row.Position == nil || row.Position.Cost == nil {
			return nil
		}
		return eval(row.Position.Cost)
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

func tagSet(txn *ast.Transaction) setValue {
	tags := txn.AllTags()
	set := make(setValue, len(tags))
	for _, tag := range tags {
		set[string(tag)] = struct{}{}
	}
	return set
}

func linkSet(txn *ast.Transaction) setValue {
	links := txn.AllLinks()
	set := make(setValue, len(links))
	for _, link := range links {
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

// postingPositions returns the positions a posting books, as the ledger
// records them (ledger.BookedPositions): a reduction books one per lot it
// was booked against, like the booked postings beancount replaces it with,
// and any other posting its own. A posting FROM's summarization created
// books the position it was made from.
func postingPositions(qctx *Context, posting *ast.Posting) []*positionValue {
	if position, ok := qctx.summarized[posting]; ok {
		copied := *position
		return []*positionValue{&copied}
	}
	booked := qctx.Ledger.BookedPositions(posting)
	positions := make([]*positionValue, 0, len(booked))
	for _, b := range booked {
		position := &positionValue{Units: amountValue{Number: b.Units, Currency: posting.Amount.Currency}}
		if c := b.Cost; c != nil {
			position.Cost = &costValue{Number: c.Number, Currency: c.Currency, Date: c.Date, Label: c.Label}
		}
		positions = append(positions, position)
	}
	return positions
}

// postingPrice returns the per-unit price attached to a posting
// (ledger.PerUnitPrice), or nil.
func postingPrice(posting *ast.Posting) any {
	number, currency, ok := ledger.PerUnitPrice(posting)
	if !ok {
		return nil
	}
	return &amountValue{Number: number, Currency: currency}
}

// postingWeight computes the booking weight of a booked position: units at
// cost if a cost basis is attached, converted at the posting's price if one
// is attached, and the plain units otherwise.
func postingWeight(posting *ast.Posting, position *positionValue) any {
	if position == nil {
		return nil
	}
	if position.Cost != nil {
		return &amountValue{
			Number:   pydecimal.Mul(position.Units.Number, position.Cost.Number),
			Currency: position.Cost.Currency,
		}
	}
	if price, ok := postingPrice(posting).(*amountValue); ok && price != nil {
		return &amountValue{
			Number:   pydecimal.Mul(position.Units.Number, price.Number),
			Currency: price.Currency,
		}
	}
	return &position.Units
}

// priceLookup fetches a conversion rate from the ledger's prices.
func priceLookup(ctx *Context, date *ast.Date, from, to string) (decimal.Decimal, bool) {
	if ctx == nil || ctx.Ledger == nil {
		return decimal.Decimal{}, false
	}
	return ctx.Ledger.GetPrice(date, from, to)
}
