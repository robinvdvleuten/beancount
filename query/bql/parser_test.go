package bql

import (
	"errors"
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
	assert.True(t, sel.OrderBy[0].Desc)
	assert.Equal(t, int64(10), sel.Limit.Int64())
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

// A quoted identifier followed by ( names a function, as written but for
// each doubled quote; "" is a string.
func TestParseQuotedFunctionCall(t *testing.T) {
	stmt, err := Parse(`SELECT "LEN""gth"('ab'), "sum"(*), "length" ('x')`)
	assert.NoError(t, err)

	targets := stmt.(*Select).Targets
	call := targets[0].Expr.(*Call)
	assert.Equal(t, `LEN"gth`, call.Func)
	assert.Equal(t, "ab", call.Args[0].(*Str).Value)
	start, end := call.Span()
	assert.Equal(t, [2]int{7, 23}, [2]int{start, end})
	assert.Equal(t, "sum", targets[1].Expr.(*Call).Func)
	assert.Equal(t, "length", targets[2].Expr.(*Call).Func)

	_, err = Parse(`SELECT ""(1)`)
	assert.Error(t, err)
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

func TestParseModuloPrecedence(t *testing.T) {
	// % binds like * and /, left to right.
	stmt, err := Parse("SELECT 1 + 7 % 4 * 2")
	assert.NoError(t, err)

	expr := stmt.(*Select).Targets[0].Expr.(*Binary)
	assert.Equal(t, PLUS, expr.Op)
	mul := expr.R.(*Binary)
	assert.Equal(t, ASTERISK, mul.Op)
	assert.Equal(t, PERCENT, mul.L.(*Binary).Op)
}

func TestParseBetween(t *testing.T) {
	// BETWEEN takes its bounds' AND, and binds tighter than NOT and AND.
	stmt, err := Parse("SELECT * WHERE NOT date BETWEEN 2023-01-01 AND 2023-06-30 + 1 AND x")
	assert.NoError(t, err)

	where := stmt.(*Select).Where.(*Binary)
	assert.Equal(t, AND, where.Op)
	between := where.L.(*Unary).X.(*Between)
	assert.Equal(t, "date", between.X.(*Ident).Name)
	assert.Equal(t, PLUS, between.Upper.(*Binary).Op)

	// It is no reserved word.
	stmt, err = Parse("SELECT between")
	assert.NoError(t, err)
	assert.Equal(t, "between", stmt.(*Select).Targets[0].Expr.(*Ident).Name)
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
		{"SELECT * WHERE account !~ 'Cash'", NOTTILDE},
		{"SELECT * WHERE account ?~ 'Cash'", QTILDE},
		{"SELECT * WHERE 'trip' NOT IN tags", NOTIN},
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
	assert.True(t, targets[0].Expr.(*Str).DoubleQuoted)
	assert.False(t, targets[1].Expr.(*Str).DoubleQuoted)
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

// TestParseFromTable checks that, like beanquery's grammar, SELECT's FROM
// tries a Table reference first: #name, # alone (the Empty table) or a
// quoted name, after which no expression or transform may follow.
func TestParseFromTable(t *testing.T) {
	tests := []struct {
		query string
		table *Table
	}{
		{"SELECT * FROM #entries WHERE x", &Table{position: position{14, 22}, Name: "entries"}},
		{"SELECT * FROM #", &Table{position: position{14, 15}, Name: ""}},
		{"SELECT * FROM #_x1 LIMIT 1", &Table{position: position{14, 18}, Name: "_x1"}},
		{`SELECT * FROM "postings" ORDER BY 1`, &Table{position: position{14, 24}, Name: "postings"}},
		{`SELECT * FROM "a""b"`, &Table{position: position{14, 20}, Name: `a"b`}},
		{`SELECT * FROM #Entries`, &Table{position: position{14, 22}, Name: "Entries"}},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			stmt, err := Parse(test.query)
			assert.NoError(t, err)
			from := stmt.(*Select).From
			assert.Equal(t, test.table, from.Table)
			assert.Zero(t, from.Expr)
		})
	}

	// An empty quoted name is no Table reference but a FROM filter.
	stmt, err := Parse(`SELECT * FROM ""`)
	assert.NoError(t, err)
	assert.Zero(t, stmt.(*Select).From.Table)
	assert.Equal(t, Expr(&Str{position: position{14, 16}, Value: "", DoubleQuoted: true}), stmt.(*Select).From.Expr)
}

// TestParseFromTableEndsTheClause checks that, as in beanquery, nothing
// but the clauses after FROM may follow a Table reference, and that only
// SELECT's FROM takes one: the error is where the Table reference ends, or
// at a # elsewhere.
func TestParseFromTableEndsTheClause(t *testing.T) {
	tests := []struct {
		query  string
		offset int
	}{
		{`SELECT date FROM "year" = 2023`, 24},
		{`SELECT date FROM "postings" OPEN ON 2024-01-01`, 28},
		{`SELECT date FROM #entries CLOSE`, 26},
		{"SELECT date FROM # entries", 19},
		{"SELECT date FROM #1", 18},
		{"PRINT FROM #entries", 11},
		{"BALANCES FROM #entries", 14},
		{"JOURNAL FROM #entries", 13},
		{"SELECT #x", 7},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			_, err := Parse(test.query)
			var parseErr *ParseError
			assert.True(t, errors.As(err, &parseErr), "got %v", err)
			assert.Equal(t, test.offset, parseErr.Pos.Offset)
		})
	}
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
	assert.Equal(t, int64(2), sel.OrderBy[0].Expr.(*ColumnIndex).Value)
	assert.Equal(t, MINUS, sel.OrderBy[1].Expr.(*Unary).Op)

	// An item that starts with an integer ends there.
	_, err = Parse("SELECT count(*) GROUP BY 1 + 1")
	assert.Error(t, err)
}

