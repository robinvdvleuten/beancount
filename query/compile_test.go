package query

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/query/bql"
)

const testLedger = `
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
  meta: "posting-level"
  Expenses:Food  4.50 USD
  Assets:Checking

2014-04-01 * "Broker" "Buy HOOL"
  Assets:Invest  10 HOOL {500.00 USD}
  Assets:Checking

2014-05-01 price HOOL 520.00 USD
`

// newTestContext parses and processes the test ledger into a query Context.
func newTestContext(t *testing.T) *Context {
	t.Helper()
	return newContextFromSource(t, testLedger)
}

func newContextFromSource(t *testing.T, source string) *Context {
	t.Helper()

	tree, err := parser.ParseBytesWithFilename(context.Background(), "test.beancount", []byte(source))
	assert.NoError(t, err)

	l := ledger.New()
	assert.NoError(t, l.Process(context.Background(), tree))

	cfg, err := config.FromAST(tree)
	assert.NoError(t, err)

	return &Context{Ledger: l, Config: cfg, AST: tree}
}

func mustCompile(t *testing.T, ctx *Context, query string) *compiledSelect {
	t.Helper()
	stmt, err := bql.Parse(query)
	assert.NoError(t, err)
	compiled, err := compile(ctx, stmt)
	assert.NoError(t, err)
	return compiled.(*compiledSelect)
}

func compileFails(t *testing.T, ctx *Context, query string) error {
	t.Helper()
	stmt, err := bql.Parse(query)
	assert.NoError(t, err)
	_, err = compile(ctx, stmt)
	assert.Error(t, err)
	return err
}

func TestCompileWildcard(t *testing.T) {
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT *")

	names := make([]string, len(compiled.Targets))
	for i, target := range compiled.Targets {
		names[i] = target.Name
	}
	assert.Equal(t, []string{"date", "flag", "payee", "narration", "position"}, names)
}

func TestCompileTargetNaming(t *testing.T) {
	// Like beanquery's get_target_name: an alias, lowercased unless
	// double-quoted; a bare column's name; any other expression's source
	// text, as written.
	ctx := newTestContext(t)

	for _, tt := range []struct {
		query string
		name  string
	}{
		{"SELECT account", "account"},
		{"SELECT ACCOUNT", "account"},
		{"SELECT (Account)", "account"},
		{"SELECT account AS ACC", "acc"},
		{`SELECT account AS "Q A"`, "Q A"},
		{"SELECT sum( Position )", "sum( Position )"},
		{"SELECT sum(cost(position))", "sum(cost(position))"},
		{"SELECT 'lit'", "'lit'"},
		{"SELECT 042", "042"},
		{"SELECT +3", "3"},
		{"SELECT (number   * 2)", "number   * 2"},
		{"SELECT (number + 1) / 2", "(number + 1) / 2"},
		{"SELECT NOT 2 = 2", "NOT 2 = 2"},
	} {
		compiled := mustCompile(t, ctx, tt.query)
		assert.Equal(t, tt.name, compiled.Targets[0].Name, tt.query)
	}
}

func TestCompileDesugaredTargetNaming(t *testing.T) {
	// BALANCES and JOURNAL targets are named by the text beanquery
	// expands them to.
	ctx := newTestContext(t)

	for _, tt := range []struct {
		query string
		names []string
	}{
		{"BALANCES", []string{"account", "SUM((position))"}},
		{"BALANCES AT Cost", []string{"account", "SUM(cost(position))"}},
		{"JOURNAL", []string{"date", "flag", "MAXWIDTH(payee, 48)", "MAXWIDTH(narration, 80)", "account", "position", "balance"}},
		{"JOURNAL AT units", []string{"date", "flag", "MAXWIDTH(payee, 48)", "MAXWIDTH(narration, 80)", "account", "units(position)", "units(balance)"}},
	} {
		compiled := mustCompile(t, ctx, tt.query)
		var names []string
		for _, target := range compiled.Targets {
			if !target.Hidden {
				names = append(names, target.Name)
			}
		}
		assert.Equal(t, tt.names, names, tt.query)
	}
}

func TestCompileTargetTypes(t *testing.T) {
	ctx := newTestContext(t)

	for _, tt := range []struct {
		query string
		typ   dtype
	}{
		{"SELECT account", tString},
		{"SELECT date", tDate},
		{"SELECT position", tPosition},
		{"SELECT balance", tInventory},
		{"SELECT number", tDecimal},
		{"SELECT lineno", tInt},
		{"SELECT tags", tSet},
		{"SELECT price", tAmount},
		{"SELECT sum(position)", tInventory},
		{"SELECT sum(number)", tDecimal},
		{"SELECT count(date)", tInt},
		{"SELECT first(account)", tString},
		{"SELECT year(date)", tInt},
		{"SELECT number / 2", tDecimal},
		{"SELECT 1 + 2", tInt},
	} {
		compiled := mustCompile(t, ctx, tt.query)
		assert.Equal(t, tt.typ, compiled.Targets[0].Type, tt.query)
	}
}

