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
	// Falls back to CStyle if original metadata is unavailable.
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

// formatStringWithMetadata formats a string using StringMetadata when available.
// If the escape style is EscapeStyleOriginal and metadata is available, uses the
// original quoted content. Otherwise falls back to escaping the logical value.
func (f *Formatter) formatStringWithMetadata(value string, meta *ast.StringMetadata, buf *strings.Builder) {
	if f.StringEscapeStyle == EscapeStyleOriginal && meta.HasOriginal() {
		buf.WriteString(meta.QuotedContent())
	} else {
		buf.WriteByte('"')
		buf.WriteString(f.escapeString(value))
		buf.WriteByte('"')
	}
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
