// Package formatter formats Beancount files as bean-format does: it aligns
// the numbers of postings, balances and prices on their currency and leaves
// every other line as written.
//
// The widths of the account and number columns, or the currency column, are
// configurable. The formatter always formats a source: it copies every line
// it does not realign from the text the AST was parsed from, and fails on a
// tree that was not parsed from that text. The printer package renders
// directives without a source, as beancount's printer does.
//
// Example usage:
//
//	// Parse a Beancount file
//	tree, err := parser.ParseBytes(ctx, source)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create a formatter with custom currency column
//	f := formatter.New(formatter.WithCurrencyColumn(60))
//
//	// Format to stdout
//	err = f.Format(ctx, tree, source, os.Stdout)
package formatter

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
)

const (
	// DefaultIndentation is the default indentation for postings and metadata
	DefaultIndentation = 4

	// MinimumSpacing is the minimum number of spaces between account/number and currency
	MinimumSpacing = 2

	// DateWidth is the width of a formatted date (YYYY-MM-DD)
	DateWidth = 10
)

// directiveKeywordWidth calculates the display width of a directive's keyword plus trailing space.
// Uses runewidth for Unicode-safe width calculation, though current directive keywords are ASCII.
//
// Example: balance directive → "balance" → 8 (7 chars + 1 space)
func directiveKeywordWidth(d ast.Directive) int {
	return runewidth.StringWidth(string(d.Kind())) + 1
}

// Formatter holds the options of a formatting: the column widths numbers
// are aligned to, and whether comments and blank lines are written.
//
// It measures by display width (via go-runewidth) rather than byte length,
// so lines with Unicode characters align. Comments and blank lines are
// copied from the source lines the AST positions them on.
//
// Example:
//
//	f := formatter.New(formatter.WithCurrencyColumn(60))
//	var buf bytes.Buffer
//	err := f.Format(ctx, tree, source, &buf)
type Formatter struct {
	// CurrencyColumn is the target column for currency alignment.
	// If set (non-zero), this overrides PrefixWidth and NumWidth.
	// If 0, it will be calculated from PrefixWidth + NumWidth, or auto-calculated.
	CurrencyColumn int

	// PrefixWidth is the width in characters to render the account name to.
	// If 0, a good value is selected automatically from the contents.
	PrefixWidth int

	// NumWidth is the width to render each number.
	// If 0, a good value is selected automatically from the contents.
	NumWidth int

	// PreserveComments controls whether the source's comments, standalone
	// and inline, are written.
	// Default: true
	PreserveComments bool

	// PreserveBlanks controls whether blank lines are preserved during formatting.
	// Default: true
	PreserveBlanks bool

	// Indentation is the number of spaces to use for indentation.
	// Default: DefaultIndentation
	Indentation int

	// indentationExplicit is true when Indentation was set via WithIndentation;
	// otherwise the posting indent follows the source (bean-format behavior).
	indentationExplicit bool

	// StringEscapeStyle has no effect.
	//
	// Deprecated: the formatter copies every string from the source.
	StringEscapeStyle StringEscapeStyle
}

// run is one Format call: the Formatter's options and the state derived
// from the source being formatted, so a Formatter keeps no state between
// calls.
type run struct {
	*Formatter

	// source is the text being formatted, which bean-format copies
	// wherever it does not realign.
	source *sourceView

	// verbatimLines tracks source lines already emitted verbatim (metadata
	// preservation), so trivia parsed from those lines (inline comments) is
	// not emitted a second time.
	verbatimLines map[int]bool

	// indent is the posting indent: the most frequent posting indent in the
	// source (ties broken by the widest, matching bean-format), or
	// Indentation when explicit or undeterminable.
	indent int

	// columns is the number layout.
	columns columns

	// err is the first item found not to own its source line, which fails
	// the run.
	err error
}

// Option is a functional option for configuring a Formatter.
type Option func(*Formatter)

// WithCurrencyColumn sets a specific column for currency alignment.
// This overrides PrefixWidth and NumWidth if set.
func WithCurrencyColumn(col int) Option {
	return func(f *Formatter) {
		f.CurrencyColumn = col
	}
}

