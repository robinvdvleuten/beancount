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
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/parser"
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
	formatter     *formatter.Formatter
	sourceContent []byte // Optional source content for parse error context
}

// TextFormatterOption is an option for configuring TextFormatter.
type TextFormatterOption func(*TextFormatter)

// WithSource sets the source content for parse error context.
func WithSource(source []byte) TextFormatterOption {
	return func(tf *TextFormatter) {
		tf.sourceContent = source
	}
}

// NewTextFormatter creates a new text formatter.
func NewTextFormatter(f *formatter.Formatter, opts ...TextFormatterOption) *TextFormatter {
	if f == nil {
		f = formatter.New()
	}
	tf := &TextFormatter{formatter: f}
	for _, opt := range opts {
		opt(tf)
	}
	return tf
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

	// Check if this is a parse error with source context
	if e, ok := err.(*parser.ParseError); ok {
		source := tf.sourceContent
		if source == nil {
			source = e.SourceRange.Source
		}
		if source != nil {
			return tf.formatWithSourceContext(e.Pos, e.Error(), source)
		}
	}

	// Check if this is an error with position only
	if e, ok := err.(interface {
		GetPosition() ast.Position
		Error() string
	}); ok {
		// If we have source content, show source context instead of just position
		if tf.sourceContent != nil {
			return tf.formatWithSourceContext(e.GetPosition(), e.Error(), tf.sourceContent)
		}
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
	return message
}

// formatWithSourceContext formats a parse error with original source context.
// Shows the error message followed by the original source lines around the error position.
func (tf *TextFormatter) formatWithSourceContext(pos ast.Position, message string, sourceContent []byte) string {
	var buf bytes.Buffer

	// Write the error message
	buf.WriteString(message)
	buf.WriteString("\n\n")

	// Split the source content into lines
	sourceStr := string(sourceContent)
	sourceLines := strings.Split(sourceStr, "\n")

	// Determine the range of lines to show (2 lines before and after the error line)
	startLine := pos.Line - 3 // 0-based indexing, show 2 lines before
	endLine := pos.Line + 1   // show 1 line after (inclusive)

	// Ensure bounds
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(sourceLines) {
		endLine = len(sourceLines) - 1
	}

	// Show the source lines
	for i := startLine; i <= endLine; i++ {
		if i >= len(sourceLines) {
			break
		}
		buf.WriteString("   ")
		buf.WriteString(sourceLines[i])
		buf.WriteByte('\n')

		// Add caret pointing to error column on the error line
		if i == pos.Line-1 && pos.Column > 0 { // pos.Line is 1-based, i is 0-based
			buf.WriteString("   ")
			// Add spaces up to the error column (adjusting for the 3-space indent)
			for j := 0; j < pos.Column-1; j++ {
				buf.WriteByte(' ')
			}
			buf.WriteString("^\n")
		}
	}

	return buf.String()
}

// formatWithContext formats an error with directive context (bean-check style).
func (tf *TextFormatter) formatWithContext(pos ast.Position, message string, directive ast.Directive) string {
	if directive == nil {
		return message
	}

	var buf bytes.Buffer

	// Write the error message
	buf.WriteString(message)
	buf.WriteString("\n\n")

	// Write the formatted directive with proper indentation
	switch d := directive.(type) {
	case *ast.Transaction:
		// Use the formatter to format transactions
		var txnBuf bytes.Buffer
		txnFormatter := formatter.New(formatter.WithIndentation(0))
		if tf.formatter != nil && tf.formatter.CurrencyColumn > 0 {
			txnFormatter = formatter.New(formatter.WithCurrencyColumn(tf.formatter.CurrencyColumn), formatter.WithIndentation(0))
		}

		if err := txnFormatter.FormatTransaction(d, &txnBuf); err == nil {
			// Indent each line with 3 spaces
			lines := bytes.Split(txnBuf.Bytes(), []byte("\n"))
			for _, line := range lines {
				if len(line) > 0 {
					buf.WriteString("   ")
					buf.Write(line)
					buf.WriteByte('\n')
				}
			}
		}

	case *ast.Balance:
		buf.WriteString("   ")
		fmt.Fprintf(&buf, "%s balance %s", d.Date.Format("2006-01-02"), d.Account)
		if d.Amount != nil {
			fmt.Fprintf(&buf, "  %s %s", d.Amount.Value, d.Amount.Currency)
		}
		buf.WriteByte('\n')

	case *ast.Pad:
		buf.WriteString("   ")
		fmt.Fprintf(&buf, "%s pad %s %s\n", d.Date.Format("2006-01-02"), d.Account, d.AccountPad)

	case *ast.Note:
		buf.WriteString("   ")
		fmt.Fprintf(&buf, "%s note %s %q\n", d.Date.Format("2006-01-02"), d.Account, d.Description)

	case *ast.Document:
		buf.WriteString("   ")
		fmt.Fprintf(&buf, "%s document %s %q\n", d.Date.Format("2006-01-02"), d.Account, d.PathToDocument)

	case *ast.Open:
		buf.WriteString("   ")
		fmt.Fprintf(&buf, "%s open %s", d.Date.Format("2006-01-02"), d.Account)
		if len(d.ConstraintCurrencies) > 0 {
			fmt.Fprintf(&buf, " %s", strings.Join(d.ConstraintCurrencies, ", "))
		}
		if d.BookingMethod != "" {
			fmt.Fprintf(&buf, " %s", d.BookingMethod)
		}
		buf.WriteByte('\n')

	case *ast.Close:
		buf.WriteString("   ")
		fmt.Fprintf(&buf, "%s close %s\n", d.Date.Format("2006-01-02"), d.Account)
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
	jsonErrors := jf.FormatAllToSlice(errs)
	data, _ := json.MarshalIndent(jsonErrors, "", "  ")
	return string(data)
}

// FormatAllToSlice returns errors as a slice of ErrorJSON structs.
func (jf *JSONFormatter) FormatAllToSlice(errs []error) []ErrorJSON {
	result := make([]ErrorJSON, 0, len(errs))
	for _, err := range errs {
		result = append(result, jf.toJSON(err))
	}
	return result
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
