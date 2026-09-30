package query

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/robinvdvleuten/beancount/internal/pyrepr"
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
// A statement that does not parse or compile is reported on w in
// bean-query's words, and Run returns nil. Run returns an error only for an
// unknown format, a qctx without an AST, a cancelled ctx, or a failed write.
func Run(ctx context.Context, qctx *Context, text string, format Format, numberify bool, w io.Writer) error {
	if _, ok := renderers[format]; !ok {
		return fmt.Errorf("unknown output format %q", format)
	}
	if qctx.AST == nil {
		return errors.New("query context has no AST")
	}

	timer := telemetry.FromContext(ctx).Start("query.run")
	defer timer.End()

	parsed, err := bql.Parse(text)
	if err != nil {
		return writeError(w, text, err)
	}
	stmt, err := compile(qctx, parsed)
	if err != nil {
		return writeError(w, text, err)
	}
	return stmt.run(ctx, qctx, output{w: w, format: format, numberify: numberify})
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

// writeError reports a query error like bean-query: parse errors verbatim as
// its parser raises them, compilation errors behind "ERROR: ".
func writeError(w io.Writer, text string, err error) error {
	var parseErr *bql.ParseError
	if !errors.As(err, &parseErr) {
		_, writeErr := fmt.Fprintf(w, "ERROR: %s\n", err.Error())
		return writeErr
	}

	// bean-query's lexer positions are character offsets.
	offset := utf8.RuneCountInString(text[:min(parseErr.Pos.Offset, len(text))])
	var message string
	switch parseErr.Kind {
	case bql.ErrUnterminated:
		message = "ERROR: unterminated statement. Missing a semicolon?"
	case bql.ErrUnknownToken:
		message = fmt.Sprintf("Unknown token: LexToken(error,%s,1,%d)", pyrepr.String(text[parseErr.Pos.Offset:]), offset)
	case bql.ErrEmptyFrom:
		message = "Empty FROM expression is not allowed"
	default:
		message = fmt.Sprintf("ERROR: Syntax error near '%s' (at %d)\n  %s\n  %s^", parseErr.Near, offset, text, strings.Repeat(" ", offset))
	}
	_, writeErr := fmt.Fprintln(w, message)
	return writeErr
}
