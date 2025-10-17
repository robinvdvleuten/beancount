package parser

import (
	"fmt"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/robinvdvleuten/beancount/ast"
)

// ParseError represents a syntax error during parsing.
type ParseError struct {
	Pos        lexer.Position
	Message    string
	Underlying error
}

func (e *ParseError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = fmt.Sprintf("line %d", e.Pos.Line)
	}

	return fmt.Sprintf("%s: %s", location, e.Message)
}

func (e *ParseError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *ParseError) GetDirective() ast.Directive {
	return nil // Parse errors don't have directive context
}

func (e *ParseError) Unwrap() error {
	return e.Underlying
}

// NewParseError creates a parse error from a participle error.
// It extracts position information and creates a clean error message.
func NewParseError(filename string, err error) *ParseError {
	// Try to extract position from participle Error interface
	if pErr, ok := err.(participle.Error); ok {
		pos := pErr.Position()
		return &ParseError{
			Pos: lexer.Position{
				Filename: filename,
				Line:     pos.Line,
				Column:   pos.Column,
			},
			Message:    pErr.Message(), // Clean message without position
			Underlying: err,
		}
	}

	// Fallback for other error types
	return &ParseError{
		Pos: lexer.Position{
			Filename: filename,
			Line:     1,
			Column:   1,
		},
		Message:    err.Error(),
		Underlying: err,
	}
}
