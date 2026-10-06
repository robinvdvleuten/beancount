package parser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// TestSyntaxErrorRecovery pins beancount's recovery: a syntax error drops
// the directive it is in, indented lines included, and parsing resumes at
// the next line that starts in column 1. An invalid token starting the line
// after a directive drops that directive too ("fine").
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
	// Like beancount's lexer, each invalid word of line 7 is an error.
	assert.Equal(t, []int{3, 7, 7, 7, 7, 8, 13}, lines)

	var kept []string
	for _, d := range tree.Directives {
		kept = append(kept, d.Date().String()+" "+string(d.Kind()))
	}
	assert.Equal(t, []string{
		"2020-01-01 open",
		"2020-01-04 balance",
		"2020-01-05 transaction",
	}, kept)
	assert.Equal(t, 1, len(tree.Directives[2].(*ast.Transaction).Postings))
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
		"posting after a string spanning lines":  {"2020-01-02 * \"x\ny\" Assets:A  1 USD\n  Assets:A", 2},
		// Like beancount's, our lexer reads a lone \r as whitespace, so the
		// string and the posting after it are on the header's line.
		"posting after a string holding a \\r": {"2020-01-02 * \"x\ry\" Assets:A  1 USD\n  Assets:A", 1},
		"metadata on a posting's line":         {"2020-01-02 *\n  Assets:A  1 USD  kk: 1\n  Assets:B", 2},
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

	// So does the rest of a header after a string holding a lone \r, which
	// beancount reads as whitespace inside the string.
	for _, source := range []string{
		"2020-01-02 * \"a\rb\" #t\n  Assets:A  1 USD\n  Assets:A  -1 USD\n",
		"2020-01-02 * \"p\rq\" \"n\"\n  Assets:A  1 USD\n  Assets:A  -1 USD\n",
		"2020-01-02 note Assets:A \"a\rb\" #t\n",
	} {
		_, err := ParseString(context.Background(), source)
		assert.NoError(t, err, "%q", source)
	}
}

// TestUndatedLineEndsAtItsLine pins beancount's grammar, where option,
// plugin, include, pushtag, poptag, pushmeta and popmeta end at their
// line's end: arguments continued on the next line are a syntax error there.
func TestUndatedLineEndsAtItsLine(t *testing.T) {
	for name, source := range map[string]string{
		"option":             "option \"title\"\n  \"x\"",
		"option in column 1": "option \"title\"\n\"x\"",
		"option name":        "option\n\"title\" \"x\"",
		"plugin":             "plugin\n  \"beancount.plugins.auto_accounts\"",
		"include":            "include\n  \"x.beancount\"",
		"pushtag":            "pushtag\n  #foo",
		"poptag":             "poptag\n  #foo",
		"pushmeta":           "pushmeta\n  kk: 1",
		"popmeta":            "popmeta\n  kk:",
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), source+"\n2020-01-05 open Assets:Z\n")

			var syntaxErrs ParseErrors
			assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
			assert.Equal(t, 1, len(syntaxErrs), "got %v", syntaxErrs)
			assert.Equal(t, 2, syntaxErrs[0].Pos.Line)
			assert.Equal(t, 1, len(tree.Directives))
			assert.Equal(t, 0, len(tree.Options)+len(tree.Plugins)+len(tree.Includes)+len(tree.Pushtags)+len(tree.Poptags)+len(tree.Pushmetas)+len(tree.Popmetas))
		})
	}
}

// TestDateAloneAfterAnErrorIsReported pins that a date alone on the last
// line fails after the date, which the grammar has read by then: with the
// two line breaks before it that is three tokens since the error on line
// 1, so beancount reports it.
func TestDateAloneAfterAnErrorIsReported(t *testing.T) {
	_, err := ParseString(context.Background(), "  Assets:Cash  1 USD\n\n2014-01-01\n")

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	assert.Equal(t, 2, len(syntaxErrs), "got %v", syntaxErrs)
	assert.Equal(t, 1, syntaxErrs[0].Pos.Line)
	assert.Equal(t, 3, syntaxErrs[1].Pos.Line)
}

