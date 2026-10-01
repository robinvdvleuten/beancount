package query

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// Format is the output format Run renders a result table in.
type Format string

const (
	// FormatText renders beanquery's default text table: headers centered
	// and truncated to the data width over a dashed rule, nothing for an
	// empty result.
	FormatText Format = "text"
	// FormatCSV renders beanquery's -f csv output: full column names,
	// cells as the text table formats them, and CRLF line endings.
	FormatCSV Format = "csv"
)

// renderers maps each Format to its renderer.
var renderers = map[Format]func(result *table, w io.Writer) error{
	FormatText: renderText,
	FormatCSV:  renderCSV,
}

// output is where and how a statement writes what it produces.
type output struct {
	w         io.Writer
	format    Format
	numberify bool
}

// statement is a compiled BQL statement, ready to run.
type statement interface {
	run(ctx context.Context, qctx *Context, out output) error
}

// Run runs one BQL statement against qctx and writes what beanquery writes
// for it: a SELECT, BALANCES or JOURNAL result table in format, with every
// amount column split into a number column per currency when numberify is
// set, and PRINT's directives as beancount text.
// A statement that does not parse or compile, or fails while it runs,
// writes nothing and returns an *Error. Like beanquery's shell, a text
// without a statement, such as an empty one, writes nothing. Run returns
// another error only for an unknown format, a qctx without an AST, a
// cancelled ctx, or a failed write.
func Run(ctx context.Context, qctx *Context, text string, format Format, numberify bool, w io.Writer) error {
	if _, ok := renderers[format]; !ok {
		return fmt.Errorf("unknown output format %q", format)
	}
	if qctx.AST == nil {
		return errors.New("query context has no AST")
	}

	// The shell strips the text and runs nothing when it does not start
	// with a command name (cmd.parseline): an empty text, or one starting
	// with ;, ( or a comment.
	text = strings.TrimFunc(text, bql.IsSpace)
	if text == "" || !isCommandChar(text[0]) {
		return nil
	}

	timer := telemetry.FromContext(ctx).Start("query.run")
	defer timer.End()

	parsed, err := bql.Parse(text)
	if err != nil {
		var parseErr *bql.ParseError
		if !errors.As(err, &parseErr) {
			return err
		}
		// beanquery words every parse error alike and marks where its
		// parser failed with a single caret.
		offset := min(parseErr.Pos.Offset, len(text))
		return &Error{message: "syntax error", text: text, hasNode: true, start: offset, end: offset}
	}
	return compileAndRun(ctx, qctx, parsed, text, output{w: w, format: format, numberify: numberify})
}

// compileAndRun compiles and runs a parsed statement. A statement that
// fails while it runs, where beanquery raises a Python exception, writes
// nothing and returns an *Error without a node.
func compileAndRun(ctx context.Context, qctx *Context, parsed bql.Statement, text string, out output) (err error) {
	defer func() {
		if r := recover(); r != nil {
			failure, ok := r.(evalError)
			if !ok {
				panic(r)
			}
			err = &Error{message: failure.message, text: text}
		}
	}()
	stmt, err := compile(qctx, parsed)
	if err != nil {
		var queryErr *Error
		if errors.As(err, &queryErr) {
			queryErr.text = text
		}
		return err
	}
	return stmt.run(ctx, qctx, out)
}

// evalError is a statement failing while it runs: an invalid regular
// expression, or an integer that overflows.
type evalError struct{ message string }

// fail aborts the statement being run, which Run reports as an *Error
// with message and no node. Run executes a statement before it writes
// any of it, so a failure leaves nothing written.
func fail(format string, args ...any) {
	panic(evalError{message: fmt.Sprintf(format, args...)})
}

// run executes a compiled SELECT and renders its result. Like beanquery's
// shell, numberify applies before the format is chosen.
func (c *compiledSelect) run(ctx context.Context, qctx *Context, out output) error {
	result, err := execute(ctx, qctx, c)
	if err != nil {
		return err
	}
	if out.numberify {
		result = numberify(result)
	}
	return renderers[out.format](result, out.w)
}

// Error is a statement that does not parse or compile, in beanquery's
// words, or one that fails while it runs.
type Error struct {
	message string
	text    string
	// hasNode is set when the error names a node of the statement, whose
	// source text runs from byte offset start to end. A parse error names
	// the empty span where the parser failed.
	hasNode    bool
	start, end int
}

// Error returns beanquery's message, such as
// `column "bogus" not found in table "postings"`.
func (e *Error) Error() string {
	return e.message
}

// Report renders the error as beanquery's shell prints it
// (shell.py:render_exception): "error: " and the message, then, when the
// error names a node, the statement's lines up to the node's, each behind
// "| ", and a caret under each of the node's characters (one for an empty
// span). The shell prints a traceback for an error without a node; Report
// prints its error line alone.
func (e *Error) Report() string {
	var b strings.Builder
	b.WriteString("error: ")
	b.WriteString(e.message)
	if !e.hasNode {
		return b.String()
	}

	// Like render_location, skip leading blank lines, strip trailing
	// whitespace and expand tabs, but count the caret's column in the
	// line's characters as written.
	pos := e.start
	leading := true
	lines := splitLines(e.text)
	for i, line := range lines {
		last := pos < len(line) || i == len(lines)-1
		stripped := strings.TrimRightFunc(line, bql.IsSpace)
		if !last && leading && stripped == "" {
			pos -= len(line)
			continue
		}
		leading = false
		b.WriteString("\n| ")
		b.WriteString(expandTabs(stripped))
		if last {
			column := utf8.RuneCountInString(line[:min(pos, len(line))])
			width := max(utf8.RuneCountInString(e.text[e.start:e.end]), 1)
			b.WriteString("\n| ")
			b.WriteString(strings.Repeat(" ", column))
			b.WriteString(strings.Repeat("^", width))
			break
		}
		pos -= len(line)
	}
	return b.String()
}

// splitLines splits text after each line break, like Python's
// str.splitlines(True): \n, \r, \r\n, \v, \f, \x1c to \x1e, \x85, U+2028
// and U+2029.
func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		switch r {
		case '\r':
			if i < len(text) && text[i] == '\n' {
				i++
			}
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029':
		default:
			continue
		}
		lines = append(lines, text[start:i])
		start = i
	}
	if start < len(text) || len(lines) == 0 {
		lines = append(lines, text[start:])
	}
	return lines
}

// isCommandChar reports whether c is one of the shell's identchars, which
// a command name is made of, or the ? it reads as help: an ASCII letter or
// digit, _ or a dot.
func isCommandChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '?'
}

// expandTabs replaces each tab with the spaces up to the next multiple of
// eight characters, like Python's str.expandtabs.
func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var b strings.Builder
	column := 0
	for _, r := range line {
		if r == '\t' {
			spaces := 8 - column%8
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			continue
		}
		b.WriteRune(r)
		column++
	}
	return b.String()
}
