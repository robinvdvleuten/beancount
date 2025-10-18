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

	writeSingleLine := func(line string) {
		if line == "" {
			return
		}
		tf.writeNumberedLines(&buf, []string{line}, 1, -1, 0)
	}

	// Write the formatted directive with proper indentation
	switch d := directive.(type) {
	case *ast.Transaction:
		// Use the formatter to format transactions
		var txnBuf bytes.Buffer
		txnFormatter := formatter.New()
		if tf.formatter != nil && tf.formatter.CurrencyColumn > 0 {
			txnFormatter = formatter.New(formatter.WithCurrencyColumn(tf.formatter.CurrencyColumn))
		}

		if err := txnFormatter.FormatTransaction(d, &txnBuf); err == nil {
			// Indent each line with 3 spaces
			raw := strings.TrimRight(txnBuf.String(), "\n")
			if raw != "" {
				lines := strings.Split(raw, "\n")
				tf.writeNumberedLines(&buf, lines, 1, -1, 0)
			}
		}

	case *ast.Balance:
		dateStr := d.Date.Format("2006-01-02")
		keyword := "balance"
		account := string(d.Account)

		if tf.styles != nil {
			dateStr = tf.styles.Date(dateStr)
			keyword = tf.styles.Keyword(keyword)
			account = tf.styles.Account(account)
		}

		var line strings.Builder
		line.WriteString(dateStr)
		line.WriteByte(' ')
		line.WriteString(keyword)
		line.WriteByte(' ')
		line.WriteString(account)

		if d.Amount != nil {
			value := d.Amount.Value
			currency := d.Amount.Currency
			if tf.styles != nil {
				value = tf.styles.Number(value)
				currency = tf.styles.Currency(currency)
			}
			line.WriteString("  ")
			line.WriteString(value)
			line.WriteByte(' ')
			line.WriteString(currency)
		}

		writeSingleLine(line.String())

	case *ast.Pad:
		dateStr := d.Date.Format("2006-01-02")
		keyword := "pad"
		account := string(d.Account)
		target := string(d.AccountPad)

		if tf.styles != nil {
			dateStr = tf.styles.Date(dateStr)
			keyword = tf.styles.Keyword(keyword)
			account = tf.styles.Account(account)
			target = tf.styles.Account(target)
		}

		writeSingleLine(fmt.Sprintf("%s %s %s %s", dateStr, keyword, account, target))

	case *ast.Note:
		dateStr := d.Date.Format("2006-01-02")
		keyword := "note"
		account := string(d.Account)
		description := fmt.Sprintf("%q", d.Description)

		if tf.styles != nil {
			dateStr = tf.styles.Date(dateStr)
			keyword = tf.styles.Keyword(keyword)
			account = tf.styles.Account(account)
			description = tf.styles.String(description)
		}

		writeSingleLine(fmt.Sprintf("%s %s %s %s", dateStr, keyword, account, description))

	case *ast.Document:
		dateStr := d.Date.Format("2006-01-02")
		keyword := "document"
		account := string(d.Account)
		path := fmt.Sprintf("%q", d.PathToDocument)

		if tf.styles != nil {
			dateStr = tf.styles.Date(dateStr)
			keyword = tf.styles.Keyword(keyword)
			account = tf.styles.Account(account)
			path = tf.styles.String(path)
		}

		writeSingleLine(fmt.Sprintf("%s %s %s %s", dateStr, keyword, account, path))

	case *ast.Open:
		dateStr := d.Date.Format("2006-01-02")
		keyword := "open"
		account := string(d.Account)
		currencies := d.ConstraintCurrencies
		booking := d.BookingMethod

		if tf.styles != nil {
			dateStr = tf.styles.Date(dateStr)
			keyword = tf.styles.Keyword(keyword)
			account = tf.styles.Account(account)
		}

		var line strings.Builder
		line.WriteString(dateStr)
		line.WriteByte(' ')
		line.WriteString(keyword)
		line.WriteByte(' ')
		line.WriteString(account)

		if len(currencies) > 0 {
			var rendered string
			if tf.styles != nil {
				parts := make([]string, 0, len(currencies))
				for _, cur := range currencies {
					parts = append(parts, tf.styles.Currency(cur))
				}
				rendered = strings.Join(parts, ", ")
			} else {
				rendered = strings.Join(currencies, ", ")
			}
			line.WriteByte(' ')
			line.WriteString(rendered)
		}

		if booking != "" {
			if tf.styles != nil {
				line.WriteByte(' ')
				line.WriteString(tf.styles.Keyword(booking))
			} else {
				line.WriteByte(' ')
				line.WriteString(booking)
			}
		}

		writeSingleLine(line.String())

	case *ast.Close:
		dateStr := d.Date.Format("2006-01-02")
		keyword := "close"
		account := string(d.Account)

		if tf.styles != nil {
			dateStr = tf.styles.Date(dateStr)
			keyword = tf.styles.Keyword(keyword)
			account = tf.styles.Account(account)
		}

		writeSingleLine(fmt.Sprintf("%s %s %s", dateStr, keyword, account))
	}

	return buf.String()
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
