package query

import (
	"strings"

	"github.com/robinvdvleuten/beancount/query/bql"
)

// desugar rewrites the BALANCES and JOURNAL shortcut statements into the
// SELECT statements the official tool expands them to. Other statements are
// returned unchanged.
func desugar(stmt bql.Statement) bql.Statement {
	switch node := stmt.(type) {
	case *bql.Balances:
		return desugarBalances(node)
	case *bql.Journal:
		return desugarJournal(node)
	}
	return stmt
}

// desugarBalances expands BALANCES [AT fn] [FROM ...] [WHERE ...] into
//
//	SELECT account, SUM([fn](position)) [FROM ...] [WHERE ...]
//	GROUP BY account ORDER BY account_sortkey(account)
//
// Its targets carry the text beanquery parses them from, which names them.
func desugarBalances(b *bql.Balances) *bql.Select {
	account := &bql.Ident{Name: "account"}
	summary := strings.ToLower(b.Summary)
	return &bql.Select{
		Targets: []bql.Target{
			{Expr: account},
			{
				Expr: call("sum", summarize(summary, &bql.Ident{Name: "position"})),
				Text: "SUM(" + summary + "(position))",
			},
		},
		From:    b.From,
		Where:   b.Where,
		GroupBy: []bql.Expr{account},
		OrderBy: []bql.Expr{call("account_sortkey", account)},
	}
}

// desugarJournal expands JOURNAL [account] [AT fn] into
//
//	SELECT date, flag, MAXWIDTH(payee, 48), MAXWIDTH(narration, 80),
//	       account, [fn](position), [fn](balance)
//	[WHERE account ~ "<account>"]
//
// Its targets carry the text beanquery parses them from, which names them.
func desugarJournal(j *bql.Journal) *bql.Select {
	summary := strings.ToLower(j.Summary)
	sel := &bql.Select{
		Targets: []bql.Target{
			{Expr: &bql.Ident{Name: "date"}},
			{Expr: &bql.Ident{Name: "flag"}},
			{Expr: call("maxwidth", &bql.Ident{Name: "payee"}, &bql.Int{Value: 48}), Text: "MAXWIDTH(payee, 48)"},
			{Expr: call("maxwidth", &bql.Ident{Name: "narration"}, &bql.Int{Value: 80}), Text: "MAXWIDTH(narration, 80)"},
			{Expr: &bql.Ident{Name: "account"}},
			{Expr: summarize(summary, &bql.Ident{Name: "position"}), Text: summary + "(position)"},
			{Expr: summarize(summary, &bql.Ident{Name: "balance"}), Text: summary + "(balance)"},
		},
		From: j.From,
	}
	if j.Account != "" {
		sel.Where = &bql.Binary{
			Op: bql.TILDE,
			L:  &bql.Ident{Name: "account"},
			R:  &bql.Str{Value: j.Account},
		}
	}
	return sel
}

func call(name string, args ...bql.Expr) *bql.Call {
	return &bql.Call{Func: name, Args: args}
}

// summarize wraps an expression in the AT summary function when present.
func summarize(summary string, expr bql.Expr) bql.Expr {
	if summary == "" {
		return expr
	}
	return call(summary, expr)
}
