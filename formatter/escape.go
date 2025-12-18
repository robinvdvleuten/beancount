package formatter

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// StringEscapeStyle controls how strings are escaped in formatter output.
type StringEscapeStyle int

const (
	// EscapeStyleNone outputs strings without escape sequences.
	// Newlines, tabs, quotes become their literal characters (multi-line output).
	EscapeStyleNone StringEscapeStyle = iota
	// EscapeStyleCStyle outputs strings with C-style escape sequences.
	// Newlines become \n, tabs become \t, quotes become \", backslashes become \\.
	EscapeStyleCStyle
	// EscapeStyleOriginal tries to match the original source escape style.
	// Falls back to CStyle if original raw token is unavailable.
	EscapeStyleOriginal
)

// escapeString escapes special characters in strings for Beancount format.
// Uses the formatter's configured escape style.
func (f *Formatter) escapeString(s string) string {
	switch f.StringEscapeStyle {
	case EscapeStyleNone:
		return s
	case EscapeStyleCStyle:
		return escapeCStyle(s)
	case EscapeStyleOriginal:
		return escapeCStyle(s) // Fallback to C-style
	default:
		return escapeCStyle(s)
	}
}

// formatRawString formats a RawString to the buffer.
// If the RawString has a raw token and EscapeStyleOriginal is set, uses the raw token directly.
// Otherwise, quotes and escapes the logical value.
func (f *Formatter) formatRawString(s ast.RawString, buf *strings.Builder) {
	// EscapeStyleOriginal: use raw token if available
	if f.StringEscapeStyle == EscapeStyleOriginal && s.HasRaw() {
		buf.WriteString(s.Raw)
		return
	}

	// Otherwise, quote and escape the logical value
	buf.WriteByte('"')
	buf.WriteString(f.escapeString(s.Value))
	buf.WriteByte('"')
}

// escapeCStyle escapes special characters using C-style escape sequences.
func escapeCStyle(s string) string {
	// Quick check if escaping is needed
	needsEscape := false
	for _, c := range s {
		if c == '"' || c == '\\' || c == '\n' || c == '\t' || c == '\r' {
			needsEscape = true
			break
		}
	}

	if !needsEscape {
		return s
	}

	// Use strings.Builder for efficient escaping
	var buf strings.Builder
	buf.Grow(len(s) + 10) // Add some extra capacity for escape sequences

	for _, c := range s {
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\t':
			buf.WriteString(`\t`)
		case '\r':
			buf.WriteString(`\r`)
		default:
			buf.WriteRune(c)
		}
	}

	return buf.String()
}
