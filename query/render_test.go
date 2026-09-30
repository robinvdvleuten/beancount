package query

import (
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

// The expected outputs in this file are byte-for-byte copies of beanquery
// 0.2.0's output for the same ledger and queries.
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

func textOutput(t *testing.T, query string, numberified bool) string {
	t.Helper()
	ctx := newContextFromSource(t, renderLedger)
	result := runQueryOn(t, ctx, query)
	if numberified {
		result = numberify(result)
	}
	var b strings.Builder
	assert.NoError(t, renderText(result, &b))
	return b.String()
}

func csvOutput(t *testing.T, query string, numberified bool) string {
	t.Helper()
	ctx := newContextFromSource(t, renderLedger)
	result := runQueryOn(t, ctx, query)
	if numberified {
		result = numberify(result)
	}
	var b strings.Builder
	assert.NoError(t, renderCSV(result, &b))
	return b.String()
}

func TestRenderTextGroupedInventory(t *testing.T) {
	// An inventory column gives each commodity its own sub-column, so the
	// lots of a commodity line up, and every amount keeps a sign column.
	expected := "" +
		"        account                     sum(position)            \n" +
		"-----------------------  ------------------------------------\n" +
		"Assets:Checking                                  -1504.50 USD\n" +
		"Equity:Opening-Balances                          -1000.00 USD\n" +
		"Income:Salary                                    -2500.00 USD\n" +
		"Expenses:Food                                        4.50 USD\n" +
		"Assets:Invest             10 HOOL { 500.00 USD}              \n"
	assert.Equal(t, expected, textOutput(t, "select account, sum(position) group by account", false))
}

func TestRenderTextInventorySortsCommodities(t *testing.T) {
	expected := "" +
		"           sum(position)            \n" +
		"------------------------------------\n" +
		" 10 HOOL { 500.00 USD}  -5000.00 USD\n"
	assert.Equal(t, expected, textOutput(t, "select sum(position)", false))
}

func TestRenderTextHeaderTruncation(t *testing.T) {
	// A column is as wide as its values: the header is cut to that width.
	expected := "" +
		"tag  l     date   \n" +
		"---  -  ----------\n" +
		"     7  2014-01-02\n" +
		"     7  2014-01-02\n" +
		"job  6  2014-02-03\n"
	assert.Equal(t, expected, textOutput(t, "select tags, length(narration), date limit 3", false))
}

func TestRenderTextScalarTypes(t *testing.T) {
	// A column is named by its source text; FALSE widens a bool column to 5.
	expected := "" +
		"FALSE  2 =   3.5  42\n" +
		"-----  ----  ---  --\n" +
		"FALSE  TRUE  3.5  42\n"
	assert.Equal(t, expected, textOutput(t, "select FALSE, 2 = 2, 3.5, 42 limit 1", false))
}

func TestRenderTextSignColumn(t *testing.T) {
	// An int column has no sign column, a number column one only when a
	// number is negative, and an amount column always one.
	expected := "" +
		"le   number    position  \n" +
		"--  -------  ------------\n" +
		"-1  1000.00   1000.00 USD\n" +
		"-2  2500.00   2500.00 USD\n" +
		"-2    -4.50     -4.50 USD\n"
	assert.Equal(t, expected, textOutput(t,
		"select length(narration) - 8, number, position where account = 'Assets:Checking' and number > -10", false))
}

func TestRenderTextNumberify(t *testing.T) {
	expected := "" +
		"        account          sum(posi  su\n" +
		"-----------------------  --------  --\n" +
		"Assets:Checking          -1504.50    \n" +
		"Assets:Invest                      10\n" +
		"Equity:Opening-Balances  -1000.00    \n" +
		"Expenses:Food                4.50    \n" +
		"Income:Salary            -2500.00    \n"
	assert.Equal(t, expected, textOutput(t, "select account, sum(position) group by account order by account", true))
}

func TestRenderEmptyResult(t *testing.T) {
	// The text table prints nothing, the csv its header.
	query := "select account where account = 'Nope'"
	assert.Equal(t, "", textOutput(t, query, false))
	assert.Equal(t, "account\r\n", csvOutput(t, query, false))
}

func TestRenderCSVPadsNumbersOnly(t *testing.T) {
	expected := "account,number\r\n" +
		"Assets:Checking, 1000.00\r\n" +
		"Equity:Opening-Balances,-1000.00\r\n"
	assert.Equal(t, expected, csvOutput(t, "select account, number limit 2", false))
}

func TestRenderCSVGroupedInventory(t *testing.T) {
	// The sub-columns of an inventory join with commas, so the cell is quoted.
	expected := "account,sum(position)\r\n" +
		"Assets:Checking,\"                      ,-1504.50 USD\"\r\n" +
		"Assets:Invest,\" 10 HOOL { 500.00 USD},            \"\r\n" +
		"Equity:Opening-Balances,\"                      ,-1000.00 USD\"\r\n" +
		"Expenses:Food,\"                      ,    4.50 USD\"\r\n" +
		"Income:Salary,\"                      ,-2500.00 USD\"\r\n"
	assert.Equal(t, expected, csvOutput(t, "select account, sum(position) group by account order by account", false))
}

func TestRenderCSVNumberify(t *testing.T) {
	expected := "account,sum(position) (USD),sum(position) (HOOL)\r\n" +
		"Assets:Checking,-1504.50,\r\n" +
		"Assets:Invest,,10\r\n" +
		"Equity:Opening-Balances,-1000.00,\r\n" +
		"Expenses:Food,    4.50,\r\n" +
		"Income:Salary,-2500.00,\r\n"
	assert.Equal(t, expected, csvOutput(t, "select account, sum(position) group by account order by account", true))
}

func TestWriteCSVRecord(t *testing.T) {
	// Like Python's csv.writer: minimal quoting, and a lone empty field
	// quoted so the record is not a blank line.
	var b strings.Builder
	writeCSVRecord(&b, []string{"a b", "c,d", `e"f`, ""})
	writeCSVRecord(&b, []string{""})
	assert.Equal(t, "a b,\"c,d\",\"e\"\"f\",\r\n\"\"\r\n", b.String())
}

func TestCenter(t *testing.T) {
	// Python's str.center puts an odd space left when the width is odd
	// too, and right otherwise; it counts code points.
	assert.Equal(t, "  ab ", center("ab", 5))
	assert.Equal(t, "  ab  ", center("ab", 6))
	assert.Equal(t, " abc  ", center("abc", 6))
	assert.Equal(t, "é ", center("é", 2))
}

func TestRenderZeros(t *testing.T) {
	// Pinned against beanquery 0.2.0, for a ledger whose USD amounts have
	// two digits: numbers that round to zero, and zero amounts.
	ctx := newContextFromSource(t, `2020-01-01 open Assets:A

2020-01-02 * "Two-digit USD"
  Assets:A   1.00 USD
  Assets:A  -1.00 USD
`)
	amount := func(number, currency string) *amountValue {
		return &amountValue{Number: decimal.RequireFromString(number), Currency: currency}
	}
	inventory := func(number, currency string) *inventoryValue {
		inv := newInventory()
		inv.AddAmount(amount(number, currency))
		return inv
	}

	for _, tt := range []struct {
		name        string
		typ         dtype
		values      []any
		numberified bool
		want        string
	}{
		{"negative dust keeps its sign", tAmount, []any{amount("-0.004", "USD")}, false, "x\r\n-0.00 USD\r\n"},
		{"numberified negative dust keeps its sign", tAmount, []any{amount("-0.004", "USD")}, true, "x (USD)\r\n-0.00\r\n"},
		{"an inventory that rounds to zero numberifies to NULL", tInventory, []any{inventory("0.004", "USD"), inventory("1.00", "USD")}, true, "x (USD)\r\n\"\"\r\n1.00\r\n"},
		{"a zero amount has no numberified column", tAmount, []any{amount("0", "EUR"), amount("1.00", "USD")}, true, "x (USD)\r\n\"\"\r\n1.00\r\n"},
	} {
		result := &table{Columns: []tableColumn{{Name: "x", Type: tt.typ}}, Display: ctx.Ledger.DisplayContext()}
		for _, value := range tt.values {
			result.Rows = append(result.Rows, []any{value})
		}
		if tt.numberified {
			result = numberify(result)
		}
		var b strings.Builder
		assert.NoError(t, renderCSV(result, &b))
		assert.Equal(t, tt.want, b.String(), tt.name)
	}
}