func TestParseOrderByList(t *testing.T) {
	// Like beanquery's grammar, each term takes its own ASC or DESC, and a
	// term without one is ascending.
	stmt, err := Parse("SELECT * ORDER BY date, account DESC, 1 ASC, -number DESC")
	assert.NoError(t, err)

	terms := stmt.(*Select).OrderBy
	assert.Equal(t, 4, len(terms))
	for i, desc := range []bool{false, true, false, true} {
		assert.Equal(t, desc, terms[i].Desc, "term %d", i)
	}
}

func TestParsePivotBy(t *testing.T) {
	stmt, err := Parse("SELECT account, year(date), sum(position) GROUP BY 1, 2 PIVOT BY account, year")
	assert.NoError(t, err)

	pivotBy := stmt.(*Select).PivotBy
	assert.Equal(t, 2, len(pivotBy))
	assert.Equal(t, "account", pivotBy[0].(*Ident).Name)

	// Like beanquery's grammar, PIVOT BY takes exactly two items, each an
	// index or a name, a double-quoted one included.
	stmt, err = Parse(`SELECT account, year GROUP BY 1, 2 PIVOT BY 2, "account"`)
	assert.NoError(t, err)
	pivotBy = stmt.(*Select).PivotBy
	assert.Equal(t, int64(2), pivotBy[0].(*ColumnIndex).Value)
	assert.Equal(t, "account", pivotBy[1].(*Ident).Name)
	for _, query := range []string{
		"SELECT account PIVOT BY account",
		"SELECT account PIVOT BY account, year, date",
		"SELECT account PIVOT BY 1 + 1, 2",
	} {
		_, err := Parse(query)
		assert.Error(t, err, query)
	}
}

func TestParseTrailingSemicolon(t *testing.T) {
	_, err := Parse("SELECT * ;")
	assert.NoError(t, err)
}

