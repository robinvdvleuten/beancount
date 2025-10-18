// Package errors provides error formatting infrastructure for beancount validation errors.
// It separates error formatting from domain logic, allowing errors to be rendered in
// multiple formats (text, JSON) for different consumers (CLI, web UI, API).
//
// The package defines a Formatter interface and provides two implementations:
//   - TextFormatter: Formats errors for command-line output in bean-check style
//   - JSONFormatter: Formats errors as structured JSON for APIs and web interfaces
//
// Domain-specific error types remain in their respective packages (e.g., ledger),
// while this package handles the presentation layer.
package errors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/output"
)

// Formatter formats errors for output in different formats.
type Formatter interface {
	// Format formats a single error.
	Format(err error) string

	// FormatAll formats multiple errors.
	FormatAll(errs []error) string
}

// TextFormatter formats errors for command-line output in bean-check style.
type TextFormatter struct {
	formatter *formatter.Formatter
	styles    *output.Styles
}

// NewTextFormatter creates a new text formatter.
func NewTextFormatter(f *formatter.Formatter, styles *output.Styles) *TextFormatter {
	if f == nil {
		f = formatter.New()
	}
	return &TextFormatter{
		formatter: f,
		styles:    styles,
	}
}

// Format formats a single error in bean-check style.
func (tf *TextFormatter) Format(err error) string {
	// Check if this is an error with position and directive context
	if e, ok := err.(interface {
		GetPosition() ast.Position
		GetDirective() ast.Directive
		Error() string
	}); ok {
		return tf.formatWithContext(e.GetPosition(), e.Error(), e.GetDirective())
	}

	// Check if this is an error with position only
	if e, ok := err.(interface {
		GetPosition() ast.Position
		Error() string
	}); ok {
		return tf.formatWithPosition(e.GetPosition(), e.Error())
	}

	// Fallback to standard error formatting
	return err.Error()
}

// FormatAll formats multiple errors, separating them with blank lines.
func (tf *TextFormatter) FormatAll(errs []error) string {
	if len(errs) == 0 {
		return ""
	}

	var buf bytes.Buffer
	for i, err := range errs {
		buf.WriteString(tf.Format(err))

		// Add blank line between errors (but not after the last one)
		if i < len(errs)-1 {
			buf.WriteString("\n\n")
		}
	}

	return buf.String()
}

// formatWithPosition formats an error message with position information.
func (tf *TextFormatter) formatWithPosition(pos ast.Position, message string) string {
	header := tf.formatErrorLine(pos, message)
	if pos.Filename == "" || pos.Line <= 0 {
		return header
	}

	lines, startLine, highlightIdx := tf.loadSourceContext(pos)
	if len(lines) == 0 {
		return header
	}

	var buf strings.Builder
	buf.WriteString(header)
	buf.WriteString("\n\n")
	tf.writeNumberedLines(&buf, lines, startLine, highlightIdx, pos.Column)

	return buf.String()
}

// formatWithContext formats an error with directive context (bean-check style).
func (tf *TextFormatter) formatWithContext(pos ast.Position, message string, directive ast.Directive) string {
	header := tf.formatErrorLine(pos, message)

	if directive == nil {
		return header
	}

	var buf strings.Builder

	// Write the error message with styling
	buf.WriteString(header)
	buf.WriteString("\n\n")

	lines := tf.directiveLines(directive)
	if len(lines) > 0 {
		tf.writeNumberedLines(&buf, lines, 1, -1, 0)
	}

	return buf.String()
}

