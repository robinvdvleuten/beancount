package bql

import (
	"testing"
)

func FuzzParseQuery(f *testing.F) {
	seeds := []string{
		// Core SELECT forms
		"SELECT *",
		"SELECT date, account, position",
		"SELECT DISTINCT account",
		"SELECT account AS a, sum(position) AS total GROUP BY a",
		"SELECT account, sum(position) FROM year = 2014 WHERE number > 0 GROUP BY account ORDER BY account, date DESC LIMIT 10",

		// Expressions
		"SELECT * WHERE a = 1 OR b = 2 AND NOT c = 3",
		"SELECT (1 + 2) * 3 - -4 / 5",
		"SELECT * WHERE account ~ 'Expenses' AND 'trip' IN tags",
		"SELECT year(date), month(date), parent(account)",
		"SELECT * WHERE date >= 2014-01-01 AND date < 2015-01-01",
		"SELECT * WHERE meta('x') IS NULL OR NOT cost_date IS NOT NULL",
		"SELECT count(*), -number, number -1, - -(1 + 2), +1, account2, _x WHERE -year < 0",
		"SELECT date, account ORDER BY 1 DESC, account ASC, -number",
		`SELECT account, year, sum(number) GROUP BY 1, 2 HAVING sum(number) > 0 PIVOT BY 1, "year" LIMIT 3`,
		`SELECT "double", 'single', 42, 3.14, TRUE, FALSE, NULL`,

		// FROM transforms
		"SELECT * FROM OPEN ON 2014-01-01 CLOSE ON 2015-01-01 CLEAR",
		"SELECT * FROM CLOSE",
		"SELECT * FROM year = 2014 CLEAR",

		// Shortcut statements
		"BALANCES",
		"BALANCES AT cost FROM year = 2014",
		`JOURNAL "Assets:Checking" AT units`,
		"PRINT FROM account ~ 'Expenses'",

		// Pivot, semicolons, multi-line
		"SELECT account, year(date), sum(position) GROUP BY 1, 2 PIVOT BY account, year",
		"SELECT * ;",
		"SELECT\n  account\nORDER BY account",

		// Edge cases
		"",
		"   \n\t ",
		"SELECT",
		"select * where !",
		"SELECT 'unterminated",
		"SELECT 9999999999999999999999999",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, query string) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("parser panicked on input %q: %v", query, r)
			}
		}()
		// The parser must never panic; errors are fine. What it parses
		// spans the query text.
		stmt, err := Parse(query)
		if err != nil {
			return
		}
		walkNodes(stmt, func(node Node) {
			if start, end := node.Span(); start < 0 || start > end || end > len(query) {
				t.Errorf("%T spans [%d, %d) of %q", node, start, end, query)
			}
		})
	})
}

// walkNodes calls fn for stmt and every node below it.
func walkNodes(stmt Statement, fn func(Node)) {
	var expr func(Expr)
	expr = func(e Expr) {
		if e == nil {
			return
		}
		fn(e)
		switch node := e.(type) {
		case *Call:
			for _, arg := range node.Args {
				expr(arg)
			}
		case *Unary:
			expr(node.X)
		case *Binary:
			expr(node.L)
			expr(node.R)
		}
	}
	from := func(f *From) {
		if f != nil {
			fn(f)
			expr(f.Expr)
		}
	}

	fn(stmt)
	switch node := stmt.(type) {
	case *Select:
		for _, target := range node.Targets {
			expr(target.Expr)
		}
		from(node.From)
		expr(node.Where)
		for _, e := range node.GroupBy {
			expr(e)
		}
		expr(node.Having)
		for _, term := range node.OrderBy {
			expr(term.Expr)
		}
		for _, ident := range node.PivotBy {
			expr(ident)
		}
	case *Balances:
		from(node.From)
		expr(node.Where)
	case *Journal:
		from(node.From)
	case *Print:
		from(node.From)
	}
}