// TestPluginEndsAtItsLine pins that a plugin's line is complete without a
// configuration: a string on the next line, indented or not, is a syntax
// error of its own and the plugin is kept, as bean-check still runs it.
func TestPluginEndsAtItsLine(t *testing.T) {
	for name, source := range map[string]string{
		"indented":    "plugin \"beancount.plugins.auto_accounts\"\n  \"cfg\"\n",
		"in column 1": "plugin \"beancount.plugins.auto_accounts\"\n\"cfg\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), source+"2020-01-05 open Assets:Z\n")

			var syntaxErrs ParseErrors
			assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
			assert.Equal(t, 1, len(syntaxErrs), "got %v", syntaxErrs)
			assert.Equal(t, 2, syntaxErrs[0].Pos.Line)
			assert.Equal(t, 1, len(tree.Plugins))
			assert.True(t, tree.Plugins[0].Config.IsEmpty())
			assert.Equal(t, 1, len(tree.Directives))
		})
	}
}

// TestInvalidLineDropsTheDirectiveItContinues pins beancount's recovery from
// an invalid indented line: it is a syntax error inside the directive it
// continues, comments between them included, which is dropped whole. After
// a blank line it continues nothing.
func TestInvalidLineDropsTheDirectiveItContinues(t *testing.T) {
	source := "2020-01-01 open Assets:A\n\n  garbage\n2020-01-01 open Assets:B\n  ; c\n  garbage\n" +
		"2020-01-02 * \"x\"\n  Assets:A  1 USD\n  garbage\n  Assets:B  -1 USD\n2020-01-03 open Assets:C\n"
	tree, err := ParseString(context.Background(), source)
	assert.Error(t, err)

	var kept []string
	for _, d := range tree.Directives {
		kept = append(kept, d.Date().String()+" "+string(d.Kind()))
	}
	assert.Equal(t, []string{"2020-01-01 open", "2020-01-03 open"}, kept)
}

// TestInvalidAccountNameKeepsTheDirective pins beancount's builder, which
// reports an account its lexer reads but its account pattern rejects and
// keeps the directive.
func TestInvalidAccountNameKeepsTheDirective(t *testing.T) {
	tree, err := ParseString(context.Background(), "2020-01-02 * \"x\"\n  Assets:A  1 USD\n  Assets:\U0001F600x  -1 USD\n")

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	assert.Equal(t, 1, len(syntaxErrs))
	assert.Equal(t, 3, syntaxErrs[0].Pos.Line)
	assert.Equal(t, 1, len(tree.Directives))
	assert.Equal(t, ast.Account("Assets:\U0001F600x"), tree.Directives[0].(*ast.Transaction).Postings[1].Account)
}

// TestSkippedInvalidTokensAreReported pins beancount's lexer, which reports
// every invalid token whatever the grammar does with it: each invalid token
// past a directive's first syntax error is an error too, a one-letter key
// (a:) among them, while what the grammar alone rejects (a lone sign, an
// unmatched parenthesis) and valid lines in the dropped directive are not.
func TestSkippedInvalidTokensAreReported(t *testing.T) {
	source := `2020-01-02 * "x"
  a: "x"
  Assets:A  1 USD
    ok: "valid"
    b: "y"
  Assets:A  -1 USD -
  Assets:A  (1 USD
  garbage garbage2
2020-01-03 * "y"
  Assets:A  1 USD
  Assets:A  -1 USD
`
	tree, err := ParseString(context.Background(), source)

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	var got []string
	for _, e := range syntaxErrs {
		got = append(got, fmt.Sprintf("%d:%d %s", e.Pos.Line, e.Pos.Column, e.Msg))
	}
	assert.Equal(t, []string{
		`2:3 invalid token "a:"`,
		`5:5 invalid token "b:"`,
		`8:3 invalid token "garbage"`,
		`8:11 invalid token "garbage2"`,
	}, got)
	assert.Equal(t, 1, len(tree.Directives))
}

