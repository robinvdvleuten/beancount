package query

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// Context carries the processed ledger data a query executes against: the
// Ledger, whose options the query reads, and the AST it processed. Run
// requires both.
type Context struct {
	Ledger *ledger.Ledger
	// AST holds the directives as the Ledger left them, with postings
	// booked and interpolated.
	AST *ast.AST
	// summarized holds the position of each posting that FROM's
	// summarization creates, which the ledger never booked.
	summarized map[*ast.Posting]*positionValue
}

// evalRow is the evaluation context for one data row. In the FROM (entry)
// environment only Entry is set; in the posting environment Txn and Posting
// identify the flattened posting row. running is the inventory the
// executor shares between a query's rows, which the balance column adds the
// row to (see balance). AggValues holds finalized aggregate results while
// group targets are evaluated.
type evalRow struct {
	Ctx       *Context
	Entry     ast.Directive
	Txn       *ast.Transaction
	Posting   *ast.Posting
	running   *inventoryValue
	balance   *inventoryValue
	AggValues []any
	// Position is what this row books: the posting's own position, or for a
	// reduction the share booked against one lot. Nil for postings without
	// an amount.
	Position *positionValue
	// Record holds what a row of a Table holds beyond its directive, if it
	// has one: a balance assertion's discrepancy, or an accountRow.
	Record any
}

// balanceValue is the balance column, lazy like beanquery's (its postings
// table caches the column per row): the first time a row evaluates it, the
// row's position joins the shared running inventory, and the row keeps a
// copy. A row that never evaluates it, because WHERE short-circuited before
// reaching it or dropped the row, never joins, so the column sums only the
// rows that read it, up to and including the current one.
func (row *evalRow) balanceValue() any {
	if row.balance == nil {
		if row.Position != nil {
			row.running.AddPosition(row.Position)
		}
		row.balance = row.running.Copy()
	}
	return row.balance
}

// columnDef declares a column available in an environment: its result type
// and how to evaluate it for a row.
type columnDef struct {
	typ  dtype
	eval func(row *evalRow) any
}

// entryColumns are the entries table's columns, on directives.
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
	"payee":       {tString, txnColumn(payeeValue)},
	"narration":   {tString, txnColumn(func(txn *ast.Transaction) any { return txn.Narration.String() })},
	"description": {tString, txnColumn(descriptionValue)},
	"tags":        {tSet, taggedColumn(tagSet)},
	"links":       {tSet, taggedColumn(linkSet)},
	"meta":        {tDict, func(row *evalRow) any { return entryMeta(row.Entry) }},
	"accounts":    {tAccountSet, func(row *evalRow) any { return accountSet(row.Entry) }},
}

// postingColumns are the postings table's columns, on posting rows.
var postingColumns = map[string]*columnDef{
	"account":       {tString, func(row *evalRow) any { return string(row.Posting.Account) }},
	"position":      {tPosition, positionColumn},
	"change":        {tPosition, positionColumn},
	"balance":       {tInventory, func(row *evalRow) any { return row.balanceValue() }},
	"number":        {tDecimal, unitsColumn(func(units amountValue) any { return units.Number })},
	"currency":      {tString, unitsColumn(func(units amountValue) any { return units.Currency })},
	"cost_number":   {tDecimal, costColumn(func(c *costValue) any { return c.Number })},
	"cost_currency": {tString, costColumn(func(c *costValue) any { return c.Currency })},
	// A cost without a date is NULL, not a nil *ast.Date boxed in an any.
	"cost_date": {tDate, costColumn(func(c *costValue) any {
		if c.Date == nil {
			return nil
		}
		return c.Date
	})},
	// Like beanquery's, cost_label is '' without a cost and NULL for a
	// cost without a label.
	"cost_label": {tString, func(row *evalRow) any {
		if row.Position == nil || row.Position.Cost == nil {
			return ""
		}
		if label := row.Position.Cost.Label; label != "" {
			return label
		}
		return nil
	}},
	"price":        {tAmount, func(row *evalRow) any { return postingPrice(row.Posting) }},
	"weight":       {tAmount, func(row *evalRow) any { return postingWeight(row.Posting, row.Position) }},
	"posting_flag": {tString, func(row *evalRow) any { return row.Posting.Flag }},
	// Like beanquery's, other_accounts leaves out the row's own posting
	// alone, so another posting to the same account still counts. Booking
	// splits a reduction across lots into one posting per lot in beancount,
	// where ours is one posting with a row per lot, so the posting's other
	// lots count too. Like beanquery's, the column is typed as a set and
	// holds a sorted list.
	"other_accounts": {tSet, func(row *evalRow) any {
		others := make(setValue)
		for _, p := range row.Txn.Postings {
			if p != row.Posting || len(postingPositions(row.Ctx, p)) > 1 {
				others[string(p.Account)] = struct{}{}
			}
		}
		sorted := others.Sorted()
		list := make(listValue, len(sorted))
		for i, account := range sorted {
			list[i] = account
		}
		return list
	}},
	// Like beanquery, the location columns are the posting's, and NULL for
	// a posting with none, such as one FROM's summarization creates.
	"location": {tString, func(row *evalRow) any {
		if pos := row.Posting.Position(); pos.Filename != "" {
			return fmt.Sprintf("%s:%d:", pos.Filename, pos.Line)
		}
		return nil
	}},
	"filename": {tString, func(row *evalRow) any {
		if pos := row.Posting.Position(); pos.Filename != "" {
			return pos.Filename
		}
		return nil
	}},
	"lineno": {tInt, func(row *evalRow) any {
		if pos := row.Posting.Position(); pos.Filename != "" {
			return int64(pos.Line)
		}
		return nil
	}},

	// Transaction-level context, shared with the entry environment.
	"date":        {tDate, func(row *evalRow) any { return row.Entry.Date() }},
	"year":        {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Year()) }},
	"month":       {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Month()) }},
	"day":         {tInt, func(row *evalRow) any { return int64(row.Entry.Date().Day()) }},
	"type":        {tString, func(row *evalRow) any { return string(row.Entry.Kind()) }},
	"id":          {tString, func(row *evalRow) any { return entryID(row.Entry) }},
	"flag":        {tString, func(row *evalRow) any { return row.Txn.Flag }},
	"payee":       {tString, func(row *evalRow) any { return payeeValue(row.Txn) }},
	"narration":   {tString, func(row *evalRow) any { return row.Txn.Narration.String() }},
	"description": {tString, func(row *evalRow) any { return descriptionValue(row.Txn) }},
	"tags":        {tSet, func(row *evalRow) any { return tagSet(row.Txn) }},
	"links":       {tSet, func(row *evalRow) any { return linkSet(row.Txn) }},
	"meta":        {tDict, func(row *evalRow) any { return postingMeta(row.Posting) }},
	"entry":       {tTransaction, func(row *evalRow) any { return &transactionValue{txn: row.Txn, ctx: row.Ctx} }},
	"accounts":    {tAccountSet, func(row *evalRow) any { return accountSet(row.Txn) }},
}

