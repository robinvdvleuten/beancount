package query

import (
	"context"
	stdErrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
)

const fixtureDir = "../testdata/compliance/query"

// errorFixtures holds what Run reports for every err_ fixture
// (Error.Report), the same in text and csv: beanquery's shell text, or ours
// for a fixture listed in queryGaps (cli/query_compliance_test.go) until
// its gap closes.
var errorFixtures = map[string]string{
	"err_aggregate_arg_count": `error: no function matches "sum(decimal, int)" name and argument types
| SELECT sum(number, 1)
|        ^^^^^^^^^^^^^^`,
	"err_aggregate_arg_type": `error: no function matches "sum(str)" name and argument types
| SELECT sum(account)
|        ^^^^^^^^^^^^`,
	"err_bad_column": `error: column "bogus" not found in table "postings"
| SELECT bogus
|        ^^^^^`,
	"err_bad_function": `error: no function matches "bogusfn(date)" name and argument types
| SELECT bogusfn(date)
|        ^^^^^^^^^^^^^`,
	"err_empty_from": `error: syntax error
| SELECT account FROM WHERE account ~ 'Assets'
|                          ^`,
	"err_from_close_before_open": `error: CLOSE date must follow OPEN date`,
	"err_from_context": `error: column "bogus" not found in table "postings"
| SELECT account FROM bogus
|                     ^^^^^`,
	"err_function_arg_count": `error: no function matches "root(str)" name and argument types
| SELECT root(account)
|        ^^^^^^^^^^^^^`,
	"err_function_arg_type": `error: no function matches "grepn(str, str, str)" name and argument types
| SELECT grepn('a', account, 'x')
|        ^^^^^^^^^^^^^^^^^^^^^^^^`,
	"err_function_arg_types": `error: no function matches "bogusfn(date, position, amount, set, decimal, bool, nonetype)" name and argument types
| SELECT bogusfn(date, position, units(position), tags, 2.7, TRUE, NULL)
|        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^`,
	"err_group_by_aggregate_expr": `error: GROUP-BY expressions may not be aggregates: "NotEqual(left=Function(fname='count', operands=[Constant(value='x')]), right=Constant(value=datetime.date(2014, 1, 1)))"`,
	"err_group_by_inventory":      `error: GROUP-BY a non-hashable type is not supported: "Column(name='balance')"`,
	"err_group_coverage":          `error: all non-aggregates must be covered by GROUP-BY clause in aggregate query: the following targets are missing: "date"`,
	"err_having": `error: the HAVING clause is not supported yet
| SELECT account GROUP BY account HAVING count(account) > 1
|                                        ^^^^^^^^^^^^^^^^^^`,
	"err_identifier_digits": `error: syntax error
| SELECT account2
|               ^`,
	"err_mixed_aggregate":  `error: mixed aggregates and non-aggregates are not allowed`,
	"err_nested_aggregate": `error: aggregates of aggregates are not allowed`,
	"err_null_argument": `error: no function matches "length(nonetype)" name and argument types
| SELECT length(NULL)
|        ^^^^^^^^^^^^`,
	"err_object_argument": `error: no function matches "year(object)" name and argument types
| SELECT year(entry_meta('x'))
|        ^^^^^^^^^^^^^^^^^^^^^`,
	"err_order_by_index": `error: invalid ORDER-BY column index 5`,
	"err_pivot": `error: the PIVOT BY clause is not supported yet
| SELECT date, account, sum(position) GROUP BY 1, 2 PIVOT BY date, account
|                                                            ^^^^`,
	"err_pivot_by_after_checks": `error: the PIVOT BY clause is not supported yet
| SELECT account, sum(number) PIVOT BY bogus
|                                      ^^^^^`,
	"err_pivot_by_expression": `error: syntax error
| SELECT account PIVOT BY foo(1)
|                            ^`,
	"err_print_close_before_open": `error: CLOSE date must follow OPEN date`,
	"err_signed_number_after_operand": `error: syntax error
| SELECT number -1
|               ^`,
	"err_syntax_near": `error: syntax error
| SELECT account, count(*) GROUP BY account
|                       ^`,
	"err_unary_minus": `error: syntax error
| SELECT -number
|        ^`,
	"err_unknown_token": `error: syntax error
| SELECT account WHERE narration ~ 'coffee
|                                  ^`,
	"err_unterminated": `error: syntax error
| SELECT account WHERE
|                     ^`,
	"err_where_aggregate": `error: aggregates are not allowed in WHERE clause`,
	"err_where_has_account": `error: no function matches "has_account(str)" name and argument types
| SELECT account WHERE has_account('Cash')
|                      ^^^^^^^^^^^^^^^^^^^`,
}

