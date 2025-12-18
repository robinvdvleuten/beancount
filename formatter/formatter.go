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
	"reflect"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
)

const (
	// DefaultCurrencyColumn is the fallback column position when no amounts are found
	// in the input (e.g., empty file). This is NOT the default behavior - the formatter
	// auto-calculates the currency column from content by default to match bean-format.
	DefaultCurrencyColumn = 52

	// DefaultIndentation is the default indentation for postings and metadata
	DefaultIndentation = 4

	// DefaultPrefixWidth is the default width for account names when auto-calculating
	DefaultPrefixWidth = 40

	// DefaultNumWidth is the default width for numeric values when auto-calculating
	DefaultNumWidth = 10

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
	return runewidth.StringWidth(d.Directive()) + 1
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

// WithIndentation sets the indentation level for postings and metadata.
func WithIndentation(indent int) Option {
	return func(f *Formatter) {
		f.Indentation = indent
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
// - Transactions must have at least 2 postings
// - Date-based directives must have valid dates (not empty string representation)
func isValidDirective(d ast.Directive) bool {
	// Check transaction postings
	if txn, ok := d.(*ast.Transaction); ok {
		return len(txn.Postings) >= 2
	}

	// Check dated directives for valid dates
	val := reflect.ValueOf(d)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	dateField := val.FieldByName("Date")
	if !dateField.IsValid() {
		return true // Not a dated directive, so it's valid
	}

	// Get the Date pointer and check its string representation
	datePtr, ok := dateField.Interface().(*ast.Date)
	if !ok || datePtr == nil {
		return false
	}

	return datePtr.String() != ""
}

// widthMetrics holds calculated width information for formatting.
type widthMetrics struct {
	maxPrefixWidth int // Maximum width of account prefix (indentation + flag + account + spacing)
	maxNumWidth    int // Maximum width of numeric values
	currencyColumn int // Calculated currency column position
}

// calculateWidthMetrics performs a single pass through the AST to calculate all width metrics.
func (f *Formatter) calculateWidthMetrics(tree *ast.AST) widthMetrics {
	metrics := widthMetrics{}

	for _, directive := range tree.Directives {
		switch d := directive.(type) {
		case *ast.Transaction:
			for _, posting := range d.Postings {
				if posting.Amount != nil {
					// Calculate prefix width: indentation + flag + account + spacing
					prefixWidth := f.Indentation
					if posting.Flag != "" {
						prefixWidth += 2 // flag + space
					}
					prefixWidth += runewidth.StringWidth(string(posting.Account)) + MinimumSpacing
					metrics.maxPrefixWidth = max(metrics.maxPrefixWidth, prefixWidth)

					// Calculate number width
					numWidth := runewidth.StringWidth(posting.Amount.Value)
					metrics.maxNumWidth = max(metrics.maxNumWidth, numWidth)

					// Calculate total width for currency column
					totalWidth := prefixWidth + numWidth
					metrics.currencyColumn = max(metrics.currencyColumn, totalWidth)
				}
			}

		case *ast.Balance:
			if d.Amount != nil {
				// Calculate width: date + "balance" + account + spacing + number
				width := DateWidth + 1 + directiveKeywordWidth(d) + runewidth.StringWidth(string(d.Account)) + MinimumSpacing
				numWidth := runewidth.StringWidth(d.Amount.Value)
				metrics.maxNumWidth = max(metrics.maxNumWidth, numWidth)
				totalWidth := width + numWidth
				metrics.currencyColumn = max(metrics.currencyColumn, totalWidth)
			}

		case *ast.Price:
			if d.Amount != nil {
				// Calculate width: date + "price" + commodity + spacing + number
				width := DateWidth + 1 + directiveKeywordWidth(d) + runewidth.StringWidth(d.Commodity) + MinimumSpacing
				numWidth := runewidth.StringWidth(d.Amount.Value)
				metrics.maxNumWidth = max(metrics.maxNumWidth, numWidth)
				totalWidth := width + numWidth
				metrics.currencyColumn = max(metrics.currencyColumn, totalWidth)
			}
		}
	}

	return metrics
}

// calculateCurrencyColumn auto-calculates the currency column from AST content.
// Returns the default column if no amounts are found.
func (f *Formatter) calculateCurrencyColumn(tree *ast.AST) int {
	metrics := f.calculateWidthMetrics(tree)
	if metrics.currencyColumn > 0 {
		return metrics.currencyColumn + MinimumSpacing
	}
	return DefaultCurrencyColumn
}

// determineCurrencyColumn calculates the currency column based on configuration.
// Priority: explicit widths (PrefixWidth/NumWidth) > auto-calculated from content > default.
func (f *Formatter) determineCurrencyColumn(tree *ast.AST) int {
	// If explicit widths are provided, use those
	if f.PrefixWidth > 0 || f.NumWidth > 0 {
		metrics := f.calculateWidthMetrics(tree)

		prefixWidth := f.PrefixWidth
		if prefixWidth == 0 {
			prefixWidth = metrics.maxPrefixWidth
			if prefixWidth == 0 {
				prefixWidth = DefaultPrefixWidth
			}
		}

		numWidth := f.NumWidth
		if numWidth == 0 {
			numWidth = metrics.maxNumWidth + MinimumSpacing
			if numWidth == MinimumSpacing {
				numWidth = DefaultNumWidth
			}
		}

		return prefixWidth + numWidth
	}

	// Auto-calculate from content
	return f.calculateCurrencyColumn(tree)
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

	// Check if the line starts with the date (ignoring leading whitespace)
	dateStr := date.String()
	trimmedLine := strings.TrimSpace(originalLine)
	return strings.HasPrefix(trimmedLine, dateStr)
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
		buf.WriteString(strings.TrimSpace(originalLine))
		buf.WriteByte('\n')
		return true
	}
	return false
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

	// Determine the currency column based on the configuration
	widthTimer := collector.Start("formatter.width_calculation")
	if f.CurrencyColumn == 0 {
		f.CurrencyColumn = f.determineCurrencyColumn(tree)
	}
	widthTimer.End()

	// Store source lines for preserving original spacing
	f.sourceLines = strings.Split(string(sourceContent), "\n")
	defer func() {
		f.sourceLines = nil            // Clear after formatting
		f.linesWithMultipleItems = nil // Clear after formatting
	}()

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
		// Skip invalid directives (transactions with < 2 postings, or directives with invalid dates)
		if item.directive != nil && !isValidDirective(item.directive) {
			continue
		}

		f.formatItem(item, &buf)
	}
	directiveTimer.End()

	// Normalize output: trim leading/trailing blank lines to ensure idempotency
	// This happens when directives are skipped or blank lines appear at edges
	output := strings.Trim(buf.String(), "\n")
	if output != "" {
		output += "\n"
	}

	// Write all output at once
	_, err := w.Write([]byte(output))
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
		f.formatComment(item.comment, buf)
	case item.blankLine != nil:
		buf.WriteByte('\n')
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
	buf.WriteByte('\n')
}

// FormatTransaction formats a single transaction and writes the output to the writer.
// This method is useful for rendering individual transactions, such as in error messages.
// The currency column is calculated from the transaction itself if not explicitly set.
func (f *Formatter) FormatTransaction(txn *ast.Transaction, w io.Writer) error {
	// Determine the currency column if not set
	if f.CurrencyColumn == 0 {
		// Create a minimal AST with just this transaction to calculate metrics
		tree := &ast.AST{
			Directives: []ast.Directive{txn},
		}
		f.CurrencyColumn = f.determineCurrencyColumn(tree)
	}

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
	case *ast.Custom:
		f.formatCustom(directive, buf)
	case *ast.Transaction:
		f.formatTransaction(directive, buf)
	}
}

