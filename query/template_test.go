package query

import (
	"regexp"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// TestPySub pins replacements against Python 3.11's re.sub.
func TestPySub(t *testing.T) {
	tests := []struct {
		pattern, template, s, want, err string
	}{
		{pattern: `(Food)`, template: `[\1]`, s: "A:Food:B", want: "A:[Food]:B"},
		{pattern: `(Food)`, template: `[\g< 1 >]`, s: "A:Food:B", want: "A:[Food]:B"},
		{pattern: `(Food)`, template: `[\$]`, s: "A:Food:B", want: `A:[\$]:B`},
		{pattern: `(Food)`, template: `[\é]`, s: "A:Food:B", want: `A:[\é]:B`},
		{pattern: `(Food)`, template: `\t\0\012\101`, s: "Food", want: "\t\x00\nA"},
		{pattern: `(?P<x>o)`, template: `<\g<x>>`, s: "Food", want: "F<o><o>d"},
		{pattern: `(o)|(X)`, template: `[\2]`, s: "Fo", want: "F[]"},
		{pattern: `Zzz`, template: `\9`, s: "Food", err: "invalid group reference 9 at position 1"},
		{pattern: `(Food)`, template: "a\n\\9", s: "Food", err: "invalid group reference 9 at position 3 (line 2, column 2)"},
		{pattern: `(Food)`, template: `[\400]`, s: "Food", err: `octal escape value \400 outside of range 0-0o377 at position 1`},
		{pattern: `(Food)`, template: `[\gx]`, s: "Food", err: "missing < at position 3"},
		{pattern: `(Food)`, template: `[\g<1]`, s: "Food", err: "missing >, unterminated name at position 4"},
		{pattern: `(Food)`, template: `[\g<>]`, s: "Food", err: "missing group name at position 4"},
		{pattern: `(Food)`, template: `[\g<-1>]`, s: "Food", err: "bad character in group name '-1' at position 4"},
		{pattern: `(Food)`, template: `[\g<1_0>]`, s: "Food", err: "invalid group reference 10 at position 4"},
		{pattern: `(Food)`, template: `[\g<9999999999>]`, s: "Food", err: "invalid group reference 9999999999 at position 4"},
		{pattern: `(Food)`, template: `\`, s: "Food", err: "bad escape (end of pattern) at position 0"},
	}
	for _, test := range tests {
		t.Run(test.template, func(t *testing.T) {
			var got string
			err := func() (message string) {
				defer func() {
					if r := recover(); r != nil {
						message = r.(evalError).message
					}
				}()
				got = pySub(regexp.MustCompile(test.pattern), test.template, test.s)
				return ""
			}()
			assert.Equal(t, test.err, err)
			assert.Equal(t, test.want, got)
		})
	}
}
