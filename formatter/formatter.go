// Package formatter provides formatting functionality for Beancount files with automatic
// alignment and comment preservation. It handles the proper spacing and alignment of
// currencies, numbers, and account names while preserving the original formatting intent.
//
// The formatter supports customizable column widths for account names, numbers, and
// currency positions, and can preserve comments and blank lines from the source file.
// It always formats a source: like bean-format, it copies every line it does not
// realign from the text the AST was parsed from. The printer package renders
// directives without a source, as beancount's printer does.
//
// Example usage:
//
//	// Parse a Beancount file
//	ast, err := parser.ParseBytes([]byte(sourceContent))
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create a formatter with custom currency column
//	f := formatter.New(
//	    formatter.WithCurrencyColumn(60),
//	    formatter.WithPreserveComments(true),
//	)
//
//	// Format to stdout
//	err = f.Format(ast, []byte(sourceContent), os.Stdout)
package formatter

import (
	"cmp"
	"context"
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

// Formatter handles formatting of Beancount files with proper alignment and spacing.
// It aligns currencies, numbers, and account names according to configurable column widths,
// and can preserve comments and blank lines from the original source.
//
// The formatter uses display width (via go-runewidth) rather than byte length for proper
// alignment with Unicode characters. Comments and blank lines are tracked by position and
// re-inserted during formatting to maintain the original structure.
//
// Example:
//
//	f := formatter.New(
//	    formatter.WithCurrencyColumn(60),
//	    formatter.WithPreserveComments(true),
//	)
//	var buf bytes.Buffer
//	err := f.Format(ast, sourceContent, &buf)
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

	// StringEscapeStyle controls how strings are escaped in the output.
	// Default: EscapeStyleCStyle
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

// WithStringEscapeStyle sets the escape style for string formatting.
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
		StringEscapeStyle: EscapeStyleOriginal, // Preserve original escape style by default
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

// astItem represents any item in the AST with its position
type astItem struct {
	line      int
	option    *ast.Option
	include   *ast.Include
	plugin    *ast.Plugin
	pushtag   *ast.Pushtag
	poptag    *ast.Poptag
	pushmeta  *ast.Pushmeta
	popmeta   *ast.Popmeta
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

// writeCopied writes a line bean-format leaves as written and reports
// true, or writes nothing and reports false when the layout is not a copy.
func (f *run) writeCopied(line lineLayout, buf *strings.Builder) bool {
	if line.kind != copyLine {
		return false
	}
	buf.WriteString(line.text)
	buf.WriteByte('\n')
	return true
}

// copyItemLine copies the source line of an item starting at pos, a
// header, metadata, tags or comment line, when the item owns it.
func (f *run) copyItemLine(pos ast.Position, buf *strings.Builder) bool {
	return f.writeCopied(plainLayout(f.source.itemLine(pos.Line, pos.Column)), buf)
}

// copyDirectiveLine copies a dated directive's source line when the
// directive owns it.
func (f *run) copyDirectiveLine(d ast.Directive, buf *strings.Builder) bool {
	return f.writeCopied(plainLayout(f.source.directiveLine(d)), buf)
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
// Formatter configuration.
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
	}
	directiveTimer.End()

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
			items = append(items, astItem{line: opt.Position().Line, option: opt})
		}
	}

	for _, inc := range tree.Includes {
		if inc != nil {
			items = append(items, astItem{line: inc.Position().Line, include: inc})
		}
	}

	for _, plugin := range tree.Plugins {
		if plugin != nil {
			items = append(items, astItem{line: plugin.Position().Line, plugin: plugin})
		}
	}

	for _, pushtag := range tree.Pushtags {
		if pushtag != nil {
			items = append(items, astItem{line: pushtag.Position().Line, pushtag: pushtag})
		}
	}

	for _, poptag := range tree.Poptags {
		if poptag != nil {
			items = append(items, astItem{line: poptag.Position().Line, poptag: poptag})
		}
	}

	for _, pushmeta := range tree.Pushmetas {
		if pushmeta != nil {
			items = append(items, astItem{line: pushmeta.Position().Line, pushmeta: pushmeta})
		}
	}

	for _, popmeta := range tree.Popmetas {
		if popmeta != nil {
			items = append(items, astItem{line: popmeta.Position().Line, popmeta: popmeta})
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

// formatItem formats a single AST item.
func (f *run) formatItem(item astItem, buf *strings.Builder) {
	switch {
	case item.comment != nil:
		if f.verbatimLines[item.comment.Position().Line] {
			return // Already contained in a verbatim-preserved line.
		}
		if !f.writeTriviaLine(item.comment.Position(), buf) {
			f.formatComment(item.comment, buf)
		}
	case item.blankLine != nil:
		f.writeTriviaLine(item.blankLine.Position(), buf)
	case item.option != nil:
		f.formatOption(item.option, buf)
	case item.include != nil:
		f.formatInclude(item.include, buf)
	case item.plugin != nil:
		f.formatPlugin(item.plugin, buf)
	case item.pushtag != nil:
		f.formatPushtag(item.pushtag, buf)
	case item.poptag != nil:
		f.formatPoptag(item.poptag, buf)
	case item.pushmeta != nil:
		f.formatPushmeta(item.pushmeta, buf)
	case item.popmeta != nil:
		f.formatPopmeta(item.popmeta, buf)
	case item.directive != nil:
		f.formatDirective(item.directive, buf)
	}
}

// formatComment formats a comment from the AST.
func (f *run) formatComment(c *ast.Comment, buf *strings.Builder) {
	buf.WriteString(c.Content)
	// Lexer includes the newline in the comment token, but only if it existed
	// If we're at EOF, there may not be a newline
	if !strings.HasSuffix(c.Content, "\n") {
		buf.WriteByte('\n')
	}
}

// writeTriviaLine writes a comment or blank line exactly as the source has
// it, indentation and whitespace included, like bean-format, which only
// touches lines holding an amount or starting with an account. It reports
// false, writing nothing, when the source line holds more than the item.
func (f *run) writeTriviaLine(pos ast.Position, buf *strings.Builder) bool {
	return f.copyItemLine(pos, buf)
}

// formatDirective formats a directive based on its type.
func (f *run) formatDirective(d ast.Directive, buf *strings.Builder) {
	switch directive := d.(type) {
	case *ast.Commodity:
		f.formatCommodity(directive, buf)
	case *ast.Open:
		f.formatOpen(directive, buf)
	case *ast.Close:
		f.formatClose(directive, buf)
	case *ast.Balance:
		f.formatBalance(directive, buf)
	case *ast.Pad:
		f.formatPad(directive, buf)
	case *ast.Note:
		f.formatNote(directive, buf)
	case *ast.Document:
		f.formatDocument(directive, buf)
	case *ast.Price:
		f.formatPrice(directive, buf)
	case *ast.Event:
		f.formatEvent(directive, buf)
	case *ast.Query:
		f.formatQuery(directive, buf)
	case *ast.Custom:
		f.formatCustom(directive, buf)
	case *ast.Transaction:
		f.formatTransaction(directive, buf)
	}
}

// formatOption formats an option directive.
func (f *run) formatOption(opt *ast.Option, buf *strings.Builder) {
	if f.copyItemLine(opt.Position(), buf) {
		return
	}

	buf.WriteString("option ")
	f.formatRawString(opt.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(opt.Value, buf)
	f.writeInlineComment(opt.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatInclude formats an include directive.
func (f *run) formatInclude(inc *ast.Include, buf *strings.Builder) {
	if f.copyItemLine(inc.Position(), buf) {
		return
	}

	buf.WriteString("include ")
	f.formatRawString(inc.Filename, buf)
	f.writeInlineComment(inc.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatCommodity formats a commodity directive.
func (f *run) formatCommodity(c *ast.Commodity, buf *strings.Builder) {
	if f.copyDirectiveLine(c, buf) {
		f.formatMetadata(c.Metadata, buf)
		return
	}

	buf.WriteString(c.Date().String())
	buf.WriteString(" commodity ")
	buf.WriteString(c.Currency)
	f.writeInlineComment(c.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatOpen formats an open directive.
func (f *run) formatOpen(o *ast.Open, buf *strings.Builder) {
	// Open directives carry no number to realign, so bean-format leaves
	// them untouched.
	if f.copyDirectiveLine(o, buf) {
		f.formatMetadata(o.Metadata, buf)
		return
	}

	buf.WriteString(o.Date().String())
	buf.WriteString(" open ")
	buf.WriteString(string(o.Account))

	if len(o.ConstraintCurrencies) > 0 {
		buf.WriteString(" ")
		for i, currency := range o.ConstraintCurrencies {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(currency)
		}
	}

	if o.BookingMethod != "" {
		buf.WriteString(" \"")
		buf.WriteString(o.BookingMethod)
		buf.WriteByte('"')
	}

	f.writeInlineComment(o.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(o.Metadata, buf)
}

// formatClose formats a close directive.
func (f *run) formatClose(c *ast.Close, buf *strings.Builder) {
	if f.copyDirectiveLine(c, buf) {
		f.formatMetadata(c.Metadata, buf)
		return
	}

	buf.WriteString(c.Date().String())
	buf.WriteString(" close ")
	buf.WriteString(string(c.Account))
	f.writeInlineComment(c.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatBalance formats a balance directive.
func (f *run) formatBalance(b *ast.Balance, buf *strings.Builder) {
	f.formatDatedLine(b, f.balanceLayout(b), string(b.Account), f.balanceAmountText(b), balanceCurrency(b), buf)
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
// bean-format aligns it, copied from the source when bean-format leaves it
// alone, or spelled from its subject, amount text and currency.
func (f *run) formatDatedLine(d ast.Directive, line lineLayout, subject, text, currency string, buf *strings.Builder) {
	if f.writeCopied(line, buf) {
		f.formatMetadata(d.GetMetadata(), buf)
		return
	}

	if line.kind == alignLine {
		buf.WriteString(line.prefix)
		buf.WriteString(strings.Repeat(" ", f.columns.padding(runewidth.StringWidth(line.prefix), line.numberWidth)))
		buf.WriteString(line.number)
	} else {
		buf.WriteString(strings.TrimSpace(f.datedHead(d, subject) + " " + text))
	}
	if currency != "" {
		buf.WriteByte(' ')
		buf.WriteString(currency)
	}

	f.writeInlineComment(d.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(d.GetMetadata(), buf)
}

// formatPad formats a pad directive.
func (f *run) formatPad(p *ast.Pad, buf *strings.Builder) {
	if f.copyDirectiveLine(p, buf) {
		f.formatMetadata(p.Metadata, buf)
		return
	}

	buf.WriteString(p.Date().String())
	buf.WriteString(" pad ")
	buf.WriteString(string(p.Account))
	buf.WriteByte(' ')
	buf.WriteString(string(p.AccountPad))
	f.writeInlineComment(p.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(p.Metadata, buf)
}

// formatNote formats a note directive.
func (f *run) formatNote(n *ast.Note, buf *strings.Builder) {
	if f.copyDirectiveLine(n, buf) {
		f.formatMetadata(n.Metadata, buf)
		return
	}

	buf.WriteString(n.Date().String())
	buf.WriteString(" note ")
	buf.WriteString(string(n.Account))
	buf.WriteByte(' ')
	f.formatRawString(n.Description, buf)
	for _, tag := range n.Tags {
		buf.WriteString(" #")
		buf.WriteString(string(tag))
	}
	for _, link := range n.Links {
		buf.WriteString(" ^")
		buf.WriteString(string(link))
	}
	f.writeInlineComment(n.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(n.Metadata, buf)
}

// formatDocument formats a document directive.
func (f *run) formatDocument(d *ast.Document, buf *strings.Builder) {
	if f.copyDirectiveLine(d, buf) {
		f.formatMetadata(d.Metadata, buf)
		return
	}

	buf.WriteString(d.Date().String())
	buf.WriteString(" document ")
	buf.WriteString(string(d.Account))
	buf.WriteByte(' ')
	f.formatRawString(d.PathToDocument, buf)
	for _, tag := range d.Tags {
		buf.WriteString(" #")
		buf.WriteString(string(tag))
	}
	for _, link := range d.Links {
		buf.WriteString(" ^")
		buf.WriteString(string(link))
	}
	f.writeInlineComment(d.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(d.Metadata, buf)
}

// formatPrice formats a price directive.
func (f *run) formatPrice(p *ast.Price, buf *strings.Builder) {
	f.formatDatedLine(p, f.priceLayout(p), p.Commodity, numberText(p.Amount), priceCurrency(p), buf)
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

// formatEvent formats an event directive.
func (f *run) formatEvent(e *ast.Event, buf *strings.Builder) {
	if f.copyDirectiveLine(e, buf) {
		f.formatMetadata(e.Metadata, buf)
		return
	}

	buf.WriteString(e.Date().String())
	buf.WriteString(" event ")
	f.formatRawString(e.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(e.Value, buf)
	f.writeInlineComment(e.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(e.Metadata, buf)
}

// formatQuery formats a query directive.
func (f *run) formatQuery(q *ast.Query, buf *strings.Builder) {
	if f.copyDirectiveLine(q, buf) {
		f.formatMetadata(q.Metadata, buf)
		return
	}

	buf.WriteString(q.Date().String())
	buf.WriteString(" query ")
	f.formatRawString(q.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(q.QueryString, buf)
	f.writeInlineComment(q.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(q.Metadata, buf)
}

// formatCustom formats a custom directive.
func (f *run) formatCustom(c *ast.Custom, buf *strings.Builder) {
	if f.copyDirectiveLine(c, buf) {
		f.formatMetadata(c.Metadata, buf)
		return
	}

	buf.WriteString(c.Date().String())
	buf.WriteString(" custom ")
	f.formatRawString(c.Type, buf)

	for _, val := range c.Values {
		buf.WriteByte(' ')
		if val.String != nil {
			buf.WriteByte('"')
			buf.WriteString(f.escapeString(*val.String))
			buf.WriteByte('"')
		} else if val.Date != nil {
			buf.WriteString(val.Date.String())
		} else if val.BooleanValue != nil {
			buf.WriteString(*val.BooleanValue)
		} else if val.Amount != nil {
			buf.WriteString(numberText(val.Amount))
			buf.WriteByte(' ')
			buf.WriteString(val.Amount.Currency)
		} else if val.Number != nil {
			buf.WriteString(*val.Number)
		}
	}
	f.writeInlineComment(c.GetComment(), buf)
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatPlugin formats a plugin directive.
func (f *run) formatPlugin(p *ast.Plugin, buf *strings.Builder) {
	if f.copyItemLine(p.Position(), buf) {
		return
	}

	buf.WriteString("plugin ")
	f.formatRawString(p.Name, buf)
	if !p.Config.IsEmpty() {
		buf.WriteByte(' ')
		f.formatRawString(p.Config, buf)
	}
	f.writeInlineComment(p.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatPushtag formats a pushtag directive.
func (f *run) formatPushtag(p *ast.Pushtag, buf *strings.Builder) {
	if f.copyItemLine(p.Position(), buf) {
		return
	}

	buf.WriteString("pushtag #")
	buf.WriteString(string(p.Tag))
	f.writeInlineComment(p.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatPoptag formats a poptag directive.
func (f *run) formatPoptag(p *ast.Poptag, buf *strings.Builder) {
	if f.copyItemLine(p.Position(), buf) {
		return
	}

	buf.WriteString("poptag #")
	buf.WriteString(string(p.Tag))
	f.writeInlineComment(p.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatPushmeta formats a pushmeta directive.
func (f *run) formatPushmeta(p *ast.Pushmeta, buf *strings.Builder) {
	if f.copyItemLine(p.Position(), buf) {
		return
	}

	buf.WriteString("pushmeta ")
	buf.WriteString(p.Key)
	buf.WriteString(": ")
	buf.WriteString(p.Value)
	f.writeInlineComment(p.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatPopmeta formats a popmeta directive.
func (f *run) formatPopmeta(p *ast.Popmeta, buf *strings.Builder) {
	if f.copyItemLine(p.Position(), buf) {
		return
	}

	buf.WriteString("popmeta ")
	buf.WriteString(p.Key)
	buf.WriteByte(':')
	f.writeInlineComment(p.GetComment(), buf)
	buf.WriteByte('\n')
}

// formatTransaction formats a transaction directive with proper structure.
func (f *run) formatTransaction(t *ast.Transaction, buf *strings.Builder) {
	// bean-format never touches a header line (its pattern cannot cross a
	// quote), so the header keeps its spelling: txn, slash dates, the order
	// of tags and links.
	if f.copyDirectiveLine(t, buf) {
		f.formatTransactionBody(t, buf)
		return
	}

	buf.WriteString(f.dateText(t))
	buf.WriteByte(' ')
	buf.WriteString(t.Flag)

	if !t.Payee.IsEmpty() {
		buf.WriteByte(' ')
		f.formatRawString(t.Payee, buf)
	}

	// When a payee is present, the narration must be emitted even if empty:
	// a lone string after the flag is parsed as the narration, so dropping
	// the empty narration would turn the payee into a narration.
	if !t.Narration.IsEmpty() || !t.Payee.IsEmpty() {
		buf.WriteByte(' ')
		f.formatRawString(t.Narration, buf)
	}

	// Tags before links, like beancount's printer.
	for _, tag := range t.Tags {
		buf.WriteString(" #")
		buf.WriteString(string(tag))
	}

	for _, link := range t.Links {
		buf.WriteString(" ^")
		buf.WriteString(string(link))
	}

	// Append inline comment if present
	f.writeInlineComment(t.GetComment(), buf)

	buf.WriteByte('\n')
	f.formatTransactionBody(t, buf)
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
	if pos := line.Position(); f.copyItemLine(pos, buf) {
		f.verbatimLines[pos.Line] = true
		return
	}
	buf.WriteString(strings.Repeat(" ", f.Indentation))
	for i, tag := range line.Tags {
		if i > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteByte('#')
		buf.WriteString(string(tag))
	}
	for i, link := range line.Links {
		if i > 0 || len(line.Tags) > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteByte('^')
		buf.WriteString(string(link))
	}
	buf.WriteByte('\n')
}

func (f *run) formatTransactionBodyItem(item ast.TransactionBodyItem, buf *strings.Builder) {
	switch {
	case item.Posting != nil:
		f.formatPosting(item.Posting, buf)
	case item.Comment != nil:
		if f.PreserveComments && !f.verbatimLines[item.Comment.Position().Line] &&
			!f.writeTriviaLine(item.Comment.Position(), buf) {
			buf.WriteString(strings.Repeat(" ", f.indent))
			f.formatComment(item.Comment, buf)
		}
	case item.BlankLine != nil:
		if f.PreserveBlanks {
			f.writeTriviaLine(item.BlankLine.Position(), buf)
		}
	}
}

// formatPosting formats a single posting with proper alignment.
func (f *run) formatPosting(p *ast.Posting, buf *strings.Builder) {
	line := f.postingLayout(p)
	if f.writeCopied(line, buf) {
		f.verbatimLines[p.Position().Line] = true
		f.formatMetadata(p.Metadata, buf)
		return
	}
	if line.kind == alignLine && line.rest != "" {
		buf.WriteString(line.prefix)
		buf.WriteString(strings.Repeat(" ", f.columns.padding(runewidth.StringWidth(line.prefix), line.numberWidth)))
		buf.WriteString(line.number)
		buf.WriteByte(' ')
		buf.WriteString(line.rest)
		buf.WriteByte('\n')
		f.verbatimLines[p.Position().Line] = true
		f.formatMetadata(p.Metadata, buf)
		return
	}

	indent := f.indent
	buf.WriteString(strings.Repeat(" ", indent))

	currentWidth := indent

	if p.Flag != "" {
		buf.WriteString(p.Flag)
		buf.WriteByte(' ')
		currentWidth += 2
	}

	buf.WriteString(string(p.Account))
	currentWidth += runewidth.StringWidth(string(p.Account))

	if p.Amount != nil {
		f.formatAmountAligned(p.Amount, currentWidth, buf)

		if p.Cost != nil {
			buf.WriteByte(' ')
			f.formatCost(p.Cost, buf)
		}

		if p.Price != nil {
			if p.PriceTotal {
				buf.WriteString(" @@")
			} else {
				buf.WriteString(" @")
			}
			// Partial annotations (bare @, number-only, currency-only) print
			// only the components present in the source.
			if value := numberText(p.Price); value != "" {
				buf.WriteByte(' ')
				buf.WriteString(value)
			}
			if p.Price.Currency != "" {
				buf.WriteByte(' ')
				buf.WriteString(p.Price.Currency)
			}
		}
	}

	// Append inline comment if present
	f.writeInlineComment(p.GetComment(), buf)

	buf.WriteByte('\n')

	// Metadata, on the lines below; kept as written when possible.
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

// formatAmountAligned formats an amount with proper alignment to the currency column.
// Uses the raw token (with commas) if available for perfect round-trip formatting.
func (f *run) formatAmountAligned(amount *ast.Amount, currentWidth int, buf *strings.Builder) {
	if amount == nil {
		return
	}

	// Use raw value if available (preserves formatting like commas), otherwise use canonical value
	displayValue := numberText(amount)

	if !isAlignedAmount(amount) || !isValidNumericValue(amount.Value) {
		// Joined with single spaces, leaving out the missing part.
		buf.WriteString(strings.Repeat(" ", MinimumSpacing))
		buf.WriteString(strings.Join(slices.DeleteFunc([]string{displayValue, amount.Currency}, func(s string) bool { return s == "" }), " "))
		return
	}

	buf.WriteString(strings.Repeat(" ", f.columns.padding(currentWidth, runewidth.StringWidth(displayValue))))
	buf.WriteString(displayValue)
	buf.WriteByte(' ')
	buf.WriteString(amount.Currency)
}

// formatCost formats a cost specification.
func (f *run) formatCost(cost *ast.Cost, buf *strings.Builder) {
	if cost == nil {
		return
	}

	if cost.IsTotal {
		buf.WriteString("{{")
	} else {
		buf.WriteByte('{')
	}

	if cost.IsEmpty() {
		buf.WriteByte('}')
		return
	}

	// Emit present components separated by commas; any of amount, date,
	// and label may be absent (e.g. a date-only spec {2020-02-01}).
	first := true
	writeSeparator := func() {
		if !first {
			buf.WriteString(", ")
		}
		first = false
	}

	if cost.Amount != nil {
		writeSeparator()
		// Any of the numbers and the currency may be left out: {USD},
		// {10}, {# 5 USD}, {5 # USD}.
		var words []string
		if cost.Amount.Value != "" {
			words = append(words, numberText(cost.Amount))
		}
		if cost.Total != nil {
			words = append(words, "#")
			if cost.Total.Value != "" {
				words = append(words, numberText(cost.Total))
			}
		}
		if cost.Amount.Currency != "" {
			words = append(words, cost.Amount.Currency)
		}
		buf.WriteString(strings.Join(words, " "))
	}

	if cost.Date != nil {
		writeSeparator()
		buf.WriteString(cost.Date.String())
	}

	if cost.Label != "" {
		writeSeparator()
		buf.WriteByte('"')
		buf.WriteString(f.escapeString(cost.Label))
		buf.WriteByte('"')
	}

	if cost.IsMerge {
		writeSeparator()
		buf.WriteByte('*')
	}

	if cost.IsTotal {
		buf.WriteString("}}")
	} else {
		buf.WriteByte('}')
	}
}

// formatMetadataValue formats a typed metadata value.
func (f *run) formatMetadataValue(value *ast.MetadataValue, buf *strings.Builder) {
	if value == nil {
		return
	}

	switch {
	case value.StringValue != nil:
		f.formatRawString(*value.StringValue, buf)
	case value.Date != nil:
		buf.WriteString(value.Date.String())
	case value.Account != nil:
		buf.WriteString(string(*value.Account))
	case value.Currency != nil:
		buf.WriteString(*value.Currency)
	case value.Tag != nil:
		buf.WriteByte('#')
		buf.WriteString(string(*value.Tag))
	case value.Link != nil:
		buf.WriteByte('^')
		buf.WriteString(string(*value.Link))
	case value.Number != nil:
		buf.WriteString(*value.Number)
	case value.Amount != nil:
		buf.WriteString(value.Amount.Value)
		buf.WriteByte(' ')
		buf.WriteString(value.Amount.Currency)
	case value.Boolean != nil:
		if *value.Boolean {
			buf.WriteString("TRUE")
		} else {
			buf.WriteString("FALSE")
		}
	}
}

// formatMetadata formats metadata entries with proper indentation.
func (f *run) formatMetadata(metadata []*ast.Metadata, buf *strings.Builder) {
	if len(metadata) == 0 {
		return
	}

	lastVerbatimLine := 0
	for _, m := range metadata {
		// Metadata without a position was pushed (pushmeta), not written
		// on the directive; bean-format leaves it out.
		if m.Position().Line == 0 {
			continue
		}
		for _, c := range m.Comments {
			if f.PreserveComments && !f.verbatimLines[c.Position().Line] &&
				!f.writeTriviaLine(c.Position(), buf) {
				buf.WriteString(strings.Repeat(" ", f.Indentation))
				f.formatComment(c, buf)
			}
		}
		// bean-format leaves metadata lines untouched; preserve the original
		// line (indentation and spacing) whenever the entry owns it. A single
		// source line may hold several metadata entries; emit it only once.
		if line := m.Position().Line; line > 0 && line == lastVerbatimLine {
			continue
		}
		if pos := m.Position(); f.copyItemLine(pos, buf) {
			lastVerbatimLine = pos.Line
			f.verbatimLines[pos.Line] = true
			continue
		}
		buf.WriteString(strings.Repeat(" ", f.Indentation))
		buf.WriteString(m.Key)
		buf.WriteByte(':')
		if m.Value != nil {
			buf.WriteByte(' ')
		}
		f.formatMetadataValue(m.Value, buf)
		buf.WriteByte('\n')
	}
}
