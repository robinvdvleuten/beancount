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

func TestSameObject(t *testing.T) {
	str := func(s string) *ast.MetadataValue { return &ast.MetadataValue{StringValue: &ast.RawString{Value: s}} }
	currency := func(s string) *ast.MetadataValue { return &ast.MetadataValue{Currency: &s} }
	tag := func(s string) *ast.MetadataValue { tag := ast.Tag(s); return &ast.MetadataValue{Tag: &tag} }
	account := func(s string) *ast.MetadataValue { a := ast.Account(s); return &ast.MetadataValue{Account: &a} }
	boolean := func(b bool) *ast.MetadataValue { return &ast.MetadataValue{Boolean: &b} }
	number := func(s string) *ast.MetadataValue { return &ast.MetadataValue{Number: &s} }

	for _, tt := range []struct {
		name string
		a, b *ast.MetadataValue
		same bool
	}{
		{"none", nil, nil, true},
		{"none and an empty string", nil, str(""), false},
		{"empty strings", str(""), str(""), true},
		{"one ASCII character", str("v"), str("v"), true},
		{"one Latin-1 character", str("é"), str("é"), true},
		{"one character outside Latin-1", str("€"), str("€"), false},
		{"two characters", str("ab"), str("ab"), false},
		{"different characters", str("v"), str("w"), false},
		{"a string and a currency", str("V"), currency("V"), true},
		{"a currency of three letters", currency("USD"), currency("USD"), false},
		{"one-letter tags", tag("t"), tag("t"), true},
		{"longer tags", tag("tt"), tag("tt"), false},
		{"accounts", account("Assets:A"), account("Assets:A"), true},
		{"an account and a string", account("Assets:A"), str("Assets:A"), false},
		{"booleans", boolean(true), boolean(true), true},
		{"different booleans", boolean(true), boolean(false), false},
		{"numbers", number("1"), number("1"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.same, sameObject(tt.a, tt.b))
			assert.Equal(t, tt.same, sameObject(tt.b, tt.a))
		})
	}
}
