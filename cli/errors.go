package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/parser"
)

var (
	errCaretStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#FF5F87", Dark: "#FF5F87"})
	errContextStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#808080", Dark: "#808080"})
)

// ErrorRenderer renders errors with terminal styling and source context.
type ErrorRenderer struct {
	source []byte
}

// NewErrorRenderer creates a renderer with source content for context.
func NewErrorRenderer(source []byte) *ErrorRenderer {
	return &ErrorRenderer{source: source}
}

// Render formats a single error with styling and context.
func (r *ErrorRenderer) Render(err error) string {
	if e, ok := err.(interface {
		GetPosition() ast.Position
		GetDirective() ast.Directive
		Error() string
	}); ok {
		return r.renderWithContext(e.GetPosition(), e.Error(), e.GetDirective())
	}

	if e, ok := err.(*parser.ParseError); ok {
		source := r.source
		if source == nil {
			source = e.SourceRange.Source
		}
		if source != nil {
			return r.renderWithSourceContext(e.Pos, e.Error(), source)
		}
	}

	if e, ok := err.(interface {
		GetPosition() ast.Position
		Error() string
	}); ok {
		if r.source != nil {
			return r.renderWithSourceContext(e.GetPosition(), e.Error(), r.source)
		}
	}

	return err.Error()
}

// RenderAll formats multiple errors, separating them with blank lines.
func (r *ErrorRenderer) RenderAll(errs []error) string {
	if len(errs) == 0 {
		return ""
	}

	var buf strings.Builder
	for i, err := range errs {
		buf.WriteString(r.Render(err))

		if i < len(errs)-1 {
			buf.WriteString("\n\n")
		}
	}

	return buf.String()
}

func (r *ErrorRenderer) renderWithSourceContext(pos ast.Position, message string, sourceContent []byte) string {
	var buf strings.Builder

	buf.WriteString(errorStyle.Render(message))
	buf.WriteString("\n\n")

	sourceStr := string(sourceContent)
	sourceLines := strings.Split(sourceStr, "\n")

	startLine := pos.Line - 3
	endLine := pos.Line + 1

	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(sourceLines) {
		endLine = len(sourceLines) - 1
	}

	for i := startLine; i <= endLine; i++ {
		if i >= len(sourceLines) {
			break
		}
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(sourceLines[i]))
		buf.WriteByte('\n')

		if i == pos.Line-1 && pos.Column > 0 {
			buf.WriteString("   ")
			for j := 0; j < pos.Column-1; j++ {
				buf.WriteByte(' ')
			}
			buf.WriteString(errCaretStyle.Render("^"))
			buf.WriteByte('\n')
		}
	}

	return buf.String()
}

func (r *ErrorRenderer) renderWithContext(pos ast.Position, message string, directive ast.Directive) string {
	if directive == nil {
		return message
	}

	var buf strings.Builder

	buf.WriteString(errorStyle.Render(message))
	buf.WriteString("\n\n")

	switch d := directive.(type) {
	case *ast.Transaction:
		var txnBuf bytes.Buffer
		txnFormatter := formatter.New(formatter.WithIndentation(0))

		if err := txnFormatter.FormatTransaction(d, &txnBuf); err == nil {
			lines := bytes.Split(txnBuf.Bytes(), []byte("\n"))
			for _, line := range lines {
				if len(line) > 0 {
					buf.WriteString("   ")
					buf.WriteString(errContextStyle.Render(string(line)))
					buf.WriteByte('\n')
				}
			}
		}

	case *ast.Balance:
		var line string
		if d.Amount != nil {
			line = fmt.Sprintf("%s balance %s  %s %s", d.Date.String(), d.Account, d.Amount.Value, d.Amount.Currency)
		} else {
			line = fmt.Sprintf("%s balance %s", d.Date.String(), d.Account)
		}
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(line))
		buf.WriteByte('\n')

	case *ast.Pad:
		line := fmt.Sprintf("%s pad %s %s", d.Date.String(), d.Account, d.AccountPad)
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(line))
		buf.WriteByte('\n')

	case *ast.Note:
		line := fmt.Sprintf("%s note %s %q", d.Date.String(), d.Account, d.Description)
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(line))
		buf.WriteByte('\n')

	case *ast.Document:
		line := fmt.Sprintf("%s document %s %q", d.Date.String(), d.Account, d.PathToDocument)
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(line))
		buf.WriteByte('\n')

	case *ast.Open:
		line := fmt.Sprintf("%s open %s", d.Date.String(), d.Account)
		if len(d.ConstraintCurrencies) > 0 {
			line += fmt.Sprintf(" %s", strings.Join(d.ConstraintCurrencies, ", "))
		}
		if d.BookingMethod != "" {
			line += fmt.Sprintf(" %s", d.BookingMethod)
		}
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(line))
		buf.WriteByte('\n')

	case *ast.Close:
		line := fmt.Sprintf("%s close %s", d.Date.String(), d.Account)
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(line))
		buf.WriteByte('\n')
	}

	return buf.String()
}
