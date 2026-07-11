package query

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// runQuery compiles and executes a query against the shared test ledger.
func runQuery(t *testing.T, query string) *Result {
	t.Helper()
	ctx, tree := newTestContext(t)
	return runQueryOn(t, ctx, tree, query)
}

func runQueryOn(t *testing.T, ctx *Context, tree *ast.AST, query string) *Result {
	t.Helper()
	stmt, err := bql.Parse(query)
	assert.NoError(t, err)
	compiled, err := Compile(ctx, stmt)
	assert.NoError(t, err)
	result, err := Execute(context.Background(), ctx, tree, compiled)
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
	inv := result.Rows[0][1].(*Inventory)
	assert.Equal(t, "-1504.5 USD", valueString(inv))

	// The HOOL position keeps its cost basis, stamped with the transaction
	// date like official booking.
	assert.Equal(t, "Assets:Invest", result.Rows[1][0].(string))
	assert.Equal(t, "10 HOOL {500 USD, 2014-04-01}", valueString(result.Rows[1][1].(*Inventory)))
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
	result := runQuery(t, "SELECT entry_meta('meta') WHERE entry_meta('meta') != NULL LIMIT 1")
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

func TestExecuteCancellation(t *testing.T) {
	qctx, tree := newTestContext(t)
	stmt, err := bql.Parse("SELECT date")
	assert.NoError(t, err)
	compiled, err := Compile(qctx, stmt)
	assert.NoError(t, err)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Execute(cancelled, qctx, tree, compiled)
	assert.Error(t, err)
}
