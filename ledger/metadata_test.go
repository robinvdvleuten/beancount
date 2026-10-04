package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestRepeatedMetadataKeys(t *testing.T) {
	// Like beancount's parser, a transaction and a posting keep a key's
	// first value and report the others; any other directive keeps its last
	// value, in the key's first place, and reports nothing.
	source := `
2020-01-01 open Assets:A
  who: "first"
  other: 1
  who: "second"
2020-01-01 open Assets:B

2020-01-02 * "x"
  who: "t1"
  kk: "a"
  who: "t2"
  Assets:A  1 USD
    who: "p1"
    who: "p2"
  Assets:B
`
	parsed := parser.MustParseString(context.Background(), source)
	l := New()
	tree, err := l.Process(context.Background(), parsed)
	assert.NoError(t, err)

	errs := l.Errors()
	assert.Equal(t, 2, len(errs), "errors: %v", errs)
	assert.Contains(t, errs[0].Error(), `key="who", value="t2": duplicate key`)
	assert.Contains(t, errs[1].Error(), `(account Assets:A): key="who", value="p2": duplicate key`)

	values := func(metadata []*ast.Metadata) []string {
		var out []string
		for _, md := range metadata {
			out = append(out, md.Key+"="+md.Value.String())
		}
		return out
	}
	open := tree.Directives[0].(*ast.Open)
	txn := tree.Directives[2].(*ast.Transaction)
	assert.Equal(t, []string{"who=second", "other=1"}, values(open.Metadata))
	assert.Equal(t, []string{"who=t1", "kk=a"}, values(txn.Metadata))
	assert.Equal(t, []string{"who=p1"}, values(txn.Postings[0].Metadata))

	// The parsed tree, which the formatter reads, keeps every line.
	assert.Equal(t, 3, len(parsed.Directives[0].GetMetadata()))
}
