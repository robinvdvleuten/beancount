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

// TestParseTargetText checks the source text and alias each target keeps,
// which name its column as in beanquery: a parenthesized expression's text
// leaves out its parentheses, a number's its plus sign, and a double-quoted
// alias keeps its case.
func TestParseTargetText(t *testing.T) {
	stmt, err := Parse("SELECT sum( number ), (number + 1) * 2, (number * 2), NOT (TRUE), -1.5, +3, account AS Acc, number AS \"Q A\"")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	var texts, aliases []string
	for _, target := range sel.Targets {
		texts = append(texts, target.Text)
		aliases = append(aliases, target.As)
	}
	assert.Equal(t, []string{"sum( number )", "(number + 1) * 2", "number * 2", "NOT (TRUE)", "-1.5", "3", "account", "number"}, texts)
	assert.Equal(t, []string{"", "", "", "", "", "", "acc", "Q A"}, aliases)
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

func TestParseNumbersAndUnaryOperators(t *testing.T) {
	// Like beanquery's grammar, numbers carry no sign: a minus is a
	// negation or a subtraction, and a unary plus leaves no node.
	stmt, err := Parse("SELECT -2, +3, -.5, 2., .5, 007, number -1, -(1 + 2), - -1, -2 * 3")
	assert.NoError(t, err)
	targets := stmt.(*Select).Targets
	neg := targets[0].Expr.(*Unary)
	assert.Equal(t, MINUS, neg.Op)
	assert.Equal(t, int64(2), neg.X.(*Int).Value)
	assert.Equal(t, int64(3), targets[1].Expr.(*Int).Value)
	assert.Equal(t, "3", targets[1].Text)
	assert.Equal(t, "0.5", targets[2].Expr.(*Unary).X.(*Dec).Value.String())
	assert.Equal(t, "2", targets[3].Expr.(*Dec).Value.String())
	assert.Equal(t, "0.5", targets[4].Expr.(*Dec).Value.String())
	assert.Equal(t, int64(7), targets[5].Expr.(*Int).Value)
	assert.Equal(t, MINUS, targets[6].Expr.(*Binary).Op)
	assert.Equal(t, "number -1", targets[6].Text)
	assert.Equal(t, PLUS, targets[7].Expr.(*Unary).X.(*Binary).Op)
	assert.Equal(t, "-(1 + 2)", targets[7].Text)
	assert.Equal(t, MINUS, targets[8].Expr.(*Unary).X.(*Unary).Op)
	mul := targets[9].Expr.(*Binary)
	assert.Equal(t, ASTERISK, mul.Op)
	assert.Equal(t, MINUS, mul.L.(*Unary).Op)

	// A unary plus takes an atom, not a parenthesized expression.
	_, err = Parse("SELECT +(1)")
	assert.Error(t, err)
}

func TestParseIdentifierDigits(t *testing.T) {
	// beanquery's identifiers are [a-zA-Z_][a-zA-Z0-9_]*.
	stmt, err := Parse("SELECT account2, _x, a_1")
	assert.NoError(t, err)
	targets := stmt.(*Select).Targets
	for i, name := range []string{"account2", "_x", "a_1"} {
		assert.Equal(t, name, targets[i].Expr.(*Ident).Name)
	}
	_, err = Parse("SELECT 2x")
	assert.Error(t, err)
}

func TestParseCallAsterisk(t *testing.T) {
	// Any function takes * as its only argument, named by its source text.
	stmt, err := Parse("SELECT count( * ), sum(*)")
	assert.NoError(t, err)
	targets := stmt.(*Select).Targets
	call := targets[0].Expr.(*Call)
	assert.Equal(t, 1, len(call.Args))
	_, ok := call.Args[0].(*Asterisk)
	assert.True(t, ok)
	assert.Equal(t, "count( * )", targets[0].Text)
	_, ok = targets[1].Expr.(*Call).Args[0].(*Asterisk)
	assert.True(t, ok)

	_, err = Parse("SELECT count(*, 1)")
	assert.Error(t, err)
}

func TestParseIdentifiersAreLowerCased(t *testing.T) {
	stmt, err := Parse("SELECT LENGTH(Account) AS Len")
	assert.NoError(t, err)
	target := stmt.(*Select).Targets[0]
	call := target.Expr.(*Call)
	assert.Equal(t, "length", call.Func)
	assert.Equal(t, "account", call.Args[0].(*Ident).Name)
	assert.Equal(t, "len", target.As)
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
	// Only a bare integer is a column index; +1 and (1) are constants.
	stmt, err := Parse("SELECT account, year(date) GROUP BY 1, year(date), +1, (1) ORDER BY 2, -1")
	assert.NoError(t, err)

	sel := stmt.(*Select)
	assert.Equal(t, 4, len(sel.GroupBy))
	assert.Equal(t, int64(1), sel.GroupBy[0].(*ColumnIndex).Value)
	assert.Equal(t, "year", sel.GroupBy[1].(*Call).Func)
	assert.Equal(t, int64(1), sel.GroupBy[2].(*Int).Value)
	assert.Equal(t, int64(1), sel.GroupBy[3].(*Int).Value)
	assert.Equal(t, int64(2), sel.OrderBy[0].(*ColumnIndex).Value)
	assert.Equal(t, MINUS, sel.OrderBy[1].(*Unary).Op)

	// An item that starts with an integer ends there.
	_, err = Parse("SELECT count(*) GROUP BY 1 + 1")
	assert.Error(t, err)
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

func TestParseBalancesWhere(t *testing.T) {
	stmt, err := Parse("BALANCES AT cost FROM year = 2014 WHERE account ~ 'Assets'")
	assert.NoError(t, err)
	balances := stmt.(*Balances)
	assert.NotZero(t, balances.From)
	assert.NotZero(t, balances.Where)

	// Only BALANCES takes WHERE; JOURNAL and trailing clauses do not.
	for _, query := range []string{
		"JOURNAL 'Assets' WHERE number > 0",
		"BALANCES WHERE account ~ 'Assets' ORDER BY account",
	} {
		_, err := Parse(query)
		assert.Error(t, err, query)
	}
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

// TestParseErrorOffset checks that a syntax error is placed where
// beanquery's parser fails, the offsets its shell puts the caret at
// (pinned with its shell).
func TestParseErrorOffset(t *testing.T) {
	for _, tc := range []struct {
		query  string
		offset int
	}{
		{"SELECT , account", 7},
		{"SELECT account account", 15},
		{"SELECT account WHERE account = )", 31},
		{"SELECT account WHERE account = 'x' SELECT", 35},
		{"SELECT account PIVOT BY foo(1)", 27},
		{"SELECT 'abc", 7},
		{"SELECT account WHERE", 20},
		{"SELECT sum(", 11},
		// A keyword where an expression was expected: after it.
		{"SELECT FROM", 11},
		{"SELECT account, FROM", 20},
		{"SELECT account WHERE ORDER BY x", 26},
		{"SELECT account GROUP BY WHERE", 29},
		{"SELECT account FROM WHERE account ~ 'Assets'", 25},
		{"SELECT account FROM LIMIT 1", 25},
		{"BALANCES FROM", 13},
		{"SELECT count(*, 1)", 13},
		{"SELECT +(1)", 10},
		{"SELECT +(number)", 9},
		{"SELECT +()", 9},
		{"SELECT +-1", 8},
		{"SELECT 2x", 8},
		{"SELECT count(*) GROUP BY 1 + 1", 27},
		{"SELECT account ORDER BY 1 + 1 LIMIT 2", 26},
		// An item that starts with digits and a dot fails at the dot.
		{"SELECT account, count(*) GROUP BY 1.0", 35},
		{"SELECT account, count(*) GROUP BY 1.", 35},
		{"SELECT count(*) GROUP BY 1.5", 26},
		{"SELECT account ORDER BY 1.5 LIMIT 2", 25},
		{"SELECT account ORDER BY 10.25 LIMIT 2", 26},
		{"SELECT account ORDER BY 1, 2.5 LIMIT 2", 28},
		{"SELECT account AS WHERE", 23},
		{"SELECT account AS 1", 18},
		// A clause that cannot be finished: at its first keyword.
		{"SELECT account LIMIT x", 15},
		{"SELECT account LIMIT", 15},
		{"SELECT account LIMIT -1", 15},
		{"SELECT account LIMIT +1", 15},
		{"SELECT account ORDER BY account ASC LIMIT x", 36},
		{"SELECT account GROUP account", 15},
		{"SELECT account ORDER", 15},
		{"SELECT account PIVOT account", 15},
		{"SELECT account WHERE account ~ 'x' GROUP", 35},
		{"SELECT account FROM year = 2023 OPEN ON", 32},
		{"SELECT account FROM year = 2023 OPEN x", 32},
		{"PRINT FROM year = 2023 OPEN ON", 23},
		{"SELECT account FROM year = 2023 CLOSE ON", 38},
		{"SELECT account FROM CLOSE ON x", 26},
		// Without an expression, beanquery reads OPEN as a column name.
		{"SELECT account FROM OPEN ON x", 28},
		{"SELECT account FROM OPEN x", 25},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
}

func TestParseHaving(t *testing.T) {
	stmt, err := Parse("SELECT account GROUP BY account HAVING count(account) > 1")
	assert.NoError(t, err)
	assert.NotZero(t, stmt.(*Select).Having)
}

func TestParseMultilineQuery(t *testing.T) {
	stmt, err := Parse("SELECT\n  account,\n  sum(position)\nGROUP BY account\nORDER BY account")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(stmt.(*Select).Targets))
}