// formatOption formats an option directive.
func (f *Formatter) formatOption(opt *ast.Option, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(opt.Pos.Line, buf) {
		return
	}

	buf.WriteString("option ")
	f.formatRawString(opt.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(opt.Value, buf)
	buf.WriteByte('\n')
}

// formatInclude formats an include directive.
func (f *Formatter) formatInclude(inc *ast.Include, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(inc.Pos.Line, buf) {
		return
	}

	buf.WriteString("include ")
	f.formatRawString(inc.Filename, buf)
	buf.WriteByte('\n')
}

// formatCommodity formats a commodity directive.
func (f *Formatter) formatCommodity(c *ast.Commodity, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(c.Pos.Line, c.Date) {
		if f.tryPreserveOriginalLine(c.Pos.Line, buf) {
			f.formatMetadata(c.Metadata, buf)
			return
		}
	}

	buf.WriteString(c.Date.String())
	buf.WriteString(" commodity ")
	buf.WriteString(c.Currency)
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatOpen formats an open directive.
func (f *Formatter) formatOpen(o *ast.Open, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(o.Pos.Line, o.Date) {
		originalLine := f.getOriginalLine(o.Pos.Line)
		if strings.Contains(originalLine, string(o.Account)) {
			if f.tryPreserveOriginalLine(o.Pos.Line, buf) {
				f.formatMetadata(o.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(o.Date.String())
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

// formatClose formats a close directive.
func (f *Formatter) formatClose(c *ast.Close, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(c.Pos.Line, c.Date) {
		originalLine := f.getOriginalLine(c.Pos.Line)
		if strings.Contains(originalLine, string(c.Account)) {
			if f.tryPreserveOriginalLine(c.Pos.Line, buf) {
				f.formatMetadata(c.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(c.Date.String())
	buf.WriteString(" close ")
	buf.WriteString(string(c.Account))
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatBalance formats a balance directive.
func (f *Formatter) formatBalance(b *ast.Balance, buf *strings.Builder) {
	buf.WriteString(b.Date.String())
	buf.WriteString(" balance ")
	buf.WriteString(string(b.Account))

	if b.Amount != nil {
		currentWidth := DateWidth + 1 + directiveKeywordWidth(b) + runewidth.StringWidth(string(b.Account))
		f.formatAmountAligned(b.Amount, currentWidth, buf)
	}

	// Append inline comment if present
	if b.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(b.GetComment().Content)
	}

	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(b.Metadata, buf)
}

// formatPad formats a pad directive.
func (f *Formatter) formatPad(p *ast.Pad, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(p.Pos.Line, p.Date) {
		originalLine := f.getOriginalLine(p.Pos.Line)
		if strings.Contains(originalLine, string(p.Account)) && strings.Contains(originalLine, string(p.AccountPad)) {
			if f.tryPreserveOriginalLine(p.Pos.Line, buf) {
				f.formatMetadata(p.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(p.Date.String())
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
	if f.canPreserveDirectiveLine(n.Pos.Line, n.Date) && !hasAnyInlineMetadata(n.Metadata) {
		originalLine := f.getOriginalLine(n.Pos.Line)
		if strings.Contains(originalLine, string(n.Account)) && strings.Contains(originalLine, "\"") {
			if f.tryPreserveOriginalLine(n.Pos.Line, buf) {
				f.formatMetadata(n.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(n.Date.String())
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
	if f.canPreserveDirectiveLine(d.Pos.Line, d.Date) && !hasAnyInlineMetadata(d.Metadata) {
		originalLine := f.getOriginalLine(d.Pos.Line)
		if strings.Contains(originalLine, string(d.Account)) && strings.Contains(originalLine, "\"") {
			if f.tryPreserveOriginalLine(d.Pos.Line, buf) {
				f.formatMetadata(d.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(d.Date.String())
	buf.WriteString(" document ")
	buf.WriteString(string(d.Account))
	buf.WriteByte(' ')
	f.formatRawString(d.PathToDocument, buf)
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(d.Metadata, buf)
}

// formatPrice formats a price directive.
func (f *Formatter) formatPrice(p *ast.Price, buf *strings.Builder) {
	buf.WriteString(p.Date.String())
	buf.WriteString(" price ")
	buf.WriteString(p.Commodity)

	if p.Amount != nil {
		currentWidth := DateWidth + 1 + directiveKeywordWidth(p) + runewidth.StringWidth(p.Commodity)
		f.formatAmountAligned(p.Amount, currentWidth, buf)
	}

	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(p.Metadata, buf)
}

// formatEvent formats an event directive.
func (f *Formatter) formatEvent(e *ast.Event, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(e.Pos.Line, e.Date) && !hasAnyInlineMetadata(e.Metadata) {
		originalLine := f.getOriginalLine(e.Pos.Line)
		if strings.Count(originalLine, "\"") >= 4 {
			if f.tryPreserveOriginalLine(e.Pos.Line, buf) {
				f.formatMetadata(e.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(e.Date.String())
	buf.WriteString(" event ")
	f.formatRawString(e.Name, buf)
	buf.WriteByte(' ')
	f.formatRawString(e.Value, buf)
	// Append inline comment if present
	buf.WriteByte('\n')
	f.formatMetadata(e.Metadata, buf)
}

// formatCustom formats a custom directive.
func (f *Formatter) formatCustom(c *ast.Custom, buf *strings.Builder) {
	if f.canPreserveDirectiveLine(c.Pos.Line, c.Date) && !hasAnyInlineMetadata(c.Metadata) {
		originalLine := f.getOriginalLine(c.Pos.Line)
		if strings.Contains(originalLine, "\"") {
			if f.tryPreserveOriginalLine(c.Pos.Line, buf) {
				f.formatMetadata(c.Metadata, buf)
				return
			}
		}
	}

	buf.WriteString(c.Date.String())
	buf.WriteString(" custom ")
	f.formatRawString(c.Type, buf)

	for _, val := range c.Values {
		buf.WriteByte(' ')
		if val.String != nil {
			buf.WriteByte('"')
			buf.WriteString(f.escapeString(*val.String))
			buf.WriteByte('"')
		} else if val.BooleanValue != nil {
			buf.WriteString(*val.BooleanValue)
		} else if val.Amount != nil {
			buf.WriteString(val.Amount.Value)
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
	if f.tryPreserveOriginalLine(p.Pos.Line, buf) {
		return
	}

	buf.WriteString("plugin ")
	f.formatRawString(p.Name, buf)
	if !p.Config.IsEmpty() {
		buf.WriteByte(' ')
		f.formatRawString(p.Config, buf)
	}
	buf.WriteByte('\n')
}

// formatPushtag formats a pushtag directive.
func (f *Formatter) formatPushtag(p *ast.Pushtag, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Pos.Line, buf) {
		return
	}

	buf.WriteString("pushtag #")
	buf.WriteString(string(p.Tag))
	buf.WriteByte('\n')
}

// formatPoptag formats a poptag directive.
func (f *Formatter) formatPoptag(p *ast.Poptag, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Pos.Line, buf) {
		return
	}

	buf.WriteString("poptag #")
	buf.WriteString(string(p.Tag))
	buf.WriteByte('\n')
}

// formatPushmeta formats a pushmeta directive.
func (f *Formatter) formatPushmeta(p *ast.Pushmeta, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Pos.Line, buf) {
		return
	}

	buf.WriteString("pushmeta ")
	buf.WriteString(p.Key)
	buf.WriteString(": ")
	buf.WriteString(p.Value)
	buf.WriteByte('\n')
}

// formatPopmeta formats a popmeta directive.
func (f *Formatter) formatPopmeta(p *ast.Popmeta, buf *strings.Builder) {
	if f.tryPreserveOriginalLine(p.Pos.Line, buf) {
		return
	}

	buf.WriteString("popmeta ")
	buf.WriteString(p.Key)
	buf.WriteString(":\n")
}

// formatTransaction formats a transaction directive with proper structure.
func (f *Formatter) formatTransaction(t *ast.Transaction, buf *strings.Builder) {
	if len(t.Postings) < 2 {
		return
	}

	buf.WriteString(t.Date.String())
	buf.WriteByte(' ')
	buf.WriteString(t.Flag)

	if !t.Payee.IsEmpty() {
		buf.WriteByte(' ')
		f.formatRawString(t.Payee, buf)
	}

	if !t.Narration.IsEmpty() {
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

	f.formatMetadata(t.Metadata, buf)

	for _, posting := range t.Postings {
		f.formatPosting(posting, buf)
	}
}

// formatPosting formats a single posting with proper alignment.
func (f *Formatter) formatPosting(p *ast.Posting, buf *strings.Builder) {
	buf.WriteString(strings.Repeat(" ", f.Indentation))

	currentWidth := f.Indentation

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
			buf.WriteByte(' ')
			buf.WriteString(p.Price.Value)
			buf.WriteByte(' ')
			buf.WriteString(p.Price.Currency)
		}
	}

	// Append inline metadata (on same line as posting)
	for _, m := range p.Metadata {
		if m.Inline {
			buf.WriteString("  ")
			buf.WriteString(m.Key)
			buf.WriteString(": ")
			f.formatMetadataValue(m.Value, buf)
		}
	}

	// Append inline comment if present
	if p.GetComment() != nil {
		buf.WriteByte(' ')
		buf.WriteString(p.GetComment().Content)
	}

	buf.WriteByte('\n')

	// Format block metadata (on separate lines)
	for _, m := range p.Metadata {
		if !m.Inline {
			buf.WriteString(strings.Repeat(" ", f.Indentation))
			buf.WriteString(m.Key)
			buf.WriteString(": ")
			f.formatMetadataValue(m.Value, buf)
			buf.WriteByte('\n')
		}
	}
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
func (f *Formatter) formatAmountAligned(amount *ast.Amount, currentWidth int, buf *strings.Builder) {
	if amount == nil {
		return
	}

	if !isValidNumericValue(amount.Value) {
		buf.WriteString(strings.Repeat(" ", MinimumSpacing))
		buf.WriteString(amount.Value)
		buf.WriteByte(' ')
		buf.WriteString(amount.Currency)
		return
	}

	padding := f.CurrencyColumn - currentWidth - runewidth.StringWidth(amount.Value) - 1
	if padding < MinimumSpacing {
		padding = MinimumSpacing
	}

	buf.WriteString(strings.Repeat(" ", padding))
	buf.WriteString(amount.Value)
	buf.WriteByte(' ')
	buf.WriteString(amount.Currency)
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

	if cost.Amount != nil {
		buf.WriteString(cost.Amount.Value)
		buf.WriteByte(' ')
		buf.WriteString(cost.Amount.Currency)
	}

	if cost.Date != nil {
		buf.WriteString(", ")
		buf.WriteString(cost.Date.String())
	}

	if cost.Label != "" {
		buf.WriteString(", \"")
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

	for _, m := range metadata {
		buf.WriteString(strings.Repeat(" ", f.Indentation))
		buf.WriteString(m.Key)
		buf.WriteString(": ")
		f.formatMetadataValue(m.Value, buf)
		buf.WriteByte('\n')
	}
}
