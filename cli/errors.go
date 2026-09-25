package cli

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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

// Render formats a single error with styling and context: like bean-check,
// an error about a directive shows the directive under it.
func (r *ErrorRenderer) Render(err error) string {
	if e, ok := err.(interface{ GetDirective() ast.Directive }); ok {
		if context := directiveContext(e.GetDirective()); context != "" {
			return errorStyle.Render(err.Error()) + "\n\n" + context
		}
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

	sourceLines := ast.SplitSourceLines(string(sourceContent))

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
			buf.WriteString(strings.Repeat(" ", caretPadding(sourceLines[i], pos.Column)))
			buf.WriteString(errCaretStyle.Render("^"))
			buf.WriteByte('\n')
		}
	}

	return buf.String()
}

func caretPadding(line string, byteColumn int) int {
	if byteColumn <= 1 {
		return 0
	}

	targetBytes := byteColumn - 1
	if targetBytes > len(line) {
		targetBytes = len(line)
	}

	width := 0
	for i := 0; i < targetBytes; {
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == utf8.RuneError && size == 1 {
			width++
			i++
			continue
		}
		if i+size > targetBytes {
			break
		}

		if r == '\t' {
			width += 8 - width%8
		} else {
			width += runewidth.RuneWidth(r)
		}
		i += size
	}
	return width
}

// directiveContext renders a directive as context lines under an error, or
// "" when there is no directive or no rendering for its kind. Every line is
// indented, so none starts with an error's path:line:.
func directiveContext(directive ast.Directive) string {
	var buf strings.Builder
	line := func(text string) {
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(text))
		buf.WriteByte('\n')
	}

	switch d := directive.(type) {
	case *ast.Transaction:
		var txnBuf bytes.Buffer
		if err := formatter.New(formatter.WithIndentation(2)).FormatTransaction(d, &txnBuf); err == nil {
			for _, text := range bytes.Split(txnBuf.Bytes(), []byte("\n")) {
				if len(text) > 0 {
					line(string(text))
				}
			}
		}

	case *ast.Balance:
		if d.Amount != nil {
			line(fmt.Sprintf("%s balance %s  %s %s", d.Date().String(), d.Account, d.Amount.Value, d.Amount.Currency))
		} else {
			line(fmt.Sprintf("%s balance %s", d.Date().String(), d.Account))
		}

	case *ast.Pad:
		line(fmt.Sprintf("%s pad %s %s", d.Date().String(), d.Account, d.AccountPad))

	case *ast.Note:
		line(fmt.Sprintf("%s note %s %q", d.Date().String(), d.Account, d.Description))

	case *ast.Document:
		line(fmt.Sprintf("%s document %s %q", d.Date().String(), d.Account, d.PathToDocument))

	case *ast.Open:
		text := fmt.Sprintf("%s open %s", d.Date().String(), d.Account)
		if len(d.ConstraintCurrencies) > 0 {
			text += " " + strings.Join(d.ConstraintCurrencies, ", ")
		}
		if d.BookingMethod != "" {
			text += " " + d.BookingMethod
		}
		line(text)

	case *ast.Close:
		line(fmt.Sprintf("%s close %s", d.Date().String(), d.Account))

	case *ast.Commodity:
		line(fmt.Sprintf("%s commodity %s", d.Date().String(), d.Currency))

	case *ast.Price:
		if d.Amount != nil {
			line(fmt.Sprintf("%s price %s  %s %s", d.Date().String(), d.Commodity, d.Amount.Value, d.Amount.Currency))
		} else {
			line(fmt.Sprintf("%s price %s", d.Date().String(), d.Commodity))
		}

	case *ast.Event:
		line(fmt.Sprintf("%s event %q %q", d.Date().String(), d.Name.Value, d.Value.Value))

	case *ast.Query:
		line(fmt.Sprintf("%s query %q %q", d.Date().String(), d.Name.Value, d.QueryString.Value))

	case *ast.Custom:
		line(fmt.Sprintf("%s custom %q", d.Date().String(), d.Type.Value))
	}

	return buf.String()
}
