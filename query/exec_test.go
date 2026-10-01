package query

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

// runQuery compiles and executes a query against the shared test ledger.
func runQuery(t *testing.T, query string) *table {
	t.Helper()
	return runQueryOn(t, newTestContext(t), query)
}

func runQueryOn(t *testing.T, ctx *Context, query string) *table {
	t.Helper()
	result, err := execute(context.Background(), ctx, mustCompile(t, ctx, query))
	assert.NoError(t, err)
	return result
}

func TestExecuteSimpleSelect(t *testing.T) {
	result := runQuery(t, "SELECT date, account, number")

	// 4 transactions with 2 postings each.
	assert.Equal(t, 8, len(result.Rows))
	assert.Equal(t, "date", result.Columns[0].Name)
	assert.Equal(t, "2014-01-02", valueString(result.Rows[0][0]))
	assert.Equal(t, "Assets:Checking", result.Rows[0][1].(string))
	assert.Equal(t, "1000", result.Rows[0][2].(decimal.Decimal).String())
}

func TestExecuteInterpolatedAmounts(t *testing.T) {
	// The second posting of the opening transaction has no explicit amount;
	// the ledger interpolates it.
	result := runQuery(t, "SELECT account, number WHERE account = 'Equity:Opening-Balances'")

	assert.Equal(t, 1, len(result.Rows))
	assert.Equal(t, "-1000", result.Rows[0][1].(decimal.Decimal).String())
}

func TestExecuteWhere(t *testing.T) {
	result := runQuery(t, "SELECT account WHERE number > 100")
	assert.Equal(t, 2, len(result.Rows))
}

func TestExecuteFromFiltersEntries(t *testing.T) {
	result := runQuery(t, "SELECT date, account FROM month = 2")
	assert.Equal(t, 2, len(result.Rows))
}

func TestExecuteGroupBySum(t *testing.T) {
	result := runQuery(t, "SELECT account, sum(position) GROUP BY account ORDER BY account")

	assert.Equal(t, 5, len(result.Rows))
	// 1000 + 2500 - 4.50 - 5000 (HOOL purchase), matching bean-query.
	assert.Equal(t, "Assets:Checking", result.Rows[0][0].(string))
	inv := result.Rows[0][1].(*inventoryValue)
	assert.Equal(t, "-1504.5 USD", valueString(inv))

	// The HOOL position keeps its cost basis, stamped with the transaction
	// date like official booking.
	assert.Equal(t, "Assets:Invest", result.Rows[1][0].(string))
	assert.Equal(t, "10 HOOL {500 USD, 2014-04-01}", valueString(result.Rows[1][1].(*inventoryValue)))
}

func TestExecuteImplicitGroupBy(t *testing.T) {
	result := runQuery(t, "SELECT currency, sum(number) ORDER BY currency")

	assert.Equal(t, 2, len(result.Rows))
	assert.Equal(t, "HOOL", result.Rows[0][0].(string))
	assert.Equal(t, "USD", result.Rows[1][0].(string))
	assert.Equal(t, "-5000", result.Rows[1][1].(decimal.Decimal).String())
}

func TestExecuteGlobalAggregate(t *testing.T) {
	result := runQuery(t, "SELECT count(date)")

	assert.Equal(t, 1, len(result.Rows))
	assert.Equal(t, int64(8), result.Rows[0][0].(int64))
}

func TestExecuteAggregates(t *testing.T) {
	result := runQuery(t, "SELECT min(date), max(date), first(account), last(account)")

	assert.Equal(t, 1, len(result.Rows))
	row := result.Rows[0]
	assert.Equal(t, "2014-01-02", valueString(row[0]))
	assert.Equal(t, "2014-04-01", valueString(row[1]))
	assert.Equal(t, "Assets:Checking", row[2].(string))
	assert.Equal(t, "Assets:Checking", row[3].(string))
}

func TestExecuteOrderBy(t *testing.T) {
	result := runQuery(t, "SELECT date ORDER BY date DESC LIMIT 2")

	assert.Equal(t, 2, len(result.Rows))
	assert.Equal(t, "2014-04-01", valueString(result.Rows[0][0]))
}

