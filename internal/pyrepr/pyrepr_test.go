package pyrepr

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestString(t *testing.T) {
	for in, want := range map[string]string{
		"abc":         `'abc'`,
		"it's":        `"it's"`,
		`say "hi"`:    `'say "hi"'`,
		`it's "x"`:    `'it\'s "x"'`,
		"a\\b":        `'a\\b'`,
		"tab\there\n": `'tab\there\n'`,
		"\x01":        `'\x01'`,
		"é":           `'é'`,
	} {
		assert.Equal(t, want, String(in), in)
	}
}