// TestSkippedInvalidAccountNamesAreReported pins beancount's lexer, which
// rejects an account-like word whose component is empty or starts or goes
// on with an ASCII character its account pattern does not take, even in a
// directive the grammar drops. A component that starts with a non-ASCII
// character passes its lexer, and a trailing colon is a token of its own,
// so the dropped directive reports neither.
func TestSkippedInvalidAccountNamesAreReported(t *testing.T) {
	source := "2020-01-02 * \"x\"\n  garbage\n" +
		"  Assets:A  1 USD Assets:x\n  Assets:A  1 USD Assets:-X\n  Assets:A  1 USD Assets::X\n" +
		"  Assets:A  1 USD Assets:X_y\n  Assets:A  1 USD Assets:X.y\n  Assets:A  1 USD Assets:X'y\n" +
		"  Assets:A  1 USD Assets:1x\n  Assets:A  1 USD Assets:\u00e9x\n  Assets:A  1 USD Assets:X:\n"
	_, err := ParseString(context.Background(), source)

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	var got []string
	for _, e := range syntaxErrs {
		got = append(got, fmt.Sprintf("%d:%d %s", e.Pos.Line, e.Pos.Column, e.Msg))
	}
	assert.Equal(t, []string{
		`2:3 invalid token "garbage"`,
		`3:19 invalid token "Assets:x"`,
		`4:19 invalid token "Assets:-X"`,
		`5:19 invalid token "Assets::X"`,
		// The account ends where its pattern does; the rest is invalid.
		`6:27 invalid token "_y"`,
		`7:27 invalid token ".y"`,
		`8:27 invalid token "'y"`,
	}, got)
}

// TestInvalidBytesAreRecovered pins beancount's lexer on bytes that are not
// UTF-8 or are control characters: a comment may hold them, a string
// holding invalid UTF-8 is an error on the line it ends on, and a word
// holding either is an invalid token. Each drops its directive, and parsing
// resumes as after any syntax error.
func TestInvalidBytesAreRecovered(t *testing.T) {
	source := "2020-01-01 open Assets:A ; caf\xe9\x01\n" +
		"2020-01-02 * \"a\" \"x\xe9y\n\nz\"\n" +
		"  Assets:A  1 USD\n" +
		"2020-01-03 open Assets:\xe9B\n" +
		"2020-01-04 open Assets:B \x01\n" +
		"2020-01-05 open Assets:C\n" +
		"  key: \"caf\xe9\"\n" +
		"2020-01-06 note Assets:A \"ok\x01\"\n"
	tree, err := ParseString(context.Background(), source)

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	var got []string
	for _, e := range syntaxErrs {
		got = append(got, fmt.Sprintf("%d:%d %s", e.Pos.Line, e.Pos.Column, e.Message()))
	}
	assert.Equal(t, []string{
		"4:3 string is not valid UTF-8",
		"6:17 invalid token \"Assets:\\xe9B\"",
		"7:26 invalid token \"\\x01\"",
		"9:8 string is not valid UTF-8",
	}, got)

	var kept []string
	for _, d := range tree.Directives {
		kept = append(kept, d.Date().String()+" "+string(d.Kind()))
	}
	assert.Equal(t, []string{"2020-01-01 open", "2020-01-06 note"}, kept)
}

// TestSyntaxErrorsCloseTogetherAreReportedOnce pins the error recovery of
// beancount's Bison parser: a grammar error is reported only once three
// tokens (a line break and an indent each counting as one, a comment as
// none) have been shifted since the last error, reported or not, while an
// invalid token is its lexer's error and always reported.
func TestSyntaxErrorsCloseTogetherAreReportedOnce(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		lines  []int
	}{
		{"next line", "\"a\"\n\"b\"\n", []int{1}},
		{"after an invalid token", "2020-01-04 * \"y\" ^\n\"a\"\n", []int{1}},
		{"mid-line", "2020-01-02 open Assets:B USD USD USD\n\"a\"\n", []int{1}},
		{"a blank line", "\"a\"\n\n\"b\"\n", []int{1}},
		{"two blank lines", "\"a\"\n\n\n\"b\"\n", []int{1, 4}},
		{"a comment line", "\"a\"\n; c\n\"b\"\n", []int{1}},
		{"a valid directive", "\"a\"\n2020-01-01 open Assets:A\n\"b\"\n", []int{1, 3}},
		{"two tokens in", "\"a\"\n2020-01-01 \"b\"\n", []int{1}},
		{"three tokens in", "\"a\"\n2020-01-01 open \"b\"\n", []int{1, 2}},
		{"an indented line", "\"a\"\n  Assets:A\n\n\n\"b\"\n", []int{1, 5}},
		{"invalid token in the window", "\"a\"\n\"b\" ^\n", []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseString(context.Background(), tc.source)
			var syntaxErrs ParseErrors
			assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
			var lines []int
			for _, e := range syntaxErrs {
				lines = append(lines, e.Pos.Line)
			}
			assert.Equal(t, tc.lines, lines)
		})
	}
}

