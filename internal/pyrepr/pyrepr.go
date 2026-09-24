// Package pyrepr renders values like Python's repr(), which bean-query
// embeds in some of its messages.
package pyrepr

import (
	"fmt"
	"strings"
)

// String quotes s like Python's repr() of a str: single quotes unless s
// holds a single quote and no double quote, with backslash escapes for
// the quote, backslashes and control characters.
func String(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}
