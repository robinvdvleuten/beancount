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
		// The parser must never panic; errors are fine.
		_, _ = Parse(query)
	})
}