// TestRejectedWordAtLineStartIsInvalidToken pins the message for a word in
// column 1 that beancount's lexer rejects (`Invalid token: 'T:'`), while a
// valid account there is the grammar's error.
func TestRejectedWordAtLineStartIsInvalidToken(t *testing.T) {
	source := "T:\n\n\nTx:y\n\n\nAssets:Cash\n"
	_, err := ParseString(context.Background(), source)

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	var got []string
	for _, e := range syntaxErrs {
		got = append(got, fmt.Sprintf("%d:%d %s", e.Pos.Line, e.Pos.Column, e.Message()))
	}
	assert.Equal(t, []string{
		`1:1 invalid token "T:"`,
		`4:1 invalid token "Tx:y"`,
		`7:1 unexpected token ACCOUNT "Assets:Cash"`,
	}, got)
}

// TestAccountRootsCheckedWhereWritten pins beancount's grammar, which checks
// an account's root on every line that writes it, against the five account
// names the options read so far set: the error keeps its directive.
func TestAccountRootsCheckedWhereWritten(t *testing.T) {
	source := `2020-01-01 open Equity:Opening
2020-01-01 open Capital:Later
option "name_equity" "Capital"
2020-01-02 open Equity:After
2020-01-02 open Capital:After
2020-01-03 * "uses"
  ref: Equity:Opening
  Capital:After   1 USD
  Equity:Opening -1 USD
2020-01-04 custom "budget" Equity:Opening
`
	tree, err := ParseString(context.Background(), source)

	var syntaxErrs ParseErrors
	assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
	var lines []int
	for _, e := range syntaxErrs {
		assert.True(t, e.Kept, "%v", e)
		lines = append(lines, e.Pos.Line)
	}
	assert.Equal(t, []int{2, 4, 7, 9, 10}, lines)
	assert.Equal(t, 6, len(tree.Directives))
}

// TestMetadataInColumnOneIsASyntaxError pins beancount's grammar, which reads
// a metadata line only after an INDENT: a key in column 1 ends the directive
// before it, which is kept without it, and is a syntax error of its own.
func TestMetadataInColumnOneIsASyntaxError(t *testing.T) {
	for name, source := range map[string]string{
		"after a header":              "2020-01-02 note Assets:A \"x\"\nwho: \"me\"\n",
		"after indented metadata":     "2020-01-02 note Assets:A \"x\"\n  first: \"a\"\nwho: \"me\"\n",
		"after a posting":             "2020-01-02 *\n  Assets:A  1 USD\nwho: \"me\"\n",
		"after a posting's metadata":  "2020-01-02 *\n  Assets:A  1 USD\n    first: \"a\"\nwho: \"me\"\n",
		"after a transaction's lines": "2020-01-02 *\n  first: \"a\"\nwho: \"me\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), source)

			var syntaxErrs ParseErrors
			assert.True(t, errors.As(err, &syntaxErrs), "got %v", err)
			assert.Equal(t, 1, len(syntaxErrs))
			assert.Equal(t, strings.Count(source, "\n"), syntaxErrs[0].Pos.Line)

			assert.Equal(t, 1, len(tree.Directives))
			for _, md := range tree.Directives[0].GetMetadata() {
				assert.NotEqual(t, "who", md.Key)
			}
			if txn, ok := tree.Directives[0].(*ast.Transaction); ok {
				for _, posting := range txn.Postings {
					for _, md := range posting.Metadata {
						assert.NotEqual(t, "who", md.Key)
					}
				}
			}
		})
	}
}
