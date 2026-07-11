package bql

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestParseSelectWildcard(t *testing.T) {
	stmt, err := Parse("SELECT *")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	assert.True(t, sel.Wildcard)
	assert.Zero(t, len(sel.Targets))
}

func TestParseSelectTargets(t *testing.T) {
	stmt, err := Parse("SELECT date, account, position AS pos")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	assert.False(t, sel.Wildcard)
	assert.Equal(t, 3, len(sel.Targets))
	assert.Equal(t, "date", sel.Targets[0].Expr.(*Ident).Name)
	assert.Equal(t, "", sel.Targets[0].As)
	assert.Equal(t, "pos", sel.Targets[2].As)
}

func TestParseSelectDistinct(t *testing.T) {
	stmt, err := Parse("SELECT DISTINCT account")
	assert.NoError(t, err)
	assert.True(t, stmt.(*Select).Distinct)
}

func TestParseSelectCaseInsensitiveKeywords(t *testing.T) {
	stmt, err := Parse("select distinct account from year = 2014 where number > 0 group by account order by account desc limit 10")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	assert.True(t, sel.Distinct)
	assert.NotZero(t, sel.From)
	assert.NotZero(t, sel.Where)
	assert.Equal(t, 1, len(sel.GroupBy))
	assert.Equal(t, 1, len(sel.OrderBy))
	assert.True(t, sel.OrderDesc)
	assert.Equal(t, int64(10), *sel.Limit)
}

func TestParseFunctionCall(t *testing.T) {
	stmt, err := Parse("SELECT account, sum(position) GROUP BY account")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	call := sel.Targets[1].Expr.(*Call)
	assert.Equal(t, "sum", call.Func)
	assert.Equal(t, 1, len(call.Args))
	assert.Equal(t, "position", call.Args[0].(*Ident).Name)
}

func TestParseNestedFunctionCall(t *testing.T) {
	stmt, err := Parse("SELECT sum(cost(position))")
	assert.NoError(t, err)

	call := stmt.(*Select).Targets[0].Expr.(*Call)
	assert.Equal(t, "sum", call.Func)
	inner := call.Args[0].(*Call)
	assert.Equal(t, "cost", inner.Func)
}

func TestParseFunctionCallNoArgs(t *testing.T) {
	stmt, err := Parse("SELECT today()")
	assert.NoError(t, err)

	call := stmt.(*Select).Targets[0].Expr.(*Call)
	assert.Equal(t, "today", call.Func)
	assert.Zero(t, len(call.Args))
}

func TestParseWherePrecedence(t *testing.T) {
	// NOT binds tighter than AND, AND tighter than OR.
	stmt, err := Parse("SELECT * WHERE a = 1 OR b = 2 AND NOT c = 3")
	assert.NoError(t, err)

	where := stmt.(*Select).Where.(*Binary)
	assert.Equal(t, OR, where.Op)
	right := where.R.(*Binary)
	assert.Equal(t, AND, right.Op)
	not := right.R.(*Unary)
	assert.Equal(t, NOT, not.Op)
}

func TestParseArithmeticPrecedence(t *testing.T) {
	stmt, err := Parse("SELECT 1 + 2 * 3")
	assert.NoError(t, err)

	expr := stmt.(*Select).Targets[0].Expr.(*Binary)
	assert.Equal(t, PLUS, expr.Op)
	assert.Equal(t, int64(1), expr.L.(*Int).Value)
	mul := expr.R.(*Binary)
	assert.Equal(t, ASTERISK, mul.Op)
}

func TestParseComparisonOperators(t *testing.T) {
	for _, tt := range []struct {
		query string
		op    TokenType
	}{
		{"SELECT * WHERE a = 1", EQ},
		{"SELECT * WHERE a != 1", NE},
		{"SELECT * WHERE a < 1", LT},
		{"SELECT * WHERE a <= 1", LTE},
		{"SELECT * WHERE a > 1", GT},
		{"SELECT * WHERE a >= 1", GTE},
		{"SELECT * WHERE account ~ 'Expenses'", TILDE},
		{"SELECT * WHERE 'trip' IN tags", IN},
	} {
		stmt, err := Parse(tt.query)
		assert.NoError(t, err, tt.query)
		assert.Equal(t, tt.op, stmt.(*Select).Where.(*Binary).Op, tt.query)
	}
}

func TestParseLiterals(t *testing.T) {
	stmt, err := Parse(`SELECT "double", 'single', 42, 3.14, 2014-01-01, TRUE, FALSE, NULL`)
	assert.NoError(t, err)

	targets := stmt.(*Select).Targets
	assert.Equal(t, "double", targets[0].Expr.(*Str).Value)
	assert.Equal(t, "single", targets[1].Expr.(*Str).Value)
	assert.Equal(t, int64(42), targets[2].Expr.(*Int).Value)
	assert.Equal(t, "3.14", targets[3].Expr.(*Dec).Value.String())
	assert.Equal(t, "2014-01-01", targets[4].Expr.(*DateLit).Value.String())
	assert.True(t, targets[5].Expr.(*Bool).Value)
	assert.False(t, targets[6].Expr.(*Bool).Value)
	_, isNull := targets[7].Expr.(*Null)
	assert.True(t, isNull)
}

func TestParseUnaryMinus(t *testing.T) {
	stmt, err := Parse("SELECT -number")
	assert.NoError(t, err)

	unary := stmt.(*Select).Targets[0].Expr.(*Unary)
	assert.Equal(t, MINUS, unary.Op)
	assert.Equal(t, "number", unary.X.(*Ident).Name)
}

