package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestNumberExpressionsEverywhere(t *testing.T) {
	source := `2000-01-01 open Assets:A
  mexpr: 2 * 21
2000-01-01 open Equity:E
2000-01-02 price HOOL 10 + 2 USD
2000-01-03 balance Assets:A 50 + 50 USD
2000-01-04 custom "expr" 3 * 7
2000-01-05 * "expressions"
  Assets:A  2 * 3.50 HOOL {100 + 2 # 8 / 2 USD}
  Equity:E  -(7 * 102 + 4) USD
`
	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	open := tree.Directives[0].(*ast.Open)
	assert.Equal(t, "42", *open.Metadata[0].Value.Number)
	price := tree.Directives[2].(*ast.Price)
	assert.Equal(t, "12", price.Amount.Value)
	balance := tree.Directives[3].(*ast.Balance)
	assert.Equal(t, "100", balance.Amount.Value)
	custom := tree.Directives[4].(*ast.Custom)
	assert.Equal(t, "21", *custom.Values[0].Number)
	txn := tree.Directives[5].(*ast.Transaction)
	assert.Equal(t, "7", txn.Postings[0].Amount.Value)
	assert.Equal(t, "102", txn.Postings[0].Cost.Amount.Value)
	assert.Equal(t, "4", txn.Postings[0].Cost.Total.Value)
	assert.Equal(t, "-718", txn.Postings[1].Amount.Value)
	assert.Equal(t, "2 * 3.50", txn.Postings[0].Amount.Raw)
}

func TestNumberExpressionErrors(t *testing.T) {
	for _, source := range []string{
		"2000-01-01 balance Assets:A 10 / 0 USD\n",
		"2000-01-01 balance Assets:A (10 + 2 USD\n",
		"2000-01-01 balance Assets:A 10 + USD\n",
	} {
		_, err := ParseString(context.Background(), source)
		assert.Error(t, err)
	}
}
