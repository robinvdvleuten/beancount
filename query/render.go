package query

import (
	"fmt"
	"io"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// RenderText writes a result as bean-query's default text table: headers
// centered and truncated to the data width, a dashed rule, and per-type
// value alignment. Empty results render as "(empty)".
func RenderText(result *Result, w io.Writer) error {
	if len(result.Rows) == 0 {
		_, err := io.WriteString(w, "(empty)\n")
		return err
	}

	renderers := prepareRenderers(result, false)

	var b strings.Builder
	for i, col := range result.Columns {
		if i > 0 {
			b.WriteByte(' ')
		}
		width := renderers[i].width()
		name := col.Name
		if columnAllNull(result, i) {
			// Columns with only NULL values render a blank header in the
			// official output.
			name = ""
		}
		b.WriteString(center(truncate(name, width), width))
	}
	b.WriteByte('\n')

	for i := range result.Columns {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strings.Repeat("-", renderers[i].width()))
	}
	b.WriteByte('\n')

	for _, row := range result.Rows {
		for i, value := range row {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(renderers[i].format(value))
		}
		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// RenderCSV writes a result as bean-query's CSV output: full column names in
// the header, width-padded cells (an official quirk), and CRLF line endings.
// With numberify, amount-bearing columns split into one numeric column per
// currency.
func RenderCSV(result *Result, w io.Writer, numberify bool) error {
	if numberify {
		result = numberifyResult(result)
	}
	renderers := prepareRenderers(result, true)

	var b strings.Builder
	header := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		header[i] = col.Name
	}
	writeCSVRecord(&b, header)

	record := make([]string, len(result.Columns))
	for _, row := range result.Rows {
		for i, value := range row {
			record[i] = renderers[i].format(value)
		}
		writeCSVRecord(&b, record)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeCSVRecord writes one CSV record with Python's QUOTE_MINIMAL rules:
// fields are quoted only when they contain a separator, quote, or newline.
// Go's encoding/csv also quotes leading spaces, which would break parity
// with the official output's padded cells.
func writeCSVRecord(b *strings.Builder, fields []string) {
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		if strings.ContainsAny(field, ",\"\r\n") {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(field, `"`, `""`))
			b.WriteByte('"')
		} else {
			b.WriteString(field)
		}
	}
	b.WriteString("\r\n")
}

func columnAllNull(result *Result, col int) bool {
	for _, row := range result.Rows {
		if row[col] != nil {
			return false
		}
	}
	return true
}

func prepareRenderers(result *Result, forCSV bool) []columnRenderer {
	renderers := make([]columnRenderer, len(result.Columns))
	for i, col := range result.Columns {
		renderers[i] = newRenderer(col.Type, forCSV)
	}
	for _, row := range result.Rows {
		for i, value := range row {
			renderers[i].prepare(value)
		}
	}
	return renderers
}

// numberifyResult splits amount, position, and inventory columns into one
// decimal column per currency, named "column (CUR)", with currencies in
// order of first appearance.
func numberifyResult(result *Result) *Result {
	type split struct {
		column     int
		currencies []string
		index      map[string]int
	}

	var columns []ResultColumn
	splits := make(map[int]*split)
	mapping := make([]int, 0, len(result.Columns)) // start index of each source column

	for i, col := range result.Columns {
		mapping = append(mapping, len(columns))
		switch col.Type {
		case TAmount, TPosition, TInventory:
			s := &split{column: i, index: make(map[string]int)}
			for _, row := range result.Rows {
				for _, currency := range valueCurrencies(row[i]) {
					if _, ok := s.index[currency]; !ok {
						s.index[currency] = len(s.currencies)
						s.currencies = append(s.currencies, currency)
					}
				}
			}
			splits[i] = s
			for _, currency := range s.currencies {
				columns = append(columns, ResultColumn{
					Name: fmt.Sprintf("%s (%s)", col.Name, currency),
					Type: TDecimal,
				})
			}
			if len(s.currencies) == 0 {
				// Keep a single empty column so the header survives.
				columns = append(columns, ResultColumn{Name: col.Name, Type: TDecimal})
			}
		default:
			columns = append(columns, col)
		}
	}

	rows := make([][]any, len(result.Rows))
	for r, row := range result.Rows {
		values := make([]any, len(columns))
		for i, value := range row {
			if s, ok := splits[i]; ok {
				for currency, offset := range s.index {
					if number, ok := currencyNumber(value, currency); ok {
						values[mapping[i]+offset] = number
					}
				}
			} else {
				values[mapping[i]] = value
			}
		}
		rows[r] = values
	}

	// Quantize each currency column to its maximum scale so 1000 and 4.50
	// render as 1000.00 and 4.50, matching the official per-currency
	// precision.
	for _, s := range splits {
		for _, offset := range s.index {
			col := mapping[s.column] + offset
			var scale int32
			for _, row := range rows {
				if number, ok := row[col].(decimal.Decimal); ok {
					scale = max(scale, -number.Exponent())
				}
			}
			for _, row := range rows {
				if number, ok := row[col].(decimal.Decimal); ok {
					row[col] = number.Round(scale)
				}
			}
		}
	}
	return &Result{Columns: columns, Rows: rows}
}

// valueCurrencies lists the currencies present in an amount-bearing value.
func valueCurrencies(v any) []string {
	switch val := v.(type) {
	case *Amount:
		return []string{val.Currency}
	case *Position:
		return []string{val.Units.Currency}
	case *Inventory:
		var currencies []string
		seen := make(map[string]bool)
		for _, p := range val.Positions() {
			if !seen[p.Units.Currency] {
				seen[p.Units.Currency] = true
				currencies = append(currencies, p.Units.Currency)
			}
		}
		return currencies
	}
	return nil
}

// currencyNumber extracts the units number of one currency from an
// amount-bearing value, summing inventory lots.
func currencyNumber(v any, currency string) (decimal.Decimal, bool) {
	switch val := v.(type) {
	case *Amount:
		if val.Currency == currency {
			return val.Number, true
		}
	case *Position:
		if val.Units.Currency == currency {
			return val.Units.Number, true
		}
	case *Inventory:
		total := decimal.Decimal{}
		found := false
		for _, p := range val.Positions() {
			if p.Units.Currency == currency {
				total = total.Add(p.Units.Number)
				found = true
			}
		}
		if found {
			return total, true
		}
	}
	return decimal.Decimal{}, false
}

// columnRenderer accumulates layout information over a column's values in
// prepare, then formats each value padded to the column width.
type columnRenderer interface {
	prepare(v any)
	width() int
	format(v any) string
}

func newRenderer(t DType, forCSV bool) columnRenderer {
	switch t {
	case TAny:
		// Object-typed columns are width-padded in text but written raw in
		// CSV, matching the official renderer.
		return &stringRenderer{unpadded: forCSV}
	case TDate:
		return &dateRenderer{}
	case TInt:
		return &intRenderer{}
	case TBool:
		return &boolRenderer{}
	case TDecimal:
		return &decimalRenderer{}
	case TAmount:
		return &amountRenderer{numbers: newNumberField()}
	case TPosition, TInventory:
		return &positionRenderer{numbers: newNumberField()}
	default:
		return &stringRenderer{}
	}
}

func truncate(s string, width int) string {
	if len(s) > width {
		return s[:width]
	}
	return s
}

// center pads s to width with the extra space on the right, like Python's
// str.center used by the official renderer.
func center(s string, width int) string {
	total := width - len(s)
	if total <= 0 {
		return s
	}
	left := total / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// stringRenderer renders strings, sets, and polymorphic values left-aligned.
type stringRenderer struct {
	w        int
	unpadded bool
}

func (r *stringRenderer) toString(v any) string {
	if v == nil {
		return ""
	}
	if set, ok := v.(Set); ok {
		return strings.Join(set.Sorted(), ",")
	}
	return valueString(v)
}

func (r *stringRenderer) prepare(v any) {
	if n := len(r.toString(v)); n > r.w {
		r.w = n
	}
}

func (r *stringRenderer) width() int { return max(r.w, 1) }

func (r *stringRenderer) format(v any) string {
	if r.unpadded {
		return r.toString(v)
	}
	return padRight(r.toString(v), r.width())
}

type dateRenderer struct{}

func (r *dateRenderer) prepare(any) {}
func (r *dateRenderer) width() int  { return 10 }

func (r *dateRenderer) format(v any) string {
	if date, ok := v.(*ast.Date); ok && date != nil {
		return date.String()
	}
	return strings.Repeat(" ", 10)
}

type intRenderer struct {
	w int
}

func (r *intRenderer) prepare(v any) {
	if n, ok := v.(int64); ok {
		if width := len(fmt.Sprintf("%d", n)); width > r.w {
			r.w = width
		}
	}
}

func (r *intRenderer) width() int { return max(r.w, 1) }

func (r *intRenderer) format(v any) string {
	if n, ok := v.(int64); ok {
		return padLeft(fmt.Sprintf("%d", n), r.width())
	}
	return strings.Repeat(" ", r.width())
}

// boolRenderer renders TRUE/FALSE with the official fixed width of 5.
type boolRenderer struct{}

func (r *boolRenderer) prepare(any) {}
func (r *boolRenderer) width() int  { return 5 }

func (r *boolRenderer) format(v any) string {
	b, ok := v.(bool)
	if !ok {
		return strings.Repeat(" ", 5)
	}
	if b {
		return padRight("TRUE", 5)
	}
	return "FALSE"
}

// decimalRenderer aligns numbers at the decimal point, preserving each
// value's own digits (space-padded, not zero-padded).
type decimalRenderer struct {
	intW  int
	fracW int
}

func decimalParts(d decimal.Decimal) (string, string) {
	scale := max(-d.Exponent(), 0)
	s := d.StringFixed(scale)
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func (r *decimalRenderer) prepare(v any) {
	d, ok := v.(decimal.Decimal)
	if !ok {
		return
	}
	intPart, fracPart := decimalParts(d)
	r.intW = max(r.intW, len(intPart))
	r.fracW = max(r.fracW, len(fracPart))
}

func (r *decimalRenderer) width() int {
	w := r.intW
	if r.fracW > 0 {
		w += 1 + r.fracW
	}
	return max(w, 1)
}

func (r *decimalRenderer) format(v any) string {
	d, ok := v.(decimal.Decimal)
	if !ok {
		return strings.Repeat(" ", r.width())
	}
	intPart, fracPart := decimalParts(d)
	s := padLeft(intPart, r.intW)
	if r.fracW > 0 {
		if fracPart != "" {
			s += "." + fracPart
		}
		s = padRight(s, r.width())
	}
	return s
}

// numberField aligns amount numbers at the decimal point with per-currency
// precision: each currency's numbers are quantized to the maximum scale seen
// for that currency, and the fraction field is space-padded to the widest
// currency's scale.
type numberField struct {
	intW     int
	fracW    int // widest fraction incl. the dot
	currFrac map[string]int
}

func newNumberField() *numberField {
	return &numberField{currFrac: make(map[string]int)}
}

func (f *numberField) observe(number decimal.Decimal, currency string) {
	scale := int(max(-number.Exponent(), 0))
	f.currFrac[currency] = max(f.currFrac[currency], scale)
	intPart, _ := decimalParts(number)
	f.intW = max(f.intW, len(intPart))
}

// finish computes the fraction field width after all values are observed.
func (f *numberField) finish() {
	for _, scale := range f.currFrac {
		if scale > 0 {
			f.fracW = max(f.fracW, 1+scale)
		}
	}
}

func (f *numberField) width() int { return f.intW + f.fracW }

func (f *numberField) format(number decimal.Decimal, currency string) string {
	scale := f.currFrac[currency]
	s := number.StringFixed(int32(scale))
	var intPart, fracPart string
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		intPart, fracPart = s[:idx], s[idx+1:]
	} else {
		intPart = s
	}
	out := padLeft(intPart, f.intW)
	if fracPart != "" {
		out += "." + fracPart
	}
	return padRight(out, f.width())
}

// amountRenderer renders "number CURRENCY" with the number field aligned.
type amountRenderer struct {
	numbers  *numberField
	curW     int
	prepared bool
}

func (r *amountRenderer) prepare(v any) {
	if a, ok := v.(*Amount); ok && a != nil {
		r.numbers.observe(a.Number, a.Currency)
		r.curW = max(r.curW, len(a.Currency))
	}
}

func (r *amountRenderer) width() int {
	if !r.prepared {
		r.numbers.finish()
		r.prepared = true
	}
	if r.curW == 0 {
		return 1
	}
	return r.numbers.width() + 1 + r.curW
}

func (r *amountRenderer) format(v any) string {
	width := r.width()
	a, ok := v.(*Amount)
	if !ok || a == nil {
		return strings.Repeat(" ", width)
	}
	return padRight(r.numbers.format(a.Number, a.Currency)+" "+a.Currency, width)
}

// positionRenderer renders positions and inventories: each position is a
// fixed-width sub-cell (aligned number, padded currency, optional cost) and
// inventories join their positions with ", ".
type positionRenderer struct {
	numbers  *numberField
	curW     int
	costW    int
	cellW    int
	prepared bool
}

func (r *positionRenderer) positions(v any) []*Position {
	switch val := v.(type) {
	case *Position:
		if val != nil {
			return []*Position{val}
		}
	case *Inventory:
		if val != nil {
			return val.Positions()
		}
	}
	return nil
}

// costString renders a cost basis for position cells. The official renderer
// omits the booking-stamped cost date, showing only number, currency, and
// label.
func costString(c *Cost) string {
	var b strings.Builder
	b.WriteByte('{')
	b.WriteString(c.Number.StringFixed(max(-c.Number.Exponent(), 0)))
	b.WriteByte(' ')
	b.WriteString(c.Currency)
	if c.Label != "" {
		fmt.Fprintf(&b, ", %q", c.Label)
	}
	b.WriteByte('}')
	return b.String()
}

func (r *positionRenderer) prepare(v any) {
	for _, p := range r.positions(v) {
		r.numbers.observe(p.Units.Number, p.Units.Currency)
		r.curW = max(r.curW, len(p.Units.Currency))
		if p.Cost != nil {
			r.costW = max(r.costW, len(costString(p.Cost)))
		}
	}
}

// subWidth is the fixed width of one rendered position.
func (r *positionRenderer) subWidth() int {
	w := r.numbers.width() + 1 + r.curW
	if r.costW > 0 {
		w += 1 + r.costW
	}
	return w
}

func (r *positionRenderer) width() int {
	if !r.prepared {
		r.numbers.finish()
		r.prepared = true
		r.cellW = max(r.subWidth(), 1)
	}
	return r.cellW
}

func (r *positionRenderer) formatPosition(p *Position) string {
	s := r.numbers.format(p.Units.Number, p.Units.Currency) + " " + padRight(p.Units.Currency, r.curW)
	if p.Cost != nil {
		s += " " + costString(p.Cost)
	}
	return padRight(s, r.subWidth())
}

func (r *positionRenderer) format(v any) string {
	width := r.width()
	positions := r.positions(v)
	if len(positions) == 0 {
		return strings.Repeat(" ", width)
	}
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = r.formatPosition(p)
	}
	return padRight(strings.Join(parts, ", "), width)
}
