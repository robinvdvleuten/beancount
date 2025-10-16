package formatter

import (
	"cmp"
	"io"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/parser"
)

const (
	// DefaultCurrencyColumn is the default column position for currency alignment
	// (matches bean-format behavior)
	DefaultCurrencyColumn = 52

	// DefaultIndentation is the default indentation for postings and metadata
	DefaultIndentation = 2

	// MinimumSpacing is the minimum number of spaces between account/number and currency
	MinimumSpacing = 2

	// DateWidth is the width of a formatted date (YYYY-MM-DD)
	DateWidth = 10

	// BalanceKeywordWidth is the width of the "balance" keyword (7 chars) + space
	BalanceKeywordWidth = 8

	// PriceKeywordWidth is the width of the "price" keyword (5 chars) + space
	PriceKeywordWidth = 6
)

// CommentType represents the type of comment in a beancount file.
type CommentType int

const (
	// StandaloneComment appears on its own line before a directive
	StandaloneComment CommentType = iota
	// InlineComment appears at the end of a directive or posting line
	InlineComment
	// SectionComment is a standalone comment followed by a blank line (section header)
	SectionComment
)

// CommentBlock represents a comment in the source file.
type CommentBlock struct {
	Line    int         // Line number where comment appears (1-indexed)
	Content string      // Comment text (including semicolon)
	Type    CommentType // Type of comment
}

// BlankLine represents a blank line in the source file.
type BlankLine struct {
	Line int // Line number (1-indexed)
}

// LineContent represents content that can appear before a directive.
type LineContent interface {
	isLineContent()
	lineNumber() int
}

func (c CommentBlock) isLineContent() {}
func (c CommentBlock) lineNumber() int { return c.Line }
func (b BlankLine) isLineContent()     {}
func (b BlankLine) lineNumber() int    { return b.Line }

// DirectiveWithComments wraps a directive with its associated comments and blank lines.
type DirectiveWithComments struct {
	PrecedingLines []LineContent // Comments/blanks that appear before this directive
	InlineComment  string         // Comment at the end of the directive line (empty if none)
}

// escapeString escapes special characters in strings for Beancount format.
// It escapes double quotes and backslashes.
func escapeString(s string) string {
	// Quick check if escaping is needed
	needsEscape := false
	for _, c := range s {
		if c == '"' || c == '\\' {
			needsEscape = true
			break
		}
	}

	if !needsEscape {
		return s
	}

	// Use strings.Builder for efficient escaping
	var buf strings.Builder
	buf.Grow(len(s) + 10) // Add some extra capacity for escape sequences

	for _, c := range s {
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		default:
			buf.WriteRune(c)
		}
	}

	return buf.String()
}

// Formatter handles formatting of Beancount files with proper alignment.
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

