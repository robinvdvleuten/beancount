package bql

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
)

// ParseError represents an error that occurred while parsing a BQL query.
// Pos is where beanquery's parser fails, which is not always the offending
// token (see the parser's nameErrorf and clauseErrorf).
type ParseError struct {
	Pos     ast.Position
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Message)
}

// GetPosition implements the positioned-error interface used by the CLI
// error renderer.
func (e *ParseError) GetPosition() ast.Position {
	return e.Pos
}

// ValueError is a statement beanquery's parser rejects with a Python
// exception rather than a syntax error: a date literal naming no date. It
// names no place in the statement.
type ValueError struct {
	Message string
}

func (e *ValueError) Error() string {
	return e.Message
}
