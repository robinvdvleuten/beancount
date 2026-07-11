package query

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
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

// newTestContext parses and processes the test ledger into a query Context
// and the processed directive tree.
func newTestContext(t *testing.T) (*Context, *ast.AST) {
	t.Helper()
	return newContextFromSource(t, testLedger)
}

func newContextFromSource(t *testing.T, source string) (*Context, *ast.AST) {
	t.Helper()

	tree, err := parser.ParseBytesWithFilename(context.Background(), "test.beancount", []byte(source))
	assert.NoError(t, err)

	l := ledger.New()
	assert.NoError(t, l.Process(context.Background(), tree))

	cfg, err := config.FromAST(tree)
	assert.NoError(t, err)

	return &Context{Ledger: l, Config: cfg}, tree
}

func mustCompile(t *testing.T, ctx *Context, query string) *Compiled {
	t.Helper()
	stmt, err := bql.Parse(query)
	assert.NoError(t, err)
	compiled, err := Compile(ctx, stmt)
	assert.NoError(t, err)
	return compiled
}

func compileError(t *testing.T, ctx *Context, query string) error {
	t.Helper()
	stmt, err := bql.Parse(query)
	assert.NoError(t, err)
	_, err = Compile(ctx, stmt)
	assert.Error(t, err)
	return err
}

func TestCompileWildcard(t *testing.T) {
	ctx, _ := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT *")

	names := make([]string, len(compiled.Targets))
	for i, target := range compiled.Targets {
		names[i] = target.Name
	}
	assert.Equal(t, []string{"date", "flag", "payee", "narration", "position"}, names)
}

func TestCompileTargetNaming(t *testing.T) {
	ctx, _ := newTestContext(t)

	for _, tt := range []struct {
		query string
		name  string
	}{
		{"SELECT account", "account"},
		{"SELECT account AS acc", "acc"},
		{"SELECT sum(position)", "sum_position"},
		{"SELECT sum(cost(position))", "sum_cost_position"},
		{"SELECT year(date)", "year_date"},
		{"SELECT 'lit'", "clit"},
		{"SELECT 42", "c42"},
		{"SELECT 3.14", "c3_14"},
		{"SELECT number + 1", "add_number_c1"},
		{"SELECT number - 1", "sub_number_c1"},
		{"SELECT number * 2", "mul_number_c2"},
		{"SELECT number / 2", "div_number_c2"},
		{"SELECT str(2 = 2)", "str_equal_c2_c2"},
	} {
		compiled := mustCompile(t, ctx, tt.query)
		assert.Equal(t, tt.name, compiled.Targets[0].Name, tt.query)
	}
}

func TestCompileTargetTypes(t *testing.T) {
	ctx, _ := newTestContext(t)

	for _, tt := range []struct {
		query string
		typ   DType
	}{
		{"SELECT account", TString},
		{"SELECT date", TDate},
		{"SELECT position", TPosition},
		{"SELECT balance", TInventory},
		{"SELECT number", TDecimal},
		{"SELECT lineno", TInt},
		{"SELECT tags", TSet},
		{"SELECT price", TAmount},
		{"SELECT sum(position)", TInventory},
		{"SELECT sum(number)", TDecimal},
		{"SELECT count(date)", TInt},
		{"SELECT first(account)", TString},
		{"SELECT year(date)", TInt},
		{"SELECT number / 2", TDecimal},
		{"SELECT 1 + 2", TInt},
	} {
		compiled := mustCompile(t, ctx, tt.query)
		assert.Equal(t, tt.typ, compiled.Targets[0].Type, tt.query)
	}
}

func TestCompileInvalidColumn(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT bogus")
	assert.Equal(t, "Invalid column name 'bogus' in targets/column context.", err.Error())
}

func TestCompileInvalidFunction(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT bogusfn(date)")
	assert.Equal(t, "Invalid function 'bogusfn(date)' in targets/column context.", err.Error())
}