func TestCompileInvalidColumn(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT bogus")
	assert.Equal(t, "Invalid column name 'bogus' in targets/column context.", err.Error())
}

func TestCompileInvalidFunction(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT bogusfn(date)")
	assert.Equal(t, "Invalid function 'bogusfn(date)' in targets/column context.", err.Error())
}

func TestCompileImplicitGroupBy(t *testing.T) {
	// An aggregate query without GROUP BY implicitly groups by all
	// non-aggregate targets (official behavior).
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account, sum(position)")

	assert.True(t, compiled.HasAgg)
	assert.Equal(t, []int{0}, compiled.GroupBy)
}

func TestCompileGroupByCoverage(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT date GROUP BY narration")
	assert.Equal(t,
		`All non-aggregates must be covered by GROUP-BY clause in aggregate query; the following targets are missing: "date".`,
		err.Error())
}

func TestCompileGroupByIndexAndAlias(t *testing.T) {
	ctx := newTestContext(t)

	compiled := mustCompile(t, ctx, "SELECT account, sum(position) GROUP BY 1")
	assert.Equal(t, []int{0}, compiled.GroupBy)

	compiled = mustCompile(t, ctx, "SELECT account AS acc, sum(position) GROUP BY acc")
	assert.Equal(t, []int{0}, compiled.GroupBy)

	compiled = mustCompile(t, ctx, "SELECT year(date) AS y, count(date) GROUP BY y")
	assert.Equal(t, []int{0}, compiled.GroupBy)
}

func TestCompileGroupByIndexOutOfRange(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT account, sum(position) GROUP BY 5")
	assert.Contains(t, err.Error(), "Invalid GROUP-BY column index 5")
}

func TestCompileGroupByAggregate(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT account, sum(position) GROUP BY 2")
	assert.Equal(t, "GROUP-BY expressions may not reference aggregates: '2'.", err.Error())
}

func TestCompileOrderByHiddenTarget(t *testing.T) {
	// ORDER BY on a column not in the targets appends a hidden target.
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account ORDER BY date")

	assert.Equal(t, 2, len(compiled.Targets))
	assert.True(t, compiled.Targets[1].Hidden)
	assert.Equal(t, []int{1}, compiled.OrderBy)
}

func TestCompileOrderByMatchesTarget(t *testing.T) {
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account, sum(position) GROUP BY account ORDER BY sum(position) DESC")

	assert.Equal(t, 2, len(compiled.Targets))
	assert.Equal(t, []int{1}, compiled.OrderBy)
	assert.True(t, compiled.OrderDesc)
}

func TestCompileClauseEnvironments(t *testing.T) {
	// Like bean-query, each clause compiles in its own environment: only
	// targets have aggregates, only FROM has has_account, and errors name
	// the clause.
	ctx := newTestContext(t)
	for query, want := range map[string]string{
		"SELECT account WHERE sum(number) > 0":                  "Invalid function 'sum(Decimal)' in WHERE clause context.",
		"SELECT account WHERE has_account('x')":                 "Invalid function 'has_account(str)' in WHERE clause context.",
		"SELECT account WHERE bogus":                            "Invalid column name 'bogus' in WHERE clause context.",
		"SELECT account FROM bogus":                             "Invalid column name 'bogus' in FROM clause context.",
		"SELECT account FROM count(date) > 0":                   "Invalid function 'count(date)' in FROM clause context.",
		"SELECT sum(sum(number))":                               "Aggregates of aggregates are not allowed.",
		"SELECT sum(number) + number":                           "Mixed aggregates and non-aggregates are not allowed.",
		"SELECT account GROUP BY sum(number) != 1.50":           "GROUP-BY expressions may not be aggregates: 'Not(operand=Equal(left=Function(fname='sum', operands=[Column(name='number')]), right=Constant(value=Decimal('1.50'))))'.",
		"SELECT sum(number) AS s GROUP BY s":                    "GROUP-BY expressions may not reference aggregates: 'Column(name='s')'.",
		"SELECT account, balance GROUP BY account, balance":     "GROUP-BY a non-hashable type is not supported: 'Column(name='balance')'.",
		"SELECT account ORDER BY 5":                             "Invalid ORDER-BY column index 5.",
		"SELECT account GROUP BY count(date) = 2014-01-02 OR 1": "GROUP-BY expressions may not be aggregates: 'Or(left=Equal(left=Function(fname='count', operands=[Column(name='date')]), right=Constant(value=datetime.date(2014, 1, 2))), right=Constant(value=1))'.",
	} {
		assert.Equal(t, want, compileFails(t, ctx, query).Error(), query)
	}
	mustCompile(t, ctx, "SELECT account, sum(number) + 1 GROUP BY account")
}

