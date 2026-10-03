package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/printer"
)

var (
	errCaretStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#FF5F87", Dark: "#FF5F87"})
	errContextStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#808080", Dark: "#808080"})
)

// ErrorRenderer renders errors with terminal styling and source context.
type ErrorRenderer struct {
	sources map[string][]byte
	print   []printer.Option    // how a directive under an error is printed
	lines   map[string][]string // each source split into lines, on first use
}

// NewErrorRenderer creates a renderer that shows an error in the context of
// the source its position names, looked up by filename in sources, and an
// error about a directive with the directive printed with opts. Pass
// printer.WithBookedPositions(Ledger.BookedPositions) to print a
// transaction's postings as booked, as bean-check does.
func NewErrorRenderer(sources map[string][]byte, opts ...printer.Option) *ErrorRenderer {
	return &ErrorRenderer{sources: sources, print: opts, lines: make(map[string][]string)}
}

// newLedgerErrorRenderer creates the renderer for the errors of a loaded
// ledger: each in the context of the loaded file its position names and,
// like bean-check, an error's transaction with its postings as booked.
func newLedgerErrorRenderer(result *ledgerload.Result) *ErrorRenderer {
	return NewErrorRenderer(result.Sources, printer.WithBookedPositions(result.Ledger.BookedPositions))
}

// loadFailureLayout is how a command lays out a load that cannot go on.
type loadFailureLayout int

const (
	// loadFailureLabelled ends with a blank line and the label.
	loadFailureLabelled loadFailureLayout = iota
	// loadFailureCompact ends with the label, as format does.
	loadFailureCompact
	// loadFailureBare is the errors alone, as query prints them, like
	// bean-query.
	loadFailureBare
)

// printLoadFailure reports on stderr what stopped a load: each error in
// the context of the source renderer has for it, then the layout's label.
func printLoadFailure(stderr io.Writer, renderer *ErrorRenderer, layout loadFailureLayout, errs ...error) {
	for _, err := range errs {
		_, _ = fmt.Fprintln(stderr, renderer.Render(err))
	}
	if layout == loadFailureBare {
		return
	}
	if layout == loadFailureLabelled {
		_, _ = fmt.Fprintln(stderr)
	}
	printError(stderr, "parse error")
}

// reportLoadFailure reports on stderr a load of f that cannot go on, in
// the context of its source, which it reads again, and returns the
// command's failure.
func (f *FileOrStdin) reportLoadFailure(stderr io.Writer, layout loadFailureLayout, err error) error {
	source, readErr := f.GetSourceContent()
	if readErr != nil {
		return fmt.Errorf("failed to read file for error context: %w", readErr)
	}
	printLoadFailure(stderr, f.errorRenderer(source), layout, err)
	return NewCommandError(1)
}

// Render formats a single error with styling and context: like bean-check,
// an error about a directive shows the directive under it.
func (r *ErrorRenderer) Render(err error) string {
	if e, ok := err.(interface{ GetDirective() ast.Directive }); ok {
		if context := directiveContext(e.GetDirective(), r.print...); context != "" {
			return errorStyle.Render(err.Error()) + "\n\n" + context
		}
	}

	if e, ok := err.(*parser.ParseError); ok {
		if lines, ok := r.sourceLines(e.Pos.Filename); ok {
			return r.renderWithSourceContext(e.Pos, e.Error(), lines)
		}
		if e.SourceRange.Source != nil {
			return r.renderWithSourceContext(e.Pos, e.Error(), ast.SplitSourceLines(string(e.SourceRange.Source)))
		}
	}

	if e, ok := err.(interface {
		GetPosition() ast.Position
		Error() string
	}); ok {
		if lines, ok := r.sourceLines(e.GetPosition().Filename); ok {
			return r.renderWithSourceContext(e.GetPosition(), e.Error(), lines)
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

// sourceLines returns the lines of filename's source, or false when the
// renderer does not have it. Each source is split once, so rendering many
// errors stays linear in the source size.
func (r *ErrorRenderer) sourceLines(filename string) ([]string, bool) {
	if lines, ok := r.lines[filename]; ok {
		return lines, true
	}
	source, ok := r.sources[filename]
	if !ok {
		return nil, false
	}
	lines := ast.SplitSourceLines(string(source))
	r.lines[filename] = lines
	return lines, true
}

func (r *ErrorRenderer) renderWithSourceContext(pos ast.Position, message string, sourceLines []string) string {
	var buf strings.Builder

	buf.WriteString(errorStyle.Render(message))
	buf.WriteString("\n\n")

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

// directiveContext renders a directive as context lines under an error, as
// beancount prints it, or "" when there is no directive. Every line is
// indented, so none starts with an error's path:line:.
func directiveContext(directive ast.Directive, opts ...printer.Option) string {
	if directive == nil {
		return ""
	}
	var buf strings.Builder
	for line := range strings.Lines(printer.Sprint(directive, opts...)) {
		buf.WriteString("   ")
		buf.WriteString(errContextStyle.Render(strings.TrimSuffix(line, "\n")))
		buf.WriteByte('\n')
	}
	return buf.String()
}
