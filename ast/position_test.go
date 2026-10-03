package ast

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

// CountLineBreaks and SplitSourceLines number lines alike: text that does
// not end in a break has one more line than it has breaks.
func TestCountLineBreaks(t *testing.T) {
	for _, s := range []string{"", "a", "a\nb", "a\rb", "a\r\nb", "a\r\rb", "a\n\rb", "\ra", "a\r\n\r\nb", "\"\r\""} {
		want := max(len(SplitSourceLines(s)), 1) - 1
		assert.Equal(t, want, CountLineBreaks(s), "%q", s)
	}
	assert.Equal(t, 2, CountLineBreaks("a\r\nb\r"))
}
