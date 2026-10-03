package query

import (
	"context"
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
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

// documentsTable has one row per document directive, those the documents
// option discovers and its pushed tags included.
var documentsTable = &environment{
	columns: map[string]*columnDef{
		"meta":     metaColumn,
		"date":     dateColumn,
		"account":  {tString, func(row *evalRow) any { return string(row.Entry.(*ast.Document).Account) }},
		"filename": {tString, func(row *evalRow) any { return row.Entry.(*ast.Document).ResolvedPath() }},
		"tags":     tagsColumn,
		"links":    linksColumn,
	},
	table:    "documents",
	wildcard: []string{"date", "account", "filename", "tags", "links"},
	rows:     directiveRows[*ast.Document],
}

// balancesTable has one row per balance assertion, a failed one included,
// with its tolerance when one is written and, when it failed, its
// discrepancy: the account's actual amount less the asserted one,
// beancount's diff_amount.
var balancesTable = &environment{
	columns: map[string]*columnDef{
		"meta":    metaColumn,
		"date":    dateColumn,
		"account": {tString, func(row *evalRow) any { return string(row.Entry.(*ast.Balance).Account) }},
		"amount":  {tAmount, func(row *evalRow) any { return directiveAmount(row.Entry.(*ast.Balance).Amount) }},
		"tolerance": {tDecimal, func(row *evalRow) any {
			if amount, ok := directiveAmount(row.Entry.(*ast.Balance).Tolerance).(*amountValue); ok {
				return amount.Number
			}
			return nil
		}},
		"discrepancy": {tAmount, func(row *evalRow) any { return row.Record }},
	},
	table:    "balances",
	wildcard: []string{"date", "account", "amount", "tolerance", "discrepancy"},
	rows:     balanceRows,
}

// balanceRows visits the balances table's rows, each with its discrepancy
// as its Record.
func balanceRows(ctx context.Context, qctx *Context, entries []ast.Directive, visit func(*evalRow)) error {
	differences := balanceDifferences(qctx)
	return directiveRows[*ast.Balance](ctx, qctx, entries, func(row *evalRow) {
		balance := row.Entry.(*ast.Balance)
		if difference, ok := differences[balance]; ok {
			row.Record = &amountValue{Number: difference, Currency: balance.Amount.Currency}
		}
		visit(row)
	})
}

// accountsTable has one row per account with an open or a close
// directive, beancount's get_account_open_close: in the order of its
// first, with its earliest open and its earliest close, the first of
// several on one date. Bare accounts is a postings column, so only a Table
// reference names it.
var accountsTable = &environment{
	columns: map[string]*columnDef{
		"account": {tString, func(row *evalRow) any { return row.Record.(*accountRow).account }},
		"open": {tOpen, func(row *evalRow) any {
			if open := row.Record.(*accountRow).open; open != nil {
				return open
			}
			return nil
		}},
		"close": {tClose, func(row *evalRow) any {
			if closing := row.Record.(*accountRow).close; closing != nil {
				return closing
			}
			return nil
		}},
	},
	table:    "accounts",
	wildcard: []string{"account", "open", "close"},
	rows:     accountRows,
}

// accountRow is a row of the accounts table, its Record.
type accountRow struct {
	account string
	open    *ast.Open
	close   *ast.Close
}

// accountRows visits the accounts table's rows.
func accountRows(ctx context.Context, qctx *Context, entries []ast.Directive, visit func(*evalRow)) error {
	var rows []*accountRow
	byAccount := make(map[ast.Account]*accountRow)
	row := func(account ast.Account) *accountRow {
		r, ok := byAccount[account]
		if !ok {
			r = &accountRow{account: string(account)}
			byAccount[account] = r
			rows = append(rows, r)
		}
		return r
	}
	for i, entry := range entries {
		if err := checkCancelled(ctx, i); err != nil {
			return err
		}
		switch directive := entry.(type) {
		case *ast.Open:
			if r := row(directive.Account); r.open == nil || directive.Date().Before(r.open.Date().Time) {
				r.open = directive
			}
		case *ast.Close:
			if r := row(directive.Account); r.close == nil || directive.Date().Before(r.close.Date().Time) {
				r.close = directive
			}
		}
	}
	for _, r := range rows {
		visit(&evalRow{Ctx: qctx, Record: r})
	}
	return nil
}

// bookingValue is an open's booking method, beancount's Booking enum
// member, by its name.
type bookingValue string

// openAttributes are the attributes of beanquery's open structure,
// beancount's Open.
var openAttributes = map[string]attributeDef{
	"meta":       {tMetadata, func(v any) any { return entryMeta(v.(*ast.Open)) }},
	"date":       {tDate, func(v any) any { return v.(*ast.Open).Date() }},
	"account":    {tString, func(v any) any { return string(v.(*ast.Open).Account) }},
	"currencies": {tList, func(v any) any { return openCurrencies(v.(*ast.Open)) }},
	"booking":    {tBooking, func(v any) any { return openBooking(v.(*ast.Open)) }},
}

// closeAttributes are the attributes of beanquery's close structure,
// beancount's Close.
var closeAttributes = map[string]attributeDef{
	"meta":    {tMetadata, func(v any) any { return entryMeta(v.(*ast.Close)) }},
	"date":    {tDate, func(v any) any { return v.(*ast.Close).Date() }},
	"account": {tString, func(v any) any { return string(v.(*ast.Close).Account) }},
}

// openCurrencies is an open's constraint currencies, a listValue, NULL
// without any, as beancount's None.
func openCurrencies(open *ast.Open) any {
	if len(open.ConstraintCurrencies) == 0 {
		return nil
	}
	currencies := make(listValue, len(open.ConstraintCurrencies))
	for i, currency := range open.ConstraintCurrencies {
		currencies[i] = currency
	}
	return currencies
}

// openBooking is an open's booking method, a bookingValue, NULL when it
// names none, or none beancount knows.
func openBooking(open *ast.Open) any {
	if !config.IsBookingMethod(open.BookingMethod) {
		return nil
	}
	return bookingValue(open.BookingMethod)
}

// openRepr renders an open like Python's repr() of beancount's Open.
func openRepr(open *ast.Open) string {
	return fmt.Sprintf("Open(meta=%s, date=%s, account=%s, currencies=%s, booking=%s)",
		entryMeta(open), pyValueRepr(open.Date()), pyrepr.String(string(open.Account)),
		pyValueRepr(openCurrencies(open)), pyValueRepr(openBooking(open)))
}

// closeRepr renders a close like Python's repr() of beancount's Close.
func closeRepr(closing *ast.Close) string {
	return fmt.Sprintf("Close(meta=%s, date=%s, account=%s)",
		entryMeta(closing), pyValueRepr(closing.Date()), pyrepr.String(string(closing.Account)))
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
