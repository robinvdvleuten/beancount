package bql

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
)

// ParseError represents an error that occurred while parsing a BQL query.
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
