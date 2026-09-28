package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFormatQueryDirective(t *testing.T) {
	source := `2024-01-01 query "cash" "SELECT * FROM accounts WHERE account ~ 'Cash'"`

	tree := parser.MustParseString(context.Background(), source)
	f := New()

	var buf bytes.Buffer
	err := f.Format(context.Background(), tree, []byte(source), &buf)
	assert.NoError(t, err)
	assert.Equal(t, source+"\n", buf.String())
}
