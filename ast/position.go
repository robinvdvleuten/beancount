package ast

import "fmt"

// Position represents a location in the source file.
type Position struct {
	Filename string
	Offset   int // Byte offset
	Line     int // Line number (1-indexed)
	Column   int // Column number (1-indexed)
}

// String returns a human-readable representation of the position.
func (p Position) String() string {
	if p.Filename != "" {
		return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// GoString returns a Go-syntax representation of the position.
func (p Position) GoString() string {
	return fmt.Sprintf("Position{Filename: %q, Line: %d, Column: %d}", p.Filename, p.Line, p.Column)
}
