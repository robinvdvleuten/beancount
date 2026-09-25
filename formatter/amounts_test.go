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

func TestFormatAlignsOnlyPlainlySpelledNumbers(t *testing.T) {
	// bean-format's line pattern aligns a number spelled as an optional
	// sign, digits and commas; expressions and repeated signs stay as
	// written, and a flagged posting's line is not even re-indented.
	source := "2020-01-02 * \"x\"\n" +
		"  Assets:A  2 * 3.50 USD\n" +
		"  Assets:A  --1 USD\n" +
		"    ! Assets:A  1.00 USD\n" +
		"  Assets:A  - 5 USD\n" +
		"  Assets:A  1,000.00 USD\n"
	want := "2020-01-02 * \"x\"\n" +
		"  Assets:A  2 * 3.50 USD\n" +
		"  Assets:A  --1 USD\n" +
		"    ! Assets:A  1.00 USD\n" +
		"  Assets:A       - 5 USD\n" +
		"  Assets:A  1,000.00 USD\n"

	tree := parser.MustParseBytes(context.Background(), []byte(source))
	var out bytes.Buffer
	assert.NoError(t, New().Format(context.Background(), tree, []byte(source), &out))
	assert.Equal(t, want, out.String())
}

func TestDatedAmountLayout(t *testing.T) {
	// The aligned number is the shortest-prefix suffix spelled as a number,
	// as in bean-format's lazy line pattern.
	for text, want := range map[string][2]string{
		"6":             {"H", "6"},
		"- 5":           {"H", "- 5"},
		"100.00 ~ 0.05": {"H 100.00 ~", "0.05"},
		"50 + 50":       {"H 50", "+ 50"},
		"2 * 3":         {"H 2 *", "3"},
		"0*  0":         {"H 0*", "0"},
	} {
		prefix, number, ok := datedAmountLayout("H", text)
		assert.True(t, ok, text)
		assert.Equal(t, want, [2]string{prefix, number}, text)
	}
	_, _, ok := datedAmountLayout("H", "(2 * 3)")
	assert.False(t, ok)
}

func TestFormatLeavesNumbersGluedToCurrenciesAsWritten(t *testing.T) {
	// bean-format's pattern needs whitespace between number and currency,
	// so "1USD" lines pass through and do not widen the columns; neither
	// does a dated line without a plainly spelled number.
	source := "2020-01-02 *\n" +
		"  Assets:A 1USD\n" +
		"  Assets:Longer  -1 USD\n" +
		"2020-01-03 balance Assets:A 1USD\n" +
		"2020-01-04 price HOOL (2 * 3)  EUR\n"

	tree := parser.MustParseBytes(context.Background(), []byte(source))
	var out bytes.Buffer
	assert.NoError(t, New().Format(context.Background(), tree, []byte(source), &out))
	assert.Equal(t, source, out.String())
}

func TestFormatWithParsedNumbers(t *testing.T) {
	// Like beancount's printer, parsed numbers drop the source's thousands
	// separators and plus sign; by default they are kept as written.
	source := "2020-01-02 * \"x\"\n" +
		"  Assets:A  +1,000.50 USD @ 1,100 EUR\n" +
		"  Assets:B\n" +
		"2020-01-03 balance Assets:A  1,000.50 USD\n"
	tree := parser.MustParseBytes(context.Background(), []byte(source))

	var parsed bytes.Buffer
	assert.NoError(t, New(WithParsedNumbers()).Format(context.Background(), tree, nil, &parsed))
	assert.Contains(t, parsed.String(), "1000.50 USD @ 1100 EUR")
	assert.Contains(t, parsed.String(), "balance Assets:A  1000.50 USD")
	assert.NotContains(t, parsed.String(), ",")

	var spelled bytes.Buffer
	assert.NoError(t, New().Format(context.Background(), tree, nil, &spelled))
	assert.Contains(t, spelled.String(), "+1,000.50 USD @ 1,100 EUR")
}