func (tf *TextFormatter) directiveLines(directive ast.Directive) []string {
	switch d := directive.(type) {
	case *ast.Transaction:
		return tf.transactionLines(d)
	case *ast.Balance:
		line := fmt.Sprintf("%s %s %s", tf.renderDate(d.Date), tf.styleKeyword("balance"), tf.styleAccount(string(d.Account)))
		if amount := tf.renderAmount(d.Amount); amount != "" {
			line += "  " + amount
		}
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Pad:
		line := fmt.Sprintf("%s %s %s %s", tf.renderDate(d.Date), tf.styleKeyword("pad"), tf.styleAccount(string(d.Account)), tf.styleAccount(string(d.AccountPad)))
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Note:
		line := fmt.Sprintf("%s %s %s %s", tf.renderDate(d.Date), tf.styleKeyword("note"), tf.styleAccount(string(d.Account)), tf.renderStringLiteral(d.Description))
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Document:
		line := fmt.Sprintf("%s %s %s %s", tf.renderDate(d.Date), tf.styleKeyword("document"), tf.styleAccount(string(d.Account)), tf.renderStringLiteral(d.PathToDocument))
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Open:
		line := fmt.Sprintf("%s %s %s", tf.renderDate(d.Date), tf.styleKeyword("open"), tf.styleAccount(string(d.Account)))
		if rendered := tf.renderCurrencyList(d.ConstraintCurrencies); rendered != "" {
			line += " " + rendered
		}
		if d.BookingMethod != "" {
			line += " " + tf.styleKeyword(d.BookingMethod)
		}
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Close:
		line := fmt.Sprintf("%s %s %s", tf.renderDate(d.Date), tf.styleKeyword("close"), tf.styleAccount(string(d.Account)))
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Commodity:
		line := fmt.Sprintf("%s %s %s", tf.renderDate(d.Date), tf.styleKeyword("commodity"), tf.styleCurrency(d.Currency))
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Price:
		line := fmt.Sprintf("%s %s %s", tf.renderDate(d.Date), tf.styleKeyword("price"), tf.styleCurrency(d.Commodity))
		if amount := tf.renderAmount(d.Amount); amount != "" {
			line += " " + amount
		}
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Event:
		line := fmt.Sprintf("%s %s %s %s", tf.renderDate(d.Date), tf.styleKeyword("event"), tf.renderStringLiteral(d.Name), tf.renderStringLiteral(d.Value))
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	case *ast.Custom:
		line := fmt.Sprintf("%s %s %s", tf.renderDate(d.Date), tf.styleKeyword("custom"), tf.renderStringLiteral(d.Type))
		if len(d.Values) > 0 {
			values := make([]string, 0, len(d.Values))
			for _, v := range d.Values {
				if rendered := tf.renderCustomValue(v); rendered != "" {
					values = append(values, rendered)
				}
			}
			if len(values) > 0 {
				line += " " + strings.Join(values, " ")
			}
		}
		return tf.appendMetadataLines([]string{line}, d.Metadata)
	default:
		return nil
	}
}

