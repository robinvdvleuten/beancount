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

// minimumSpacing is the number of spaces bean-format writes at least
// between an aligned line's prefix and its number.
const minimumSpacing = 2

// Formatter holds the options of a formatting: the column widths numbers
// are aligned to, bean-format's --prefix-width, --num-width and
// --currency-column.
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
	// source, ties broken by the widest, as bean-format's.
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

// New creates a new Formatter with the given options.
func New(opts ...Option) *Formatter {
	f := &Formatter{}

	for _, opt := range opts {
		opt(f)
	}

	return f
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
		record(f.directiveLayout(directive))
		if txn, ok := directive.(*ast.Transaction); ok {
			for _, posting := range txn.Postings {
				record(f.postingLayout(posting))
			}
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
		return max(c.currency-prefixWidth-numberWidth-2, minimumSpacing)
	}
	return max(c.prefix-prefixWidth, 0) + minimumSpacing + max(c.number-numberWidth, 0)
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

// resolveIndent returns the posting indent for a run, as bean-format
// computes it: the most frequent indent of the lines it re-indents wins,
// with ties broken by the widest indent.
func (f *run) resolveIndent(tree *ast.AST) int {
	frequencies := make(map[int]int)
	for _, directive := range tree.Directives {
		txn, ok := directive.(*ast.Transaction)
		if !ok {
			continue
		}
		for _, posting := range txn.Postings {
			if width, ok := postingIndent(f.source.line(posting.Position().Line)); ok {
				frequencies[width]++
			}
		}
	}

	indent, count := 0, 0
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

// copyItemLine copies the source line of an item starting at pos: a
// metadata, tags, comment or blank line, or an undated directive.
func (f *run) copyItemLine(pos ast.Position, buf *strings.Builder) {
	f.writeLine(pos, plainLayout(f.source.itemLine(pos.Line, pos.Column)), buf)
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
// the writer. The source must be the text the AST was parsed from: every
// line is copied from it or aligned within it, so an item that does not
// own its source line, or a directive without a valid date, is an error
// naming the item's position, and nothing is written.
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
	// items exactly as collected, then an unterminated whitespace line,
	// which no item stands for.
	if r.source.unterminated != "" && (len(items) == 0 || items[len(items)-1].line < len(r.source.lines)) {
		buf.WriteString(r.source.unterminated)
		buf.WriteByte('\n')
	}
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

	for _, comment := range tree.Comments {
		if comment != nil {
			items = append(items, astItem{line: comment.Position().Line, comment: comment})
		}
	}

	for _, blankLine := range tree.BlankLines {
		if blankLine != nil {
			items = append(items, astItem{line: blankLine.Position().Line, blankLine: blankLine})
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

// formatDirective formats a directive. A dated line bean-format's pattern
// matches, a balance's or a price's, is aligned; it leaves every other
// directive's line untouched, and so its header keeps its spelling: txn,
// slash dates, the order of tags and links.
func (f *run) formatDirective(d ast.Directive, buf *strings.Builder) {
	// Only a tree built by hand has a directive without a valid date,
	// and so no source line of its own.
	if d.Date().String() == "" {
		f.fail(d.Position())
		return
	}
	f.writeLine(d.Position(), f.directiveLayout(d), buf)
	if txn, ok := d.(*ast.Transaction); ok {
		f.formatTransactionBody(txn, buf)
		return
	}
	f.formatMetadata(d.GetMetadata(), buf)
}

// directiveLayout reads a dated directive's line.
func (f *run) directiveLayout(d ast.Directive) lineLayout {
	text, owned := f.source.directiveLine(d)
	return layout(text, owned, f.indent)
}

// writeLine writes the line of the item at pos: aligned as bean-format
// aligns it, or copied when bean-format leaves it alone. An item that does
// not own its line fails the run.
func (f *run) writeLine(pos ast.Position, line lineLayout, buf *strings.Builder) {
	switch line.kind {
	case copyLine:
		buf.WriteString(line.text)
	case alignLine:
		buf.WriteString(line.prefix)
		buf.WriteString(strings.Repeat(" ", f.columns.padding(runewidth.StringWidth(line.prefix), line.numberWidth)))
		buf.WriteString(line.number)
		buf.WriteByte(' ')
		buf.WriteString(line.rest)
	default:
		f.fail(pos)
		return
	}
	buf.WriteByte('\n')
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
		if !f.verbatimLines[item.Comment.Position().Line] {
			f.copyItemLine(item.Comment.Position(), buf)
		}
	}
}

// formatPosting writes a posting's line, aligned or copied, and the
// metadata lines below it.
func (f *run) formatPosting(p *ast.Posting, buf *strings.Builder) {
	f.writeLine(p.Position(), f.postingLayout(p), buf)
	if f.err != nil {
		return
	}
	f.verbatimLines[p.Position().Line] = true
	f.formatMetadata(p.Metadata, buf)
}

// postingLayout reads a posting's line, which must hold the posting: its
// flag, then its account.
func (f *run) postingLayout(p *ast.Posting) lineLayout {
	text, owned := f.source.itemLine(p.Position().Line, p.Position().Column)
	body, ok := strings.CutPrefix(strings.TrimLeft(text, " \t"), p.Flag)
	if !ok || !strings.HasPrefix(strings.TrimLeft(body, " \t"), string(p.Account)) {
		owned = false
	}
	return layout(text, owned, f.indent)
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
			if !f.verbatimLines[c.Position().Line] {
				f.copyItemLine(c.Position(), buf)
			}
		}
		f.copyItemLine(m.Position(), buf)
		f.verbatimLines[m.Position().Line] = true
	}
}
