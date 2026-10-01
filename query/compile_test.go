package query

import (
	"context"
	"errors"
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
	assert.Equal(t, `column "bogus" not found in table "postings"`, err.Error())
}

func TestCompileInvalidFunction(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT bogusfn(date)")
	assert.Equal(t, `no function matches "bogusfn(date)" name and argument types`, err.Error())
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
		`all non-aggregates must be covered by GROUP-BY clause in aggregate query: the following targets are missing: "date"`,
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
	assert.Equal(t, "invalid GROUP-BY column index 5", err.Error())
}

func TestCompileGroupByAggregate(t *testing.T) {
	ctx := newTestContext(t)
	err := compileFails(t, ctx, "SELECT account, sum(position) GROUP BY 2")
	assert.Equal(t, `GROUP-BY expressions may not reference aggregates: "2"`, err.Error())
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
	// Like beanquery, every clause registers the aggregates and rejects
	// them outside the targets once the clause compiles; only FROM has
	// has_account; and errors name the table.
	ctx := newTestContext(t)
	for query, want := range map[string]string{
		"SELECT account WHERE sum(number) > 0":                       "aggregates are not allowed in WHERE clause",
		"SELECT account WHERE sum(account) > 0":                      `no function matches "sum(str)" name and argument types`,
		"SELECT account WHERE has_account('x')":                      `no function matches "has_account(str)" name and argument types`,
		"SELECT account WHERE bogus":                                 `column "bogus" not found in table "postings"`,
		"SELECT account FROM bogus":                                  `column "bogus" not found in table "postings"`,
		"PRINT FROM bogus":                                           `column "bogus" not found in table "entries"`,
		"SELECT account FROM count(date) > 0":                        "aggregates are not allowed in FROM clause",
		"SELECT sum(sum(number))":                                    "aggregates of aggregates are not allowed",
		"SELECT sum(number) + number":                                "mixed aggregates and non-aggregates are not allowed",
		"SELECT sum(number) + number, bogus":                         "mixed aggregates and non-aggregates are not allowed",
		"SELECT account GROUP BY sum(number) != 1.50":                `GROUP-BY expressions may not be aggregates: "NotEqual(left=Function(fname='sum', operands=[Column(name='number')]), right=Constant(value=Decimal('1.50')))"`,
		"SELECT sum(number) AS s GROUP BY s":                         `GROUP-BY expressions may not reference aggregates: "Column(name='s')"`,
		"SELECT account, balance GROUP BY account, balance":          `GROUP-BY a non-hashable type is not supported: "Column(name='balance')"`,
		"SELECT account ORDER BY 5":                                  "invalid ORDER-BY column index 5",
		"SELECT account GROUP BY count(date) = 1 OR TRUE OR FALSE":   `GROUP-BY expressions may not be aggregates: "Or(args=[Equal(left=Function(fname='count', operands=[Column(name='date')]), right=Constant(value=1)), Constant(value=True), Constant(value=False)])"`,
		"SELECT account GROUP BY (count(date) = 1 OR TRUE) OR FALSE": `GROUP-BY expressions may not be aggregates: "Or(args=[Or(args=[Equal(left=Function(fname='count', operands=[Column(name='date')]), right=Constant(value=1)), Constant(value=True)]), Constant(value=False)])"`,
		"SELECT (1 = 1) = 1":                                         `operator "equal(bool, int)" not supported`,
		"SELECT weight = 1":                                          `operator "equal(amount, int)" not supported`,
		"SELECT position < 'x'":                                      `operator "less(position, str)" not supported`,
		"SELECT balance = 1":                                         `operator "equal(inventory, int)" not supported`,
		"SELECT NULL = NULL":                                         `operator "equal(NULL, NULL)" not supported`,
		"SELECT account GROUP BY first(account) IN tags":             `GROUP-BY expressions may not be aggregates: "In(left=Function(fname='first', operands=[Column(name='account')]), right=Column(name='tags'))"`,
	} {
		assert.Equal(t, want, compileFails(t, ctx, query).Error(), query)
	}
	mustCompile(t, ctx, "SELECT account, sum(number) + 1 GROUP BY account")
}

func TestCompileFromUsesEntryEnvironment(t *testing.T) {
	ctx := newTestContext(t)

	// account is a posting column, not available in the FROM filter
	// (beanquery's is; KNOWN_GAPS.md).
	err := compileFails(t, ctx, "SELECT date FROM account ~ 'Assets'")
	assert.Equal(t, `column "account" is not supported in FROM clause`, err.Error())

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
	assert.Equal(t, `no function matches "units(str)" name and argument types`, err.Error())
}

func TestCompileDistinctAndLimit(t *testing.T) {
	ctx := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT DISTINCT account LIMIT 5")
	assert.True(t, compiled.Distinct)
	assert.Equal(t, int64(5), *compiled.Limit)
}

func TestCompileNoMatchingFunction(t *testing.T) {
	// Like beanquery, a call no signature matches names the function and
	// its arguments' lower-cased Python types; an empty want compiles.
	for query, want := range map[string]string{
		"SELECT sum(number, 1)":           "sum(decimal, int)",
		"SELECT sum(account)":             "sum(str)",
		"SELECT today(1)":                 "today(int)",
		"SELECT year(account)":            "year(str)",
		"SELECT year(NULL)":               "year(nonetype)",
		"SELECT year(entry_meta('x'))":    "year(object)",
		"SELECT length(number)":           "length(decimal)",
		"SELECT only('USD', number)":      "only(str, decimal)",
		"SELECT units(account)":           "units(str)",
		"SELECT count(entry_meta('x'))":   "",
		"SELECT str(entry_meta('x'))":     "",
		"SELECT coalesce(account, NULL)":  "",
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
		var queryErr *Error
		assert.True(t, errors.As(err, &queryErr), query)
		assert.Equal(t, `no function matches "`+want+`" name and argument types`, queryErr.Error(), query)
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
