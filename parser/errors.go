package parser

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
)

// ParseError represents an error that occurred during parsing.
type ParseError struct {
	Pos     ast.Position
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Message)
}

// newErrorf creates a new parse error with formatted message.
func newErrorf(pos ast.Position, format string, args ...interface{}) *ParseError {
	return &ParseError{
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
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