func (tf *TextFormatter) transactionLines(txn *ast.Transaction) []string {
	var txnBuf bytes.Buffer
	txnFormatter := formatter.New()
	if tf.formatter != nil && tf.formatter.CurrencyColumn > 0 {
		txnFormatter = formatter.New(formatter.WithCurrencyColumn(tf.formatter.CurrencyColumn))
	}

	if err := txnFormatter.FormatTransaction(txn, &txnBuf); err != nil {
		return nil
	}

	raw := strings.TrimSuffix(txnBuf.String(), "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func (tf *TextFormatter) appendMetadataLines(lines []string, metadata []*ast.Metadata) []string {
	if len(metadata) == 0 {
		return lines
	}

	for _, meta := range metadata {
		if meta == nil {
			continue
		}
		lines = append(lines, tf.formatMetadataEntry(meta))
	}

	return lines
}

func (tf *TextFormatter) formatMetadataEntry(meta *ast.Metadata) string {
	key := meta.Key
	value := strings.TrimSpace(meta.Value)

	if tf.styles != nil {
		key = tf.styleKeyword(key)
		value = tf.styleMetadataValue(value)
	}

	return fmt.Sprintf("  %s: %s", key, value)
}

func (tf *TextFormatter) renderAmount(amount *ast.Amount) string {
	if amount == nil {
		return ""
	}

	value := amount.Value
	currency := amount.Currency

	if tf.styles != nil {
		value = tf.styleNumber(value)
		currency = tf.styleCurrency(currency)
	}

	return strings.TrimSpace(fmt.Sprintf("%s %s", value, currency))
}

func (tf *TextFormatter) renderCurrencyList(currencies []string) string {
	if len(currencies) == 0 {
		return ""
	}

	rendered := make([]string, 0, len(currencies))
	for _, cur := range currencies {
		rendered = append(rendered, tf.styleCurrency(cur))
	}

	return strings.Join(rendered, ", ")
}

func (tf *TextFormatter) renderStringLiteral(value string) string {
	literal := strconv.Quote(value)
	if tf.styles != nil {
		return tf.styleString(literal)
	}
	return literal
}

func (tf *TextFormatter) renderCustomValue(value *ast.CustomValue) string {
	switch {
	case value == nil:
		return ""
	case value.String != nil:
		return tf.renderStringLiteral(*value.String)
	case value.BooleanValue != nil:
		return tf.styleKeyword(*value.BooleanValue)
	case value.Amount != nil:
		return tf.renderAmount(value.Amount)
	case value.Number != nil:
		num := *value.Number
		if tf.styles != nil {
			num = tf.styleNumber(num)
		}
		return num
	default:
		return ""
	}
}

func (tf *TextFormatter) renderDate(date *ast.Date) string {
	if date == nil {
		return ""
	}
	formatted := date.Format("2006-01-02")
	if tf.styles != nil {
		return tf.styles.Date(formatted)
	}
	return formatted
}

func (tf *TextFormatter) styleKeyword(text string) string {
	if tf.styles == nil {
		return text
	}
	return tf.styles.Keyword(text)
}

func (tf *TextFormatter) styleAccount(text string) string {
	if tf.styles == nil {
		return text
	}
	return tf.styles.Account(text)
}

func (tf *TextFormatter) styleCurrency(text string) string {
	if tf.styles == nil {
		return text
	}
	return tf.styles.Currency(text)
}

func (tf *TextFormatter) styleNumber(text string) string {
	if tf.styles == nil {
		return text
	}
	return tf.styles.Number(text)
}

func (tf *TextFormatter) styleString(text string) string {
	if tf.styles == nil {
		return text
	}
	return tf.styles.String(text)
}

func (tf *TextFormatter) styleMetadataValue(value string) string {
	if value == "" {
		return value
	}

	if tf.styles == nil {
		return value
	}

	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return tf.styles.String(value)
	}

	switch strings.ToUpper(value) {
	case "TRUE", "FALSE":
		return tf.styles.Keyword(value)
	}

	if fields := strings.Fields(value); len(fields) == 2 && tf.isNumberLike(fields[0]) {
		return fmt.Sprintf("%s %s", tf.styles.Number(fields[0]), tf.styles.Currency(fields[1]))
	}

	if tf.isNumberLike(value) {
		return tf.styles.Number(value)
	}

	return value
}

