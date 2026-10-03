package formatter

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func parseDirective(t *testing.T, source string) ast.Directive {
	t.Helper()
	tree, err := parser.ParseString(context.Background(), source)
	assert.NoError(t, err)
	return tree.Directives[0]
}

func TestLinePattern(t *testing.T) {
	posting := func(line string) (string, bool, *ast.Posting) {
		txn := parseDirective(t, "2020-01-01 *\n"+line+"\n").(*ast.Transaction)
		return line, true, txn.Postings[0]
	}
	postingLine := func(line string) func() lineLayout {
		return func() lineLayout {
			line, owned, p := posting(line)
			return postingLayout(line, owned, p, 4)
		}
	}
	balanceLine := func(line string) func() lineLayout {
		return func() lineLayout {
			b := parseDirective(t, line+"\n").(*ast.Balance)
			text := numberText(b.Amount)
			last := numberText(b.Amount)
			if b.Tolerance != nil {
				text += " ~ " + numberText(b.Tolerance)
				last = numberText(b.Tolerance)
			}
			return datedLayout(line, true, "2020-01-01 balance "+string(b.Account), text, last, balanceCurrency(b))
		}
	}

	tests := []struct {
		name   string
		layout func() lineLayout
		want   lineLayout
	}{
		{
			name:   "aligned posting",
			layout: postingLine("  Assets:Cash   10.00 USD {5 EUR} ; c"),
			want: lineLayout{
				kind: alignLine, prefix: "    Assets:Cash", number: "10.00", currency: "USD", rest: "USD {5 EUR} ; c",
				prefixWidth: 13, numberWidth: 5,
			},
		},
		{
			name: "posting whose line is not its own",
			layout: func() lineLayout {
				line, _, p := posting("  Assets:Cash  10.00 USD")
				return postingLayout(line, false, p, 4)
			},
			want: lineLayout{kind: unownedLine},
		},
		{
			name:   "number glued to its currency",
			layout: postingLine("  Assets:Cash  10USD"),
			want:   lineLayout{kind: copyLine, text: "    Assets:Cash  10USD"},
		},
		{
			name:   "flagged posting",
			layout: postingLine("  ! Assets:Cash  10 USD"),
			want:   lineLayout{kind: copyLine, text: "  ! Assets:Cash  10 USD"},
		},
		{
			name:   "parenthesised binary expression",
			layout: postingLine("  Assets:Cash  (5 + 5) USD"),
			want: lineLayout{
				kind: alignLine, prefix: "    Assets:Cash", number: "(5 + 5)", currency: "USD", rest: "USD",
				prefixWidth: 13, numberWidth: 7,
			},
		},
		{
			name:   "longer expression",
			layout: postingLine("  Assets:Cash  (5 + 5 + 1) USD"),
			want:   lineLayout{kind: copyLine, text: "    Assets:Cash  (5 + 5 + 1) USD"},
		},
		{
			name:   "posting without an amount",
			layout: postingLine("  Assets:Cash   ; c"),
			want:   lineLayout{kind: accountLine, prefix: "    Assets:Cash"},
		},
		{
			name:   "balance",
			layout: balanceLine("2020-01-01 balance Assets:Cash  100.00 USD"),
			want: lineLayout{
				kind: alignLine, prefix: "2020-01-01 balance Assets:Cash", number: "100.00", currency: "USD",
				prefixWidth: 30, numberWidth: 6,
			},
		},
		{
			name:   "balance tolerance",
			layout: balanceLine("2020-01-01 balance Assets:Cash  100.00 ~ 0.05 USD"),
			want: lineLayout{
				kind: alignLine, prefix: "2020-01-01 balance Assets:Cash 100.00 ~", number: "0.05", currency: "USD",
				prefixWidth: 39, numberWidth: 4,
			},
		},
		{
			name:   "balance glued to its currency",
			layout: balanceLine("2020-01-01 balance Assets:Cash 100.00USD"),
			want:   lineLayout{kind: copyLine, text: "2020-01-01 balance Assets:Cash 100.00USD"},
		},
		{
			name:   "metadata line",
			layout: func() lineLayout { return plainLayout("    key:   1", true) },
			want:   lineLayout{kind: copyLine, text: "    key:   1"},
		},
		{
			name:   "comment line",
			layout: func() lineLayout { return plainLayout("  ; a comment  ", true) },
			want:   lineLayout{kind: copyLine, text: "  ; a comment  "},
		},
		{
			name:   "line the item does not own",
			layout: func() lineLayout { return plainLayout("2020-01-01 open Assets:A", false) },
			want:   lineLayout{kind: unownedLine},
		},
		{
			name: "balance whose line is not its own",
			layout: func() lineLayout {
				return datedLayout("", false, "2020-01-01 balance Assets:Cash", "100.00", "100.00", "USD")
			},
			want: lineLayout{kind: unownedLine},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.layout())
		})
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