// WithPrefixWidth sets the width in characters to render account names to.
func WithPrefixWidth(width int) Option {
	return func(f *Formatter) {
		f.PrefixWidth = width
	}
}

// WithNumWidth sets the width to render each number.
func WithNumWidth(width int) Option {
	return func(f *Formatter) {
		f.NumWidth = width
	}
}

// WithPreserveComments enables or disables writing the source's comments.
func WithPreserveComments(preserve bool) Option {
	return func(f *Formatter) {
		f.PreserveComments = preserve
	}
}

// WithPreserveBlanks enables or disables blank line preservation.
func WithPreserveBlanks(preserve bool) Option {
	return func(f *Formatter) {
		f.PreserveBlanks = preserve
	}
}

// WithIndentation sets the indentation level for postings and metadata,
// overriding the source-derived indent.
func WithIndentation(indent int) Option {
	return func(f *Formatter) {
		f.Indentation = indent
		f.indentationExplicit = true
	}
}

// WithStringEscapeStyle has no effect.
//
// Deprecated: the formatter copies every string from the source.
func WithStringEscapeStyle(style StringEscapeStyle) Option {
	return func(f *Formatter) {
		f.StringEscapeStyle = style
	}
}

// New creates a new Formatter with the given options.
func New(opts ...Option) *Formatter {
	f := &Formatter{
		CurrencyColumn:    0,                   // Auto-calculate by default (0 = auto)
		Indentation:       DefaultIndentation,  // Use default indentation
		PreserveComments:  true,                // Preserve comments by default
		PreserveBlanks:    true,                // Preserve blank lines by default
		StringEscapeStyle: EscapeStyleOriginal, // No effect: strings are copied from the source
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// isValidDirective returns true if a directive is valid to format.
// Checks:
// - Date-based directives must have valid dates (not empty string representation)
func isValidDirective(d ast.Directive) bool {
	// All directives implement Date() method, check if date is valid
	date := d.Date()
	if date == nil {
		return false
	}

	return date.String() != ""
}

// widthMetrics holds calculated width information for formatting.
type widthMetrics struct {
	maxPrefixWidth int // Maximum width of account prefix (indentation + flag + account + spacing)
	maxNumWidth    int // Maximum width of numeric values
}

// calculateWidthMetrics performs a single pass through the AST to calculate all width metrics.
func (f *run) calculateWidthMetrics(tree *ast.AST) widthMetrics {
	metrics := widthMetrics{}

	// bean-format decouples the two maxima: every number line is rendered as
	// prefix ljust(maxPrefix) + two spaces + number rjust(maxNum) + space +
	// currency, so the widest prefix and the widest number may come from
	// different lines. Prefix widths exclude trailing spacing.
	record := func(line lineLayout) {
		if line.kind != alignLine {
			return
		}
		metrics.maxPrefixWidth = max(metrics.maxPrefixWidth, line.prefixWidth)
		metrics.maxNumWidth = max(metrics.maxNumWidth, line.numberWidth)
	}

	for _, directive := range tree.Directives {
		switch d := directive.(type) {
		case *ast.Transaction:
			for _, posting := range d.Postings {
				record(f.postingLayout(posting))
			}

		case *ast.Balance:
			record(f.balanceLayout(d))

		case *ast.Price:
			record(f.priceLayout(d))
		}
	}

	return metrics
}

// columns is the number layout of one formatting run, as bean-format lays
// out aligned lines: at a fixed currency column when one is configured,
// otherwise the prefix padded to one width and the number right-aligned to
// another. The two widths stay apart, since a longer prefix or number
// overflows its own width without taking space from the other.
type columns struct {
	currency       int
	prefix, number int
}

// resolveColumns computes the layout for tree: the configured currency
// column, or the configured widths with the content's maxima filling in.
func (f *run) resolveColumns(tree *ast.AST) columns {
	if f.CurrencyColumn > 0 {
		return columns{currency: f.CurrencyColumn}
	}
	metrics := f.calculateWidthMetrics(tree)
	c := columns{prefix: f.PrefixWidth, number: f.NumWidth}
	if c.prefix == 0 {
		c.prefix = metrics.maxPrefixWidth
	}
	if c.number == 0 {
		c.number = metrics.maxNumWidth
	}
	return c
}

// padding returns the spaces between a prefix and a number of the given
// display widths: bean-format's '{:<W}  {:>N}', or with a currency column,
// what reaches that column but at least two spaces.
func (c columns) padding(prefixWidth, numberWidth int) int {
	if c.currency > 0 {
		return max(c.currency-prefixWidth-numberWidth-2, MinimumSpacing)
	}
	return max(c.prefix-prefixWidth, 0) + MinimumSpacing + max(c.number-numberWidth, 0)
}

// astItem is a top-level item of the AST with its line.
type astItem struct {
	line int

	// undated is an option, include, plugin, pushtag, poptag, pushmeta or
	// popmeta: a line bean-format leaves as written.
	undated   ast.Positioned
	directive ast.Directive
	comment   *ast.Comment
	blankLine *ast.BlankLine
}

// resolveIndent returns the posting indent for a run. Unless an
// explicit indentation was configured, it follows bean-format: the most
// frequent posting indent in the source wins, with ties broken by the
// widest indent.
func (f *run) resolveIndent(tree *ast.AST) int {
	if f.indentationExplicit {
		return f.Indentation
	}

	frequencies := make(map[int]int)
	for _, directive := range tree.Directives {
		txn, ok := directive.(*ast.Transaction)
		if !ok {
			continue
		}
		for _, posting := range txn.Postings {
			if column := posting.Position().Column; column > 1 {
				frequencies[column-1]++
			}
		}
	}

	indent, count := f.Indentation, 0
	for width, frequency := range frequencies {
		if frequency > count || (frequency == count && width > indent) {
			indent, count = width, frequency
		}
	}
	return indent
}

// fail records that the item at pos does not own its source line: the one
// fallback for an item the formatter can neither copy nor align. The run
// stops at the first one and Format returns it.
func (f *run) fail(pos ast.Position) {
	if f.err == nil {
		f.err = fmt.Errorf("%s: item does not own its source line; the source must be the text the AST was parsed from", pos)
	}
}

// writeCopied writes the line of the item at pos as bean-format leaves it:
// as written. An item that does not own its line fails the run.
func (f *run) writeCopied(pos ast.Position, line lineLayout, buf *strings.Builder) {
	if line.kind != copyLine {
		f.fail(pos)
		return
	}
	buf.WriteString(line.text)
	buf.WriteByte('\n')
}

// copyItemLine copies the source line of an item starting at pos: a
// metadata, tags, comment or blank line, or an undated directive.
func (f *run) copyItemLine(pos ast.Position, buf *strings.Builder) {
	f.writeCopied(pos, plainLayout(f.source.itemLine(pos.Line, pos.Column)), buf)
}

// copyDirectiveLine copies a dated directive's source line, its header.
func (f *run) copyDirectiveLine(d ast.Directive, buf *strings.Builder) {
	f.writeCopied(d.Position(), plainLayout(f.source.directiveLine(d)), buf)
}

// newRun starts a run formatting tree, parsed from source. The source
// comes first: the widths read spellings from it.
func newRun(f *Formatter, tree *ast.AST, source []byte) *run {
	return &run{
		Formatter:     f,
		source:        newSourceView(source, tree),
		verbatimLines: make(map[int]bool),
	}
}

// Format formats tree, parsed from sourceContent, and writes the output to
// the writer. Comments and blank lines from the AST are preserved based on
// Formatter configuration. The source must be the text the AST was parsed
// from: every line is copied from it or aligned within it, so an item that
// does not own its source line is an error naming the item's position, and
// nothing is written.
func (f *Formatter) Format(ctx context.Context, tree *ast.AST, sourceContent []byte, w io.Writer) error {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

	r := newRun(f, tree, sourceContent)

	// Determine the currency column based on the configuration
	widthTimer := collector.Start("formatter.width_calculation")
	r.indent = r.resolveIndent(tree)
	r.columns = r.resolveColumns(tree)
	widthTimer.End()

	// Use a string builder to buffer all output, then write once
	var buf strings.Builder

	// Estimate initial capacity to reduce allocations
	estimatedSize := (len(tree.Options) + len(tree.Includes) + len(tree.Directives)) * 100
	buf.Grow(estimatedSize)

	// Collect all items with their positions using the Positioned interface
	formatTimer := collector.Start("formatter.item_collection")
	items := r.collectItems(tree)
	formatTimer.End()

	// Format all items in order
	directiveTimer := collector.Start("formatter.directive_formatting")
	for _, item := range items {
		// Skip invalid directives, such as directives with invalid dates.
		if item.directive != nil && !isValidDirective(item.directive) {
			continue
		}

		r.formatItem(item, &buf)
		if r.err != nil {
			break
		}
	}
	directiveTimer.End()
	if r.err != nil {
		return r.err
	}

	// bean-format preserves blank lines at the edges of the file; emit the
	// items exactly as collected.
	_, err := w.Write([]byte(buf.String()))
	return err
}

// collectItems gathers all AST items into a sorted slice by line position.
func (f *run) collectItems(tree *ast.AST) []astItem {
	totalItems := len(tree.Options) + len(tree.Includes) + len(tree.Plugins) +
		len(tree.Pushtags) + len(tree.Poptags) + len(tree.Pushmetas) + len(tree.Popmetas) +
		len(tree.Directives) + len(tree.Comments) + len(tree.BlankLines)
	items := make([]astItem, 0, totalItems)

	for _, opt := range tree.Options {
		if opt != nil {
			items = append(items, astItem{line: opt.Position().Line, undated: opt})
		}
	}

	for _, inc := range tree.Includes {
		if inc != nil {
			items = append(items, astItem{line: inc.Position().Line, undated: inc})
		}
	}

	for _, plugin := range tree.Plugins {
		if plugin != nil {
			items = append(items, astItem{line: plugin.Position().Line, undated: plugin})
		}
	}

	for _, pushtag := range tree.Pushtags {
		if pushtag != nil {
			items = append(items, astItem{line: pushtag.Position().Line, undated: pushtag})
		}
	}

	for _, poptag := range tree.Poptags {
		if poptag != nil {
			items = append(items, astItem{line: poptag.Position().Line, undated: poptag})
		}
	}

	for _, pushmeta := range tree.Pushmetas {
		if pushmeta != nil {
			items = append(items, astItem{line: pushmeta.Position().Line, undated: pushmeta})
		}
	}

	for _, popmeta := range tree.Popmetas {
		if popmeta != nil {
			items = append(items, astItem{line: popmeta.Position().Line, undated: popmeta})
		}
	}

	for _, directive := range tree.Directives {
		if directive != nil {
			items = append(items, astItem{line: directive.Position().Line, directive: directive})
		}
	}

	// Add comments and blank lines if preservation is enabled
	if f.PreserveComments {
		for _, comment := range tree.Comments {
			if comment != nil {
				items = append(items, astItem{line: comment.Position().Line, comment: comment})
			}
		}
	}

	if f.PreserveBlanks {
		for _, blankLine := range tree.BlankLines {
			if blankLine != nil {
				items = append(items, astItem{line: blankLine.Position().Line, blankLine: blankLine})
			}
		}
	}

	// Sort all items by their original position in the file
	slices.SortFunc(items, func(a, b astItem) int {
		return cmp.Compare(a.line, b.line)
	})

	return items
}

// formatItem formats a single AST item. bean-format only touches lines
// holding an amount or starting with an account, so every item but a
// directive is copied exactly as the source has it, indentation and
// whitespace included.
func (f *run) formatItem(item astItem, buf *strings.Builder) {
	switch {
	case item.comment != nil:
		if f.verbatimLines[item.comment.Position().Line] {
			return // Already contained in a verbatim-preserved line.
		}
		f.copyItemLine(item.comment.Position(), buf)
	case item.blankLine != nil:
		f.copyItemLine(item.blankLine.Position(), buf)
	case item.undated != nil:
		f.copyItemLine(item.undated.Position(), buf)
	case item.directive != nil:
		f.formatDirective(item.directive, buf)
	}
}

// formatDirective formats a directive based on its type. Only a balance, a
// price and a transaction's postings carry a number to realign; bean-format
// leaves every other directive's line untouched, and so its header keeps
// its spelling: txn, slash dates, the order of tags and links.
func (f *run) formatDirective(d ast.Directive, buf *strings.Builder) {
	switch directive := d.(type) {
	case *ast.Balance:
		f.formatBalance(directive, buf)
	case *ast.Price:
		f.formatPrice(directive, buf)
	case *ast.Transaction:
		f.copyDirectiveLine(d, buf)
		f.formatTransactionBody(directive, buf)
	default:
		f.copyDirectiveLine(d, buf)
		f.formatMetadata(d.GetMetadata(), buf)
	}
}

// formatBalance formats a balance directive.
func (f *run) formatBalance(b *ast.Balance, buf *strings.Builder) {
	f.formatDatedLine(b, f.balanceLayout(b), buf)
}

// balanceLayout reads a balance's line; the number before the currency is
// the tolerance's when there is one.
func (f *run) balanceLayout(b *ast.Balance) lineLayout {
	last := b.Amount
	if b.Tolerance != nil {
		last = b.Tolerance
	}
	return f.datedLayout(b, string(b.Account), f.balanceAmountText(b), numberText(last), balanceCurrency(b))
}

// balanceAmountText spells a balance's amount, with its tolerance, without
// the currency.
func (f *run) balanceAmountText(b *ast.Balance) string {
	if b.Amount == nil {
		return ""
	}
	text := numberText(b.Amount)
	if b.Tolerance != nil {
		text += " ~ " + numberText(b.Tolerance)
	}
	return text
}

func balanceCurrency(b *ast.Balance) string {
	if b.Amount != nil && b.Amount.Currency != "" {
		return b.Amount.Currency
	}
	if b.Tolerance != nil {
		return b.Tolerance.Currency
	}
	return ""
}

// datedHead spells the start of a dated directive: date, keyword and
// subject (account or commodity).
func (f *run) datedHead(d ast.Directive, subject string) string {
	return f.dateText(d) + " " + string(d.Kind()) + " " + subject
}

// dateText spells a directive's date as its source line does.
func (f *run) dateText(d ast.Directive) string {
	return spelledDate(f.source.line(d.Position().Line), d.Date())
}

// datedLayout reads the line of a dated directive ending in an amount.
func (f *run) datedLayout(d ast.Directive, subject, text, lastNumber, currency string) lineLayout {
	line, owned := f.source.directiveLine(d)
	return datedLayout(line, owned, f.datedHead(d, subject), text, lastNumber, currency)
}

// formatDatedLine writes a dated directive ending in an amount: aligned as
// bean-format aligns it, or copied from the source when bean-format leaves
// it alone.
func (f *run) formatDatedLine(d ast.Directive, line lineLayout, buf *strings.Builder) {
	switch line.kind {
	case copyLine:
		buf.WriteString(line.text)
	case alignLine:
		buf.WriteString(line.prefix)
		buf.WriteString(strings.Repeat(" ", f.columns.padding(runewidth.StringWidth(line.prefix), line.numberWidth)))
		buf.WriteString(line.number)
		buf.WriteByte(' ')
		buf.WriteString(line.currency)
		f.writeInlineComment(d.GetComment(), buf)
	default:
		f.fail(d.Position())
		return
	}
	buf.WriteByte('\n')
	f.formatMetadata(d.GetMetadata(), buf)
}

// formatPrice formats a price directive.
func (f *run) formatPrice(p *ast.Price, buf *strings.Builder) {
	f.formatDatedLine(p, f.priceLayout(p), buf)
}

// priceLayout reads a price's line.
func (f *run) priceLayout(p *ast.Price) lineLayout {
	number := numberText(p.Amount)
	return f.datedLayout(p, p.Commodity, number, number, priceCurrency(p))
}

func priceCurrency(p *ast.Price) string {
	if p.Amount == nil {
		return ""
	}
	return p.Amount.Currency
}

// formatTransactionBody writes the lines after a transaction's header.
func (f *run) formatTransactionBody(t *ast.Transaction, buf *strings.Builder) {
	f.formatLeadingTransactionBody(t, buf)

	if len(t.BodyItems) > 0 {
		for _, item := range t.BodyItems {
			f.formatTransactionBodyItem(item, buf)
		}
		return
	}

	// A tree built without BodyItems: its postings take the fallback
	// instead of being dropped silently.
	for _, posting := range t.Postings {
		f.formatPosting(posting, buf)
	}
}

// formatLeadingTransactionBody writes the metadata and tag/link lines before
// the first posting in source order. Like bean-format, tag/link lines are
// kept as written.
func (f *run) formatLeadingTransactionBody(t *ast.Transaction, buf *strings.Builder) {
	// Pushed metadata (no position) comes first, and an own entry whose key
	// was pushed takes the pushed key's place: restore source order.
	metadata := slices.DeleteFunc(slices.Clone(t.Metadata), func(m *ast.Metadata) bool { return m.Position().Line == 0 })
	slices.SortStableFunc(metadata, func(a, b *ast.Metadata) int { return a.Position().Line - b.Position().Line })
	for _, line := range t.BodyTagsLinks {
		split := 0
		for split < len(metadata) && metadata[split].Position().Line < line.Position().Line {
			split++
		}
		f.formatMetadata(metadata[:split], buf)
		metadata = metadata[split:]
		f.formatTagsLinks(line, buf)
	}
	f.formatMetadata(metadata, buf)
}

func (f *run) formatTagsLinks(line *ast.TagsLinks, buf *strings.Builder) {
	f.copyItemLine(line.Position(), buf)
	f.verbatimLines[line.Position().Line] = true
}

func (f *run) formatTransactionBodyItem(item ast.TransactionBodyItem, buf *strings.Builder) {
	switch {
	case item.Posting != nil:
		f.formatPosting(item.Posting, buf)
	case item.Comment != nil:
		if f.PreserveComments && !f.verbatimLines[item.Comment.Position().Line] {
			f.copyItemLine(item.Comment.Position(), buf)
		}
	case item.BlankLine != nil:
		if f.PreserveBlanks {
			f.copyItemLine(item.BlankLine.Position(), buf)
		}
	}
}

// formatPosting writes a posting's line, aligned or copied, and the
// metadata lines below it.
func (f *run) formatPosting(p *ast.Posting, buf *strings.Builder) {
	line := f.postingLayout(p)
	switch line.kind {
	case copyLine:
		buf.WriteString(line.text)
		f.verbatimLines[p.Position().Line] = true
	case alignLine:
		buf.WriteString(line.prefix)
		buf.WriteString(strings.Repeat(" ", f.columns.padding(runewidth.StringWidth(line.prefix), line.numberWidth)))
		buf.WriteString(line.number)
		buf.WriteByte(' ')
		buf.WriteString(line.rest)
		f.verbatimLines[p.Position().Line] = true
	default:
		f.fail(p.Position())
		return
	}
	buf.WriteByte('\n')
	f.formatMetadata(p.Metadata, buf)
}

// writeInlineComment writes a comment at the end of a line, unless
// PreserveComments is off.
func (f *run) writeInlineComment(c *ast.Comment, buf *strings.Builder) {
	if c == nil || !f.PreserveComments {
		return
	}
	buf.WriteByte(' ')
	buf.WriteString(c.Content)
}

// postingLayout reads a posting's line.
func (f *run) postingLayout(p *ast.Posting) lineLayout {
	line, owned := f.source.itemLine(p.Position().Line, p.Position().Column)
	return postingLayout(line, owned, p, f.indent)
}

// formatMetadata writes metadata lines and the comments leading them as
// the source has them: bean-format leaves both untouched.
func (f *run) formatMetadata(metadata []*ast.Metadata, buf *strings.Builder) {
	for _, m := range metadata {
		// Metadata without a position was pushed (pushmeta), not written
		// on the directive; bean-format leaves it out.
		if m.Position().Line == 0 {
			continue
		}
		for _, c := range m.Comments {
			if f.PreserveComments && !f.verbatimLines[c.Position().Line] {
				f.copyItemLine(c.Position(), buf)
			}
		}
		f.copyItemLine(m.Position(), buf)
		f.verbatimLines[m.Position().Line] = true
	}
}
