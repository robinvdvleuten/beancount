// Package formatter provides formatting functionality for Beancount files with automatic
// alignment and comment preservation. It handles the proper spacing and alignment of
// currencies, numbers, and account names while preserving the original formatting intent.
//
// The formatter supports customizable column widths for account names, numbers, and
// currency positions, and can preserve comments and blank lines from the source file.
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
	"regexp"
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

	// PreserveComments controls whether comments are preserved during formatting.
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

	// columns is the number layout for this Format run.
	columns columns

	// resolvedIndent is the posting indent used for this Format run: the most
	// frequent posting indent found in the source (ties broken by the widest,
	// matching bean-format), or Indentation when explicit or undeterminable.
	resolvedIndent int

	// StringEscapeStyle controls how strings are escaped in the output.
	// Default: EscapeStyleCStyle
	StringEscapeStyle StringEscapeStyle

	// sourceLines holds the original source lines for preserving spacing.
	// This is set during Format() and cleared after.
	sourceLines []string

	// linesWithMultipleItems tracks which lines have multiple directives/items.
	// Lines with multiple items should not have their original content preserved
	// as it may contain content from multiple directives.
	// This is set during Format() and cleared after.
	linesWithMultipleItems map[int]bool

	// verbatimLines tracks source lines already emitted verbatim (metadata
	// preservation), so trivia parsed from those lines (inline comments) is
	// not emitted a second time. Set during Format() and cleared after.
	verbatimLines map[int]bool
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

