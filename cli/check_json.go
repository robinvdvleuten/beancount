package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
)

// writeJSON prints the errors check finds as bean-check --json does:
// {"errors": [...]} alone on stdout, in Python's json.dump spelling, each
// error its message, filename and line. A load that fails is its one error.
func (cmd *CheckCmd) writeJSON(ctx context.Context, stdout io.Writer, loadResult *loader.LoadResult, loadErr error) error {
	var errs []error
	switch joined := loadErr.(type) {
	case nil:
		loadErrors, validationErrors, err := ledgerErrors(ctx, loadResult)
		if err != nil {
			return err
		}
		errs = append(loadErrors, validationErrors...)
	case interface{ Unwrap() []error }:
		errs = joined.Unwrap()
	default:
		errs = []error{loadErr}
	}
	return writeJSONErrors(stdout, errs)
}

// writeJSONErrors prints errs as {"errors": [...]} and fails the command
// when there are any.
func writeJSONErrors(stdout io.Writer, errs []error) error {
	var b strings.Builder
	b.WriteString(`{"errors": [`)
	for i, err := range errs {
		if i > 0 {
			b.WriteString(", ")
		}
		writeJSONError(&b, err)
	}
	b.WriteString("]}\n")
	if _, err := io.WriteString(stdout, b.String()); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}
	if len(errs) > 0 {
		return NewCommandError(1)
	}
	return nil
}

// missingFileError is bean-check's error for a ledger file that does not
// exist, which it blames on <load>:0.
type missingFileError struct{ path string }

func newMissingFileError(path string) *missingFileError { return &missingFileError{path: path} }

func (e *missingFileError) Error() string {
	return "File \"" + e.path + "\" does not exist"
}

func (e *missingFileError) GetPosition() ast.Position {
	return ast.Position{Filename: "<load>"}
}

// writeJSONError writes one error as bean-check's _error_to_json does: its
// message without the location, and a filename and lineno, null when the
// error has no position.
func writeJSONError(b *strings.Builder, err error) {
	message := err.Error()
	filename, lineno := "null", "null"
	if positioned, ok := err.(interface{ GetPosition() ast.Position }); ok {
		pos := positioned.GetPosition()
		if pos.Filename != "" {
			filename = pythonJSONString(pos.Filename)
			lineno = strconv.Itoa(pos.Line)
		}
		message = strings.TrimPrefix(message, pos.String()+": ")
		message = strings.TrimPrefix(message, fmt.Sprintf("%s:%d: ", pos.Filename, pos.Line))
	}
	switch e := err.(type) {
	case interface{ Message() string }:
		message = e.Message()
	case *parser.ParseError:
		message = e.Message
	}
	b.WriteString(`{"message": `)
	b.WriteString(pythonJSONString(message))
	b.WriteString(`, "filename": `)
	b.WriteString(filename)
	b.WriteString(`, "lineno": `)
	b.WriteString(lineno)
	b.WriteString("}")
}

// pythonJSONString quotes s as Python's json.dumps does by default
// (ensure_ascii): every character outside space to ~ is an escape, as a
// surrogate pair beyond the Basic Multilingual Plane.
func pythonJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20 || (r >= 0x7f && r <= 0xffff):
				fmt.Fprintf(&b, `\u%04x`, r)
			case r > 0xffff:
				r -= 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
