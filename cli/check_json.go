package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledgerload"
)

// checkJSONErrors returns the errors check --json prints: those of the
// loaded ledger, or a load that fails as its one error.
func checkJSONErrors(result *ledgerload.Result, loadErr error) []error {
	switch joined := loadErr.(type) {
	case nil:
		return diagnostic.Errors(result.Diagnostics())
	case interface{ Unwrap() []error }:
		return joined.Unwrap()
	default:
		return []error{loadErr}
	}
}

// writeJSONErrors prints errs as bean-check --json does: {"errors": [...]}
// alone on stdout, in Python's json.dump spelling, each error its message,
// filename and line. It fails the command when there are any.
func writeJSONErrors(stdout io.Writer, errs []error) error {
	var b strings.Builder
	b.WriteString(`{"errors": [`)
	for i, err := range errs {
		if i > 0 {
			b.WriteString(", ")
		}
		writeJSONError(&b, positioned(err))
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

// Error has no Error line: bean-check prints the message alone.
func (e *missingFileError) Error() string { return e.Message() }

func (e *missingFileError) Kind() string { return "MissingFileError" }

func (e *missingFileError) Message() string {
	return "File \"" + e.path + "\" does not exist"
}

func (e *missingFileError) GetPosition() ast.Position {
	return ast.Position{Filename: "<load>"}
}

// unpositionedError gives an error without the shape, such as an I/O
// failure, the shape of one that blames no file: its whole text is its
// message.
type unpositionedError struct{ error }

func (e unpositionedError) Kind() string              { return "LoadError" }
func (e unpositionedError) Message() string           { return e.Error() }
func (e unpositionedError) GetPosition() ast.Position { return ast.Position{} }

// positioned returns err in the shape every error of a loaded ledger has.
func positioned(err error) diagnostic.Positioned {
	if shaped, ok := err.(diagnostic.Positioned); ok {
		return shaped
	}
	return unpositionedError{err}
}

// writeJSONError writes one error as bean-check's _error_to_json does: its
// message without the Error line, and a filename and lineno, null when the
// error blames no file.
func writeJSONError(b *strings.Builder, err diagnostic.Positioned) {
	filename, lineno := "null", "null"
	if pos := err.GetPosition(); pos.Filename != "" {
		filename = pythonJSONString(pos.Filename)
		lineno = strconv.Itoa(pos.Line)
	}
	b.WriteString(`{"message": `)
	b.WriteString(pythonJSONString(err.Message()))
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