func TestParseParenthesizedExpression(t *testing.T) {
	stmt, err := Parse("SELECT (1 + 2) * 3")
	assert.NoError(t, err)

	expr := stmt.(*Select).Targets[0].Expr.(*Binary)
	assert.Equal(t, ASTERISK, expr.Op)
	assert.Equal(t, PLUS, expr.L.(*Binary).Op)
}

func TestParseFromExpression(t *testing.T) {
	stmt, err := Parse("SELECT * FROM year = 2014 AND account ~ 'Assets'")
	assert.NoError(t, err)

	from := stmt.(*Select).From
	assert.NotZero(t, from.Expr)
	assert.Equal(t, AND, from.Expr.(*Binary).Op)
}

func TestParseFromTransforms(t *testing.T) {
	stmt, err := Parse("SELECT * FROM year = 2014 OPEN ON 2014-01-01 CLOSE ON 2015-01-01 CLEAR")
	assert.NoError(t, err)

	from := stmt.(*Select).From
	assert.NotZero(t, from.Expr)
	assert.Equal(t, "2014-01-01", from.OpenOn.String())
	assert.True(t, from.Close)
	assert.Equal(t, "2015-01-01", from.CloseOn.String())
	assert.True(t, from.Clear)
}

func TestParseFromBareClose(t *testing.T) {
	stmt, err := Parse("SELECT * FROM CLOSE")
	assert.NoError(t, err)

	from := stmt.(*Select).From
	assert.Zero(t, from.Expr)
	assert.True(t, from.Close)
	assert.Zero(t, from.CloseOn)
}

func TestParseFromTransformsOnly(t *testing.T) {
	stmt, err := Parse("SELECT * FROM OPEN ON 2014-01-01")
	assert.NoError(t, err)

	from := stmt.(*Select).From
	assert.Zero(t, from.Expr)
	assert.Equal(t, "2014-01-01", from.OpenOn.String())
}

func TestParseGroupByIndexAndName(t *testing.T) {
	stmt, err := Parse("SELECT account, year(date) GROUP BY 1, year(date)")
	assert.NoError(t, err)

	groupBy := stmt.(*Select).GroupBy
	assert.Equal(t, 2, len(groupBy))
	assert.Equal(t, int64(1), groupBy[0].(*Int).Value)
	assert.Equal(t, "year", groupBy[1].(*Call).Func)
}

func TestParseOrderByList(t *testing.T) {
	// The official grammar accepts a single trailing ASC/DESC that applies
	// to the whole ORDER BY list, not one direction per term.
	stmt, err := Parse("SELECT * ORDER BY date, account DESC")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	assert.Equal(t, 2, len(sel.OrderBy))
	assert.True(t, sel.OrderDesc)

	_, err = Parse("SELECT * ORDER BY date DESC, account ASC")
	assert.Error(t, err)
}

func TestParsePivotBy(t *testing.T) {
	stmt, err := Parse("SELECT account, year(date), sum(position) GROUP BY 1, 2 PIVOT BY account, year")
	assert.NoError(t, err)

	pivotBy := stmt.(*Select).PivotBy
	assert.Equal(t, 2, len(pivotBy))
}

func TestParseTrailingSemicolon(t *testing.T) {
	_, err := Parse("SELECT * ;")
	assert.NoError(t, err)
}

func TestParseBalances(t *testing.T) {
	stmt, err := Parse("BALANCES AT cost FROM year = 2014")
	assert.NoError(t, err)

	balances := stmt.(*Balances)
	assert.Equal(t, "cost", balances.Summary)
	assert.NotZero(t, balances.From)
}

func TestParseBalancesBare(t *testing.T) {
	stmt, err := Parse("BALANCES")
	assert.NoError(t, err)

	balances := stmt.(*Balances)
	assert.Equal(t, "", balances.Summary)
	assert.Zero(t, balances.From)
}

func TestParseJournal(t *testing.T) {
	stmt, err := Parse(`JOURNAL "Assets:Checking" AT units FROM year = 2014`)
	assert.NoError(t, err)

	journal := stmt.(*Journal)
	assert.Equal(t, "Assets:Checking", journal.Account)
	assert.Equal(t, "units", journal.Summary)
	assert.NotZero(t, journal.From)
}

func TestParsePrint(t *testing.T) {
	stmt, err := Parse("PRINT FROM account ~ 'Expenses'")
	assert.NoError(t, err)
	assert.NotZero(t, stmt.(*Print).From)
}

func TestParseErrors(t *testing.T) {
	for _, query := range []string{
		"",
		"FOO",
		"SELECT",
		"SELECT date,",
		"SELECT * WHERE",
		"SELECT * GROUP account",
		"SELECT * ORDER BY",
		"SELECT * LIMIT abc",
		"SELECT * LIMIT",
		"SELECT sum(",
		"SELECT (1 + 2",
		"SELECT * FROM",
		"SELECT * FROM OPEN 2014-01-01",
		"SELECT * FROM OPEN ON",
		"SELECT * extra",
		"SELECT 'unterminated",
		"SELECT a = b = c",
		"SELECT 2014-13-45",
	} {
		_, err := Parse(query)
		assert.Error(t, err, query)
	}
}

func TestParseErrorHasPosition(t *testing.T) {
	_, err := Parse("SELECT date,\n  bogus(")
	assert.Error(t, err)

	parseErr := err.(*ParseError)
	assert.Equal(t, 2, parseErr.Pos.Line)
	assert.NotZero(t, parseErr.GetPosition())
}

func TestParseMultilineQuery(t *testing.T) {
	stmt, err := Parse("SELECT\n  account,\n  sum(position)\nGROUP BY account\nORDER BY account")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(stmt.(*Select).Targets))
}
