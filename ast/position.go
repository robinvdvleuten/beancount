package ast

import (
	"fmt"
	"strings"
)

// Position represents a location in the source file.
type Position struct {
	Filename string `json:"filename"`
	Offset   int    `json:"offset"` // Byte offset
	Line     int    `json:"line"`   // Line number (1-indexed)
	Column   int    `json:"column"` // Byte column number (1-indexed)
}

// Positioned is implemented by all AST nodes that have a source position.
type Positioned interface {
	Position() Position
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

// CountLineBreaks counts the line breaks in s as SplitSourceLines splits
// them: \r\n is one break, and so is a lone \r or \n.
func CountLineBreaks(s string) int {
	count := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			count++
		case '\r':
			count++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		}
	}
	return count
}

// SplitSourceLines splits source text on \r\n, \r, or \n, matching the
// lexer's line-break semantics. Use this instead of strings.Split(s, "\n")
// whenever the result must align with lexer-assigned Position.Line values.
func SplitSourceLines(s string) []string {
	var lines []string
	for len(s) > 0 {
		i := strings.IndexAny(s, "\r\n")
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:i])
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			s = s[i+2:]
		} else {
			s = s[i+1:]
		}
	}
	return lines
}
