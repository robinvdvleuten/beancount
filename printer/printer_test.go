package printer

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
)

// parse returns the directives of a Beancount source, as the parser leaves
// them (not booked).
func parse(t *testing.T, source string) []ast.Directive {
	t.Helper()
	return parser.MustParseString(context.Background(), source).Directives
}

// parseOne returns the single directive of a Beancount source.
func parseOne(t *testing.T, source string) ast.Directive {
	t.Helper()
	directives := parse(t, source)
	assert.Equal(t, 1, len(directives), "source holds one directive")
	return directives[0]
}

// The expected texts below are what bean-query 2.3.6's print (beancount's
// printer.print_entries) writes for the same directives.
func TestSprintDirectiveKinds(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "Commodity",
			source: "2020-01-01 commodity USD\n",
			want:   "2020-01-01 commodity USD\n",
		},
		{
			name:   "OpenPadsTheAccountOnlyBeforeCurrencies",
			source: "2020-01-01 open Assets:Cash\n",
			want:   "2020-01-01 open Assets:Cash\n",
		},
		{
			name:   "OpenWithCurrenciesAndBooking",
			source: "2020-01-01 open Assets:Cash USD, EUR \"FIFO\"\n",
			want:   "2020-01-01 open Assets:Cash                                     USD,EUR \"FIFO\"\n",
		},
		{
			name:   "OpenWithBookingOnly",
			source: "2020-01-01 open Assets:Stock \"NONE\"\n",
			want:   "2020-01-01 open Assets:Stock                                     \"NONE\"\n",
		},
		{
			name:   "OpenPadsByCharactersNotBytes",
			source: "2020-01-01 open Assets:Ünïcode GBP\n",
			want:   "2020-01-01 open Assets:Ünïcode                                  GBP\n",
		},
		{
			name:   "Close",
			source: "2020-01-09 close Equity:O\n",
			want:   "2020-01-09 close Equity:O\n",
		},
		{
			name:   "Balance",
			source: "2020-01-04 balance Assets:Cash  -1302 USD\n",
			want:   "2020-01-04 balance Assets:Cash                                     -1302 USD\n",
		},
		{
			name:   "BalanceWithTolerance",
			source: "2020-01-04 balance Assets:Cash  -1302.00 ~ 0.01 USD\n",
			want:   "2020-01-04 balance Assets:Cash                                     -1302.00 ~ 0.01 USD\n",
		},
		{
			name:   "Pad",
			source: "2020-01-07 pad Assets:Pound Equity:O\n",
			want:   "2020-01-07 pad Assets:Pound Equity:O\n",
		},
		{
			name:   "NoteIsNotEscaped",
			source: "2020-01-05 note Assets:Cash \"a \\\"b\\\" c\"\n",
			want:   "2020-01-05 note Assets:Cash \"a \"b\" c\"\n",
		},
		{
			name:   "DocumentSortsTagsAndLinksWithoutSpaces",
			source: "2020-01-06 document Assets:Cash \"/tmp/x.pdf\" #t2 #t1 ^l1\n",
			want:   "2020-01-06 document Assets:Cash \"/tmp/x.pdf\" #t1#t2^l1\n",
		},
		{
			name:   "Price",
			source: "2020-01-05 price HOOL 120.123456 USD\n",
			want:   "2020-01-05 price HOOL                           120.123456 USD\n",
		},
		{
			name:   "Event",
			source: "2020-01-07 event \"location\" \"Home\"\n",
			want:   "2020-01-07 event \"location\" \"Home\"\n",
		},
		{
			name:   "Query",
			source: "2020-01-07 query \"q\" \"select 1\"\n",
			want:   "2020-01-07 query \"q\" \"select 1\"\n",
		},
		{
			name:   "Custom",
			source: "2020-01-07 custom \"budget\" \"x\" 10 USD 2020-01-01 TRUE 42\n",
			want:   "2020-01-07 custom \"budget\" \"x\" 10 USD 2020-01-01 TRUE 42\n",
		},
		{
			name: "Transaction",
			source: "2020-01-02 * \"narration\" #zz #aa ^zlink ^alink\n" +
				"  ! Assets:Cash  1.000 USD\n" +
				"  Expenses:Food   -1,000.5 USD\n" +
				"  Equity:O   +999.500 USD\n" +
				"  Assets:Ünïcode  0 USD\n",
			want: "2020-01-02 * \"narration\" #aa #zz ^alink ^zlink\n" +
				"  ! Assets:Cash     1.000 USD\n" +
				"  Expenses:Food   -1000.5 USD\n" +
				"  Equity:O        999.500 USD\n" +
				"  Assets:Ünïcode        0 USD\n",
		},
		{
			name:   "TransactionWithoutStrings",
			source: "2020-01-02 txn\n  Assets:Cash  1 USD\n  Equity:O\n",
			want:   "2020-01-02 * \n  Assets:Cash  1 USD\n  Equity:O\n",
		},
		{
			name:   "TransactionWithPayeeOnly",
			source: "2020-01-02 * \"only payee\" \"\"\n  Assets:Cash  1 USD\n  Equity:O  -1 USD\n",
			want:   "2020-01-02 * \"only payee\" \"\"\n  Assets:Cash   1 USD\n  Equity:O     -1 USD\n",
		},
		{
			name:   "TransactionEscapesPayeeAndNarration",
			source: "2020-01-02 * \"a \\\"p\\\"\" \"b \\\\ n\"\n  Assets:Cash  1 USD\n  Equity:O\n",
			want:   "2020-01-02 * \"a \\\"p\\\"\" \"b \\\\ n\"\n  Assets:Cash  1 USD\n  Equity:O\n",
		},
		{
			name:   "TransactionMergesBodyTagsAndLinks",
			source: "2020-01-02 * #only #body\n  #body ^blink\n  Assets:Cash  1 USD\n  Equity:O\n",
			want:   "2020-01-02 * #body #only ^blink\n  Assets:Cash  1 USD\n  Equity:O\n",
		},
		{
			name: "UnbookedCostSpecs",
			source: "2020-01-02 * \"a\"\n" +
				"  Assets:Stock  -1 HOOL {{300 USD}}\n" +
				"  Assets:Stock  -1 HOOL {5 # 10 USD, 2020-01-01, \"lbl\"} @@ 3 USD\n" +
				"  Assets:Stock  -1 HOOL {USD}\n" +
				"  Assets:Stock  -1 HOOL {2020-01-01}\n" +
				"  Assets:Cash\n",
			want: "2020-01-02 * \"a\"\n" +
				"  Assets:Stock  -1 HOOL {0 # 300 USD}\n" +
				"  Assets:Stock  -1 HOOL {5 # 10 USD, 2020-01-01, \"lbl\"} @ 3 USD\n" +
				"  Assets:Stock  -1 HOOL {}\n" +
				"  Assets:Stock  -1 HOOL {2020-01-01}\n" +
				"  Assets:Cash\n",
		},
		{
			name:   "LeavesOutComments",
			source: "2020-01-02 * \"x\" ; header\n  ; body\n  Assets:Cash  10 USD ; posting\n  Equity:O\n",
			want:   "2020-01-02 * \"x\"\n  Assets:Cash  10 USD\n  Equity:O\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Sprint(parseOne(t, tt.source)))
		})
	}
}

