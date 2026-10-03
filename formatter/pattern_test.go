package formatter

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestLinePattern(t *testing.T) {
	tests := []struct {
		name string
		line string
		want lineLayout
	}{
		{
			name: "aligned posting",
			line: "  Assets:Cash   10.00 USD {5 EUR} ; c  ",
			want: lineLayout{
				kind: alignLine, prefix: "    Assets:Cash", number: "10.00", rest: "USD {5 EUR} ; c  ",
				prefixWidth: 13, numberWidth: 5,
			},
		},
		{
			name: "posting indented with a tab",
			line: "\tAssets:Cash 1 USD",
			want: lineLayout{
				kind: alignLine, prefix: "    Assets:Cash", number: "1", rest: "USD",
				prefixWidth: 12, numberWidth: 1,
			},
		},
		{
			name: "number glued to its currency",
			line: "  Assets:Cash  10USD",
			want: lineLayout{kind: copyLine, text: "    Assets:Cash  10USD"},
		},
		{
			name: "flagged posting",
			line: "  ! Assets:Cash  10 USD",
			want: lineLayout{kind: copyLine, text: "  ! Assets:Cash  10 USD"},
		},
		{
			name: "parenthesised binary expression",
			line: "  Assets:Cash  (5 + 5) USD",
			want: lineLayout{
				kind: alignLine, prefix: "    Assets:Cash", number: "(5 + 5)", rest: "USD",
				prefixWidth: 13, numberWidth: 7,
			},
		},
		{
			name: "longer expression",
			line: "  Assets:Cash  (5 + 5 + 1) USD",
			want: lineLayout{kind: copyLine, text: "    Assets:Cash  (5 + 5 + 1) USD"},
		},
		{
			name: "posting without an amount",
			line: "  Assets:Cash {10 USD}   ; c  ",
			want: lineLayout{kind: copyLine, text: "    Assets:Cash {10 USD}   ; c  "},
		},
		{
			// ACCOUNT_RE wants an uppercase letter or a digit to start
			// each component.
			name: "account component starting with another letter",
			line: "  Assets:日本  100 JPY",
			want: lineLayout{kind: copyLine, text: "  Assets:日本  100 JPY"},
		},
		{
			name: "account whose later component is not one to bean-format",
			line: "  Assets:Cash:日本  100 JPY",
			want: lineLayout{kind: copyLine, text: "    Assets:Cash:日本  100 JPY"},
		},
		{
			name: "balance",
			line: "2020-01-01 balance Assets:Cash  100.00 USD   ",
			want: lineLayout{
				kind: alignLine, prefix: "2020-01-01 balance Assets:Cash", number: "100.00", rest: "USD   ",
				prefixWidth: 30, numberWidth: 6,
			},
		},
		{
			name: "balance tolerance",
			line: "2020-01-01 balance Assets:Cash  100.00  ~ 0.05 USD",
			want: lineLayout{
				kind: alignLine, prefix: "2020-01-01 balance Assets:Cash  100.00  ~", number: "0.05", rest: "USD",
				prefixWidth: 41, numberWidth: 4,
			},
		},
		{
			name: "balance glued to its currency",
			line: "2020-01-01 balance Assets:Cash 100.00USD",
			want: lineLayout{kind: copyLine, text: "2020-01-01 balance Assets:Cash 100.00USD"},
		},
		{
			name: "header",
			line: "2020-01-01 * \"10 USD\"",
			want: lineLayout{kind: copyLine, text: "2020-01-01 * \"10 USD\""},
		},
		{
			name: "string spanning lines",
			line: "2020-01-01 price HOOL  1 USD\n  more",
			want: lineLayout{
				kind: alignLine, prefix: "2020-01-01 price HOOL", number: "1", rest: "USD\n  more",
				prefixWidth: 21, numberWidth: 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, layout(tt.line, true, 4))
		})
	}

	t.Run("line the item does not own", func(t *testing.T) {
		assert.Equal(t, lineLayout{kind: unownedLine}, layout("2020-01-01 balance Assets:Cash  1 USD", false, 4))
		assert.Equal(t, lineLayout{kind: unownedLine}, plainLayout("2020-01-01 open Assets:A", false))
	})
	t.Run("metadata line", func(t *testing.T) {
		assert.Equal(t, lineLayout{kind: copyLine, text: "    key:   1"}, plainLayout("    key:   1", true))
	})
}

func TestDatedLinePrefix(t *testing.T) {
	// The aligned number is the shortest-prefix suffix spelled as a number,
	// as in bean-format's lazy line pattern.
	for text, want := range map[string][2]string{
		"6":             {"H", "6"},
		"- 5":           {"H", "- 5"},
		"100.00 ~ 0.05": {"H 100.00 ~", "0.05"},
		"50 + 50":       {"H 50", "+ 50"},
		"2 * 3":         {"H 2 *", "3"},
		"0*  0":         {"H 0*", "0"},
		"(2 * 3)":       {"H", "(2 * 3)"},
		"(-1-2)":        {"H", "(-1-2)"},
	} {
		line := layout("2020-01-01 price H "+text+" USD", true, 4)
		assert.Equal(t, alignLine, line.kind, text)
		assert.Equal(t, [2]string{"2020-01-01 price " + want[0], want[1]}, [2]string{line.prefix, line.number}, text)
	}
	// Only one operator between two numbers, in one pair of parentheses.
	for _, text := range []string{"((1 + 2) * 3)", "(1 + 2 + 3)", "( 1 * 2 )"} {
		assert.Equal(t, copyLine, layout("2020-01-01 price H "+text+" USD", true, 4).kind, text)
	}
}

func TestSourceViewItemLine(t *testing.T) {
	source := "2020-01-01 open Assets:A\n" +
		"  key: \"multi\n" +
		"line\"\n" +
		"2020-01-02 note Assets:A \"two\n" +
		"lines\"\n" +
		"; comment\n" +
		"2020-01-03 * \"x\"\n" +
		"  Assets:A  10 USD\n" +
		"  Assets:B\n" +
		"   "
	tree, err := parser.ParseString(context.Background(), source)
	assert.NoError(t, err)
	view := newSourceView([]byte(source), tree)

	tests := []struct {
		name         string
		line, column int
		owned        bool
		text         string
	}{
		{"directive on its own line", 1, 1, true, "2020-01-01 open Assets:A"},
		{"metadata running onto the next line", 2, 3, true, "  key: \"multi\nline\""},
		{"directive running onto the next line", 4, 1, true, "2020-01-02 note Assets:A \"two\nlines\""},
		{"comment", 6, 1, true, "; comment"},
		{"posting", 8, 3, true, "  Assets:A  10 USD"},
		{"last posting, before trailing whitespace", 9, 3, true, "  Assets:B"},
		{"column other than the line's start", 8, 5, false, "  Assets:A  10 USD"},
		{"past the end", 11, 1, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, owned := view.itemLine(tt.line, tt.column)
			assert.Equal(t, tt.owned, owned)
			assert.Equal(t, tt.text, text)
		})
	}
}