// TestParseSemicolonComment checks beanquery's end-of-line comment: a ;
// whose rest of the text holds no newline, or one that ends it, starts a
// comment; any other ; is a token, which only the end may follow.
func TestParseSemicolonComment(t *testing.T) {
	for _, query := range []string{
		"SELECT 1; bogus",
		"SELECT 1;;",
		"SELECT 1; SELECT bogus\n",
		"SELECT 1;\n;",
		"SELECT 1;\n; bogus",
	} {
		_, err := Parse(query)
		assert.NoError(t, err, query)
	}

	for _, tc := range []struct {
		query  string
		offset int
	}{
		{"SELECT 1;\nbogus", 10},
		{"SELECT 1 ; x\n , 2", 11},
		{"SELECT 1; x\n\n", 10},
		{"SELECT ; 1", 10},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
}

// TestParseBlockComment checks beanquery's block comment: /* to the first
// */, across lines, is white space wherever white space may go, and a
// target's text keeps a comment between its tokens. An unterminated /* is
// no comment, so its / is a token.
func TestParseBlockComment(t *testing.T) {
	stmt, err := Parse("/* a */ SELECT /* b */ 1 /* c\n */ + 2, 3/**/*/**/4 /* ; */ /* d */")
	assert.NoError(t, err)
	targets := stmt.(*Select).Targets
	assert.Equal(t, 2, len(targets))
	assert.Equal(t, "1 /* c\n */ + 2", targets[0].Text)
	assert.Equal(t, "3/**/*/**/4", targets[1].Text)

	for _, tc := range []struct {
		query  string
		offset int
	}{
		{"SELECT /* x", 7},
		{"SELECT 1 /*/ 2", 10},
		{"SELECT 1 /* x */*/ 2", 17},
		{"SELECT 1 /* a */ /* b", 18},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
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
		{"SELECT account LIMIT .5", 15},
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
		// Without an expression, beanquery commits to OPEN, CLOSE or
		// CLEAR and fails after it.
		{"SELECT account FROM OPEN ON x", 28},
		{"SELECT account FROM OPEN x", 25},
		{"SELECT account FROM open = 1", 25},
		{"SELECT account FROM clear = 1", 26},
		{"SELECT account FROM CLEAR open", 26},
		// AT, OPEN, CLOSE, CLEAR and ON are names outside their clauses,
		// so they fail where a name would.
		{"SELECT account open", 15},
		{"SELECT account FROM year = 2023 OPEN", 32},
		{"SELECT account FROM year = 2023 CLOSE on", 38},
		{"BALANCES at", 11},
		{"JOURNAL AT", 10},
		// So is NULL, where no literal fits.
		{"SELECT account null", 15},
		{"SELECT account FROM year = 2023 null", 32},
		// LIMIT's integer takes the digits before a dot, and the
		// statement fails at the dot or the letter after them.
		{"SELECT account LIMIT 1.5", 22},
		{"SELECT account LIMIT 12.5", 23},
		{"SELECT account LIMIT 1.", 22},
		{"SELECT account LIMIT 1e2", 22},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
}

// TestParseList checks beanquery's list constants: a ( whose first
// literal a comma follows, keeping its literals but empty slots and a
// NULL after the first; any other ( is a parenthesized expression.
func TestParseList(t *testing.T) {
	stmt, err := Parse("SELECT (NULL, 1, NULL,, 'a',), (2023-01-01,), +(TRUE, 2.5), (1), (1 + 2) WHERE x IN ('a', 'b')")
	assert.NoError(t, err)
	sel := stmt.(*Select)

	list := sel.Targets[0].Expr.(*List)
	assert.Equal(t, 3, len(list.Items))
	assert.Equal(t, "(NULL, 1, NULL,, 'a',)", sel.Targets[0].Text)
	_, null := list.Items[0].(*Null)
	assert.True(t, null)
	assert.Equal(t, int64(1), list.Items[1].(*Int).Value)
	assert.Equal(t, "a", list.Items[2].(*Str).Value)

	assert.Equal(t, 1, len(sel.Targets[1].Expr.(*List).Items))
	// A unary plus leaves no node of its own.
	assert.Equal(t, "(TRUE, 2.5)", sel.Targets[2].Text)
	assert.Equal(t, 2, len(sel.Targets[2].Expr.(*List).Items))
	assert.Equal(t, int64(1), sel.Targets[3].Expr.(*Int).Value)
	assert.Equal(t, PLUS, sel.Targets[4].Expr.(*Binary).Op)
	assert.Equal(t, 2, len(sel.Where.(*Binary).R.(*List).Items))

	for _, tc := range []struct {
		query  string
		offset int
	}{
		{"SELECT (1, 2 + 3)", 13},
		{"SELECT (1, -2)", 11},
		{"SELECT (1, 2", 12},
		{"SELECT (1, 2)(3)", 13},
		{"SELECT (1 + 2, 3)", 13},
		{"SELECT (account, 1)", 15},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
}

// TestParseAttributeSubscript checks beanquery's primary: an atom followed
// by attribute and subscript accesses, binding tighter than a unary minus,
// while a unary plus and a parenthesized expression take none.
func TestParseAttributeSubscript(t *testing.T) {
	stmt, err := Parse(`SELECT -a.B['c'] . "D", f(x).y, 2.5.x, (1, 2).open, a ["k"]`)
	assert.NoError(t, err)
	sel := stmt.(*Select)

	neg := sel.Targets[0].Expr.(*Unary)
	outer := neg.X.(*Attribute)
	assert.Equal(t, "D", outer.Name)
	assert.Equal(t, `a.B['c'] . "D"`, sel.Targets[0].Text[1:])
	sub := outer.X.(*Subscript)
	assert.Equal(t, "c", sub.Key)
	inner := sub.X.(*Attribute)
	assert.Equal(t, "b", inner.Name)
	assert.Equal(t, "a", inner.X.(*Ident).Name)

	assert.Equal(t, "f", sel.Targets[1].Expr.(*Attribute).X.(*Call).Func)
	assert.Equal(t, "x", sel.Targets[2].Expr.(*Attribute).Name)
	assert.Equal(t, "open", sel.Targets[3].Expr.(*Attribute).Name)
	assert.Equal(t, "k", sel.Targets[4].Expr.(*Subscript).Key)

	for _, tc := range []struct {
		query  string
		offset int
	}{
		{"SELECT 2.5.5", 11},
		{"SELECT 1 .5", 10},
		{"SELECT position.1", 16},
		{"SELECT position.", 16},
		{"SELECT position .", 17},
		{"SELECT position.'units'", 16},
		{"SELECT position.from", 20},
		{"SELECT a.true", 13},
		{"SELECT account[1]", 15},
		{"SELECT account['x'", 18},
		{"SELECT +position.units", 16},
		{"SELECT +position['x']", 16},
		{"SELECT (position).units", 17},
		{"SELECT 1.x", 9},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
}

// TestParseUnreservedKeywords checks that AT, OPEN, CLOSE, CLEAR and ON,
// which beanquery does not reserve, are names outside their clauses.
func TestParseUnreservedKeywords(t *testing.T) {
	stmt, err := Parse("SELECT open, Close(at) AS clear, on AS at FROM at WHERE clear GROUP BY open ORDER BY on PIVOT BY at, on")
	assert.NoError(t, err)
	sel := stmt.(*Select)
	assert.Equal(t, Expr(&Ident{position: position{7, 11}, Name: "open"}), sel.Targets[0].Expr)
	assert.Equal(t, "close", sel.Targets[1].Expr.(*Call).Func)
	assert.Equal(t, "clear", sel.Targets[1].As)
	assert.Equal(t, "at", sel.Targets[2].As)
	assert.Equal(t, "at", sel.From.Expr.(*Ident).Name)
	assert.Zero(t, sel.From.OpenOn)
	assert.Equal(t, "clear", sel.Where.(*Ident).Name)

	stmt, err = Parse("BALANCES AT open FROM on OPEN ON 2023-01-01 CLOSE CLEAR")
	assert.NoError(t, err)
	balances := stmt.(*Balances)
	assert.Equal(t, "open", balances.Summary)
	assert.Equal(t, "on", balances.From.Expr.(*Ident).Name)
	assert.NotZero(t, balances.From.OpenOn)
	assert.True(t, balances.From.Close)
	assert.True(t, balances.From.Clear)
}

// TestParseNullName checks that NULL, which beanquery does not reserve, is
// a literal where an expression takes one and a name elsewhere.
func TestParseNullName(t *testing.T) {
	stmt, err := Parse("SELECT null, Null(x) AS NULL, position.null, null.x FROM null WHERE x = NULL AND x IS NULL GROUP BY null ORDER BY null PIVOT BY null, x")
	assert.NoError(t, err)
	sel := stmt.(*Select)
	assert.Equal(t, Expr(&Null{position: position{7, 11}}), sel.Targets[0].Expr)
	assert.Equal(t, "null", sel.Targets[1].Expr.(*Call).Func)
	assert.Equal(t, "null", sel.Targets[1].As)
	assert.Equal(t, "null", sel.Targets[2].Expr.(*Attribute).Name)
	assert.Equal(t, Expr(&Null{position: position{45, 49}}), sel.Targets[3].Expr.(*Attribute).X)
	assert.Equal(t, Expr(&Null{position: position{57, 61}}), sel.From.Expr)
	assert.Equal(t, Expr(&Null{position: position{72, 76}}), sel.Where.(*Binary).L.(*Binary).R)
	assert.Equal(t, Expr(&Null{position: position{100, 104}}), sel.GroupBy[0])
	assert.Equal(t, "null", sel.PivotBy[0].(*Ident).Name)

	stmt, err = Parse("BALANCES AT null")
	assert.NoError(t, err)
	assert.Equal(t, "null", stmt.(*Balances).Summary)
}

// TestParsePythonWhitespace checks that, like beanquery's lexer, white
// space is what Python's \s matches, and nothing more.
func TestParsePythonWhitespace(t *testing.T) {
	for _, space := range []string{"\v", "\f", "\x1c", "\x1d", "\x1e", "\x1f", "\u0085", "\u00a0", "\u1680", "\u2000", "\u2028", "\u2029", "\u202f", "\u3000"} {
		stmt, err := Parse("SELECT account," + space + "date" + space + "LIMIT 1")
		assert.NoError(t, err, "%q", space)
		assert.Equal(t, 2, len(stmt.(*Select).Targets), "%q", space)
	}

	// A zero-width space is not white space.
	_, err := Parse("SELECT account,\u200bdate")
	parseErr, ok := err.(*ParseError)
	assert.True(t, ok)
	assert.Equal(t, 15, parseErr.Pos.Offset)
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

// TestParseDoubledQuotes checks beanquery's doubled quotes: a single-quoted
// string runs on over two single quotes and keeps them in its value, as
// beanquery's raw text does, while a quoted identifier reads "" as one
// quote and a double-quoted string ends at its first closing quote.
func TestParseDoubledQuotes(t *testing.T) {
	stmt, err := Parse(`SELECT 'it''s', '''', '', 'a''''b' AS "a""b", x."y""" AS """" PIVOT BY "a""b", """"`)
	assert.NoError(t, err)
	sel := stmt.(*Select)

	assert.Equal(t, "it''s", sel.Targets[0].Expr.(*Str).Value)
	assert.Equal(t, "''", sel.Targets[1].Expr.(*Str).Value)
	assert.Equal(t, "", sel.Targets[2].Expr.(*Str).Value)
	assert.Equal(t, "a''''b", sel.Targets[3].Expr.(*Str).Value)
	assert.Equal(t, `a"b`, sel.Targets[3].As)
	assert.Equal(t, `y"`, sel.Targets[4].Expr.(*Attribute).Name)
	assert.Equal(t, `x."y"""`, sel.Targets[4].Text)
	assert.Equal(t, `"`, sel.Targets[4].As)
	assert.Equal(t, `a"b`, sel.PivotBy[0].(*Ident).Name)
	assert.Equal(t, `"`, sel.PivotBy[1].(*Ident).Name)

	for _, tc := range []struct {
		query  string
		offset int
	}{
		{`SELECT "a""b"`, 10},
		{`SELECT 1 AS ""`, 12},
		{`SELECT 1 AS "a" "b"`, 16},
		{`SELECT x.""`, 9},
		{"SELECT 'x''", 7},
		{"SELECT 'a' 'b'", 11},
	} {
		_, err := Parse(tc.query)
		parseErr, ok := err.(*ParseError)
		assert.True(t, ok, tc.query)
		assert.Equal(t, tc.offset, parseErr.Pos.Offset, tc.query)
	}
}