func TestCompileImplicitGroupBy(t *testing.T) {
	// An aggregate query without GROUP BY implicitly groups by all
	// non-aggregate targets (official behavior).
	ctx, _ := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account, sum(position)")

	assert.True(t, compiled.HasAgg)
	assert.Equal(t, []int{0}, compiled.GroupBy)
}

func TestCompileGroupByCoverage(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT date GROUP BY narration")
	assert.Equal(t,
		`All non-aggregates must be covered by GROUP-BY clause in aggregate query; the following targets are missing: "date".`,
		err.Error())
}

func TestCompileGroupByIndexAndAlias(t *testing.T) {
	ctx, _ := newTestContext(t)

	compiled := mustCompile(t, ctx, "SELECT account, sum(position) GROUP BY 1")
	assert.Equal(t, []int{0}, compiled.GroupBy)

	compiled = mustCompile(t, ctx, "SELECT account AS acc, sum(position) GROUP BY acc")
	assert.Equal(t, []int{0}, compiled.GroupBy)

	compiled = mustCompile(t, ctx, "SELECT year(date) AS y, count(date) GROUP BY y")
	assert.Equal(t, []int{0}, compiled.GroupBy)
}

func TestCompileGroupByIndexOutOfRange(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT account, sum(position) GROUP BY 5")
	assert.Contains(t, err.Error(), "Invalid GROUP-BY column index 5")
}

func TestCompileGroupByAggregate(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT account, sum(position) GROUP BY 2")
	assert.Equal(t, "GROUP-BY expressions may not be aggregates.", err.Error())
}

func TestCompileOrderByHiddenTarget(t *testing.T) {
	// ORDER BY on a column not in the targets appends a hidden target.
	ctx, _ := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account ORDER BY date")

	assert.Equal(t, 2, len(compiled.Targets))
	assert.True(t, compiled.Targets[1].Hidden)
	assert.Equal(t, []int{1}, compiled.OrderBy)
}

func TestCompileOrderByMatchesTarget(t *testing.T) {
	ctx, _ := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT account, sum(position) GROUP BY account ORDER BY sum(position) DESC")

	assert.Equal(t, 2, len(compiled.Targets))
	assert.Equal(t, []int{1}, compiled.OrderBy)
	assert.True(t, compiled.OrderDesc)
}

func TestCompileAggregateInWhere(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT account WHERE sum(number) > 0")
	assert.Contains(t, err.Error(), "Aggregates are disallowed")
}

func TestCompileFromUsesEntryEnvironment(t *testing.T) {
	ctx, _ := newTestContext(t)

	// account is a posting column, not available in the FROM filter.
	err := compileError(t, ctx, "SELECT date FROM account ~ 'Assets'")
	assert.Contains(t, err.Error(), "Invalid column name 'account'")

	// has_account is the FROM-environment predicate for that.
	mustCompile(t, ctx, "SELECT date FROM has_account('Assets')")
}

func TestCompileFunctionOverloads(t *testing.T) {
	ctx, _ := newTestContext(t)

	assert.Equal(t, TAmount, mustCompile(t, ctx, "SELECT units(position)").Targets[0].Type)
	assert.Equal(t, TInventory, mustCompile(t, ctx, "SELECT units(sum(position))").Targets[0].Type)
	assert.Equal(t, TAmount, mustCompile(t, ctx, "SELECT convert(price, 'USD')").Targets[0].Type)
	assert.Equal(t, TDecimal, mustCompile(t, ctx, "SELECT safediv(number, 2)").Targets[0].Type)
}

func TestCompileInvalidOverload(t *testing.T) {
	ctx, _ := newTestContext(t)
	err := compileError(t, ctx, "SELECT units(account)")
	assert.True(t, strings.HasPrefix(err.Error(), "Invalid function 'units(str)'"), err.Error())
}

func TestCompileDistinctAndLimit(t *testing.T) {
	ctx, _ := newTestContext(t)
	compiled := mustCompile(t, ctx, "SELECT DISTINCT account LIMIT 5")
	assert.True(t, compiled.Distinct)
	assert.Equal(t, int64(5), *compiled.Limit)
}
