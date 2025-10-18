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
	return message
}

// formatWithContext formats an error with directive context (bean-check style).
func (tf *TextFormatter) formatWithContext(pos ast.Position, message string, directive ast.Directive) string {
	if directive == nil {
		if tf.styles != nil {
			return tf.styles.Error(message)
		}
		return message
	}

	var buf bytes.Buffer

	// Write the error message with styling
	if tf.styles != nil {
		buf.WriteString(tf.styles.Error(message))
	} else {
		buf.WriteString(message)
	}
	buf.WriteString("\n\n")

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
		dateStr := d.Date.Format("2006-01-02")
		if tf.styles != nil {
			fmt.Fprintf(&buf, "%s %s %s", dateStr, tf.styles.Keyword("balance"), tf.styles.Account(string(d.Account)))
		} else {
			fmt.Fprintf(&buf, "%s balance %s", dateStr, d.Account)
		}
		if d.Amount != nil {
			if tf.styles != nil {
				fmt.Fprintf(&buf, "  %s", tf.styles.Amount(fmt.Sprintf("%s %s", d.Amount.Value, d.Amount.Currency)))
			} else {
				fmt.Fprintf(&buf, "  %s %s", d.Amount.Value, d.Amount.Currency)
			}
		}
		buf.WriteByte('\n')

	case *ast.Pad:
		buf.WriteString("   ")
		dateStr := d.Date.Format("2006-01-02")
		if tf.styles != nil {
			fmt.Fprintf(&buf, "%s %s %s %s\n", dateStr, tf.styles.Keyword("pad"), tf.styles.Account(string(d.Account)), tf.styles.Account(string(d.AccountPad)))
		} else {
			fmt.Fprintf(&buf, "%s pad %s %s\n", dateStr, d.Account, d.AccountPad)
		}

	case *ast.Note:
		buf.WriteString("   ")
		dateStr := d.Date.Format("2006-01-02")
		if tf.styles != nil {
			fmt.Fprintf(&buf, "%s %s %s %q\n", dateStr, tf.styles.Keyword("note"), tf.styles.Account(string(d.Account)), d.Description)
		} else {
			fmt.Fprintf(&buf, "%s note %s %q\n", dateStr, d.Account, d.Description)
		}

	case *ast.Document:
		buf.WriteString("   ")
		dateStr := d.Date.Format("2006-01-02")
		if tf.styles != nil {
			fmt.Fprintf(&buf, "%s %s %s %q\n", dateStr, tf.styles.Keyword("document"), tf.styles.Account(string(d.Account)), d.PathToDocument)
		} else {
			fmt.Fprintf(&buf, "%s document %s %q\n", dateStr, d.Account, d.PathToDocument)
		}

	case *ast.Open:
		buf.WriteString("   ")
		dateStr := d.Date.Format("2006-01-02")
		if tf.styles != nil {
			fmt.Fprintf(&buf, "%s %s %s", dateStr, tf.styles.Keyword("open"), tf.styles.Account(string(d.Account)))
		} else {
			fmt.Fprintf(&buf, "%s open %s", dateStr, d.Account)
		}
		if len(d.ConstraintCurrencies) > 0 {
			if tf.styles != nil {
				fmt.Fprintf(&buf, " %s", tf.styles.Amount(strings.Join(d.ConstraintCurrencies, ", ")))
			} else {
				fmt.Fprintf(&buf, " %s", strings.Join(d.ConstraintCurrencies, ", "))
			}
		}
		if d.BookingMethod != "" {
			fmt.Fprintf(&buf, " %s", d.BookingMethod)
		}
		buf.WriteByte('\n')

	case *ast.Close:
		buf.WriteString("   ")
		dateStr := d.Date.Format("2006-01-02")
		if tf.styles != nil {
			fmt.Fprintf(&buf, "%s %s %s\n", dateStr, tf.styles.Keyword("close"), tf.styles.Account(string(d.Account)))
		} else {
			fmt.Fprintf(&buf, "%s close %s\n", dateStr, d.Account)
		}
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
