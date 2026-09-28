// Package printer renders directives as Beancount text the way beancount's
// printer module does (beancount/parser/printer.py): numbers as parsed, or
// as booked for postings, each transaction's postings aligned and indented
// two spaces, metadata lines, and none of the source's comments or layout.
//
// It is the canonical rendering behind query PRINT, import's Extracted
// directives, doctor missing_open and the CLI's error context. The
// formatter package, held to bean-format, keeps a file's own layout instead.
package printer

import (
	"context"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// Option configures a Print or Sprint call.
type Option func(*printer)

// WithBookedPositions prints each posting that lookup returns positions
// for as its booked postings, one per position: its units, the lot's
// per-unit cost, date and label, and a per-unit price. A posting it returns
// none for prints as parsed. Pass Ledger.BookedPositions.
func WithBookedPositions(lookup func(*ast.Posting) []ledger.BookedPosition) Option {
	return func(p *printer) {
		p.booked = lookup
	}
}

// WithBalanceDiffs appends each balance assertion's non-zero difference,
// its account's actual amount less the expected one, as a "Diff" comment,
// like beancount prints a failed assertion.
func WithBalanceDiffs(diffs map[*ast.Balance]decimal.Decimal) Option {
	return func(p *printer) {
		p.diffs = diffs
	}
}

type printer struct {
	booked func(*ast.Posting) []ledger.BookedPosition
	diffs  map[*ast.Balance]decimal.Decimal
}

func newPrinter(opts []Option) *printer {
	p := &printer{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Print writes directives like beancount's print_entries: a blank line
// precedes every transaction and commodity, and every directive of another
// kind than the one printed before it.
func Print(ctx context.Context, w io.Writer, directives []ast.Directive, opts ...Option) error {
	timer := telemetry.FromContext(ctx).Start("printer.print")
	defer timer.End()

	p := newPrinter(opts)
	var buf strings.Builder
	var previous ast.DirectiveKind
	for i, directive := range directives {
		if err := ctx.Err(); err != nil {
			return err
		}
		kind := directive.Kind()
		if kind == ast.KindTransaction || kind == ast.KindCommodity || i > 0 && kind != previous {
			buf.WriteByte('\n')
		}
		previous = kind
		p.write(directive, &buf)
		if _, err := io.WriteString(w, buf.String()); err != nil {
			return err
		}
		buf.Reset()
	}
	return nil
}

// Sprint renders one directive like beancount's format_entry, ending in a
// newline.
func Sprint(directive ast.Directive, opts ...Option) string {
	var buf strings.Builder
	newPrinter(opts).write(directive, &buf)
	return buf.String()
}

// writer renders one kind of directive.
type writer func(p *printer, directive ast.Directive, buf *strings.Builder)

// entry adapts a method rendering one directive type to a writer.
func entry[T ast.Directive](write func(*printer, T, *strings.Builder)) writer {
	return func(p *printer, directive ast.Directive, buf *strings.Builder) {
		write(p, directive.(T), buf)
	}
}

// writers maps each directive kind to its rendering, like the methods of
// beancount's EntryPrinter.
var writers = map[ast.DirectiveKind]writer{
	ast.KindTransaction: entry((*printer).transaction),
	ast.KindBalance:     entry((*printer).balance),
	ast.KindNote:        entry((*printer).note),
	ast.KindDocument:    entry((*printer).document),
	ast.KindPad:         entry((*printer).pad),
	ast.KindOpen:        entry((*printer).open),
	ast.KindClose:       entry((*printer).close),
	ast.KindCommodity:   entry((*printer).commodity),
	ast.KindPrice:       entry((*printer).price),
	ast.KindEvent:       entry((*printer).event),
	ast.KindQuery:       entry((*printer).query),
	ast.KindCustom:      entry((*printer).custom),
}

func (p *printer) write(directive ast.Directive, buf *strings.Builder) {
	if write, ok := writers[directive.Kind()]; ok {
		write(p, directive, buf)
	}
}

// metadataIndent and postingMetadataIndent are the prefixes of a
// directive's and a posting's metadata lines.
const (
	metadataIndent        = "  "
	postingMetadataIndent = "    "
)

func (p *printer) transaction(t *ast.Transaction, buf *strings.Builder) {
	strs := make([]string, 0, 2)
	if !t.Payee.IsEmpty() {
		strs = append(strs, quote(t.Payee.Value))
	}
	if !t.Narration.IsEmpty() {
		strs = append(strs, quote(t.Narration.Value))
	} else if !t.Payee.IsEmpty() {
		strs = append(strs, `""`)
	}
	for _, tag := range sortedUnique(t.AllTags()) {
		strs = append(strs, "#"+string(tag))
	}
	for _, link := range sortedUnique(t.AllLinks()) {
		strs = append(strs, "^"+string(link))
	}

	// The strings follow a space even when there are none, as in beancount.
	buf.WriteString(t.Date().String())
	buf.WriteByte(' ')
	buf.WriteString(t.Flag)
	buf.WriteByte(' ')
	buf.WriteString(strings.Join(strs, " "))
	buf.WriteByte('\n')
	writeMetadata(t.Metadata, metadataIndent, buf)

	var rows []postingRow
	for _, posting := range t.Postings {
		rows = p.appendPostingRows(rows, posting)
	}
	accountWidth := 0
	positions := make([]string, len(rows))
	for i, row := range rows {
		accountWidth = max(accountWidth, width(row.account))
		positions[i] = row.position
	}
	positions = alignPositions(positions)

	// Like beancount, a line is padded to the widest account and position,
	// then right-trimmed.
	for i, row := range rows {
		line := "  " + padRight(row.account, accountWidth) + "  " + positions[i]
		buf.WriteString(strings.TrimRight(line, " "))
		buf.WriteByte('\n')
		writeMetadata(row.metadata, postingMetadataIndent, buf)
	}
}

// postingRow is one printed posting: its flag and account, its position
// (units, cost and price) and its metadata.
type postingRow struct {
	account, position string
	metadata          []*ast.Metadata
}

// appendPostingRows appends a posting's rows: one per booked position, or
// the posting as parsed when it has none.
func (p *printer) appendPostingRows(rows []postingRow, posting *ast.Posting) []postingRow {
	account := string(posting.Account)
	if posting.Flag != "" {
		account = posting.Flag + " " + account
	}
	price := priceText(posting)

	var booked []ledger.BookedPosition
	if p.booked != nil && posting.Amount != nil {
		booked = p.booked(posting)
	}
	if len(booked) == 0 {
		return append(rows, postingRow{account: account, position: unitsText(posting.Amount) + costSpecText(posting.Cost) + price, metadata: posting.Metadata})
	}
	for _, position := range booked {
		text := numberText(position.Units) + " " + posting.Amount.Currency + bookedCostText(position.Cost) + price
		rows = append(rows, postingRow{account: account, position: text, metadata: posting.Metadata})
	}
	return rows
}

// unitsText spells a posting's units, leaving out a missing number or
// currency.
func unitsText(amount *ast.Amount) string {
	if amount == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if amount.Value != "" {
		parts = append(parts, parsedNumber(amount.Value))
	}
	if amount.Currency != "" {
		parts = append(parts, amount.Currency)
	}
	return strings.Join(parts, " ")
}

// priceText spells a posting's price as beancount's parser keeps it, per
// unit; a price it cannot spread over the units is spelled as parsed.
func priceText(posting *ast.Posting) string {
	if posting.Price == nil {
		return ""
	}
	if number, currency, ok := ledger.PerUnitPrice(posting); ok {
		return " @ " + amountText(numberText(number), currency)
	}
	marker := " @"
	if posting.PriceTotal {
		marker = " @@"
	}
	if text := unitsText(posting.Price); text != "" {
		return marker + " " + text
	}
	return marker
}

// costSpecText spells a posting's cost as parsed, like beancount prints a
// cost spec: a total cost {{N CUR}} is the compound {0 # N CUR}, and a
// currency without a number is left out.
func costSpecText(cost *ast.Cost) string {
	if cost == nil {
		return ""
	}
	parts := make([]string, 0, 4)

	var perUnit, total, currency string
	if cost.Amount != nil {
		currency = cost.Amount.Currency
		if cost.Amount.Value != "" {
			perUnit = parsedNumber(cost.Amount.Value)
		}
	}
	if cost.IsTotal && perUnit != "" {
		perUnit, total = "0", perUnit
	}
	if cost.Total != nil {
		if currency == "" {
			currency = cost.Total.Currency
		}
		if cost.Total.Value != "" {
			total = parsedNumber(cost.Total.Value)
		}
	}
	if perUnit != "" || total != "" {
		amount := make([]string, 0, 4)
		if perUnit != "" {
			amount = append(amount, perUnit)
		}
		if total != "" {
			amount = append(amount, "#", total)
		}
		if currency != "" {
			amount = append(amount, currency)
		}
		parts = append(parts, strings.Join(amount, " "))
	}

	if cost.Date != nil {
		parts = append(parts, cost.Date.String())
	}
	if cost.Label != "" {
		parts = append(parts, `"`+cost.Label+`"`)
	}
	if cost.IsMerge {
		parts = append(parts, "*")
	}
	return " {" + strings.Join(parts, ", ") + "}"
}

// bookedCostText spells a booked position's lot: per-unit number and
// currency, date and label.
func bookedCostText(cost *ledger.BookedCost) string {
	if cost == nil {
		return ""
	}
	parts := []string{amountText(numberText(cost.Number), cost.Currency)}
	if cost.Date != nil {
		parts = append(parts, cost.Date.String())
	}
	if cost.Label != "" {
		parts = append(parts, `"`+cost.Label+`"`)
	}
	return " {" + strings.Join(parts, ", ") + "}"
}

// alignPositions pads position strings so that their first currency (the
// first uppercase letter) lines up, like beancount's
// align_position_strings. A string without one, or starting with one, is
// padded on the right.
func alignPositions(positions []string) []string {
	maxBefore, maxAfter, maxUnknown := 0, 0, 0
	splits := make([]int, len(positions))
	for i, position := range positions {
		split := strings.IndexFunc(position, func(r rune) bool { return 'A' <= r && r <= 'Z' })
		if split > 0 {
			before := width(position[:split])
			maxBefore = max(maxBefore, before)
			maxAfter = max(maxAfter, width(position)-before)
		} else {
			split = -1
			maxUnknown = max(maxUnknown, width(position))
		}
		splits[i] = split
	}

	total := max(maxBefore+maxAfter, maxUnknown)
	aligned := make([]string, len(positions))
	for i, position := range positions {
		if split := splits[i]; split > 0 {
			aligned[i] = padLeft(position[:split], maxBefore) + padRight(position[split:], total-maxBefore)
		} else {
			aligned[i] = padRight(position, total)
		}
	}
	return aligned
}

func (p *printer) balance(b *ast.Balance, buf *strings.Builder) {
	currency := ""
	if b.Amount != nil {
		currency = b.Amount.Currency
	}
	if currency == "" && b.Tolerance != nil {
		currency = b.Tolerance.Currency
	}

	buf.WriteString(b.Date().String())
	buf.WriteString(" balance ")
	buf.WriteString(padRight(string(b.Account), 47))
	buf.WriteByte(' ')
	if b.Amount != nil {
		buf.WriteString(parsedNumber(b.Amount.Value))
	}
	buf.WriteByte(' ')
	if b.Tolerance != nil {
		if tolerance, err := decimal.NewFromString(b.Tolerance.Value); err != nil || !tolerance.IsZero() {
			buf.WriteString("~ ")
			buf.WriteString(parsedNumber(b.Tolerance.Value))
			buf.WriteByte(' ')
		}
	}
	buf.WriteString(currency)
	if diff, ok := p.diffs[b]; ok && !diff.IsZero() {
		buf.WriteString("   ; Diff: ")
		buf.WriteString(amountText(numberText(diff), currency))
	}
	buf.WriteByte('\n')
	writeMetadata(b.Metadata, metadataIndent, buf)
}

func (p *printer) note(n *ast.Note, buf *strings.Builder) {
	buf.WriteString(n.Date().String() + " note " + string(n.Account) + ` "` + n.Description.Value + "\"\n")
	writeMetadata(n.Metadata, metadataIndent, buf)
}

func (p *printer) document(d *ast.Document, buf *strings.Builder) {
	buf.WriteString(d.Date().String() + " document " + string(d.Account) + ` "` + d.PathToDocument.Value + `"`)
	if len(d.Tags) > 0 || len(d.Links) > 0 {
		buf.WriteByte(' ')
		for _, tag := range sortedUnique(d.Tags) {
			buf.WriteString("#" + string(tag))
		}
		for _, link := range sortedUnique(d.Links) {
			buf.WriteString("^" + string(link))
		}
	}
	buf.WriteByte('\n')
	writeMetadata(d.Metadata, metadataIndent, buf)
}

func (p *printer) pad(d *ast.Pad, buf *strings.Builder) {
	buf.WriteString(d.Date().String() + " pad " + string(d.Account) + " " + string(d.AccountPad) + "\n")
	writeMetadata(d.Metadata, metadataIndent, buf)
}

func (p *printer) open(o *ast.Open, buf *strings.Builder) {
	booking := ""
	if o.BookingMethod != "" {
		booking = `"` + o.BookingMethod + `"`
	}
	line := o.Date().String() + " open " + padRight(string(o.Account), 47) + " " + strings.Join(o.ConstraintCurrencies, ",") + " " + booking
	buf.WriteString(strings.TrimRight(line, " "))
	buf.WriteByte('\n')
	writeMetadata(o.Metadata, metadataIndent, buf)
}

func (p *printer) close(c *ast.Close, buf *strings.Builder) {
	buf.WriteString(c.Date().String() + " close " + string(c.Account) + "\n")
	writeMetadata(c.Metadata, metadataIndent, buf)
}

func (p *printer) commodity(c *ast.Commodity, buf *strings.Builder) {
	buf.WriteString(c.Date().String() + " commodity " + c.Currency + "\n")
	writeMetadata(c.Metadata, metadataIndent, buf)
}

func (p *printer) price(d *ast.Price, buf *strings.Builder) {
	amount := unitsText(d.Amount)
	buf.WriteString(d.Date().String() + " price " + padRight(d.Commodity, 22) + " " + padLeft(amount, 22) + "\n")
	writeMetadata(d.Metadata, metadataIndent, buf)
}

func (p *printer) event(e *ast.Event, buf *strings.Builder) {
	buf.WriteString(e.Date().String() + ` event "` + e.Name.Value + `" "` + e.Value.Value + "\"\n")
	writeMetadata(e.Metadata, metadataIndent, buf)
}

func (p *printer) query(q *ast.Query, buf *strings.Builder) {
	buf.WriteString(q.Date().String() + ` query "` + q.Name.Value + `" "` + q.QueryString.Value + "\"\n")
	writeMetadata(q.Metadata, metadataIndent, buf)
}

func (p *printer) custom(c *ast.Custom, buf *strings.Builder) {
	values := make([]string, 0, len(c.Values))
	for _, value := range c.Values {
		switch {
		case value.String != nil:
			values = append(values, `"`+*value.String+`"`)
		case value.Date != nil:
			values = append(values, value.Date.String())
		case value.BooleanValue != nil:
			values = append(values, *value.BooleanValue)
		case value.Amount != nil:
			values = append(values, unitsText(value.Amount))
		case value.Number != nil:
			values = append(values, parsedNumber(*value.Number))
		}
	}
	// The values follow a space even when there are none, as in beancount.
	buf.WriteString(c.Date().String() + ` custom "` + c.Type.Value + `" ` + strings.Join(values, " ") + "\n")
	writeMetadata(c.Metadata, metadataIndent, buf)
}

// writeMetadata writes metadata lines, like beancount's write_metadata: a
// string, account, currency, tag or link value is a quoted string, and a
// missing value is left empty after "key: ".
func writeMetadata(metadata []*ast.Metadata, indent string, buf *strings.Builder) {
	for _, m := range metadata {
		buf.WriteString(indent)
		buf.WriteString(m.Key)
		buf.WriteString(": ")
		buf.WriteString(metadataValueText(m.Value))
		buf.WriteByte('\n')
	}
}

func metadataValueText(value *ast.MetadataValue) string {
	switch {
	case value == nil:
		return ""
	case value.StringValue != nil:
		return quote(value.StringValue.Value)
	case value.Account != nil:
		return quote(string(*value.Account))
	case value.Currency != nil:
		return quote(*value.Currency)
	case value.Tag != nil:
		return quote(string(*value.Tag))
	case value.Link != nil:
		return quote(string(*value.Link))
	case value.Date != nil:
		return value.Date.String()
	case value.Number != nil:
		return parsedNumber(*value.Number)
	case value.Amount != nil:
		return unitsText(value.Amount)
	case value.Boolean != nil:
		if *value.Boolean {
			return "TRUE"
		}
		return "FALSE"
	}
	return ""
}

// escaper escapes backslashes and double quotes like beancount's
// escape_string.
var escaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// quote quotes a string, escaped like beancount's escape_string.
func quote(s string) string {
	return `"` + escaper.Replace(s) + `"`
}

// parsedNumber spells a number as parsed: without the source's thousands
// separators or plus sign. A value that is not a number is kept as is.
func parsedNumber(value string) string {
	number, err := decimal.NewFromString(value)
	if err != nil {
		return value
	}
	return numberText(number)
}

// numberText spells a number like Python's format(Decimal, "f"), which is
// how beancount prints numbers without a display context: every digit of
// its exponent, and no exponent notation.
func numberText(number decimal.Decimal) string {
	return number.StringFixed(max(-number.Exponent(), 0))
}

// amountText joins a number and its currency, leaving out a missing
// currency.
func amountText(number, currency string) string {
	if currency == "" {
		return number
	}
	return number + " " + currency
}

// sortedUnique returns the sorted distinct values, like beancount's sorted
// tag and link sets.
func sortedUnique[S ~string](values []S) []S {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

// width counts characters, as Python's str.format pads.
func width(s string) int {
	return utf8.RuneCountInString(s)
}

func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(n-width(s), 0))
}

func padLeft(s string, n int) string {
	return strings.Repeat(" ", max(n-width(s), 0)) + s
}