func TestSprintMetadata(t *testing.T) {
	t.Run("OnADirective", func(t *testing.T) {
		// Like beancount, a string, account, currency or tag value is a
		// quoted string, and a missing value leaves "key: ".
		source := "2020-01-01 open Expenses:Food\n" +
			"  str: \"a \\\"q\\\" b\"\n" +
			"  acct: Assets:Cash\n" +
			"  cur: USD\n" +
			"  tg: #foo\n" +
			"  num: 12.50\n" +
			"  dt: 2020-01-02\n" +
			"  amt: 3.5 USD\n" +
			"  bool: TRUE\n" +
			"  none:\n"
		assert.Equal(t, "2020-01-01 open Expenses:Food\n"+
			"  str: \"a \\\"q\\\" b\"\n"+
			"  acct: \"Assets:Cash\"\n"+
			"  cur: \"USD\"\n"+
			"  tg: \"foo\"\n"+
			"  num: 12.50\n"+
			"  dt: 2020-01-02\n"+
			"  amt: 3.5 USD\n"+
			"  bool: TRUE\n"+
			"  none: \n", Sprint(parseOne(t, source)))
	})

	t.Run("OnATransactionAndItsPostings", func(t *testing.T) {
		// Posting metadata is indented four spaces, the transaction's two.
		source := "2020-01-02 * \"x\"\n" +
			"  key: \"v\"\n" +
			"  Assets:Cash  1 USD\n" +
			"    pmeta: 1\n" +
			"  Equity:O\n"
		assert.Equal(t, "2020-01-02 * \"x\"\n"+
			"  key: \"v\"\n"+
			"  Assets:Cash  1 USD\n"+
			"    pmeta: 1\n"+
			"  Equity:O\n", Sprint(parseOne(t, source)))
	})

	t.Run("OnACommodity", func(t *testing.T) {
		assert.Equal(t, "2020-01-01 commodity USD\n  name: \"US Dollar\"\n",
			Sprint(parseOne(t, "2020-01-01 commodity USD\n  name: \"US Dollar\"\n")))
	})
}