// WithPreserveComments enables or disables comment preservation.
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
func (f *Formatter) calculateWidthMetrics(tree *ast.AST) widthMetrics {
	metrics := widthMetrics{}

	// bean-format decouples the two maxima: every number line is rendered as
	// prefix ljust(maxPrefix) + two spaces + number rjust(maxNum) + space +
	// currency, so the widest prefix and the widest number may come from
	// different lines. Prefix widths exclude trailing spacing.
	record := func(prefixWidth int, number string) {
		metrics.maxPrefixWidth = max(metrics.maxPrefixWidth, prefixWidth)
		metrics.maxNumWidth = max(metrics.maxNumWidth, runewidth.StringWidth(number))
	}

	for _, directive := range tree.Directives {
		switch d := directive.(type) {
		case *ast.Transaction:
			for _, posting := range d.Postings {
				if posting.Flag != "" || !isAlignedAmount(posting.Amount) {
					continue
				}
				// bean-format quirk: width maxima come from the original,
				// un-normalized prefixes, even though emission uses the
				// normalized indent.
				indent := f.postingIndent()
				if column := posting.Position().Column; column > 1 {
					indent = column - 1
				}
				record(indent+runewidth.StringWidth(string(posting.Account)), amountDisplayValue(posting.Amount))
			}

		case *ast.Balance:
			if prefix, number, ok := f.balanceLayout(d); ok {
				record(runewidth.StringWidth(prefix), number)
			}

		case *ast.Price:
			if prefix, number, ok := f.priceLayout(d); ok {
				record(runewidth.StringWidth(prefix), number)
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
func (f *Formatter) resolveColumns(tree *ast.AST) columns {
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

// resolveIndent returns the posting indent for a Format run. Unless an
// explicit indentation was configured, it follows bean-format: the most
// frequent posting indent in the source wins, with ties broken by the
// widest indent.
func (f *Formatter) resolveIndent(tree *ast.AST) int {
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

// postingIndent returns the resolved posting indent, falling back to the
// configured indentation outside a Format run (e.g. FormatTransaction).
func (f *Formatter) postingIndent() int {
	if f.resolvedIndent > 0 {
		return f.resolvedIndent
	}
	return f.Indentation
}

// getOriginalLine returns the original line from source by line number (1-indexed).
// Returns empty string if line number is out of bounds.
func (f *Formatter) getOriginalLine(lineNum int) string {
	if lineNum < 1 || lineNum > len(f.sourceLines) {
		return ""
	}
	return f.sourceLines[lineNum-1]
}

// canPreserveDirectiveLine checks if a directive line can be preserved.
// For date-prefixed directives, only allows preservation if the line contains the date.
// This prevents preserving incomplete directives that span multiple lines.
func (f *Formatter) canPreserveDirectiveLine(lineNum int, date *ast.Date) bool {
	if date == nil {
		return true // Non-date-prefixed directives can always be preserved
	}

	originalLine := f.getOriginalLine(lineNum)
	if originalLine == "" {
		return false
	}

	// Check if the line starts with the date (ignoring leading whitespace),
	// spelled with dashes or slashes.
	dateStr := date.String()
	trimmedLine := strings.TrimSpace(originalLine)
	return strings.HasPrefix(trimmedLine, dateStr) ||
		strings.HasPrefix(trimmedLine, strings.ReplaceAll(dateStr, "-", "/"))
}

// tryPreserveOriginalLine attempts to preserve the original source line for a directive.
// If the original line is available and doesn't contain multiple items, it writes the trimmed line
// to buf and returns true. If the original line is not available or contains multiple items,
// it returns false and the caller should reconstruct the directive. This helper reduces
// duplication across formatting functions.
func (f *Formatter) tryPreserveOriginalLine(lineNum int, buf *strings.Builder) bool {
	// Don't preserve lines that have multiple items (directives/options/etc)
	// as they may contain partial content from multiple directives
	if f.linesWithMultipleItems != nil && f.linesWithMultipleItems[lineNum] {
		return false
	}

	if originalLine := f.getOriginalLine(lineNum); originalLine != "" {
		// Trailing whitespace stays, as bean-format keeps it.
		trimmedLine := strings.TrimLeft(originalLine, " \t")
		if hasOpenStringLiteral(trimmedLine) {
			return false
		}
		buf.WriteString(trimmedLine)
		buf.WriteByte('\n')
		return true
	}
	return false
}

func hasOpenStringLiteral(line string) bool {
	inString := false
	escaped := false

	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ';':
			if !inString {
				return false
			}
		case '"':
			if !inString {
				inString = true
				continue
			}
			if !escaped {
				inString = false
			}
		case '\\':
			if inString {
				escaped = !escaped
				continue
			}
		default:
		}
		escaped = false
	}

	return inString
}

// hasAnyInlineMetadata returns true if any of the metadata entries are marked as inline.
// This allows the formatter to detect inline metadata from the AST rather than parsing source text.
func hasAnyInlineMetadata(metadata []*ast.Metadata) bool {
	for _, m := range metadata {
		if m.Inline {
			return true
		}
	}
	return false
}

// Format formats the given AST and writes the output to the writer.
// Comments and blank lines from the AST are preserved based on Formatter configuration.
func (f *Formatter) Format(ctx context.Context, tree *ast.AST, sourceContent []byte, w io.Writer) error {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

	// Store source lines for preserving original spacing, before the
	// widths, which read spellings from them.
	// Must split on \r\n, \r, and \n to match the lexer's lineBreakLenAt semantics.
	f.sourceLines = ast.SplitSourceLines(string(sourceContent))
	f.verbatimLines = make(map[int]bool)
	defer func() {
		f.sourceLines = nil            // Clear after formatting
		f.linesWithMultipleItems = nil // Clear after formatting
		f.verbatimLines = nil          // Clear after formatting
	}()

	// Determine the currency column based on the configuration
	widthTimer := collector.Start("formatter.width_calculation")
	f.resolvedIndent = f.resolveIndent(tree)
	f.columns = f.resolveColumns(tree)
	widthTimer.End()

	// Use a string builder to buffer all output, then write once
	var buf strings.Builder

	// Estimate initial capacity to reduce allocations
	estimatedSize := (len(tree.Options) + len(tree.Includes) + len(tree.Directives)) * 100
	buf.Grow(estimatedSize)

	// Collect all items with their positions using the Positioned interface
	formatTimer := collector.Start("formatter.item_collection")
	items := f.collectItems(tree)
	formatTimer.End()

	// Build a set of lines that have multiple items (can't preserve those lines safely)
	f.linesWithMultipleItems = ast.LinesWithMultipleItems(tree)

	// Format all items in order
	directiveTimer := collector.Start("formatter.directive_formatting")
	for _, item := range items {
		// Skip invalid directives, such as directives with invalid dates.
		if item.directive != nil && !isValidDirective(item.directive) {
			continue
		}

		f.formatItem(item, &buf)
	}
	directiveTimer.End()

	// bean-format preserves blank lines at the edges of the file; emit the
	// items exactly as collected.
	_, err := w.Write([]byte(buf.String()))
	return err
}

// collectItems gathers all AST items into a sorted slice by line position.
func (f *Formatter) collectItems(tree *ast.AST) []astItem {
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
func (f *Formatter) formatItem(item astItem, buf *strings.Builder) {
	switch {
	case item.comment != nil:
		if f.verbatimLines[item.comment.Position().Line] {
			return // Already contained in a verbatim-preserved line.
		}
		if !f.writeTriviaLine(item.comment.Position().Line, item.comment.Content, buf) {
			f.formatComment(item.comment, buf)
		}
	case item.blankLine != nil:
		f.writeTriviaLine(item.blankLine.Position().Line, "", buf)
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
func (f *Formatter) formatComment(c *ast.Comment, buf *strings.Builder) {
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
// false, writing nothing, when the source line holds more than content.
// Without source, a blank line is written empty.
func (f *Formatter) writeTriviaLine(line int, content string, buf *strings.Builder) bool {
	original := f.getOriginalLine(line)
	if strings.TrimSpace(original) != strings.TrimSpace(content) {
		return false
	}
	buf.WriteString(original)
	buf.WriteByte('\n')
	return true
}

// FormatTransaction formats a single transaction and writes the output to the writer.
// This method is useful for rendering individual transactions, such as in error messages.
// The currency column is calculated from the transaction itself if not explicitly set.
func (f *Formatter) FormatTransaction(txn *ast.Transaction, w io.Writer) error {
	f.columns = f.resolveColumns(&ast.AST{Directives: []ast.Directive{txn}})

	// Use a string builder to buffer output
	var buf strings.Builder
	buf.Grow(200) // Reasonable estimate for a transaction

	// Format the transaction
	f.formatTransaction(txn, &buf)

	// Write output
	_, err := w.Write([]byte(buf.String()))
	return err
}

// formatDirective formats a directive based on its type.
func (f *Formatter) formatDirective(d ast.Directive, buf *strings.Builder) {
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
func (f *Formatter) formatOption(opt *ast.Option, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(opt.Position().Line, buf) {
		return
	}

	buf.WriteString("option ")
	f.formatRawString(opt.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(opt.Value, buf)
	if opt.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(opt.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatInclude formats an include directive.
func (f *Formatter) formatInclude(inc *ast.Include, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(inc.Position().Line, buf) {
		return
	}

	buf.WriteString("include ")
	f.formatRawString(inc.Filename, buf)
	if inc.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(inc.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatCommodity formats a commodity directive.
func (f *Formatter) formatCommodity(c *ast.Commodity, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(c.Position().Line, c.Date()) {
		if f.tryPreserveOriginalLine(c.Position().Line, buf) {
			f.formatMetadata(c.Metadata, buf)
			return
		}
	}

	buf.WriteString(c.Date().String())
	buf.WriteString(" commodity ")
	buf.WriteString(c.Currency)
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatOpen formats an open directive.
func (f *Formatter) formatOpen(o *ast.Open, buf *strings.Builder) {
	// Open directives are single-line and carry no number to realign, so
	// bean-format leaves them untouched; preserve the original line whenever
	// it contains the whole directive.
	if f.canPreserveDirectiveLine(o.Position().Line, o.Date()) {
		originalLine := f.getOriginalLine(o.Position().Line)
		if strings.Contains(originalLine, string(o.Account)) && openLineComplete(originalLine, o) {
			if f.tryPreserveOriginalLine(o.Position().Line, buf) {
				f.formatMetadata(o.Metadata, buf)
				return
			}
		}
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

	buf.WriteByte('\n')
	f.formatMetadata(o.Metadata, buf)
}

// openLineComplete reports whether the original source line contains every
// component of the open directive, so preserving it verbatim loses nothing.
func openLineComplete(line string, o *ast.Open) bool {
	for _, currency := range o.ConstraintCurrencies {
		if !strings.Contains(line, currency) {
			return false
		}
	}
	return o.BookingMethod == "" || strings.Contains(line, `"`+o.BookingMethod+`"`)
}

// formatClose formats a close directive.
func (f *Formatter) formatClose(c *ast.Close, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(c.Position().Line, c.Date()) {
		originalLine := f.getOriginalLine(c.Position().Line)
		if strings.Contains(originalLine, string(c.Account)) {
			if f.tryPreserveOriginalLine(c.Position().Line, buf) {
				f.formatMetadata(c.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(c.Date().String())
	buf.WriteString(" close ")
	buf.WriteString(string(c.Account))
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatBalance formats a balance directive.
func (f *Formatter) formatBalance(b *ast.Balance, buf *strings.Builder) {
	f.formatDatedAmount(b, string(b.Account), balanceAmountText(b), balanceCurrency(b), buf)
}

// balanceLayout splits a balance line into bean-format's aligned prefix
// and number.
func (f *Formatter) balanceLayout(b *ast.Balance) (prefix, number string, ok bool) {
	if b.Amount == nil || balanceCurrency(b) == "" {
		return "", "", false
	}
	return datedAmountLayout(f.datedHead(b, string(b.Account)), balanceAmountText(b))
}

// balanceAmountText spells a balance's amount, with its tolerance, without
// the currency.
func balanceAmountText(b *ast.Balance) string {
	if b.Amount == nil {
		return ""
	}
	text := amountDisplayValue(b.Amount)
	if b.Tolerance != nil {
		text += " ~ " + amountDisplayValue(b.Tolerance)
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
func (f *Formatter) datedHead(d ast.Directive, subject string) string {
	return f.dateText(d) + " " + string(d.Kind()) + " " + subject
}

// dateText spells a directive's date as the source does, with dashes or
// slashes; without source, with dashes.
func (f *Formatter) dateText(d ast.Directive) string {
	date := d.Date().String()
	slashed := strings.ReplaceAll(date, "-", "/")
	if strings.HasPrefix(f.getOriginalLine(d.Position().Line), slashed) {
		return slashed
	}
	return date
}

// formatDatedAmount writes a dated directive that ends in an amount,
// aligning the number bean-format aligns and keeping any text before it
// (an expression's leading operands, a balance's amount before its
// tolerance) in the prefix.
func (f *Formatter) formatDatedAmount(d ast.Directive, subject, text, currency string, buf *strings.Builder) {
	head := f.datedHead(d, subject)
	if prefix, number, ok := datedAmountLayout(head, text); ok && currency != "" {
		buf.WriteString(prefix)
		buf.WriteString(strings.Repeat(" ", f.columns.padding(runewidth.StringWidth(prefix), runewidth.StringWidth(number))))
		buf.WriteString(number)
		buf.WriteByte(' ')
		buf.WriteString(currency)
	} else {
		buf.WriteString(head)
		for _, part := range []string{text, currency} {
			if part != "" {
				buf.WriteByte(' ')
				buf.WriteString(part)
			}
		}
	}

	if d.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(d.GetComment().Content)
	}
	buf.WriteByte('\n')
	f.formatMetadata(d.GetMetadata(), buf)
}

// formatPad formats a pad directive.
func (f *Formatter) formatPad(p *ast.Pad, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(p.Position().Line, p.Date()) {
		originalLine := f.getOriginalLine(p.Position().Line)
		if strings.Contains(originalLine, string(p.Account)) && strings.Contains(originalLine, string(p.AccountPad)) {
			if f.tryPreserveOriginalLine(p.Position().Line, buf) {
				f.formatMetadata(p.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(p.Date().String())
	buf.WriteString(" pad ")
	buf.WriteString(string(p.Account))
	buf.WriteByte(' ')
	buf.WriteString(string(p.AccountPad))
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(p.Metadata, buf)
}

// formatNote formats a note directive.
func (f *Formatter) formatNote(n *ast.Note, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(n.Position().Line, n.Date()) && !hasAnyInlineMetadata(n.Metadata) {
		originalLine := f.getOriginalLine(n.Position().Line)
		if strings.Contains(originalLine, string(n.Account)) && strings.Contains(originalLine, "\"") {
			if f.tryPreserveOriginalLine(n.Position().Line, buf) {
				f.formatMetadata(n.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(n.Date().String())
	buf.WriteString(" note ")
	buf.WriteString(string(n.Account))
	buf.WriteByte(' ')
	f.formatRawString(n.Description, buf)
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(n.Metadata, buf)
}

// formatDocument formats a document directive.
func (f *Formatter) formatDocument(d *ast.Document, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(d.Position().Line, d.Date()) && !hasAnyInlineMetadata(d.Metadata) {
		originalLine := f.getOriginalLine(d.Position().Line)
		if strings.Contains(originalLine, string(d.Account)) && strings.Contains(originalLine, "\"") {
			if f.tryPreserveOriginalLine(d.Position().Line, buf) {
				f.formatMetadata(d.Metadata, buf)
				return
			}
		}
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
	buf.WriteByte('\n')
	f.formatMetadata(d.Metadata, buf)
}

// formatPrice formats a price directive.
func (f *Formatter) formatPrice(p *ast.Price, buf *strings.Builder) {
	f.formatDatedAmount(p, p.Commodity, amountDisplayValue(p.Amount), priceCurrency(p), buf)
}

// priceLayout splits a price line into bean-format's aligned prefix and
// number.
func (f *Formatter) priceLayout(p *ast.Price) (prefix, number string, ok bool) {
	if priceCurrency(p) == "" {
		return "", "", false
	}
	return datedAmountLayout(f.datedHead(p, p.Commodity), amountDisplayValue(p.Amount))
}

func priceCurrency(p *ast.Price) string {
	if p.Amount == nil {
		return ""
	}
	return p.Amount.Currency
}

// formatEvent formats an event directive.
func (f *Formatter) formatEvent(e *ast.Event, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(e.Position().Line, e.Date()) && !hasAnyInlineMetadata(e.Metadata) {
		originalLine := f.getOriginalLine(e.Position().Line)
		if strings.Count(originalLine, "\"") >= 4 {
			if f.tryPreserveOriginalLine(e.Position().Line, buf) {
				f.formatMetadata(e.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(e.Date().String())
	buf.WriteString(" event ")
	f.formatRawString(e.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(e.Value, buf)
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(e.Metadata, buf)
}

// formatQuery formats a query directive.
func (f *Formatter) formatQuery(q *ast.Query, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(q.Position().Line, q.Date()) && !hasAnyInlineMetadata(q.Metadata) {
		originalLine := f.getOriginalLine(q.Position().Line)
		if strings.Count(originalLine, "\"") >= 4 {
			if f.tryPreserveOriginalLine(q.Position().Line, buf) {
				f.formatMetadata(q.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(q.Date().String())
	buf.WriteString(" query ")
	f.formatRawString(q.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(q.QueryString, buf)
	if q.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(q.GetComment().Content)
	}
	buf.WriteByte('\n')
	f.formatMetadata(q.Metadata, buf)
}

// formatCustom formats a custom directive.
func (f *Formatter) formatCustom(c *ast.Custom, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(c.Position().Line, c.Date()) && !hasAnyInlineMetadata(c.Metadata) {
		originalLine := f.getOriginalLine(c.Position().Line)
		if strings.Contains(originalLine, "\"") {
			if f.tryPreserveOriginalLine(c.Position().Line, buf) {
				f.formatMetadata(c.Metadata, buf)
				return
			}
		}
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
			displayValue := val.Amount.Value
			if val.Amount.HasRaw() {
				displayValue = val.Amount.Raw
			}
			buf.WriteString(displayValue)
			buf.WriteByte(' ')
			buf.WriteString(val.Amount.Currency)
		} else if val.Number != nil {
			buf.WriteString(*val.Number)
		}
	}
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatPlugin formats a plugin directive.
func (f *Formatter) formatPlugin(p *ast.Plugin, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Position().Line, buf) {
		return
	}

	buf.WriteString("plugin ")
	f.formatRawString(p.Name, buf)
	if !p.Config.IsEmpty() {
		buf.WriteByte(' ')
		f.formatRawString(p.Config, buf)
	}
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatPushtag formats a pushtag directive.
func (f *Formatter) formatPushtag(p *ast.Pushtag, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Position().Line, buf) {
		return
	}

	buf.WriteString("pushtag #")
	buf.WriteString(string(p.Tag))
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatPoptag formats a poptag directive.
func (f *Formatter) formatPoptag(p *ast.Poptag, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Position().Line, buf) {
		return
	}

	buf.WriteString("poptag #")
	buf.WriteString(string(p.Tag))
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatPushmeta formats a pushmeta directive.
func (f *Formatter) formatPushmeta(p *ast.Pushmeta, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Position().Line, buf) {
		return
	}

	buf.WriteString("pushmeta ")
	buf.WriteString(p.Key)
	buf.WriteString(": ")
	buf.WriteString(p.Value)
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatPopmeta formats a popmeta directive.
func (f *Formatter) formatPopmeta(p *ast.Popmeta, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Position().Line, buf) {
		return
	}

	buf.WriteString("popmeta ")
	buf.WriteString(p.Key)
	buf.WriteByte(':')
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}
	buf.WriteByte('\n')
}

// formatTransaction formats a transaction directive with proper structure.
func (f *Formatter) formatTransaction(t *ast.Transaction, buf *strings.Builder) {
	// bean-format never touches a header line (its pattern cannot cross a
	// quote), so the header keeps its spelling: txn, slash dates, the order
	// of tags and links.
	if line := t.Position().Line; f.canPreserveDirectiveLine(line, t.Date()) && f.tryPreserveOriginalLine(line, buf) {
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

	for _, link := range t.Links {
		buf.WriteString(" ^")
		buf.WriteString(string(link))
	}

	for _, tag := range t.Tags {
		buf.WriteString(" #")
		buf.WriteString(string(tag))
	}

	// Append inline comment if present
	if t.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(t.GetComment().Content)
	}

	buf.WriteByte('\n')
	f.formatTransactionBody(t, buf)
}

// formatTransactionBody writes the lines after a transaction's header.
func (f *Formatter) formatTransactionBody(t *ast.Transaction, buf *strings.Builder) {
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
func (f *Formatter) formatLeadingTransactionBody(t *ast.Transaction, buf *strings.Builder) {
	metadata := t.Metadata
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

func (f *Formatter) formatTagsLinks(line *ast.TagsLinks, buf *strings.Builder) {
	if n := line.Position().Line; n > 0 && !f.linesWithMultipleItems[n] {
		if original := f.getOriginalLine(n); original != "" {
			buf.WriteString(original)
			buf.WriteByte('\n')
			f.verbatimLines[n] = true
			return
		}
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

func (f *Formatter) formatTransactionBodyItem(item ast.TransactionBodyItem, buf *strings.Builder) {
	switch {
	case item.Posting != nil:
		f.formatPosting(item.Posting, buf)
	case item.Comment != nil:
		if f.PreserveComments && !f.verbatimLines[item.Comment.Position().Line] &&
			!f.writeTriviaLine(item.Comment.Position().Line, item.Comment.Content, buf) {
			buf.WriteString(strings.Repeat(" ", f.postingIndent()))
			f.formatComment(item.Comment, buf)
		}
	case item.BlankLine != nil:
		if f.PreserveBlanks {
			f.writeTriviaLine(item.BlankLine.Position().Line, "", buf)
		}
	}
}

// formatPosting formats a single posting with proper alignment.
func (f *Formatter) formatPosting(p *ast.Posting, buf *strings.Builder) {
	if (p.Flag != "" || p.Amount != nil && !isAlignedAmount(p.Amount)) && f.writePostingLineAsWritten(p, buf) {
		f.formatMetadata(p.Metadata, buf)
		return
	}

	indent := f.postingIndent()
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
			if value := amountDisplayValue(p.Price); value != "" {
				buf.WriteByte(' ')
				buf.WriteString(value)
			}
			if p.Price.Currency != "" {
				buf.WriteByte(' ')
				buf.WriteString(p.Price.Currency)
			}
		}
	}

	// Append inline metadata (on same line as posting)
	for _, m := range p.Metadata {
		if m.Inline {
			buf.WriteString("  ")
			buf.WriteString(m.Key)
			buf.WriteByte(':')
			if m.Value != nil {
				buf.WriteByte(' ')
			}
			f.formatMetadataValue(m.Value, buf)
		}
	}

	// Append inline comment if present
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}

	buf.WriteByte('\n')

	// Block metadata, on the lines below; kept as written when possible.
	f.formatMetadata(p.Metadata, buf)
}

// alignedNumber is how bean-format's line pattern spells a number it
// aligns: an optional sign, digits with thousands commas, and an optional
// fraction. Expressions, repeated signs and parentheses do not match.
var alignedNumber = regexp.MustCompile(`^[-+]?\s*[\d,]+(?:\.\d*)?$`)

// isAlignedAmount reports whether bean-format aligns an amount: only a
// plainly spelled number followed by a currency matches its line pattern.
// Any other amount leaves the line as written.
func isAlignedAmount(amount *ast.Amount) bool {
	return amount != nil && amount.Currency != "" && alignedNumber.MatchString(amountDisplayValue(amount))
}

// datedAmountLayout splits a dated directive's amount text like
// bean-format's line pattern, whose prefix is the shortest one followed by
// a plainly spelled number and the currency: head plus any text before
// that number is the prefix. In "100.00 ~ 0.05" the tolerance is aligned,
// in "50 + 50" the "+ 50". It reports false when no suffix is a number.
func datedAmountLayout(head, text string) (prefix, number string, ok bool) {
	// Candidate numbers start after a run of spaces; the prefix before
	// them is right-trimmed, like bean-format's prefix.rstrip().
	for i := 0; i < len(text); i++ {
		if i > 0 && text[i-1] != ' ' || text[i] == ' ' {
			continue
		}
		if suffix := text[i:]; alignedNumber.MatchString(suffix) {
			if before := strings.TrimRight(text[:i], " "); before != "" {
				return head + " " + before, suffix, true
			}
			return head, suffix, true
		}
	}
	return "", "", false
}

// writePostingLineAsWritten copies a posting's source line, as bean-format
// leaves every line it does not align. A line starting with an account is
// re-indented to the posting indent, like bean-format re-indents them; a
// flagged posting's line is copied untouched. It reports false, writing
// nothing, when the source line is unavailable, shared with other items,
// or does not hold the whole posting.
func (f *Formatter) writePostingLineAsWritten(p *ast.Posting, buf *strings.Builder) bool {
	line := p.Position().Line
	original := f.getOriginalLine(line)
	if original == "" || f.linesWithMultipleItems[line] {
		return false
	}
	text := strings.TrimLeft(original, " \t")
	body, ok := strings.CutPrefix(text, p.Flag)
	if !ok {
		return false
	}
	// The line must hold the whole posting: its account, then its amount.
	rest, ok := strings.CutPrefix(strings.TrimLeft(body, " \t"), string(p.Account))
	if !ok {
		return false
	}
	if p.Amount != nil && (!strings.Contains(rest, amountDisplayValue(p.Amount)) || !strings.Contains(rest, p.Amount.Currency)) {
		return false
	}
	if p.Flag == "" {
		buf.WriteString(strings.Repeat(" ", f.postingIndent()))
		buf.WriteString(text)
	} else {
		buf.WriteString(original)
	}
	buf.WriteByte('\n')
	f.verbatimLines[line] = true
	return true
}

// isValidNumericValue checks if a value looks like a valid numeric amount.
func isValidNumericValue(value string) bool {
	if value == "" {
		return false
	}

	i := 0
	if value[0] == '+' || value[0] == '-' {
		i = 1
	}

	if i >= len(value) {
		return false
	}

	hasDigit := false
	for i < len(value) {
		c := value[i]
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c == '.' || c == ',' {
			// Allow decimal separators
		} else {
			return false
		}
		i++
	}

	return hasDigit
}

// formatAmountAligned formats an amount with proper alignment to the currency column.
// Uses the raw token (with commas) if available for perfect round-trip formatting.
func (f *Formatter) formatAmountAligned(amount *ast.Amount, currentWidth int, buf *strings.Builder) {
	if amount == nil {
		return
	}

	// Use raw value if available (preserves formatting like commas), otherwise use canonical value
	displayValue := amountDisplayValue(amount)

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

func amountDisplayValue(amount *ast.Amount) string {
	if amount != nil && amount.HasRaw() {
		return amount.Raw
	}
	if amount == nil {
		return ""
	}
	return amount.Value
}

// formatCost formats a cost specification.
func (f *Formatter) formatCost(cost *ast.Cost, buf *strings.Builder) {
	if cost == nil {
		return
	}

	if cost.IsTotal {
		buf.WriteString("{{")
	} else {
		buf.WriteByte('{')
	}

	if cost.IsMerge {
		buf.WriteByte('*')
		buf.WriteByte('}')
		return
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
		buf.WriteString(amountDisplayValue(cost.Amount))
		if cost.Total != nil {
			buf.WriteString(" # ")
			buf.WriteString(amountDisplayValue(cost.Total))
		}
		buf.WriteByte(' ')
		buf.WriteString(cost.Amount.Currency)
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

	if cost.IsTotal {
		buf.WriteString("}}")
	} else {
		buf.WriteByte('}')
	}
}

// formatMetadataValue formats a typed metadata value.
func (f *Formatter) formatMetadataValue(value *ast.MetadataValue, buf *strings.Builder) {
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
func (f *Formatter) formatMetadata(metadata []*ast.Metadata, buf *strings.Builder) {
	if len(metadata) == 0 {
		return
	}

	lastVerbatimLine := 0
	for _, m := range metadata {
		// Skip inline metadata - it's already been formatted on the directive line
		if m.Inline {
			continue
		}
		// bean-format leaves metadata lines untouched; preserve the original
		// line (indentation and spacing) whenever it is available. A single
		// source line may hold several metadata entries; emit it only once.
		if line := m.Position().Line; line > 0 && !f.linesWithMultipleItems[line] {
			if line == lastVerbatimLine {
				continue
			}
			original := f.getOriginalLine(line)
			// Only preserve lines that actually begin with this metadata key;
			// the line's start may belong to another construct (e.g. the tail
			// of a multiline string) that is rendered separately.
			indent := len(original) - len(strings.TrimLeft(original, " \t"))
			startsAtKey := original != "" && indent == m.Position().Column-1 &&
				strings.HasPrefix(original[indent:], m.Key)
			if startsAtKey && !hasOpenStringLiteral(original) {
				buf.WriteString(original)
				buf.WriteByte('\n')
				lastVerbatimLine = line
				f.verbatimLines[line] = true
				continue
			}
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