func (tf *TextFormatter) isNumberLike(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

// formatErrorLine builds the first line of an error with positional context.
func (tf *TextFormatter) formatErrorLine(pos ast.Position, message string) string {
	var prefixBuilder strings.Builder
	var hasPrefix bool

	if pos.Filename != "" {
		prefixBuilder.WriteString(pos.Filename)
		hasPrefix = true
	}

	if pos.Line > 0 {
		if hasPrefix {
			prefixBuilder.WriteByte(':')
		}
		prefixBuilder.WriteString(strconv.Itoa(pos.Line))
		hasPrefix = true

		if pos.Column > 0 {
			prefixBuilder.WriteByte(':')
			prefixBuilder.WriteString(strconv.Itoa(pos.Column))
		}
	}

	var buf strings.Builder
	if hasPrefix {
		prefix := prefixBuilder.String()
		if tf.styles != nil {
			buf.WriteString(tf.styles.Position(prefix))
		} else {
			buf.WriteString(prefix)
		}
		buf.WriteString(": ")
	}

	if tf.styles != nil {
		buf.WriteString(tf.styles.Error(message))
	} else {
		buf.WriteString(message)
	}

	return buf.String()
}

// JSONFormatter formats errors as JSON.
type JSONFormatter struct{}

// NewJSONFormatter creates a new JSON formatter.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

// ErrorJSON represents an error in JSON format.
type ErrorJSON struct {
	Type     string                 `json:"type"`
	Message  string                 `json:"message"`
	Position *PositionJSON          `json:"position,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// PositionJSON represents a file position in JSON format.
type PositionJSON struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

// Format formats a single error as JSON.
func (jf *JSONFormatter) Format(err error) string {
	errJSON := jf.toJSON(err)
	data, _ := json.Marshal(errJSON)
	return string(data)
}

// FormatAll formats multiple errors as a JSON array.
func (jf *JSONFormatter) FormatAll(errs []error) string {
	var jsonErrors []ErrorJSON
	for _, err := range errs {
		jsonErrors = append(jsonErrors, jf.toJSON(err))
	}
	data, _ := json.MarshalIndent(jsonErrors, "", "  ")
	return string(data)
}

// toJSON converts an error to ErrorJSON.
func (jf *JSONFormatter) toJSON(err error) ErrorJSON {
	errJSON := ErrorJSON{
		Type:    fmt.Sprintf("%T", err),
		Message: err.Error(),
		Details: make(map[string]interface{}),
	}

	// Extract position if available
	if e, ok := err.(interface{ GetPosition() ast.Position }); ok {
		pos := e.GetPosition()
		errJSON.Position = &PositionJSON{
			Filename: pos.Filename,
			Line:     pos.Line,
			Column:   pos.Column,
		}
	}

	// Extract additional details based on error type
	// This will be extended as we add more error types
	switch e := err.(type) {
	case interface{ GetAccount() ast.Account }:
		errJSON.Details["account"] = string(e.GetAccount())
	case interface{ GetDate() *ast.Date }:
		if date := e.GetDate(); date != nil {
			errJSON.Details["date"] = date.Format("2006-01-02")
		}
	}

	return errJSON
}

func (tf *TextFormatter) loadSourceContext(pos ast.Position) ([]string, int, int) {
	data, err := os.ReadFile(pos.Filename)
	if err != nil {
		return nil, 0, 0
	}

	lines := strings.Split(string(data), "\n")
	if pos.Line < 1 || pos.Line > len(lines) {
		return nil, 0, 0
	}

	lineIndex := pos.Line - 1
	start := lineIndex - 2
	if start < 0 {
		start = 0
	}
	end := lineIndex + 1
	if end >= len(lines) {
		end = len(lines) - 1
	}

	context := lines[start : end+1]
	return context, start + 1, lineIndex - start
}

func (tf *TextFormatter) writeNumberedLines(buf *strings.Builder, lines []string, startLine int, highlightIdx int, caretColumn int) {
	if len(lines) == 0 {
		return
	}

	maxLineNumber := startLine + len(lines) - 1
	width := len(strconv.Itoa(maxLineNumber))
	indent := "   "

	for i, line := range lines {
		lineNumber := startLine + i
		prefix := fmt.Sprintf("%s%*d │ ", indent, width, lineNumber)
		if tf.styles != nil {
			buf.WriteString(tf.styles.LineNumber(prefix))
		} else {
			buf.WriteString(prefix)
		}
		buf.WriteString(line)
		buf.WriteByte('\n')

		if i == highlightIdx && caretColumn > 0 {
			caretPrefix := fmt.Sprintf("%s%*s │ ", indent, width, "")
			if tf.styles != nil {
				buf.WriteString(tf.styles.LineNumber(caretPrefix))
			} else {
				buf.WriteString(caretPrefix)
			}
			buf.WriteString(tf.buildCaretLine(line, caretColumn))
			buf.WriteByte('\n')
		}
	}
}

func (tf *TextFormatter) buildCaretLine(line string, column int) string {
	if column <= 0 {
		column = 1
	}

	runes := []rune(line)
	index := column - 1
	if index < 0 {
		index = 0
	}
	if index > len(runes) {
		index = len(runes)
	}

	var spacing strings.Builder
	for _, r := range runes[:index] {
		width := runewidth.RuneWidth(r)
		if width < 1 {
			width = 1
		}
		for i := 0; i < width; i++ {
			spacing.WriteByte(' ')
		}
	}

	highlightWidth := 0
	for j := index; j < len(runes); j++ {
		r := runes[j]
		if j > index && unicode.IsSpace(r) {
			break
		}
		if unicode.IsSpace(r) {
			break
		}
		width := runewidth.RuneWidth(r)
		if width < 1 {
			width = 1
		}
		highlightWidth += width
	}
	if highlightWidth == 0 {
		highlightWidth = 1
	}

	caret := strings.Repeat("^", highlightWidth)
	if tf.styles != nil {
		caret = tf.styles.Error(caret)
	}

	return spacing.String() + caret
}
