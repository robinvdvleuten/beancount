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

// errorFixtures holds bean-query 2.3.6's output for every err_ fixture,
// which is the same in text and csv.
var errorFixtures = map[string]string{
	"err_aggregate_arg_count":    "ERROR: Invalid number of arguments for Sum: found 2 expected 1.\n",
	"err_aggregate_arg_type":     "ERROR: Invalid type for argument 0 of Sum: found <class 'str'> expected (<class 'int'>, <class 'float'>, <class 'decimal.Decimal'>).\n",
	"err_bad_column":             "ERROR: Invalid column name 'bogus' in targets/column context.\n",
	"err_bad_function":           "ERROR: Invalid function 'bogusfn(date)' in targets/column context.\n",
	"err_empty_from":             "Empty FROM expression is not allowed\n",
	"err_from_context":           "ERROR: Invalid column name 'bogus' in FROM clause context.\n",
	"err_from_close_before_open": "ERROR: Invalid dates: CLOSE date must follow OPEN date.\n",
	"err_function_arg_count":     "ERROR: Invalid number of arguments for Root: found 1 expected 2.\n",
	"err_function_arg_type":      "ERROR: Invalid type for argument 2 of GrepN: found <class 'str'> expected <class 'int'>.\n",
	"err_function_arg_types":     "ERROR: Invalid function 'bogusfn(date, Position, Amount, set, Decimal, bool, NoneType)' in targets/column context.\n",
	"err_group_by_aggregate_expr": "ERROR: GROUP-BY expressions may not be aggregates: " +
		"'Not(operand=Equal(left=Function(fname='count', operands=[Constant(value='x')]), right=Constant(value=datetime.date(2014, 1, 1))))'.\n",
	"err_group_by_inventory": "ERROR: GROUP-BY a non-hashable type is not supported: 'Column(name='balance')'.\n",
	"err_group_coverage": "ERROR: All non-aggregates must be covered by GROUP-BY clause in aggregate query; " +
		"the following targets are missing: \"date\".\n",
	"err_having":                      "ERROR: The HAVING clause is not supported yet.\n",
	"err_identifier_digits":           "ERROR: Syntax error near '2' (at 14)\n  SELECT account2\n                ^\n",
	"err_mixed_aggregate":             "ERROR: Mixed aggregates and non-aggregates are not allowed.\n",
	"err_nested_aggregate":            "ERROR: Aggregates of aggregates are not allowed.\n",
	"err_null_argument":               "ERROR: Invalid type for argument 0 of Length: found <class 'NoneType'> expected (<class 'list'>, <class 'set'>, <class 'str'>).\n",
	"err_object_argument":             "ERROR: Invalid type for argument 0 of Year: found <class 'object'> expected <class 'datetime.date'>.\n",
	"err_order_by_index":              "ERROR: Invalid ORDER-BY column index 5.\n",
	"err_pivot":                       "ERROR: The PIVOT BY clause is not supported yet.\n",
	"err_pivot_by_after_checks":       "ERROR: The PIVOT BY clause is not supported yet.\n",
	"err_print_close_before_open":     "ERROR: Invalid dates: CLOSE date must follow OPEN date.\n",
	"err_pivot_by_expression":         "ERROR: Syntax error near '(' (at 27)\n  SELECT account PIVOT BY foo(1)\n                             ^\n",
	"err_signed_number_after_operand": "ERROR: Syntax error near '-1' (at 14)\n  SELECT number -1\n                ^\n",
	"err_syntax_near":                 "ERROR: Syntax error near '*' (at 22)\n  SELECT account, count(*) GROUP BY account\n                        ^\n",
	"err_unary_minus":                 "ERROR: Syntax error near '-' (at 7)\n  SELECT -number\n         ^\n",
	"err_unknown_token":               "Unknown token: LexToken(error,\"'coffee\",1,33)\n",
	"err_unterminated":                "ERROR: unterminated statement. Missing a semicolon?\n",
	"err_where_aggregate":             "ERROR: Invalid function 'sum(Decimal)' in WHERE clause context.\n",
	"err_where_has_account":           "ERROR: Invalid function 'has_account(str)' in WHERE clause context.\n",
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

// TestRunErrorFixtures runs every err_ fixture through Run and checks the
// error lines byte for byte, so they hold without bean-query installed.
func TestRunErrorFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(fixtureDir, "err_*.bql"))
	assert.NoError(t, err)
	assert.Equal(t, len(errorFixtures), len(paths), "every err_ fixture needs its output in errorFixtures, and every entry a fixture")

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".bql")
		t.Run(name, func(t *testing.T) {
			expected, ok := errorFixtures[name]
			assert.True(t, ok, "add bean-query's output for %s to errorFixtures", name)

			text, qctx := loadFixture(t, name)
			for _, format := range []Format{FormatText, FormatCSV} {
				assert.Equal(t, expected, run(t, qctx, text, format, false), "format %s", format)
			}
		})
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
	assert.Equal(t, "ERROR: Invalid column name 'account' in FROM clause context.\n",
		run(t, qctx, "PRINT FROM account = 'x'", FormatText, false))
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
