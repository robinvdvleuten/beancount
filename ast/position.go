package ast

import "fmt"

// Position represents a location in the source file.
type Position struct {
	Filename string
	Offset   int // Byte offset
	Line     int // Line number (1-indexed)
	Column   int // Column number (1-indexed)
}

// Span represents a range in the source file.
// Used to preserve original source text for formatting (e.g., expressions like "(100 + 50)").
type Span struct {
	Start int // Starting byte offset (inclusive)
	End   int // Ending byte offset (exclusive)
}

// IsZero returns true if this is an uninitialized span.
func (s Span) IsZero() bool {
	return s.Start == 0 && s.End == 0
}

// Text extracts the source text for this span (zero-copy slice).
// Returns empty string if span is invalid or zero.
func (s Span) Text(source []byte) string {
	if s.IsZero() || s.Start < 0 || s.End <= s.Start || s.End > len(source) {
		return ""
	}
	return string(source[s.Start:s.End])
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
