package parser

import (
	"context"
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// TestSyntaxErrorRecovery pins beancount's recovery: a syntax error drops
// the directive it is in, indented lines included, and parsing resumes at
// the next line that starts in column 1.
func TestSyntaxErrorRecovery(t *testing.T) {
	source := `2020-01-01 open Assets:A
2020-01-02 * "typo in a posting"
  Assets:A  1 USDD!
  Assets:B  -1 USD
2020-01-03 * "fine"
  Assets:A  1 USD
this is not beancount
option "title"
2020-01-04 balance Assets:A 1 USD
2020-01-05 * "split by an org line"
  Assets:A  1 USD
:PROPERTIES:
  Assets:B  -1 USD
`
	tree, err := ParseString(context.Background(), source)

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	var lines []int
	for _, e := range syntaxErrs {
		lines = append(lines, e.Pos.Line)
	}
	assert.Equal(t, []int{3, 7, 8, 13}, lines)

	var kept []string
	for _, d := range tree.Directives {
		kept = append(kept, d.Date().String()+" "+string(d.Kind()))
	}
	assert.Equal(t, []string{
		"2020-01-01 open",
		"2020-01-03 transaction",
		"2020-01-04 balance",
		"2020-01-05 transaction",
	}, kept)
	assert.Equal(t, 1, len(tree.Directives[3].(*ast.Transaction).Postings))
}