// loadFixture returns the query and query context of a .bql fixture: its
// same-named ledger, or else the shared ledger.beancount, processed like the
// query command does.
func loadFixture(t *testing.T, name string) (string, *Context) {
	t.Helper()

	source, err := os.ReadFile(filepath.Join(fixtureDir, name+".bql"))
	assert.NoError(t, err)

	ledgerPath := filepath.Join(fixtureDir, name+".beancount")
	if _, err := os.Stat(ledgerPath); err != nil {
		ledgerPath = filepath.Join(fixtureDir, "ledger.beancount")
	}

	ctx := context.Background()
	result, err := loader.New(loader.WithFollowIncludes()).Load(ctx, ledgerPath)
	assert.NoError(t, err)

	l := ledger.New()
	if err := l.Process(ctx, result.AST); err != nil {
		var validationErrors *ledger.ValidationErrors
		assert.True(t, stdErrors.As(err, &validationErrors), "unexpected process error: %v", err)
	}
	cfg, err := config.FromAST(result.AST)
	assert.NoError(t, err)

	return strings.TrimSpace(string(source)), &Context{Ledger: l, Config: cfg, AST: result.AST}
}

func run(t *testing.T, qctx *Context, text string, format Format, numberify bool) string {
	t.Helper()
	var out strings.Builder
	assert.NoError(t, Run(context.Background(), qctx, text, format, numberify, &out))
	return out.String()
}

// runError runs a statement that does not parse or compile and returns
// what its Error reports; Run writes nothing for it.
func runError(t *testing.T, qctx *Context, text string, format Format) string {
	t.Helper()
	var out strings.Builder
	err := Run(context.Background(), qctx, text, format, false, &out)
	var queryErr *Error
	assert.True(t, stdErrors.As(err, &queryErr), "want an *Error, got %v", err)
	assert.Equal(t, "", out.String())
	return queryErr.Report()
}

// TestRunErrorFixtures runs every err_ fixture through Run and checks its
// report byte for byte, so it holds without bean-query installed.
func TestRunErrorFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(fixtureDir, "err_*.bql"))
	assert.NoError(t, err)
	assert.Equal(t, len(errorFixtures), len(paths), "every err_ fixture needs its report in errorFixtures, and every entry a fixture")

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".bql")
		t.Run(name, func(t *testing.T) {
			expected, ok := errorFixtures[name]
			assert.True(t, ok, "add beanquery's report for %s to errorFixtures", name)

			text, qctx := loadFixture(t, name)
			for _, format := range []Format{FormatText, FormatCSV} {
				assert.Equal(t, expected, runError(t, qctx, text, format), "format %s", format)
			}
		})
	}
}

// TestErrorReport checks how an error's report places the statement's
// lines and the caret, against beanquery's shell.
func TestErrorReport(t *testing.T) {
	qctx := newTestContext(t)
	for _, tt := range []struct {
		text string
		want string
	}{
		// The lines up to the node's, leading blank lines skipped.
		{"SELECT account,\n\n  bogus\nWHERE TRUE", "error: column \"bogus\" not found in table \"postings\"\n" +
			"| SELECT account,\n" +
			"| \n" +
			"|   bogus\n" +
			"|   ^^^^^"},
		{"\n\nSELECT bogus", "error: column \"bogus\" not found in table \"postings\"\n" +
			"| SELECT bogus\n" +
			"|        ^^^^^"},
		// Tabs expand in the line but not in the caret's column.
		{"SELECT\tbogus", "error: column \"bogus\" not found in table \"postings\"\n" +
			"| SELECT  bogus\n" +
			"|        ^^^^^"},
		// Columns and carets count characters.
		{"SELECT 'é', bogus", "error: column \"bogus\" not found in table \"postings\"\n" +
			"| SELECT 'é', bogus\n" +
			"|             ^^^^^"},
		// A node spanning lines gets a caret per character, line break
		// included, under its first line.
		{"SELECT sum(number,\n 1)", "error: no function matches \"sum(decimal, int)\" name and argument types\n" +
			"| SELECT sum(number,\n" +
			"|        ^^^^^^^^^^^^^^^"},
		{"SELECT account\nWHERE", "error: syntax error\n" +
			"| SELECT account\n" +
			"| WHERE\n" +
			"|      ^"},
		// Any of Python's line breaks ends a line, \r\n as one.
		{"SELECT account,\rbogus", "error: column \"bogus\" not found in table \"postings\"\n" +
			"| SELECT account,\n" +
			"| bogus\n" +
			"| ^^^^^"},
		{"SELECT account,\r\n\r\n bogus", "error: column \"bogus\" not found in table \"postings\"\n" +
			"| SELECT account,\n" +
			"| \n" +
			"|  bogus\n" +
			"|  ^^^^^"},
		// A node BALANCES desugars to has no source text to underline.
		{"BALANCES AT bogus", "error: no function matches \"bogus(position)\" name and argument types"},
	} {
		assert.Equal(t, tt.want, runError(t, qctx, tt.text, FormatText), tt.text)
	}
}

