package cli

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestPythonJSONString(t *testing.T) {
	// Python's json.dumps("a\"b\\c\n\t\r\b\f\x01\x7fé中😀").
	assert.Equal(t, `"a\"b\\c\n\t\r\b\f\u0001\u007f\u00e9\u4e2d\ud83d\ude00"`,
		pythonJSONString("a\"b\\c\n\t\r\b\f\x01\x7fé中😀"))
}
