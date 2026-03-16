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

func TestFormatQueryDirectivePreservesInlineCommentWhenReconstructed(t *testing.T) {
	source := `2024-01-01 query "cash" "SELECT 1" ; comment`

	tree := parser.MustParseString(context.Background(), source)
	f := New()

	var buf bytes.Buffer
	err := f.Format(context.Background(), tree, nil, &buf)
	assert.NoError(t, err)
	assert.Equal(t, source+"\n", buf.String())
}

func TestFormatOptionPreservesInlineCommentWhenReconstructed(t *testing.T) {
	source := `option "title" "Ledger" ; comment`

	tree := parser.MustParseString(context.Background(), source)
	f := New()

	var buf bytes.Buffer
	err := f.Format(context.Background(), tree, nil, &buf)
	assert.NoError(t, err)
	assert.Equal(t, source+"\n", buf.String())
}