// environment is a Table, what a query reads and its clauses compile
// against, like one of beanquery's tables: its columns, the name its errors
// quote, the columns SELECT * expands to, and its rows. Every environment
// registers every function and aggregate.
type environment struct {
	columns  map[string]*columnDef
	table    string
	wildcard []string
	// rows visits the Table's rows. A Table that is read from the
	// ledger's directives reads them from entries, which FROM's
	// transforms have summarized for the postings table.
	rows func(ctx context.Context, qctx *Context, entries []ast.Directive, visit func(*evalRow)) error
}

var (
	// postingsTable is the Table a SELECT reads unless its FROM names
	// another: one row per booked position of a transaction's posting.
	postingsTable = &environment{
		columns:  postingColumns,
		table:    "postings",
		wildcard: []string{"date", "flag", "payee", "narration", "position"},
		rows:     postingRows,
	}
	// entriesTable is the Table PRINT's FROM filters: one row per
	// directive. Like beanquery's, SELECT * expands to every column, in
	// the order beanquery registers them.
	entriesTable = &environment{
		columns: entryColumns,
		table:   "entries",
		wildcard: []string{"id", "type", "filename", "lineno", "date", "year", "month", "day", "flag",
			"payee", "narration", "description", "tags", "links", "meta", "accounts"},
		rows: entryRows,
	}
	// emptyTable is the Empty table, which # names: one row and no
	// columns.
	emptyTable = &environment{columns: map[string]*columnDef{}, rows: emptyRows}
)

// tables are the Tables a Table reference names, by name.
var tables = map[string]*environment{
	postingsTable.table:     postingsTable,
	entriesTable.table:      entriesTable,
	emptyTable.table:        emptyTable,
	transactionsTable.table: transactionsTable,
	pricesTable.table:       pricesTable,
	eventsTable.table:       eventsTable,
	commoditiesTable.table:  commoditiesTable,
	notesTable.table:        notesTable,
	documentsTable.table:    documentsTable,
	balancesTable.table:     balancesTable,
	accountsTable.table:     accountsTable,
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

// payeeValue is the payee column: NULL for a transaction written without a
// payee, as beancount holds it (None), and the string otherwise, empty for
// an empty payee written as "". Narration has no such NULL: beancount
// gives a transaction without strings an empty narration.
func payeeValue(txn *ast.Transaction) any {
	if txn.Payee.HasRaw() || !txn.Payee.IsEmpty() {
		return txn.Payee.String()
	}
	return nil
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

// tagged is a directive with tags and links: a transaction, a note or a
// document.
type tagged interface {
	AllTags() []ast.Tag
	AllLinks() []ast.Link
}

// taggedColumn wraps a tags or links accessor into an entry-environment
// column that, like beanquery's getattr, yields NULL for a directive without
// tags and links.
func taggedColumn(eval func(tagged) setValue) func(row *evalRow) any {
	return func(row *evalRow) any {
		if entry, ok := row.Entry.(tagged); ok {
			return eval(entry)
		}
		return nil
	}
}

func tagSet(entry tagged) setValue {
	tags := entry.AllTags()
	set := make(setValue, len(tags))
	for _, tag := range tags {
		set[string(tag)] = struct{}{}
	}
	return set
}

func linkSet(entry tagged) setValue {
	links := entry.AllLinks()
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
// positionColumn is the row's position, or NULL for a posting without one:
// a nil *positionValue boxed in an any would not read as NULL.
func positionColumn(row *evalRow) any {
	if row.Position == nil {
		return nil
	}
	return row.Position
}

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
	return ctx.Ledger.GetPrice(date, from, to)
}
