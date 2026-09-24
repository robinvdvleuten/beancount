package bql

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
)

// ErrorKind classifies a parse error the way bean-query reports it.
type ErrorKind uint8

const (
	// ErrSyntax is an unexpected token; Near holds its value.
	ErrSyntax ErrorKind = iota
	// ErrUnterminated is a query that ends where more input was expected.
	ErrUnterminated
	// ErrUnknownToken is input the lexer cannot tokenize.
	ErrUnknownToken
	// ErrEmptyFrom is a FROM clause with neither an expression nor a
	// transform.
	ErrEmptyFrom
)

// ParseError represents an error that occurred while parsing a BQL query.
type ParseError struct {
	Pos     ast.Position
	Message string
	Kind    ErrorKind
	// Near is the offending token's value as bean-query's lexer produces it:
	// keywords upper-cased, identifiers lower-cased, strings unquoted.
	Near string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Pos, e.Message)
}

// GetPosition implements the positioned-error interface used by the CLI
// error renderer.
func (e *ParseError) GetPosition() ast.Position {
	return e.Pos
}