func TestSprintBookedPostings(t *testing.T) {
	// A posting with booked positions prints one posting per position: its
	// units, the lot's per-unit cost, date and label, and a per-unit price.
	// Its metadata is repeated under each. A posting without any prints as
	// parsed.
	txn := parseOne(t, "2020-01-04 * \"sell\"\n"+
		"  Assets:Stock  -12 HOOL {} @ 120 USD\n"+
		"    lotmeta: \"x\"\n"+
		"  Assets:Cash  1440 USD\n"+
		"  Income:Gains  -220 USD\n"+
		"  Assets:Cash  8 EUR @@ 10 USD\n").(*ast.Transaction)
	date, err := ast.NewDate("2020-01-03")
	assert.NoError(t, err)
	booked := map[*ast.Posting][]ledger.BookedPosition{
		txn.Postings[0]: {
			{Units: decimal.RequireFromString("-10"), Cost: &ledger.BookedCost{Number: decimal.RequireFromString("100"), Currency: "USD", Date: date}, Reduced: true},
			{Units: decimal.RequireFromString("-2"), Cost: &ledger.BookedCost{Number: decimal.RequireFromString("110"), Currency: "USD", Date: date, Label: "lot"}, Reduced: true},
		},
		txn.Postings[3]: {{Units: decimal.RequireFromString("8")}},
	}
	lookup := func(posting *ast.Posting) []ledger.BookedPosition { return booked[posting] }

	assert.Equal(t, "2020-01-04 * \"sell\"\n"+
		"  Assets:Stock   -10 HOOL {100 USD, 2020-01-03} @ 120 USD\n"+
		"    lotmeta: \"x\"\n"+
		"  Assets:Stock    -2 HOOL {110 USD, 2020-01-03, \"lot\"} @ 120 USD\n"+
		"    lotmeta: \"x\"\n"+
		"  Assets:Cash   1440 USD\n"+
		"  Income:Gains  -220 USD\n"+
		"  Assets:Cash      8 EUR @ 1.25 USD\n", Sprint(txn, WithBookedPositions(lookup)))
}

func TestSprintBalanceDiff(t *testing.T) {
	// A failed assertion carries its difference; a zero one carries none.
	balance := parseOne(t, "2020-01-03 balance Assets:Cash 12.00 USD\n").(*ast.Balance)
	passing := parseOne(t, "2020-01-04 balance Assets:Cash 10.25 USD\n").(*ast.Balance)
	diffs := map[*ast.Balance]decimal.Decimal{
		balance: decimal.RequireFromString("-1.75"),
		passing: decimal.Zero,
	}

	assert.Equal(t, "2020-01-03 balance Assets:Cash                                     12.00 USD   ; Diff: -1.75 USD\n",
		Sprint(balance, WithBalanceDiffs(diffs)))
	assert.Equal(t, "2020-01-04 balance Assets:Cash                                     10.25 USD\n",
		Sprint(passing, WithBalanceDiffs(diffs)))
}

func TestPrintSeparatesDirectives(t *testing.T) {
	// Like print_entries: a blank line before every transaction and
	// commodity, and before a directive of another kind than the one
	// printed before it, but not before a first directive of another kind.
	directives := parse(t, "2020-01-01 open Assets:Cash\n"+
		"2020-01-01 open Equity:O\n"+
		"2020-01-01 commodity USD\n"+
		"2020-01-01 commodity EUR\n"+
		"2020-01-02 * \"a\"\n  Assets:Cash  1 USD\n  Equity:O\n"+
		"2020-01-03 * \"b\"\n  Assets:Cash  1 USD\n  Equity:O\n"+
		"2020-01-04 note Assets:Cash \"first\"\n"+
		"2020-01-04 note Assets:Cash \"second\"\n"+
		"2020-01-05 close Equity:O\n")

	var out strings.Builder
	assert.NoError(t, Print(context.Background(), &out, directives))
	assert.Equal(t, "2020-01-01 open Assets:Cash\n"+
		"2020-01-01 open Equity:O\n"+
		"\n"+
		"2020-01-01 commodity USD\n"+
		"\n"+
		"2020-01-01 commodity EUR\n"+
		"\n"+
		"2020-01-02 * \"a\"\n  Assets:Cash  1 USD\n  Equity:O\n"+
		"\n"+
		"2020-01-03 * \"b\"\n  Assets:Cash  1 USD\n  Equity:O\n"+
		"\n"+
		"2020-01-04 note Assets:Cash \"first\"\n"+
		"2020-01-04 note Assets:Cash \"second\"\n"+
		"\n"+
		"2020-01-05 close Equity:O\n", out.String())
}

func TestPrintStartsATransactionWithABlankLine(t *testing.T) {
	var out strings.Builder
	assert.NoError(t, Print(context.Background(), &out, parse(t, "2020-01-02 * \"a\"\n  Assets:Cash  1 USD\n  Equity:O\n")))
	assert.Equal(t, "\n2020-01-02 * \"a\"\n  Assets:Cash  1 USD\n  Equity:O\n", out.String())
}

func TestPrintStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out strings.Builder
	assert.Error(t, Print(ctx, &out, parse(t, "2020-01-01 open Assets:Cash\n")))
	assert.Equal(t, "", out.String())
}
