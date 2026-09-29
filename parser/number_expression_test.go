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
	assert.Equal(t, "7.00", txn.Postings[0].Amount.Value)
	assert.Equal(t, "102", txn.Postings[0].Cost.Amount.Value)
	assert.Equal(t, "4", txn.Postings[0].Cost.Total.Value)
	assert.Equal(t, "-718", txn.Postings[1].Amount.Value)
	assert.Equal(t, "2 * 3.50", txn.Postings[0].Amount.Raw)
}

func TestNumberExpressionPrecision(t *testing.T) {
	// Expected values are Python decimal results, which beancount evaluates
	// amounts with: the stated precision decides tolerances and display.
	for expr, want := range map[string]string{
		"1 + 2.20":    "3.20",
		"2.20 - 1":    "1.20",
		"2 * 3.50":    "7.00",
		"-(0.10)":     "-0.10",
		"10 / 4":      "2.5",
		"10.00 / 2":   "5.00",
		"1.00 / 4":    "0.25",
		"7.5 / 2.5":   "3",
		"-3.30 / 1.1": "-3.0",
		"100 / 8":     "12.5",
	} {
		tree, err := ParseString(context.Background(), "2000-01-01 balance Assets:A "+expr+" USD\n")
		assert.NoError(t, err, expr)
		assert.Equal(t, want, tree.Directives[0].(*ast.Balance).Amount.Value, expr)
	}
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

func TestNumberTrailingDotAndSigns(t *testing.T) {
	// Values as beancount computes them.
	for expr, want := range map[string]string{
		"5.":     "5",
		"1,000.": "1000",
		"(5.)":   "5",
		"5. + 1": "6",
		"--1":    "1",
		"---1":   "-1",
		"+-1":    "-1",
		"- 1":    "-1",
	} {
		tree, err := ParseString(context.Background(), "2000-01-01 balance Assets:A "+expr+" USD\n")
		assert.NoError(t, err, expr)
		assert.Equal(t, want, tree.Directives[0].(*ast.Balance).Amount.Value, expr)
	}
	for _, expr := range []string{".5", "1.5.", "5.00."} {
		_, err := ParseString(context.Background(), "2000-01-01 balance Assets:A "+expr+" USD\n")
		assert.Error(t, err, expr)
	}
}

func TestSlashCurrencyEndsNumberExpression(t *testing.T) {
	// Like beancount v3's lexer, a slash that starts a currency is not a
	// division: the expression ends before it.
	for amount, want := range map[string][3]string{
		"10 /ESZ24":        {"10", "10", "/ESZ24"},
		"10/ESZ24":         {"10", "10", "/ESZ24"},
		"10 /6J":           {"10", "10", "/6J"},
		"-10 /ESZ24":       {"-10", "-10", "/ESZ24"},
		"2 / 4 /ESZ24":     {"2 / 4", "0.5", "/ESZ24"},
		"2/4/ESZ24":        {"2/4", "0.5", "/ESZ24"},
		"10 /2 /ESZ24":     {"10 /2", "5", "/ESZ24"},
		"2 * 3 /ESZ24":     {"2 * 3", "6", "/ESZ24"},
		"(1 + 2) /ESZ24":   {"(1 + 2)", "3", "/ESZ24"},
		"(1 + 2)/ESZ24":    {"(1 + 2)", "3", "/ESZ24"},
		"(10 / 2) /ESZ24":  {"(10 / 2)", "5", "/ESZ24"},
		"1 / 2 USD":        {"1 / 2", "0.5", "USD"},
		"1 /2 USD":         {"1 /2", "0.5", "USD"},
		"1/ 2 USD":         {"1/ 2", "0.5", "USD"},
		"1/2 USD":          {"1/2", "0.5", "USD"},
		"1 ~ 0.1 /ESZ24":   {"1", "1", "/ESZ24"},
		"1 / 2 ~ 1 /ESZ24": {"1 / 2", "0.5", "/ESZ24"},
	} {
		tree, err := ParseString(context.Background(), "2000-01-01 balance Assets:A "+amount+"\n")
		assert.NoError(t, err, amount)
		got := tree.Directives[0].(*ast.Balance).Amount
		assert.Equal(t, want, [3]string{got.Raw, got.Value, got.Currency}, amount)
	}

	for _, amount := range []string{"10 / /ESZ24", "(10 /ESZ24)", "10 / USD", "10 /63", "10 /"} {
		_, err := ParseString(context.Background(), "2000-01-01 balance Assets:A "+amount+"\n")
		assert.Error(t, err, amount)
	}
}

func TestSlashCurrencyEverywhere(t *testing.T) {
	source := `2000-01-01 open Assets:A /ESZ24,USD, /6J "FIFO"
  cur: /ESZ24
  amt: 10 /ESZ24
2000-01-01 commodity /ESZ24
2000-01-02 price /ESZ24 5000 USD
2000-01-02 price USD 0.0002 /ESZ24
2000-01-04 custom "x" 3 /ESZ24
2000-01-05 * "x"
  Assets:A  1 /ESZ24 {100 /6J} @ 110 /6J
  Assets:A  1 HOOL {10 # 5 /6J} @@ 15 /6J
  Assets:A  /ESZ24
`
	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	open := tree.Directives[0].(*ast.Open)
	assert.Equal(t, []string{"/ESZ24", "USD", "/6J"}, open.ConstraintCurrencies)
	assert.Equal(t, "FIFO", open.BookingMethod)
	assert.Equal(t, "/ESZ24", *open.Metadata[0].Value.Currency)
	assert.Equal(t, "/ESZ24", open.Metadata[1].Value.Amount.Currency)
	assert.Equal(t, "/ESZ24", tree.Directives[1].(*ast.Commodity).Currency)
	assert.Equal(t, "/ESZ24", tree.Directives[2].(*ast.Price).Commodity)
	assert.Equal(t, "/ESZ24", tree.Directives[3].(*ast.Price).Amount.Currency)
	assert.Equal(t, "/ESZ24", tree.Directives[4].(*ast.Custom).Values[0].Amount.Currency)
	txn := tree.Directives[5].(*ast.Transaction)
	assert.Equal(t, "/ESZ24", txn.Postings[0].Amount.Currency)
	assert.Equal(t, "/6J", txn.Postings[0].Cost.Amount.Currency)
	assert.Equal(t, "/6J", txn.Postings[0].Price.Currency)
	assert.Equal(t, "/6J", txn.Postings[1].Cost.Total.Currency)
	assert.Equal(t, "/6J", txn.Postings[1].Price.Currency)
	assert.Equal(t, "/ESZ24", txn.Postings[2].Amount.Currency)
}

func TestSlashCurrencyIsNoMetadataKey(t *testing.T) {
	for _, source := range []string{
		"2000-01-01 open Assets:A\n  /ESZ24: 1\n",
		"2000-01-05 * \"x\"\n  Assets:A  1 USD\n    /ESZ24: 1\n",
		"pushmeta /ESZ24: 1\n",
		"popmeta /ESZ24:\n",
	} {
		tree, err := ParseString(context.Background(), source)
		assert.Error(t, err, source)
		assert.Equal(t, 0, len(tree.Pushmetas)+len(tree.Popmetas), source)
	}
}

func TestPushmetaKey(t *testing.T) {
	// Like beancount's KEY token, the key of a pushmeta or popmeta is that
	// of a metadata line: two or more characters, a keyword included.
	tree, err := ParseString(context.Background(), "pushmeta open: 1\npopmeta open:\n")
	assert.NoError(t, err)
	assert.Equal(t, "open", tree.Pushmetas[0].Key)
	assert.Equal(t, "open", tree.Popmetas[0].Key)

	for _, source := range []string{"pushmeta k: 1\n", "popmeta k:\n", "pushmeta USD: 1\n", "pushmeta kk : 1\n"} {
		tree, err := ParseString(context.Background(), source)
		assert.Error(t, err, source)
		assert.Equal(t, 0, len(tree.Pushmetas)+len(tree.Popmetas), source)
	}
}