func TestCompileFromUsesEntryEnvironment(t *testing.T) {
	ctx := newTestContext(t)

	// account is a posting column, not available in the FROM filter.
	err := compileFails(t, ctx, "SELECT date FROM account ~ 'Assets'")
	assert.Contains(t, err.Error(), "Invalid column name 'account'")

	// has_account is the FROM-environment predicate for that.
	mustCompile(t, ctx, "SELECT date FROM has_account('Assets')")
}

func TestCompileFunctionOverloads(t *testing.T) {
	ctx := newTestContext(t)

	assert.Equal(t, tAmount, mustCompile(t, ctx, "SELECT units(position)").Targets[0].Type)
	assert.Equal(t, tInventory, mustCompile(t, ctx, "SELECT units(sum(position))").Targets[0].Type)
	assert.Equal(t, tAmount, mustCompile(t, ctx, "SELECT convert(price, 'USD')").Targets[0].Type)
	assert.Equal(t, tDecimal, mustCompile(t, ctx, "SELECT safediv(number, 2)").Targets[0].Type)
}

func TestCompileInvalidOverload(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT units(account)")
	assert.True(t, strings.HasPrefix(err.Error(), "Invalid function 'units(str)'"), err.Error())
}

func TestCompileDistinctAndLimit(t *testing.T) {
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT DISTINCT account LIMIT 5")
	assert.True(t, compiled.Distinct)
	assert.Equal(t, int64(5), *compiled.Limit)
}

func TestCompileFallbackClassErrors(t *testing.T) {
	// With no matching signature, bean-query instantiates the function's
	// by-name class, whose constructor names the class and Python types.
	for query, want := range map[string]string{
		"SELECT root(account)":            "Invalid number of arguments for Root: found 1 expected 2.",
		"SELECT sum(number, 1)":           "Invalid number of arguments for Sum: found 2 expected 1.",
		"SELECT today(1)":                 "Invalid number of arguments for Today: found 1 expected 0.",
		"SELECT year(account)":            "Invalid type for argument 0 of Year: found <class 'str'> expected <class 'datetime.date'>.",
		"SELECT year(NULL)":               "Invalid type for argument 0 of Year: found <class 'NoneType'> expected <class 'datetime.date'>.",
		"SELECT year(entry_meta('x'))":    "Invalid type for argument 0 of Year: found <class 'object'> expected <class 'datetime.date'>.",
		"SELECT length(number)":           "Invalid type for argument 0 of Length: found <class 'decimal.Decimal'> expected (<class 'list'>, <class 'set'>, <class 'str'>).",
		"SELECT only('USD', number)":      "Invalid type for argument 1 of OnlyInventory: found <class 'decimal.Decimal'> expected <class 'beancount.core.inventory.Inventory'>.",
		"SELECT sum(account)":             "Invalid type for argument 0 of Sum: found <class 'str'> expected (<class 'int'>, <class 'float'>, <class 'decimal.Decimal'>).",
		"SELECT units(account)":           "Invalid function 'units(str)' in targets/column context.",
		"SELECT coalesce(account, NULL)":  "",
		"SELECT count(entry_meta('x'))":   "",
		"SELECT str(entry_meta('x'))":     "",
		"SELECT maxwidth(account, 1 + 1)": "",
	} {
		ctx := newTestContext(t)
		stmt, err := bql.Parse(query)
		assert.NoError(t, err, query)
		_, err = compile(ctx, stmt)
		if want == "" {
			assert.NoError(t, err, query)
			continue
		}
		var compileErr *compileError
		assert.True(t, errors.As(err, &compileErr), query)
		assert.Equal(t, want, compileErr.Message, query)
	}
}

func TestCompileKeepsRepeatedTargetNames(t *testing.T) {
	// Like beanquery, a repeated name, given or with AS, stays as it is.
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account, account, number AS n, date AS n, number AS n")
	var names []string
	for _, target := range compiled.Targets {
		if !target.Hidden {
			names = append(names, target.Name)
		}
	}
	assert.Equal(t, []string{"account", "account", "n", "n", "n"}, names)
}
