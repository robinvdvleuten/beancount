package query

import (
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// columnSeparator separates the columns of a text table; textListSep and
// csvListSep separate the elements of a set, or the positions of an
// inventory, in a text and a csv cell.
const (
	columnSeparator = "  "
	textListSep     = "  "
	csvListSep      = ","
)

// renderText writes a result as beanquery's default text table: each column
// as wide as its values, its header cut to that width and centered over a
// dashed rule, and cells padded to it. An empty result writes nothing.
func renderText(result *table, w io.Writer) error {
	if len(result.Rows) == 0 {
		return nil
	}
	renderers := prepareRenderers(result, textListSep)
	widths := make([]int, len(renderers))
	for i, r := range renderers {
		widths[i] = max(1, r.width())
	}

	var b strings.Builder
	cells := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		cells[i] = center(truncate(col.Name, widths[i]), widths[i])
	}
	writeTextRow(&b, cells)
	for i, width := range widths {
		cells[i] = strings.Repeat("-", width)
	}
	writeTextRow(&b, cells)

	for _, row := range result.Rows {
		for i, value := range row {
			cell := ""
			if value != nil {
				cell = renderers[i].format(value)
			}
			if renderers[i].rightAligned() {
				cells[i] = padLeft(cell, widths[i])
			} else {
				cells[i] = padRight(cell, widths[i])
			}
		}
		writeTextRow(&b, cells)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func writeTextRow(b *strings.Builder, cells []string) {
	for i, cell := range cells {
		if i > 0 {
			b.WriteString(columnSeparator)
		}
		b.WriteString(cell)
	}
	b.WriteByte('\n')
}

// renderCSV writes a result as beanquery's -f csv output: the full column
// names, then one record per row. Cells are formatted as for text, so
// number, amount, position and inventory cells keep the padding that aligns
// them, while other cells and NULLs are written as they are.
func renderCSV(result *table, w io.Writer) error {
	renderers := prepareRenderers(result, csvListSep)

	var b strings.Builder
	record := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		record[i] = col.Name
	}
	writeCSVRecord(&b, record)

	for _, row := range result.Rows {
		for i, value := range row {
			record[i] = ""
			if value != nil {
				record[i] = renderers[i].format(value)
			}
		}
		writeCSVRecord(&b, record)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeCSVRecord writes one CSV record like Python's csv.writer in its excel
// dialect: a field is quoted only when it holds a separator, quote or line
// break, and a record of one empty field is written as "" so it is not a
// blank line. Go's encoding/csv also quotes leading spaces, which would
// break parity with the padded cells.
func writeCSVRecord(b *strings.Builder, fields []string) {
	if len(fields) == 1 && fields[0] == "" {
		b.WriteString("\"\"\r\n")
		return
	}
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

// prepareRenderers builds a renderer per column and feeds it the column's
// non-NULL values, as beanquery primes its renderers.
func prepareRenderers(result *table, listSep string) []columnRenderer {
	ctx := &renderContext{display: result.Display, listSep: listSep}
	renderers := make([]columnRenderer, len(result.Columns))
	for i, col := range result.Columns {
		newRenderer, ok := columnRenderers[col.Type]
		if !ok {
			newRenderer = columnRenderers[tAny]
		}
		renderers[i] = newRenderer(ctx)
	}
	for _, row := range result.Rows {
		for i, value := range row {
			if value != nil {
				renderers[i].update(value)
			}
		}
	}
	for _, r := range renderers {
		r.prepare()
	}
	return renderers
}

// renderContext is what a column renderer is built with.
type renderContext struct {
	// display is the ledger's display context, which quantizes amounts;
	// nil leaves them as they are.
	display *ledger.DisplayContext
	listSep string
}

// columnRenderer renders one column like beanquery's ColumnRenderer: update
// sees every non-NULL value, prepare sizes the column, and format renders a
// non-NULL value, most renderers padded to width.
type columnRenderer interface {
	update(v any)
	prepare()
	width() int
	format(v any) string
	rightAligned() bool
}

// columnRenderers maps each column type to its renderer, like beanquery's
// RENDERERS.
var columnRenderers = map[dtype]func(ctx *renderContext) columnRenderer{
	tAny:      func(*renderContext) columnRenderer { return &strRenderer{str: objectString} },
	tInterval: func(*renderContext) columnRenderer { return &strRenderer{str: objectString} },
	tString:   func(*renderContext) columnRenderer { return &strRenderer{str: valueString} },
	tInt:      func(*renderContext) columnRenderer { return &strRenderer{str: valueString, right: true} },
	tBool:     func(*renderContext) columnRenderer { return &boolRenderer{} },
	tDate:     func(*renderContext) columnRenderer { return &dateRenderer{} },
	tSet:      func(ctx *renderContext) columnRenderer { return &setRenderer{sep: ctx.listSep} },
	tBooking: func(*renderContext) columnRenderer {
		return &strRenderer{str: func(v any) string { return string(v.(bookingValue)) }}
	},
	tAccountSet: func(ctx *renderContext) columnRenderer { return &setRenderer{sep: ctx.listSep} },
	tMetadata: func(*renderContext) columnRenderer {
		return &strRenderer{str: func(v any) string { return v.(*dictValue).metadataString() }}
	},
	tDecimal:   func(*renderContext) columnRenderer { return &decimalRenderer{} },
	tAmount:    func(ctx *renderContext) columnRenderer { return newAmountRenderer(ctx) },
	tPosition:  func(ctx *renderContext) columnRenderer { return newPositionRenderer(ctx) },
	tInventory: func(ctx *renderContext) columnRenderer { return newInventoryRenderer(ctx) },
	tCost:      func(ctx *renderContext) columnRenderer { return &costRenderer{amount: newAmountRenderer(ctx)} },
}

// costRenderer renders a cost column like beanquery's CostRenderer: its
// number and currency as an aligned amount, then its date and its quoted
// label, each after a comma.
type costRenderer struct {
	leftAligned
	amount     *amountRenderer
	dateWidth  int
	labelWidth int
}

func (r *costRenderer) update(v any) {
	cost, ok := v.(*costValue)
	if !ok || cost == nil {
		return
	}
	r.amount.observe(cost.Number, cost.Currency)
	if cost.Date != nil {
		r.dateWidth = 10 + 2
	}
	if cost.Label != "" {
		r.labelWidth = max(r.labelWidth, length(cost.Label)+4)
	}
}

func (r *costRenderer) prepare()   { r.amount.prepare() }
func (r *costRenderer) width() int { return r.amount.width() + r.dateWidth + r.labelWidth }

func (r *costRenderer) format(v any) string {
	cost, ok := v.(*costValue)
	if !ok || cost == nil {
		return ""
	}
	parts := []string{r.amount.formatAmount(cost.Number, cost.Currency)}
	if cost.Date != nil {
		parts = append(parts, cost.Date.String())
	}
	if cost.Label != "" {
		parts = append(parts, `"`+cost.Label+`"`)
	}
	return strings.Join(parts, ", ")
}

// Python measures and pads strings in code points, so these helpers do too.

func length(s string) int { return utf8.RuneCountInString(s) }

func truncate(s string, width int) string {
	if length(s) <= width {
		return s
	}
	return string([]rune(s)[:width])
}

// center pads s to width like Python's str.center: the extra space goes
// right, unless both the padding and the width are odd.
func center(s string, width int) string {
	total := width - length(s)
	if total <= 0 {
		return s
	}
	left := total/2 + (total & width & 1)
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", total-left)
}

func padRight(s string, width int) string {
	if n := length(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func padLeft(s string, width int) string {
	if n := length(s); n < width {
		return strings.Repeat(" ", width-n) + s
	}
	return s
}

// strRenderer renders a value as Python's str() spells it, like
// beanquery's ObjectRenderer and its string and int kin: the column is as
// wide as the widest value, and cells are not padded.
type strRenderer struct {
	str   func(any) string
	right bool
	w     int
}

func (r *strRenderer) update(v any)        { r.w = max(r.w, length(r.str(v))) }
func (r *strRenderer) prepare()            {}
func (r *strRenderer) width() int          { return r.w }
func (r *strRenderer) format(v any) string { return r.str(v) }
func (r *strRenderer) rightAligned() bool  { return r.right }

// leftAligned is embedded by the renderers whose text cells are padded on
// the right, all but int columns'.
type leftAligned struct{}

func (leftAligned) rightAligned() bool { return false }

// objectString renders a value like Python's str(), for object columns;
// beanquery's str() function is strValue. Numbers keep the digits they
// were written with (3.10, 10.50 USD), and an inventory is parenthesized
// like beancount's.
func objectString(v any) string {
	switch val := v.(type) {
	case nil:
		return "None"
	case decimal.Decimal:
		return decimalLiteral(val)
	case *amountValue:
		return decimalLiteral(val.Number) + " " + val.Currency
	case *positionValue:
		return positionString(val, decimalLiteral)
	case *inventoryValue:
		return "(" + inventoryString(val, decimalLiteral) + ")"
	}
	return valueString(v)
}

// boolRenderer renders TRUE and FALSE, the column 5 wide once it holds a
// FALSE and 4 otherwise.
type boolRenderer struct {
	leftAligned
	w int
}

func (r *boolRenderer) update(v any) {
	if b, _ := v.(bool); b {
		r.w = max(r.w, 4)
	} else {
		r.w = max(r.w, 5)
	}
}

func (r *boolRenderer) prepare()   {}
func (r *boolRenderer) width() int { return r.w }

func (r *boolRenderer) format(v any) string {
	if b, _ := v.(bool); b {
		return "TRUE"
	}
	return "FALSE"
}

type dateRenderer struct {
	leftAligned
	w int
}

func (r *dateRenderer) update(any) { r.w = 10 }
func (r *dateRenderer) prepare()   {}
func (r *dateRenderer) width() int { return r.w }
func (r *dateRenderer) format(v any) string {
	if date, ok := v.(*ast.Date); ok && date != nil {
		return date.String()
	}
	return ""
}

// setRenderer joins a set's elements, sorted, with the list separator.
type setRenderer struct {
	leftAligned
	sep string
	w   int
}

func (r *setRenderer) update(v any) {
	elems, _ := stringElements(v)
	w := -length(r.sep)
	for _, elem := range elems {
		w += length(elem) + length(r.sep)
	}
	r.w = max(r.w, w)
}

func (r *setRenderer) prepare()   {}
func (r *setRenderer) width() int { return r.w }

func (r *setRenderer) format(v any) string {
	elems, _ := stringElements(v)
	return strings.Join(elems, r.sep)
}

// coefficientDigits counts the digits of d's coefficient, as Python's
// Decimal.as_tuple() holds them.
func coefficientDigits(d decimal.Decimal) int {
	coefficient := d.Coefficient()
	return len(coefficient.Abs(coefficient).String())
}

// numberParts is what Python's Decimal holds of a number cell: its sign, the
// number of its coefficient's digits, its exponent, and its str().
type numberParts struct {
	sign, digits, exponent int
	str                    string
}

// numberCell reads a number cell: an int, a decimal, or the negative zero
// numberify gives a negative number that rounds to zero.
func numberCell(v any) (numberParts, bool) {
	negative := false
	if zero, ok := v.(negativeZero); ok {
		v, negative = zero.Decimal, true
	}
	d, ok := asDecimal(v)
	if !ok {
		return numberParts{}, false
	}
	n := numberParts{digits: coefficientDigits(d), exponent: int(d.Exponent()), str: pydecimal.String(d)}
	if negative || d.Sign() < 0 {
		n.sign = 1
		if !strings.HasPrefix(n.str, "-") {
			n.str = "-" + n.str
		}
	}
	return n, true
}

// decimalRenderer renders a number column like beanquery's DecimalRenderer:
// every number as Python's str() spells it, never rounded, aligned on its
// decimal point. A number with a positive exponent, which Python spells in
// scientific notation (2E+1), is right-aligned before the point instead.
type decimalRenderer struct {
	leftAligned
	integral   int // widest integer part, sign included
	fractional int // widest fractional part
	w          int
}

func (r *decimalRenderer) update(v any) {
	n, ok := numberCell(v)
	if !ok {
		return
	}
	if n.exponent > 0 {
		r.integral = max(r.integral, length(n.str))
		return
	}
	r.integral = max(r.integral, max(1, n.digits+n.exponent)+n.sign)
	r.fractional = max(r.fractional, -n.exponent)
}

func (r *decimalRenderer) prepare() {
	r.w = r.integral + r.fractional
	if r.fractional > 0 {
		r.w++
	}
}

func (r *decimalRenderer) width() int { return r.w }

func (r *decimalRenderer) format(v any) string {
	n, ok := numberCell(v)
	if !ok {
		return ""
	}
	if n.exponent > 0 {
		return padRight(padLeft(n.str, r.integral), r.w)
	}
	left := r.integral - (max(1, n.digits+n.exponent) + n.sign)
	return strings.Repeat(" ", max(left, 0)) + padRight(n.str, r.w-left)
}

// amountRenderer renders an amount column like beanquery's AmountRenderer:
// each number is quantized with the ledger's display context, then laid
// out by a display context of the column's own, as beancount builds it with
// dot alignment: a sign column that is always there, the widest integer
// part, and per currency the most common number of fractional digits,
// padded to the widest so the currencies line up.
type amountRenderer struct {
	leftAligned
	display   *ledger.DisplayContext
	integral  int                      // widest integer part
	fractions map[string]map[int32]int // currency -> fractional digits -> count
	currencyW int

	numberW   int            // the number's width, sign and padding included
	digits    map[string]int // currency -> fractional digits
	maxDigits int
	w         int
}

func newAmountRenderer(ctx *renderContext) *amountRenderer {
	return &amountRenderer{display: ctx.display, integral: 1, fractions: make(map[string]map[int32]int)}
}

func (r *amountRenderer) observe(number decimal.Decimal, currency string) {
	number = r.display.Quantize(number, currency)
	exponent := int(number.Exponent())
	r.integral = max(r.integral, coefficientDigits(number)+exponent)

	counts, ok := r.fractions[currency]
	if !ok {
		counts = make(map[int32]int)
		r.fractions[currency] = counts
	}
	counts[int32(max(-exponent, 0))]++
	r.currencyW = max(r.currencyW, length(currency))
}

func (r *amountRenderer) update(v any) {
	if a, ok := v.(*amountValue); ok && a != nil {
		r.observe(a.Number, a.Currency)
	}
}

func (r *amountRenderer) prepare() {
	r.digits = make(map[string]int, len(r.fractions))
	r.maxDigits = -1
	period := 0
	for currency, counts := range r.fractions {
		digits := int(ledger.MostCommonDigits(counts))
		r.digits[currency] = digits
		r.maxDigits = max(r.maxDigits, digits)
		if digits > 0 {
			period = 1
		}
	}
	if r.maxDigits < 0 {
		return // no amounts: the column is empty
	}
	r.numberW = 1 + r.integral + period + r.maxDigits
	r.w = r.numberW + 1 + r.currencyW
}

func (r *amountRenderer) width() int { return r.w }

// formatNumber renders number like the column's formatter for currency,
// "{: W.Nf}" followed by the padding that lines the currencies up.
func (r *amountRenderer) formatNumber(number decimal.Decimal, currency string) string {
	digits, ok := r.digits[currency]
	if !ok {
		digits = r.maxDigits
	}
	padding := r.maxDigits - digits
	if r.maxDigits > 0 && digits == 0 {
		padding++
	}
	// Python keeps the sign of a negative number that rounds to zero
	// (-0.001 is -0.00), which the rounded decimal has lost.
	s := number.RoundBank(int32(digits)).StringFixed(int32(digits))
	switch {
	case strings.HasPrefix(s, "-"):
	case number.Sign() < 0:
		s = "-" + s
	default:
		s = " " + s
	}
	return padLeft(s, r.numberW-padding) + strings.Repeat(" ", padding)
}

func (r *amountRenderer) formatAmount(number decimal.Decimal, currency string) string {
	return r.formatNumber(number, currency) + " " + padRight(currency, r.currencyW)
}

func (r *amountRenderer) format(v any) string {
	a, ok := v.(*amountValue)
	if !ok || a == nil {
		return ""
	}
	return r.formatAmount(a.Number, a.Currency)
}

// positionRenderer renders a position column like beanquery's
// PositionRenderer: the units, aligned, and a cost as a second aligned
// amount in braces, without its date or label.
type positionRenderer struct {
	leftAligned
	units *amountRenderer
	costs *amountRenderer
	w     int
}

func newPositionRenderer(ctx *renderContext) *positionRenderer {
	return &positionRenderer{units: newAmountRenderer(ctx), costs: newAmountRenderer(ctx)}
}

func (r *positionRenderer) observe(p *positionValue) {
	r.units.observe(p.Units.Number, p.Units.Currency)
	if p.Cost != nil {
		r.costs.observe(p.Cost.Number, p.Cost.Currency)
	}
}

func (r *positionRenderer) update(v any) {
	if p, ok := v.(*positionValue); ok && p != nil {
		r.observe(p)
	}
}

func (r *positionRenderer) prepare() {
	r.units.prepare()
	r.costs.prepare()
	r.w = r.units.width() + r.costs.width()
	if r.costs.width() > 0 {
		r.w += 3
	}
}

func (r *positionRenderer) width() int { return r.w }

func (r *positionRenderer) formatPosition(p *positionValue) string {
	units := r.units.formatAmount(p.Units.Number, p.Units.Currency)
	if p.Cost == nil {
		return padRight(units, r.w)
	}
	return units + " {" + r.costs.formatAmount(p.Cost.Number, p.Cost.Currency) + "}"
}

func (r *positionRenderer) format(v any) string {
	p, ok := v.(*positionValue)
	if !ok || p == nil {
		return ""
	}
	return r.formatPosition(p)
}

// maxTabularLots is the most lots an inventory column lays out as a table.
const maxTabularLots = 5

// inventoryRenderer renders an inventory column like beanquery's
// InventoryRenderer without row expansion. Each commodity has a position
// renderer of its own and as many slots as it has lots in any one row;
// cells list the commodities sorted, their lots in slots, and blanks for
// the slots a row leaves empty. A column needing more than five slots lists
// each cell's lots instead.
type inventoryRenderer struct {
	leftAligned
	ctx         *renderContext
	renderers   map[string]*positionRenderer
	counts      map[string]int // commodity -> most lots in one row
	commodities []string       // sorted, once prepared
	slots       int
	w           int
}

func newInventoryRenderer(ctx *renderContext) *inventoryRenderer {
	return &inventoryRenderer{ctx: ctx, renderers: make(map[string]*positionRenderer), counts: make(map[string]int)}
}

func (r *inventoryRenderer) update(v any) {
	inv, ok := v.(*inventoryValue)
	if !ok || inv == nil {
		return
	}
	counts := make(map[string]int)
	for _, p := range inv.Positions() {
		currency := p.Units.Currency
		renderer, ok := r.renderers[currency]
		if !ok {
			renderer = newPositionRenderer(r.ctx)
			r.renderers[currency] = renderer
		}
		renderer.observe(p)
		counts[currency]++
	}
	for currency, count := range counts {
		r.counts[currency] = max(r.counts[currency], count)
	}
}

func (r *inventoryRenderer) prepare() {
	sep := length(r.ctx.listSep)
	r.commodities = make([]string, 0, len(r.renderers))
	for currency, renderer := range r.renderers {
		renderer.prepare()
		r.commodities = append(r.commodities, currency)
		r.slots += r.counts[currency]
		r.w += r.counts[currency] * (renderer.width() + sep)
	}
	slices.Sort(r.commodities)
	r.w -= sep
}

func (r *inventoryRenderer) width() int { return r.w }

func (r *inventoryRenderer) format(v any) string {
	inv, ok := v.(*inventoryValue)
	if !ok || inv == nil {
		return ""
	}
	positions := inv.Positions()
	slices.SortStableFunc(positions, comparePositionsForDisplay)

	if r.slots > maxTabularLots {
		parts := make([]string, len(positions))
		for i, p := range positions {
			parts[i] = r.renderers[p.Units.Currency].formatPosition(p)
		}
		return padRight(strings.Join(parts, r.ctx.listSep), r.w)
	}

	// The positions are sorted by currency, like the commodities, so each
	// commodity's lots are the next run of them.
	parts := make([]string, 0, r.slots)
	next := 0
	for _, currency := range r.commodities {
		renderer := r.renderers[currency]
		lots := 0
		for ; next < len(positions) && positions[next].Units.Currency == currency; next++ {
			parts = append(parts, renderer.formatPosition(positions[next]))
			lots++
		}
		for range r.counts[currency] - lots {
			parts = append(parts, strings.Repeat(" ", renderer.width()))
		}
	}
	return strings.Join(parts, r.ctx.listSep)
}

// comparePositionsForDisplay orders positions like beanquery's
// positionsortkey: by currency, then the most units first, then positions
// without cost before those with, by cost currency, the highest cost
// first, and the earliest lot date.
func comparePositionsForDisplay(a, b *positionValue) int {
	if c := strings.Compare(a.Units.Currency, b.Units.Currency); c != 0 {
		return c
	}
	if c := b.Units.Number.Cmp(a.Units.Number); c != 0 {
		return c
	}
	switch {
	case a.Cost == nil && b.Cost == nil:
		return 0
	case a.Cost == nil:
		return -1
	case b.Cost == nil:
		return 1
	}
	if c := strings.Compare(a.Cost.Currency, b.Cost.Currency); c != 0 {
		return c
	}
	if c := b.Cost.Number.Cmp(a.Cost.Number); c != 0 {
		return c
	}
	switch {
	case a.Cost.Date == nil && b.Cost.Date == nil:
		return 0
	case a.Cost.Date == nil:
		return -1
	case b.Cost.Date == nil:
		return 1
	}
	return a.Cost.Date.Compare(b.Cost.Date.Time)
}