func TestExecuteOrderByHiddenColumnIsStripped(t *testing.T) {
	result := runQuery(t, "SELECT account ORDER BY date DESC LIMIT 1")

	assert.Equal(t, 1, len(result.Columns))
	assert.Equal(t, 1, len(result.Rows[0]))
	assert.Equal(t, "Assets:Invest", result.Rows[0][0].(string))
}

func TestExecuteDistinct(t *testing.T) {
	result := runQuery(t, "SELECT DISTINCT currency")
	assert.Equal(t, 2, len(result.Rows))
}

func TestExecuteLimit(t *testing.T) {
	result := runQuery(t, "SELECT date LIMIT 3")
	assert.Equal(t, 3, len(result.Rows))
}

func TestExecuteRunningBalance(t *testing.T) {
	result := runQuery(t, "SELECT balance WHERE account = 'Assets:Checking'")

	assert.Equal(t, 4, len(result.Rows))
	assert.Equal(t, "1000 USD", valueString(result.Rows[0][0]))
	assert.Equal(t, "3500 USD", valueString(result.Rows[1][0]))
	assert.Equal(t, "3495.5 USD", valueString(result.Rows[2][0]))
	// Buying HOOL for -5000.00 USD sends the balance negative.
	assert.Equal(t, "-1504.5 USD", valueString(result.Rows[3][0]))
}

// Like beanquery, balance is lazy: a row joins the running inventory the
// first time it evaluates the column, so WHERE sees the current posting
// too, and a row WHERE drops after reading balance still counts.
func TestExecuteBalanceInWhere(t *testing.T) {
	// The last posting sees the HOOL lot, held at cost.
	assert.Equal(t, 6, len(runQuery(t, "SELECT date WHERE str(balance) = str(units(balance))").Rows))

	// Every row reads balance in WHERE, so the dropped ones count too: the
	// salary's income leg is dropped, yet the food's checking leg sees it.
	result := runQuery(t, "SELECT account, balance WHERE number(only('USD', balance)) <= 1000")
	assert.Equal(t, 7, len(result.Rows))
	assert.Equal(t, "1000 USD", valueString(result.Rows[0][1]))
	assert.Equal(t, "Equity:Opening-Balances", result.Rows[1][0])
	assert.Equal(t, "", valueString(result.Rows[2][1]))

	// Short-circuiting keeps the balance to the rows that reach it.
	result = runQuery(t, "SELECT balance WHERE account = 'Assets:Checking' AND number(only('USD', balance)) > 0")
	assert.Equal(t, 3, len(result.Rows))
	assert.Equal(t, "3495.5 USD", valueString(result.Rows[2][0]))
}

// TestExecuteHavingReadsLastRow pins a beanquery quirk: HAVING reads its
// columns from the table's last row (Assets:Checking of "Buy HOOL"), after
// FROM's transforms but before its filter, not from the group's own rows.
func TestExecuteHavingReadsLastRow(t *testing.T) {
	for query, rows := range map[string]int{
		"SELECT account, count(*) GROUP BY account HAVING count(*) > 0 AND account = 'Assets:Checking'": 5,
		"SELECT account, count(*) GROUP BY account HAVING count(*) > 0 AND account = 'Expenses:Food'":   0,
		// FROM keeps only the coffee, yet the last row is still the HOOL buy.
		"SELECT account, count(*) FROM narration ~ 'Coffee' GROUP BY account HAVING count(*) > 0 AND narration ~ 'HOOL'":   2,
		"SELECT account, count(*) FROM narration ~ 'Coffee' GROUP BY account HAVING count(*) > 0 AND narration ~ 'Coffee'": 0,
	} {
		assert.Equal(t, rows, len(runQuery(t, query).Rows), query)
	}
}

func TestExecuteTagsAndLinks(t *testing.T) {
	result := runQuery(t, "SELECT date WHERE 'job' in tags")
	assert.Equal(t, 2, len(result.Rows))

	result = runQuery(t, "SELECT date WHERE 'ticket' in links")
	assert.Equal(t, 2, len(result.Rows))
}