// New creates a new Formatter with the given options.
func New(opts ...Option) *Formatter {
	f := &Formatter{
		CurrencyColumn:   0,    // 0 means auto-calculate
		PreserveComments: true, // Preserve comments by default
		PreserveBlanks:   true, // Preserve blank lines by default
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// widthMetrics holds calculated width information for formatting.
type widthMetrics struct {
	maxPrefixWidth int // Maximum width of account prefix (indentation + flag + account + spacing)
	maxNumWidth    int // Maximum width of numeric values
	currencyColumn int // Calculated currency column position
}

// calculateWidthMetrics performs a single pass through the AST to calculate all width metrics.
func (f *Formatter) calculateWidthMetrics(ast *parser.AST) widthMetrics {
	metrics := widthMetrics{}

	for _, directive := range ast.Directives {
		switch d := directive.(type) {
		case *parser.Transaction:
			for _, posting := range d.Postings {
				if posting.Amount != nil {
					// Calculate prefix width: indentation + flag + account + spacing
					prefixWidth := DefaultIndentation
					if posting.Flag != "" {
						prefixWidth += 2 // flag + space
					}
					prefixWidth += len(string(posting.Account)) + MinimumSpacing
					metrics.maxPrefixWidth = max(metrics.maxPrefixWidth, prefixWidth)

					// Calculate number width
					numWidth := len(posting.Amount.Value)
					metrics.maxNumWidth = max(metrics.maxNumWidth, numWidth)

					// Calculate total width for currency column
					totalWidth := prefixWidth + numWidth
					metrics.currencyColumn = max(metrics.currencyColumn, totalWidth)
				}
			}

		case *parser.Balance:
			if d.Amount != nil {
				// Calculate width: date + "balance" + account + spacing + number
				width := DateWidth + 1 + BalanceKeywordWidth + len(string(d.Account)) + MinimumSpacing
				numWidth := len(d.Amount.Value)
				metrics.maxNumWidth = max(metrics.maxNumWidth, numWidth)
				totalWidth := width + numWidth
				metrics.currencyColumn = max(metrics.currencyColumn, totalWidth)
			}

		case *parser.Price:
			if d.Amount != nil {
				// Calculate width: date + "price" + commodity + spacing + number
				width := DateWidth + 1 + PriceKeywordWidth + len(d.Commodity) + MinimumSpacing
				numWidth := len(d.Amount.Value)
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
func (f *Formatter) calculateCurrencyColumn(ast *parser.AST) int {
	metrics := f.calculateWidthMetrics(ast)
	if metrics.currencyColumn > 0 {
		return metrics.currencyColumn + MinimumSpacing
	}
	return DefaultCurrencyColumn
}

// determineCurrencyColumn calculates the currency column based on configuration.
// Priority: explicit widths (PrefixWidth/NumWidth) > auto-calculated from content > default.
func (f *Formatter) determineCurrencyColumn(ast *parser.AST) int {
	// If explicit widths are provided, use those
	if f.PrefixWidth > 0 || f.NumWidth > 0 {
		metrics := f.calculateWidthMetrics(ast)

		prefixWidth := f.PrefixWidth
		if prefixWidth == 0 {
			prefixWidth = metrics.maxPrefixWidth
			if prefixWidth == 0 {
				prefixWidth = 40 // Default prefix width
			}
		}

		numWidth := f.NumWidth
		if numWidth == 0 {
			numWidth = metrics.maxNumWidth + MinimumSpacing
			if numWidth == MinimumSpacing {
				numWidth = 10 // Default number width
			}
		}

		return prefixWidth + numWidth
	}

	// Auto-calculate from content
	return f.calculateCurrencyColumn(ast)
}

// Format formats the given AST and writes the output to the writer.
// astItem represents any item in the AST with its position
type astItem struct {
	pos       int
	option    *parser.Option
	include   *parser.Include
	directive parser.Directive
}

// Comments and blank lines from sourceContent are preserved based on Formatter configuration.
func (f *Formatter) Format(ast *parser.AST, sourceContent []byte, w io.Writer) error {
	// Determine the currency column based on the configuration
	if f.CurrencyColumn == 0 {
		f.CurrencyColumn = f.determineCurrencyColumn(ast)
	}

	// Extract comments and blank lines if preservation is enabled
	var lineContentMap map[int][]LineContent
	if f.PreserveComments || f.PreserveBlanks {
		comments, blanks := extractCommentsAndBlanks(sourceContent)

		// Filter based on configuration
		if !f.PreserveComments {
			comments = nil
		}
		if !f.PreserveBlanks {
			blanks = nil
		}

		// Build a map of line numbers to content
		lineContentMap = buildLineContentMap(comments, blanks)
	}

	// Use a string builder to buffer all output, then write once
	var buf strings.Builder

	// Estimate initial capacity to reduce allocations
	estimatedSize := (len(ast.Options) + len(ast.Includes) + len(ast.Directives)) * 100
	buf.Grow(estimatedSize)

	// Collect all items (options, includes, directives) with their positions
	var items []astItem

	for _, opt := range ast.Options {
		if opt != nil {
			items = append(items, astItem{pos: opt.Pos.Line, option: opt})
		}
	}

	for _, inc := range ast.Includes {
		if inc != nil {
			items = append(items, astItem{pos: inc.Pos.Line, include: inc})
		}
	}

	for _, directive := range ast.Directives {
		if directive != nil {
			pos := getDirectivePos(directive)
			items = append(items, astItem{pos: pos, directive: directive})
		}
	}

	// Sort all items by their original position in the file
	slices.SortFunc(items, func(a, b astItem) int {
		return cmp.Compare(a.pos, b.pos)
	})

	// Track the last line we've processed
	lastLine := 0

	// Format all items in order
	for _, item := range items {
		if lineContentMap != nil {
			f.outputPrecedingContent(item.pos, lastLine, lineContentMap, &buf)
			lastLine = item.pos
		}

		if item.option != nil {
			f.formatOption(item.option, &buf)
		} else if item.include != nil {
			f.formatInclude(item.include, &buf)
		} else if item.directive != nil {
			f.formatDirective(item.directive, &buf)
		}
	}

	// Write all output at once
	_, err := w.Write([]byte(buf.String()))
	return err
}

// determineCommentType checks if a comment is a section header by looking at the next line.
func determineCommentType(currentIndex int, lines []string) CommentType {
	if currentIndex+1 < len(lines) && strings.TrimSpace(lines[currentIndex+1]) == "" {
		return SectionComment
	}
	return StandaloneComment
}

// extractCommentsAndBlanks scans the source content and extracts all comments and blank lines.
// Returns sorted slices of comments and blank lines by line number.
func extractCommentsAndBlanks(sourceContent []byte) ([]CommentBlock, []BlankLine) {
	var comments []CommentBlock
	var blanks []BlankLine

	lines := strings.Split(string(sourceContent), "\n")

	for i, line := range lines {
		lineNum := i + 1 // 1-indexed line numbers
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			// Blank line
			blanks = append(blanks, BlankLine{Line: lineNum})
		} else if strings.HasPrefix(trimmed, ";") {
			// Beancount comment line
			comments = append(comments, CommentBlock{
				Line:    lineNum,
				Content: trimmed, // Store trimmed version
				Type:    determineCommentType(i, lines),
			})
		} else if strings.HasPrefix(trimmed, "#") && !isBeancountDirectiveLine(trimmed) {
			// Hash line (org-mode headers, markdown, etc.) - but not Beancount tags
			// Tags like "#vacation" appear on directive lines, not standalone
			comments = append(comments, CommentBlock{
				Line:    lineNum,
				Content: trimmed, // Store trimmed version
				Type:    determineCommentType(i, lines),
			})
		}
		// Note: Inline comments are handled separately during formatting
		// as they require parsing the line structure
	}

	return comments, blanks
}

// isBeancountDirectiveLine checks if a line looks like it starts with a Beancount directive.
// This helps distinguish between hash headers (# Options) and tag usage on directive lines.
func isBeancountDirectiveLine(line string) bool {
	// Beancount directives start with a date (YYYY-MM-DD) or keywords like "option", "include"
	if len(line) >= 10 && line[4] == '-' && line[7] == '-' {
		// Looks like a date at the start
		return true
	}
	// Check for directive keywords
	if strings.HasPrefix(line, "option ") || strings.HasPrefix(line, "include ") {
		return true
	}
	return false
}

// buildLineContentMap creates a map from line numbers to the content (comments/blanks) on those lines.
func buildLineContentMap(comments []CommentBlock, blanks []BlankLine) map[int][]LineContent {
	lineMap := make(map[int][]LineContent)

	for _, comment := range comments {
		lineMap[comment.Line] = append(lineMap[comment.Line], comment)
	}

	for _, blank := range blanks {
		lineMap[blank.Line] = append(lineMap[blank.Line], blank)
	}

	return lineMap
}

// getDirectivePos extracts the line position from any directive type.
func getDirectivePos(d parser.Directive) int {
	switch directive := d.(type) {
	case *parser.Commodity:
		return directive.Pos.Line
	case *parser.Open:
		return directive.Pos.Line
	case *parser.Close:
		return directive.Pos.Line
	case *parser.Balance:
		return directive.Pos.Line
	case *parser.Pad:
		return directive.Pos.Line
	case *parser.Note:
		return directive.Pos.Line
	case *parser.Document:
		return directive.Pos.Line
	case *parser.Price:
		return directive.Pos.Line
	case *parser.Event:
		return directive.Pos.Line
	case *parser.Transaction:
		return directive.Pos.Line
	default:
		return 0
	}
}

// outputPrecedingContent outputs any comments or blank lines that appear between
// lastLine and currentLine in the source file.
func (f *Formatter) outputPrecedingContent(currentLine, lastLine int, lineContentMap map[int][]LineContent, buf *strings.Builder) {
	// Output content for lines between lastLine and currentLine (exclusive)
	for line := lastLine + 1; line < currentLine; line++ {
		if content, exists := lineContentMap[line]; exists {
			for _, item := range content {
				switch c := item.(type) {
				case CommentBlock:
					buf.WriteString(c.Content)
					buf.WriteByte('\n')
				case BlankLine:
					buf.WriteByte('\n')
				}
			}
		}
	}
}

// formatDirective formats a directive based on its type.
func (f *Formatter) formatDirective(d parser.Directive, buf *strings.Builder) {
	switch directive := d.(type) {
	case *parser.Commodity:
		f.formatCommodity(directive, buf)
	case *parser.Open:
		f.formatOpen(directive, buf)
	case *parser.Close:
		f.formatClose(directive, buf)
	case *parser.Balance:
		f.formatBalance(directive, buf)
	case *parser.Pad:
		f.formatPad(directive, buf)
	case *parser.Note:
		f.formatNote(directive, buf)
	case *parser.Document:
		f.formatDocument(directive, buf)
	case *parser.Price:
		f.formatPrice(directive, buf)
	case *parser.Event:
		f.formatEvent(directive, buf)
	case *parser.Transaction:
		f.formatTransaction(directive, buf)
	}
}

// formatOption formats an option directive.
func (f *Formatter) formatOption(opt *parser.Option, buf *strings.Builder) {
	buf.WriteString("option \"")
	buf.WriteString(escapeString(opt.Name))
	buf.WriteString("\" \"")
	buf.WriteString(escapeString(opt.Value))
	buf.WriteString("\"\n")
}

// formatInclude formats an include directive.
func (f *Formatter) formatInclude(inc *parser.Include, buf *strings.Builder) {
	buf.WriteString("include \"")
	buf.WriteString(escapeString(inc.Filename))
	buf.WriteString("\"\n")
}

// formatCommodity formats a commodity directive.
func (f *Formatter) formatCommodity(c *parser.Commodity, buf *strings.Builder) {
	buf.WriteString(c.Date.Format("2006-01-02"))
	buf.WriteString(" commodity ")
	buf.WriteString(c.Currency)
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatOpen formats an open directive.
func (f *Formatter) formatOpen(o *parser.Open, buf *strings.Builder) {
	buf.WriteString(o.Date.Format("2006-01-02"))
	buf.WriteString(" open ")
	buf.WriteString(string(o.Account))

	// Add constraint currencies if present, with minimal spacing (not aligned)
	if len(o.ConstraintCurrencies) > 0 {
		buf.WriteString(" ")
		for i, currency := range o.ConstraintCurrencies {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(currency)
		}
	}

	// Add booking method if present
	if o.BookingMethod != "" {
		buf.WriteString(" \"")
		buf.WriteString(o.BookingMethod)
		buf.WriteByte('"')
	}

	buf.WriteByte('\n')
	f.formatMetadata(o.Metadata, buf)
}

// formatClose formats a close directive.
func (f *Formatter) formatClose(c *parser.Close, buf *strings.Builder) {
	buf.WriteString(c.Date.Format("2006-01-02"))
	buf.WriteString(" close ")
	buf.WriteString(string(c.Account))
	buf.WriteByte('\n')
	f.formatMetadata(c.Metadata, buf)
}

// formatBalance formats a balance directive.
func (f *Formatter) formatBalance(b *parser.Balance, buf *strings.Builder) {
	buf.WriteString(b.Date.Format("2006-01-02"))
	buf.WriteString(" balance ")
	buf.WriteString(string(b.Account))

	if b.Amount != nil {
		f.formatAmountAligned(b.Amount, buf.Len(), buf)
	}

	buf.WriteByte('\n')
	f.formatMetadata(b.Metadata, buf)
}

// formatPad formats a pad directive.
func (f *Formatter) formatPad(p *parser.Pad, buf *strings.Builder) {
	buf.WriteString(p.Date.Format("2006-01-02"))
	buf.WriteString(" pad ")
	buf.WriteString(string(p.Account))
	buf.WriteByte(' ')
	buf.WriteString(string(p.AccountPad))
	buf.WriteByte('\n')
	f.formatMetadata(p.Metadata, buf)
}

// formatNote formats a note directive.
func (f *Formatter) formatNote(n *parser.Note, buf *strings.Builder) {
	buf.WriteString(n.Date.Format("2006-01-02"))
	buf.WriteString(" note ")
	buf.WriteString(string(n.Account))
	buf.WriteString(" \"")
	buf.WriteString(escapeString(n.Description))
	buf.WriteString("\"\n")
	f.formatMetadata(n.Metadata, buf)
}

// formatDocument formats a document directive.
func (f *Formatter) formatDocument(d *parser.Document, buf *strings.Builder) {
	buf.WriteString(d.Date.Format("2006-01-02"))
	buf.WriteString(" document ")
	buf.WriteString(string(d.Account))
	buf.WriteString(" \"")
	buf.WriteString(escapeString(d.PathToDocument))
	buf.WriteString("\"\n")
	f.formatMetadata(d.Metadata, buf)
}

// formatPrice formats a price directive.
func (f *Formatter) formatPrice(p *parser.Price, buf *strings.Builder) {
	buf.WriteString(p.Date.Format("2006-01-02"))
	buf.WriteString(" price ")
	buf.WriteString(p.Commodity)

	if p.Amount != nil {
		f.formatAmountAligned(p.Amount, buf.Len(), buf)
	}

	buf.WriteByte('\n')
	f.formatMetadata(p.Metadata, buf)
}

// formatEvent formats an event directive.
func (f *Formatter) formatEvent(e *parser.Event, buf *strings.Builder) {
	buf.WriteString(e.Date.Format("2006-01-02"))
	buf.WriteString(" event \"")
	buf.WriteString(escapeString(e.Name))
	buf.WriteString("\" \"")
	buf.WriteString(escapeString(e.Value))
	buf.WriteString("\"\n")
	f.formatMetadata(e.Metadata, buf)
}

// formatTransaction formats a transaction directive with proper structure.
// Format: date flag [payee] [narration] [links] [tags]
// Note: Strings are re-quoted as the parser unquotes them during parsing.
// The parser's lexer doesn't support escaped quotes within strings.
func (f *Formatter) formatTransaction(t *parser.Transaction, buf *strings.Builder) {
	// First line: date, flag, payee (optional), narration, tags, links
	buf.WriteString(t.Date.Format("2006-01-02"))
	buf.WriteByte(' ')
	buf.WriteString(t.Flag)

	// Add payee if present (always quoted)
	if t.Payee != "" {
		buf.WriteString(" \"")
		buf.WriteString(escapeString(t.Payee))
		buf.WriteByte('"')
	}

	// Add narration if present (always quoted)
	if t.Narration != "" {
		buf.WriteString(" \"")
		buf.WriteString(escapeString(t.Narration))
		buf.WriteByte('"')
	}

	// Add links (prefixed with ^)
	for _, link := range t.Links {
		buf.WriteString(" ^")
		buf.WriteString(string(link))
	}

	// Add tags (prefixed with #)
	for _, tag := range t.Tags {
		buf.WriteString(" #")
		buf.WriteString(string(tag))
	}

	buf.WriteByte('\n')

	// Transaction-level metadata (indented with 2 spaces)
	f.formatMetadata(t.Metadata, buf)

	// Format each posting with proper alignment
	for _, posting := range t.Postings {
		f.formatPosting(posting, buf)
	}
}

// formatPosting formats a single posting with proper alignment.
// Handles both postings with explicit amounts and implied amounts (nil).
func (f *Formatter) formatPosting(p *parser.Posting, buf *strings.Builder) {
	buf.WriteString("  ")

	// Add flag if present
	if p.Flag != "" {
		buf.WriteString(p.Flag)
		buf.WriteByte(' ')
	}

	buf.WriteString(string(p.Account))

	// Add amount if present (explicit amount)
	// If amount is nil, this is an implied/calculated amount posting
	if p.Amount != nil {
		f.formatAmountAligned(p.Amount, buf.Len(), buf)

		// Add cost specification if present (e.g., {150.00 USD})
		if p.Cost != nil {
			buf.WriteByte(' ')
			f.formatCost(p.Cost, buf)
		}

		// Add price annotation if present (e.g., @ 1.50 EUR or @@ 150.00 EUR)
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

	buf.WriteByte('\n')

	// Posting-level metadata (always format, even for implied amounts)
	f.formatMetadata(p.Metadata, buf)
}

// formatAmountAligned formats an amount with proper alignment to the currency column.
func (f *Formatter) formatAmountAligned(amount *parser.Amount, currentWidth int, buf *strings.Builder) {
	if amount == nil {
		return
	}

	// Calculate padding needed
	padding := f.CurrencyColumn - currentWidth - len(amount.Value)
	if padding < MinimumSpacing {
		padding = MinimumSpacing
	}

	// Use strings.Repeat for efficient padding
	buf.WriteString(strings.Repeat(" ", padding))
	buf.WriteString(amount.Value)
	buf.WriteByte(' ')
	buf.WriteString(amount.Currency)
}

// formatCost formats a cost specification.
func (f *Formatter) formatCost(cost *parser.Cost, buf *strings.Builder) {
	if cost == nil {
		return
	}

	buf.WriteByte('{')

	if cost.Amount != nil {
		buf.WriteString(cost.Amount.Value)
		buf.WriteByte(' ')
		buf.WriteString(cost.Amount.Currency)
	}

	if cost.Date != nil {
		buf.WriteString(", ")
		buf.WriteString(cost.Date.Format("2006-01-02"))
	}

	if cost.Label != "" {
		buf.WriteString(", \"")
		buf.WriteString(escapeString(cost.Label))
		buf.WriteByte('"')
	}

	buf.WriteByte('}')
}

// formatMetadata formats metadata entries with proper indentation.
func (f *Formatter) formatMetadata(metadata []*parser.Metadata, buf *strings.Builder) {
	if len(metadata) == 0 {
		return
	}

	for _, m := range metadata {
		buf.WriteString("  ")
		buf.WriteString(m.Key)
		buf.WriteString(": \"")
		buf.WriteString(m.Value)
		buf.WriteString("\"\n")
	}
}
