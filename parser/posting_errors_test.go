package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// TestPostingGrammarErrors pins the errors beancount's grammar raises as it
// reads a posting, in its words: they keep their transaction, and, raised
// as the cost spec or the posting's line is read, stay reported when a
// later syntax error drops it.
func TestPostingGrammarErrors(t *testing.T) {
	source := `2020-01-01 * "kept"
  Assets:A  1 HOOL {*, "a", "b", 2020-01-01, 2020-01-02, *}
  Assets:A  1 HOOL {1 USD, 2 USD}
  Assets:A  1 HOOL {{2 # 3 EUR}}
  Assets:A  4 HOOL @@ -10 USD
  Assets:A  HOOL @@ 5 USD
  Assets:A  1 HOOL {2 EUR} @ 3 USD
  Assets:B
2020-01-05 * "negative total price, then a syntax error"
  Assets:A      4 HOOL @@ -10 USD
  Assets:Cash   10 USD #tag
2020-01-06 * "merge cost, a syntax error on its line"
  Assets:A  1 HOOL {*} #tag
2020-01-07 * "negative price, a syntax error on its line"
  Assets:A  1 HOOL @ -2 USD #tag
`
	tree, err := ParseString(context.Background(), source)

	var errs ParseErrors
	assert.True(t, errors.As(err, &errs), "got %v", err)
	var got []string
	for _, e := range errs {
		got = append(got, fmt.Sprintf("%d %t %s", e.Pos.Line, e.Kept, e.Msg))
	}
	assert.Equal(t, []string{
		"2 true Cost merging is not supported yet",
		"2 true Duplicate label: 'b'.",
		"2 true Duplicate date: '2020-01-02'.",
		"2 true Duplicate merge-cost spec",
		"3 true Duplicate cost: '2 USD'.",
		"4 true Per-unit cost may not be specified using total cost syntax: '2 # 3 EUR'; ignoring per-unit cost",
		"5 true Negative prices are not allowed: -10 USD",
		"6 true Total price on a posting without units: 5 USD",
		"7 true Cost and price currencies must match: EUR != USD",
		"10 true Negative prices are not allowed: -10 USD",
		`11 false unexpected token TAG "#tag"`,
		"13 true Cost merging is not supported yet",
		`13 false unexpected token TAG "#tag"`,
		`15 false unexpected token TAG "#tag"`,
	}, got)
	assert.Equal(t, 1, len(tree.Directives))
}