func TestExecuteRegexMatch(t *testing.T) {
	result := runQuery(t, "SELECT DISTINCT account WHERE account ~ 'Assets'")
	assert.Equal(t, 2, len(result.Rows))
}

func TestExecutePriceConversion(t *testing.T) {
	result := runQuery(t, "SELECT convert(units(position), 'USD') WHERE currency = 'HOOL'")

	assert.Equal(t, 1, len(result.Rows))
	assert.Equal(t, "5200 USD", valueString(result.Rows[0][0]))
}

func TestExecuteValueAtCost(t *testing.T) {
	result := runQuery(t, "SELECT value(position) WHERE currency = 'HOOL'")

	assert.Equal(t, 1, len(result.Rows))
	assert.Equal(t, "5200 USD", valueString(result.Rows[0][0]))
}

func TestExecuteMetadata(t *testing.T) {
	result := runQuery(t, "SELECT entry_meta('meta') WHERE entry_meta('meta') IS NOT NULL LIMIT 1")
	assert.Equal(t, 1, len(result.Rows))
	assert.Equal(t, "posting-level", result.Rows[0][0].(string))
}

func TestExecuteOpenOnSummarizes(t *testing.T) {
	// OPEN ON replaces earlier transactions with S-flagged opening entries
	// at the day before the open date.
	result := runQuery(t, "SELECT date, flag, account, narration FROM OPEN ON 2014-03-01 WHERE flag = 'S' ORDER BY account")

	assert.True(t, len(result.Rows) > 0)
	assert.Equal(t, "2014-02-28", valueString(result.Rows[0][0]))
	assert.Equal(t, "Opening balance for 'Assets:Checking' (Summarization)", result.Rows[0][3].(string))
}

func TestExecuteOpenOnPostsEquityLegPerLot(t *testing.T) {
	// Each summarized lot is followed by its own equity leg, as in
	// beancount's create_entries_from_balances.
	ctx := newContextFromSource(t, `
2020-01-01 open Assets:Stock
2020-01-01 open Equity:Opening-Balances

2020-01-02 * "Buy"
  Assets:Stock  10 HOOL {100.00 USD}
  Equity:Opening-Balances

2020-01-03 * "Buy"
  Assets:Stock   5 HOOL {110.00 USD}
  Equity:Opening-Balances
`)
	result := runQueryOn(t, ctx, "SELECT account, weight FROM OPEN ON 2020-01-04 WHERE narration ~ 'Assets:Stock'")

	var got []string
	for _, row := range result.Rows {
		got = append(got, row[0].(string)+" "+valueString(row[1]))
	}
	assert.Equal(t, []string{
		"Assets:Stock 1000 USD",
		"Equity:Opening-Balances -1000 USD",
		"Assets:Stock 550 USD",
		"Equity:Opening-Balances -550 USD",
	}, got)
}

func TestExecuteCancellation(t *testing.T) {
	qctx := newTestContext(t)
	compiled := mustCompile(t, qctx, "SELECT date")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := execute(cancelled, qctx, compiled)
	assert.Error(t, err)
}

func TestInventoryKeepsInsertionOrder(t *testing.T) {
	// Like beancount's dict-backed inventory: first-added order, and a
	// position that sums to zero goes to the end when added again.
	inv := newInventory()
	aapl := &amountValue{Number: decimal.NewFromInt(1), Currency: "AAPL"}
	zzz := &amountValue{Number: decimal.NewFromInt(2), Currency: "ZZZ"}
	inv.AddAmount(aapl)
	inv.AddAmount(zzz)
	assert.Equal(t, []string{"AAPL", "ZZZ"}, positionCurrencies(inv.Positions()))

	inv.AddAmount(&amountValue{Number: decimal.NewFromInt(-1), Currency: "AAPL"})
	inv.AddAmount(aapl)
	assert.Equal(t, []string{"ZZZ", "AAPL"}, positionCurrencies(inv.Positions()))
}

