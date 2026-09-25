package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// ParseError represents an error that occurred during parsing.
type ParseError struct {
	Pos         ast.Position
	Message     string
	SourceRange SourceRange // Range in source for context extraction
}

// ParseErrors lists a source's syntax errors in source order. The parser
// drops the directive each one is in and returns it with the AST of the rest.
type ParseErrors []*ParseError

func (e ParseErrors) Error() string {
	var b strings.Builder
	for i, err := range e {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(err.Error())
	}
	return b.String()
}

// Unwrap lets errors.As find each *ParseError.
func (e ParseErrors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, err := range e {
		errs[i] = err
	}
	return errs
}

// SourceRange defines a range in the source content for error context.
type SourceRange struct {
	StartOffset int    // Byte offset where source content starts
	EndOffset   int    // Byte offset where source content ends (exclusive)
	Source      []byte // Source content (could be nil if using lazy loading)
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Message)
}

func (e *ParseError) GetPosition() ast.Position {
	return e.Pos
}

func (e *ParseError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "ParseError",
		"message":  e.Error(),
		"position": e.Pos,
	})
}

// newErrorfWithSource creates a new parse error with formatted message and source range.
func newErrorfWithSource(pos ast.Position, sourceRange SourceRange, format string, args ...any) *ParseError {
	return &ParseError{
		Pos:         pos,
		Message:     fmt.Sprintf(format, args...),
		SourceRange: sourceRange,
	}
}

// NewParseError wraps an existing parse error with filename context.
// This is used by the loader to wrap errors from parser with file information.
func NewParseError(filename string, err error) *ParseError {
	// If it's already a ParseError, return it as-is (it already has position info)
	if pErr, ok := err.(*ParseError); ok {
		return pErr
	}

	// Otherwise, wrap it in a new ParseError
	return &ParseError{
		Pos:     ast.Position{Filename: filename, Line: 1, Column: 1},
		Message: err.Error(),
	}
}

// StringLiteralError represents an error in string literal parsing.
type StringLiteralError struct {
	Message string
}

func (e *StringLiteralError) Error() string {
	return e.Message
}

// NewParseErrorWithSource wraps an existing parse error with filename context and source range.
// This is used by the loader to wrap errors from parser with file information and context.
func NewParseErrorWithSource(filename string, err error, source []byte) *ParseError {
	// If it's already a ParseError, return it as-is (it already has position info)
	if pErr, ok := err.(*ParseError); ok {
		return pErr
	}

	// Otherwise, wrap it in a new ParseError with full source range
	// For fallback errors, we include the entire source for context
	return &ParseError{
		Pos:     ast.Position{Filename: filename, Line: 1, Column: 1},
		Message: err.Error(),
		SourceRange: SourceRange{
			StartOffset: 0,
			EndOffset:   len(source),
			Source:      source,
		},
	}
}