// TestRunEmptyResult checks that an empty result prints nothing as text
// and its header as csv, like beanquery's shell, with or without
// numberify, under which a position column with no rows has no currency
// column.
func TestRunEmptyResult(t *testing.T) {
	text, qctx := loadFixture(t, "empty_result")

	for _, tt := range []struct {
		format    Format
		numberify bool
		want      string
	}{
		{FormatText, false, ""},
		{FormatText, true, ""},
		{FormatCSV, false, "date,account,position\r\n"},
		{FormatCSV, true, "date,account\r\n"},
	} {
		assert.Equal(t, tt.want, run(t, qctx, text, tt.format, tt.numberify), "format %s, numberify %v", tt.format, tt.numberify)
	}
}

// TestRunSelect checks Run's bytes against beanquery's for testLedger.
func TestRunSelect(t *testing.T) {
	qctx := newTestContext(t)

	text := "SELECT account, sum(number) WHERE account ~ 'Assets' GROUP BY account ORDER BY account"
	assert.Equal(t, "    account      sum(numb\n"+
		"---------------  --------\n"+
		"Assets:Checking  -1504.50\n"+
		"Assets:Invest       10   \n", run(t, qctx, text, FormatText, false))
	assert.Equal(t, "account,sum(number)\r\n"+
		"Assets:Checking,-1504.50\r\n"+
		"Assets:Invest,   10   \r\n", run(t, qctx, text, FormatCSV, false))
}

// TestRunPrint checks that PRINT takes SELECT's FROM clause, filter and
// errors included, and prints like bean-query.
func TestRunPrint(t *testing.T) {
	qctx := newTestContext(t)

	assert.Equal(t, "\n"+
		"2014-03-05 * \"Cafe\" \"Coffee\" #food\n"+
		"  meta: \"posting-level\"\n"+
		"  Expenses:Food     4.50 USD\n"+
		"  Assets:Checking  -4.50 USD\n", run(t, qctx, "PRINT FROM 'food' IN tags", FormatText, false))
	assert.Equal(t, "error: column \"account\" not found in table \"entries\"\n"+
		"| PRINT FROM account = 'x'\n"+
		"|            ^^^^^^^", runError(t, qctx, "PRINT FROM account = 'x'", FormatText))
}

func TestRunUnknownFormat(t *testing.T) {
	qctx := newTestContext(t)
	var out strings.Builder
	assert.Error(t, Run(context.Background(), qctx, "SELECT date", Format("xml"), false, &out))
	assert.Equal(t, "", out.String())
}

func TestRunWithoutAST(t *testing.T) {
	qctx := newTestContext(t)
	qctx.AST = nil
	var out strings.Builder
	assert.EqualError(t, Run(context.Background(), qctx, "PRINT", FormatText, false, &out), "query context has no AST")
	assert.Equal(t, "", out.String())
}

// TestRunCancellation checks that Run returns a cancelled context's error
// rather than printing it as a query error.
func TestRunCancellation(t *testing.T) {
	qctx := newTestContext(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for _, text := range []string{"SELECT date", "PRINT"} {
		var out strings.Builder
		assert.IsError(t, Run(cancelled, qctx, text, FormatText, false, &out), context.Canceled)
		assert.Equal(t, "", out.String())
	}
}
