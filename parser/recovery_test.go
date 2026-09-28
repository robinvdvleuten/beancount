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

// TestDirectiveHeaderEndsAtItsLine pins beancount's grammar, where a dated
// directive's header ends at its line's end: a header whose tokens continue
// on the next line, indented or not, and metadata on the header's own line
// are syntax errors, on the first token past the header, and the directive
// is dropped.
func TestDirectiveHeaderEndsAtItsLine(t *testing.T) {
	for name, tt := range map[string]struct {
		header string
		line   int
	}{
		"open":                                   {"2020-01-02 open\n  Assets:C", 2},
		"open in column 1":                       {"2020-01-02 open\nAssets:C", 2},
		"commodity":                              {"2020-01-02 commodity\n  EUR", 2},
		"close":                                  {"2020-01-02 close\n  Assets:A", 2},
		"pad":                                    {"2020-01-02 pad Assets:A\n  Assets:B", 2},
		"note":                                   {"2020-01-02 note Assets:A\n  \"x\"", 2},
		"document":                               {"2020-01-02 document Assets:A\n  \"/tmp/x\"", 2},
		"event":                                  {"2020-01-02 event \"a\"\n  \"b\"", 2},
		"query":                                  {"2020-01-02 query \"a\"\n  \"select 1\"", 2},
		"custom":                                 {"2020-01-02 custom \"a\"\n  \"b\"", 2},
		"balance amount":                         {"2020-01-02 balance Assets:A\n  0 USD", 2},
		"balance currency":                       {"2020-01-02 balance Assets:A 0\n  USD", 2},
		"price":                                  {"2020-01-02 price USD\n  1 EUR", 2},
		"price currency":                         {"2020-01-02 price USD 1\n  EUR", 2},
		"note metadata":                          {"2020-01-02 note Assets:A \"x\"  kk: 1", 1},
		"balance metadata":                       {"2020-01-02 balance Assets:A 0 USD  kk: 1", 1},
		"metadata after a string spanning lines": {"2020-01-02 note Assets:A \"x\ny\"  kk: 1", 2},
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), tt.header+"\n2020-01-05 open Assets:Z\n")

			var syntaxErrs ParseErrors
			assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
			assert.Equal(t, 1, len(syntaxErrs), "got %v", syntaxErrs)
			assert.Equal(t, tt.line, syntaxErrs[0].Pos.Line)
			assert.Equal(t, 1, len(tree.Directives))
		})
	}

	// Metadata lines under the header and a string running over several
	// lines still parse.
	_, err := ParseString(context.Background(), "2020-01-02 open Assets:C\n  kk: 1\n2020-01-03 note Assets:C \"two\nlines\"\n  kk: 2\n")
	assert.NoError(t, err)
}
