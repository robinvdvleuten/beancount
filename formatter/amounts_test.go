package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFormatLeavesIncompleteAmountsAsWritten(t *testing.T) {
	// bean-format aligns only a number followed by a currency. A posting
	// missing either keeps its line as written, apart from the indent, and
	// does not widen the number column.
	source := "2020-01-02 * \"x\"\n" +
		"  Expenses:B  1.00 USD\n" +
		"    Assets:A   -1.00\n" +
		"  Assets:A          USD\n" +
		"  Assets:Stock  HOOL {10 USD}\n"
	want := "2020-01-02 * \"x\"\n" +
		"  Expenses:B  1.00 USD\n" +
		"  Assets:A   -1.00\n" +
		"  Assets:A          USD\n" +
		"  Assets:Stock  HOOL {10 USD}\n"

	tree := parser.MustParseBytes(context.Background(), []byte(source))
	var out bytes.Buffer
	assert.NoError(t, New().Format(context.Background(), tree, []byte(source), &out))
	assert.Equal(t, want, out.String())
}

func TestFormatIncompleteAmountWithoutSource(t *testing.T) {
	// Without source text the parts present are joined by single spaces,
	// with no trailing space for a missing currency.
	source := "2020-01-02 * \"x\"\n  Expenses:B  1.00 USD\n  Assets:A  -1.00\n  Assets:C  USD\n"
	tree := parser.MustParseBytes(context.Background(), []byte(source))
	var out bytes.Buffer
	assert.NoError(t, New().Format(context.Background(), tree, nil, &out))
	assert.Contains(t, out.String(), "  Assets:A  -1.00\n  Assets:C  USD\n")
}
