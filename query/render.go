package query

import (
	"fmt"
	"io"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// RenderText writes a result as bean-query's default text table: headers
// centered and truncated to the data width, a dashed rule, and per-type
// value alignment.
func RenderText(result *Result, w io.Writer) error {
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
		renderers[i] = newRenderer(col.Type, forCSV, result.Display)
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
// order of first appearance. Numbers are rounded to their currency's display
// precision, like bean-query's numberify.
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
						if result.Display != nil {
							number = result.Display.Quantize(number, currency)
						}
						values[mapping[i]+offset] = number
					}
				}
			} else {
				values[mapping[i]] = value
			}
		}
		rows[r] = values
	}

	return &Result{Columns: columns, Rows: rows, Display: result.Display}
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

func newRenderer(t DType, forCSV bool, display *ledger.DisplayContext) columnRenderer {
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
		return &decimalRenderer{numbers: newNumberField(display)}
	case TAmount:
		return &amountRenderer{amounts: amountField{numbers: newNumberField(display)}}
	case TPosition, TInventory:
		return &positionRenderer{
			units: amountField{numbers: newNumberField(display)},
			costs: amountField{numbers: newNumberField(display)},
		}
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

// integralWidth sizes an integer field like bean-query: one sign column
// when any value is negative, plus the most integer digits of any value.
// Positive values are padded into the sign column, so 10 and -1 render as
// " 10" and " -1".
type integralWidth struct {
	negative bool
	digits   int
}

// observe records the integer part of a formatted number, e.g. "-12".
func (w *integralWidth) observe(intPart string) {
	if digits, ok := strings.CutPrefix(intPart, "-"); ok {
		w.negative = true
		intPart = digits
	}
	w.digits = max(w.digits, len(intPart))
}

func (w *integralWidth) width() int {
	if w.negative {
		return w.digits + 1
	}
	return w.digits
}

type intRenderer struct {
	integral integralWidth
}

func (r *intRenderer) prepare(v any) {
	if n, ok := v.(int64); ok {
		r.integral.observe(fmt.Sprintf("%d", n))
	}
}

func (r *intRenderer) width() int { return max(r.integral.width(), 1) }

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

// decimalRenderer renders a plain decimal column. Its numbers have no
// currency, so they are laid out at their own precision.
type decimalRenderer struct {
	numbers *numberField
}

func (r *decimalRenderer) prepare(v any) {
	if d, ok := v.(decimal.Decimal); ok {
		r.numbers.observe(d, "")
	}
}

func (r *decimalRenderer) width() int {
	return max(r.numbers.width(), 1)
}

func (r *decimalRenderer) format(v any) string {
	d, ok := v.(decimal.Decimal)
	if !ok {
		return strings.Repeat(" ", r.width())
	}
	return padRight(r.numbers.format(d), r.width())
}

func decimalParts(d decimal.Decimal) (string, string) {
	scale := max(-d.Exponent(), 0)
	s := d.StringFixed(scale)
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

// numberField lays out a column of numbers like bean-query's
// DecimalRenderer. Each number is quantized to its currency's display
// precision only to size the field: a sign column when any value is
// negative, the most integer digits, and the widest per-currency most common
// count of fractional digits. Numbers then print as written, aligned at the
// decimal point and cut to the field width, so 2500.00 in a column of whole
// numbers renders as 2500.
type numberField struct {
	display  *ledger.DisplayContext
	integral integralWidth
	digits   map[string]map[int32]int // currency -> fractional digits -> count
	fracW    int                      // widest fraction incl. the dot
	finished bool
}

func newNumberField(display *ledger.DisplayContext) *numberField {
	return &numberField{display: display, digits: make(map[string]map[int32]int)}
}

// observe records a number of the given currency ("" for plain decimals).
func (f *numberField) observe(number decimal.Decimal, currency string) {
	if f.display != nil {
		number = f.display.Quantize(number, currency)
	}
	intPart, _ := decimalParts(number)
	f.integral.observe(intPart)

	counts, ok := f.digits[currency]
	if !ok {
		counts = make(map[int32]int)
		f.digits[currency] = counts
	}
	counts[max(-number.Exponent(), 0)]++
}

func (f *numberField) width() int {
	if !f.finished {
		for _, counts := range f.digits {
			if digits := int(mostCommonDigits(counts)); digits > 0 {
				f.fracW = max(f.fracW, 1+digits)
			}
		}
		f.finished = true
	}
	return f.integral.width() + f.fracW
}

func (f *numberField) format(number decimal.Decimal) string {
	intPart, fracPart := decimalParts(number)
	s := padLeft(intPart, f.integral.width())
	if fracPart != "" {
		s += "." + fracPart
	}
	return padRight(truncate(s, f.width()), f.width())
}

// mostCommonDigits returns the most frequent fractional digit count,
// preferring more digits on a tie, like beancount's Distribution.mode.
func mostCommonDigits(counts map[int32]int) int32 {
	var digits int32
	best := 0
	for d, count := range counts {
		if count > best || (count == best && d > digits) {
			digits, best = d, count
		}
	}
	return digits
}

// amountField renders "number CURRENCY" with the numbers aligned and the
// currencies padded, like bean-query's AmountRenderer.
type amountField struct {
	numbers *numberField
	curW    int
}

func (a *amountField) observe(number decimal.Decimal, currency string) {
	a.numbers.observe(number, currency)
	a.curW = max(a.curW, len(currency))
}

// width is 0 when no amount was observed.
func (a *amountField) width() int {
	if a.curW == 0 {
		return 0
	}
	return a.numbers.width() + 1 + a.curW
}

func (a *amountField) format(number decimal.Decimal, currency string) string {
	return a.numbers.format(number) + " " + padRight(currency, a.curW)
}

// amountRenderer renders amount columns.
type amountRenderer struct {
	amounts amountField
}

func (r *amountRenderer) prepare(v any) {
	if a, ok := v.(*Amount); ok && a != nil {
		r.amounts.observe(a.Number, a.Currency)
	}
}

func (r *amountRenderer) width() int {
	return max(r.amounts.width(), 1)
}

func (r *amountRenderer) format(v any) string {
	a, ok := v.(*Amount)
	if !ok || a == nil {
		return strings.Repeat(" ", r.width())
	}
	return padRight(r.amounts.format(a.Number, a.Currency), r.width())
}

// positionRenderer renders positions and inventories like bean-query's
// PositionRenderer: aligned units, then the cost as a second aligned amount
// in braces (its date and label are not shown). Inventories join their
// positions with ", ".
type positionRenderer struct {
	units amountField
	costs amountField
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

func (r *positionRenderer) prepare(v any) {
	for _, p := range r.positions(v) {
		r.units.observe(p.Units.Number, p.Units.Currency)
		if p.Cost != nil {
			r.costs.observe(p.Cost.Number, p.Cost.Currency)
		}
	}
}

// subWidth is the fixed width of one rendered position.
func (r *positionRenderer) subWidth() int {
	w := r.units.width()
	if costW := r.costs.width(); costW > 0 {
		w += 1 + costW + 2
	}
	return w
}

func (r *positionRenderer) width() int {
	return max(r.subWidth(), 1)
}

func (r *positionRenderer) formatPosition(p *Position) string {
	s := r.units.format(p.Units.Number, p.Units.Currency)
	if p.Cost != nil {
		s += " {" + r.costs.format(p.Cost.Number, p.Cost.Currency) + "}"
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
