package query

import (
	"context"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
)

// The Tables beanquery reads from the ledger's directives besides postings
// and entries (beanquery/sources/beancount.py): one row per directive of a
// kind, which FROM's transforms never summarize. Like beanquery's, SELECT *
// leaves out a Table's meta column.

// transactionsTable has one row per transaction, padding transactions
// included, and the Transaction structure's attributes as its columns.
var transactionsTable = &environment{
	columns:  transactionColumns(),
	table:    "transactions",
	wildcard: []string{"date", "flag", "payee", "narration", "tags", "links", "accounts"},
	rows:     directiveRows[*ast.Transaction],
}

// pricesTable has one row per price directive, two on one date included.
var pricesTable = &environment{
	columns: map[string]*columnDef{
		"meta":     metaColumn,
		"date":     dateColumn,
		"currency": {tString, func(row *evalRow) any { return row.Entry.(*ast.Price).Commodity }},
		"amount":   {tAmount, func(row *evalRow) any { return directiveAmount(row.Entry.(*ast.Price).Amount) }},
	},
	table:    "prices",
	wildcard: []string{"date", "currency", "amount"},
	rows:     directiveRows[*ast.Price],
}

// eventsTable has one row per event directive.
var eventsTable = &environment{
	columns: map[string]*columnDef{
		"meta":        metaColumn,
		"date":        dateColumn,
		"type":        {tString, func(row *evalRow) any { return row.Entry.(*ast.Event).Name.String() }},
		"description": {tString, func(row *evalRow) any { return row.Entry.(*ast.Event).Value.String() }},
	},
	table:    "events",
	wildcard: []string{"date", "type", "description"},
	rows:     directiveRows[*ast.Event],
}

// commoditiesTable has one row per currency with a commodity directive,
// in the order of its first, like beancount's get_commodity_directives:
// the row of a currency with several is its last. Like beanquery's, its
// SELECT * takes in meta.
var commoditiesTable = &environment{
	columns: map[string]*columnDef{
		"meta": metaColumn,
		"date": dateColumn,
		"name": {tString, func(row *evalRow) any { return row.Entry.(*ast.Commodity).Currency }},
	},
	table:    "commodities",
	wildcard: []string{"meta", "date", "name"},
	rows:     commodityRows,
}

// commodityRows visits the commodities table's rows.
func commodityRows(ctx context.Context, qctx *Context, entries []ast.Directive, visit func(*evalRow)) error {
	var currencies []string
	last := make(map[string]*ast.Commodity)
	for i, entry := range entries {
		if err := checkCancelled(ctx, i); err != nil {
			return err
		}
		if commodity, ok := entry.(*ast.Commodity); ok {
			if _, seen := last[commodity.Currency]; !seen {
				currencies = append(currencies, commodity.Currency)
			}
			last[commodity.Currency] = commodity
		}
	}
	for _, currency := range currencies {
		visit(&evalRow{Ctx: qctx, Entry: last[currency]})
	}
	return nil
}

// notesTable has one row per note directive, its pushed tags included.
var notesTable = &environment{
	columns: map[string]*columnDef{
		"meta":    metaColumn,
		"date":    dateColumn,
		"account": {tString, func(row *evalRow) any { return string(row.Entry.(*ast.Note).Account) }},
		"comment": {tString, func(row *evalRow) any { return row.Entry.(*ast.Note).Description.String() }},
		"tags":    tagsColumn,
		"links":   linksColumn,
	},
	table:    "notes",
	wildcard: []string{"date", "account", "comment", "tags", "links"},
	rows:     directiveRows[*ast.Note],
}

var (
	// tagsColumn and linksColumn are a note's or a document's tags and
	// links, pushed tags included.
	tagsColumn  = &columnDef{tFrozenset, func(row *evalRow) any { return tagSet(row.Entry.(tagged)) }}
	linksColumn = &columnDef{tFrozenset, func(row *evalRow) any { return linkSet(row.Entry.(tagged)) }}
)

var (
	// metaColumn is a directive's meta, typed like beanquery's Metadata
	// columns, which render without filename and lineno.
	metaColumn = &columnDef{tMetadata, func(row *evalRow) any { return entryMeta(row.Entry) }}
	// dateColumn is a directive's date.
	dateColumn = &columnDef{tDate, func(row *evalRow) any { return row.Entry.Date() }}
)

// directiveAmount is a directive's amount, NULL when it has none or its
// number does not read.
func directiveAmount(amount *ast.Amount) any {
	if amount == nil {
		return nil
	}
	number, err := ledger.ParseAmount(amount)
	if err != nil {
		return nil
	}
	return &amountValue{Number: number, Currency: amount.Currency}
}

// transactionColumns are transactionAttributes read from a row's
// transaction.
func transactionColumns() map[string]*columnDef {
	columns := make(map[string]*columnDef, len(transactionAttributes))
	for name, attr := range transactionAttributes {
		columns[name] = &columnDef{attr.typ, func(row *evalRow) any {
			return attr.get(&transactionValue{txn: row.Entry.(*ast.Transaction), ctx: row.Ctx})
		}}
	}
	return columns
}

// directiveRows visits one row per directive that is a T, in ledger order.
func directiveRows[T ast.Directive](ctx context.Context, qctx *Context, entries []ast.Directive, visit func(*evalRow)) error {
	for i, entry := range entries {
		if err := checkCancelled(ctx, i); err != nil {
			return err
		}
		if _, ok := entry.(T); ok {
			visit(&evalRow{Ctx: qctx, Entry: entry})
		}
	}
	return nil
}
