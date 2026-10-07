package loader

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

// TestFnmatch holds fnmatch to what Python 3.9's fnmatch.fnmatchcase
// answers for each pattern and name.
func TestFnmatch(t *testing.T) {
	tests := []struct {
		pattern string
		matches map[string]bool
	}{
		{"foo[.beancount", map[string]bool{"foo[.beancount": true, "foo.beancount": false, "fooa.beancount": false}},
		{"[!x].beancount", map[string]bool{"a.beancount": true, "x.beancount": false, "!.beancount": true}},
		{"[^x].beancount", map[string]bool{"a.beancount": false, "x.beancount": true, "^.beancount": true}},
		{"[]]x", map[string]bool{"]x": true, "ax": false}},
		{"[!]]x", map[string]bool{"]x": false, "ax": true}},
		{"[a-c]", map[string]bool{"a": true, "b": true, "d": false, "-": false}},
		{"[a-]", map[string]bool{"a": true, "-": true, "b": false}},
		{"[-a]", map[string]bool{"a": true, "-": true, "b": false}},
		{`[a\]x`, map[string]bool{"ax": true, `\x`: true, "]x": false}},
		{`a\*b`, map[string]bool{`a\xb`: true, "a*b": false, `a\*b`: true}},
		{"[!a-c]", map[string]bool{"b": false, "d": true}},
		{"[[:]x", map[string]bool{"[x": true, ":x": true, "ax": false}},
		{"[&&b]", map[string]bool{"&": true, "b": true, "c": false}},
		{"*.beancount", map[string]bool{"a.beancount": true, ".beancount": true, "a.txt": false}},
		{"a?c", map[string]bool{"abc": true, "ac": false, "a\nc": true}},
		{"[!]", map[string]bool{"[!]": true, "!": false, "]": false, "a": false}},
		{"[", map[string]bool{"[": true, "a": false}},
		{"*[", map[string]bool{"a[": true, "[": true}},
		// Python's re rejects these ranges, and glob raises: they match
		// nothing here.
		{"[z-a]", map[string]bool{"a": false, "z": false}},
		{"[a--c]", map[string]bool{"a": false, "-": false}},
	}
	for _, test := range tests {
		match := fnmatch(test.pattern)
		for name, want := range test.matches {
			assert.Equal(t, want, match(name), "pattern %q, name %q", test.pattern, name)
		}
	}
}