func TestInventoryStringSortsPositions(t *testing.T) {
	// Like beancount's str() of an inventory: major currencies first, then
	// the others by length, whatever order they were added in.
	inv := newInventory()
	for _, currency := range []string{"HOOL", "CAD", "ZZZ", "USD"} {
		inv.AddAmount(&amountValue{Number: decimal.NewFromInt(1), Currency: currency})
	}
	assert.Equal(t, "1 USD, 1 CAD, 1 ZZZ, 1 HOOL", valueString(inv))
	assert.Equal(t, []string{"HOOL", "CAD", "ZZZ", "USD"}, positionCurrencies(inv.Positions()))
}

func positionCurrencies(positions []*positionValue) []string {
	currencies := make([]string, len(positions))
	for i, p := range positions {
		currencies[i] = p.Units.Currency
	}
	return currencies
}

func TestSumSkipsNullPosition(t *testing.T) {
	acc := &sumInventoryAcc{inv: newInventory()}
	acc.update((*positionValue)(nil))
	acc.update(&positionValue{Units: amountValue{Number: decimal.NewFromInt(1), Currency: "USD"}})
	assert.Equal(t, "1 USD", valueString(acc.finalize()))
}

func TestHasAccountMatchesEveryEntryAccount(t *testing.T) {
	// Like bean-query, has_account searches every account an entry
	// references, case-insensitively, so it also selects open and pad
	// directives.
	ctx := newContextFromSource(t, `
2020-01-01 open Assets:Cash
2020-01-01 open Equity:Opening
2020-01-02 pad Assets:Cash Equity:Opening
2020-01-03 balance Assets:Cash 10 USD
2020-01-04 open Expenses:Food
`)
	out := run(t, ctx, "PRINT FROM has_account('opening')", FormatText, false)
	assert.Contains(t, out, "open Equity:Opening")
	assert.Contains(t, out, "pad Assets:Cash Equity:Opening")
	assert.NotContains(t, out, "balance")
	assert.NotContains(t, out, "Expenses:Food")
}

func TestPrintShowsAFailedBalancesDifference(t *testing.T) {
	// Like bean-query, a failed assertion carries the actual amount less
	// the expected one; a passing one carries nothing.
	source := `
2020-01-01 open Assets:Cash
2020-01-01 open Equity:O
2020-01-02 * "x"
  Assets:Cash  10.25 USD
  Equity:O
2020-01-03 balance Assets:Cash 12.00 USD
2020-01-04 balance Assets:Cash 10.25 USD
2020-01-05 balance Assets:Cash 8 ~ 0.5 USD
`
	tree, err := parser.ParseString(context.Background(), source)
	assert.NoError(t, err)
	l := ledger.New()
	assert.Error(t, l.Process(context.Background(), tree))
	ctx := &Context{Ledger: l, Config: config.New(), AST: tree}

	out := run(t, ctx, "PRINT FROM type = 'balance'", FormatText, false)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Equal(t, 3, len(lines), out)
	assert.True(t, strings.HasSuffix(lines[0], "12.00 USD   ; Diff: -1.75 USD"), lines[0])
	assert.True(t, strings.HasSuffix(lines[1], "10.25 USD"), lines[1])
	assert.True(t, strings.HasSuffix(lines[2], "USD   ; Diff: 2.25 USD"), lines[2])
}

func TestConvertPositionThroughItsCostCurrency(t *testing.T) {
	// Like beancount's convert_position, a position without a price in the
	// target converts through its cost currency; a plain amount does not.
	ctx := newContextFromSource(t, `
2020-01-01 open Assets:Stock
2020-01-01 open Equity:O
2020-01-01 price GOOG 3 USD
2020-01-01 price USD 0.9 CHF
2020-01-02 * "buy"
  Assets:Stock  2 GOOG {2 USD}
  Equity:O
`)
	result := runQueryOn(t, ctx, "SELECT convert(position, 'CHF'), convert(units(position), 'CHF') WHERE account = 'Assets:Stock'")
	assert.Equal(t, 1, len(result.Rows))
	converted := result.Rows[0][0].(*amountValue)
	assert.Equal(t, "5.4 CHF", converted.Number.String()+" "+converted.Currency)
	units := result.Rows[0][1].(*amountValue)
	assert.Equal(t, "2 GOOG", units.Number.String()+" "+units.Currency)
}
