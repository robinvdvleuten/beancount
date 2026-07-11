package query

import (
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// The expected outputs in this file are byte-for-byte copies of bean-query
// 2.3.6 output for the same ledger and queries. Line numbers matter (the
// lineno column), so this fixture must not be reformatted.
const renderLedger = `option "title" "Query Reference"
option "operating_currency" "USD"

2014-01-01 open Assets:Checking USD
2014-01-01 open Assets:Invest HOOL
2014-01-01 open Income:Salary USD
2014-01-01 open Expenses:Food USD
2014-01-01 open Equity:Opening-Balances USD

2014-01-02 * "Opening"
  Assets:Checking  1000.00 USD
  Equity:Opening-Balances

2014-02-03 * "Acme" "Salary" #job ^ticket
  Assets:Checking  2500.00 USD
  Income:Salary

2014-03-05 * "Cafe" "Coffee" #food
  Expenses:Food  4.50 USD
  Assets:Checking

2014-04-01 * "Broker" "Buy HOOL"
  Assets:Invest  10 HOOL {500.00 USD}
  Assets:Checking

2014-05-01 price HOOL 520.00 USD
`

func renderText(t *testing.T, query string) string {
	t.Helper()
	ctx, tree := newContextFromSource(t, renderLedger)
	var b strings.Builder
	assert.NoError(t, RenderText(runQueryOn(t, ctx, tree, query), &b))
	return b.String()
}

func renderCSV(t *testing.T, query string, numberify bool) string {
	t.Helper()
	ctx, tree := newContextFromSource(t, renderLedger)
	var b strings.Builder
	assert.NoError(t, RenderCSV(runQueryOn(t, ctx, tree, query), &b, numberify))
	return b.String()
}

func TestRenderTextGroupedInventory(t *testing.T) {
	expected := "" +
		"        account                sum_position       \n" +
		"----------------------- --------------------------\n" +
		"Assets:Checking         -1504.50 USD              \n" +
		"Equity:Opening-Balances -1000.00 USD              \n" +
		"Income:Salary           -2500.00 USD              \n" +
		"Expenses:Food               4.50 USD              \n" +
		"Assets:Invest              10    HOOL {500.00 USD}\n"
	assert.Equal(t, expected, renderText(t, "select account, sum(position) group by account"))
}

func TestRenderTextInventoryJoinsPositions(t *testing.T) {
	// A multi-position inventory joins fixed-width sub-cells with ", " and
	// overflows the column, matching the official renderer.
	expected := "" +
		"       sum_position       \n" +
		"--------------------------\n" +
		"   10    HOOL {500.00 USD}, -5000.00 USD              \n"
	assert.Equal(t, expected, renderText(t, "select sum(position)"))
}

func TestRenderTextHeaderTruncation(t *testing.T) {
	expected := "" +
		"tag li    date   \n" +
		"--- -- ----------\n" +
		"    10 2014-01-02\n" +
		"    10 2014-01-02\n" +
		"job 14 2014-02-03\n"
	assert.Equal(t, expected, renderText(t, "select tags, lineno, date limit 3"))
}

func TestRenderTextScalarTypes(t *testing.T) {
	expected := "" +
		"equal c3_ c4\n" +
		"----- --- --\n" +
		"TRUE  3.5 42\n"
	assert.Equal(t, expected, renderText(t, "select 2 = 2, 3.5, 42 limit 1"))
}

func TestRenderTextEmpty(t *testing.T) {
	assert.Equal(t, "(empty)\n", renderText(t, "select account where account = 'NOPE'"))
}

func TestRenderCSVPadsValues(t *testing.T) {
	expected := "account\r\n" +
		"Assets:Checking        \r\n" +
		"Equity:Opening-Balances\r\n"
	assert.Equal(t, expected, renderCSV(t, "select account limit 2", false))
}

func TestRenderCSVGroupedInventory(t *testing.T) {
	expected := "account,sum_position\r\n" +
		"Assets:Checking        ,-1504.50 USD              \r\n" +
		"Assets:Invest          ,   10    HOOL {500.00 USD}\r\n" +
		"Equity:Opening-Balances,-1000.00 USD              \r\n" +
		"Expenses:Food          ,    4.50 USD              \r\n" +
		"Income:Salary          ,-2500.00 USD              \r\n"
	assert.Equal(t, expected, renderCSV(t, "select account, sum(position) group by account order by account", false))
}

func TestRenderCSVNumberify(t *testing.T) {
	expected := "account,sum_position (USD),sum_position (HOOL)\r\n" +
		"Assets:Checking        ,-1504.50,  \r\n" +
		"Assets:Invest          ,        ,10\r\n" +
		"Equity:Opening-Balances,-1000.00,  \r\n" +
		"Expenses:Food          ,    4.50,  \r\n" +
		"Income:Salary          ,-2500.00,  \r\n"
	assert.Equal(t, expected, renderCSV(t, "select account, sum(position) group by account order by account", true))
}
