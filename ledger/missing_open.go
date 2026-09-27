package ledger

import (
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
)

// accountUse is an account's first use: the date of the first directive
// that uses it.
type accountUse struct {
	account ast.Account
	date    *ast.Date
}

// firstUses returns every account the sorted directives use that known
// does not hold, dated at its first use, in order of first use, like
// beancount's getters.get_accounts_use_map.
func firstUses(directives []ast.Directive, known map[ast.Account]bool) []accountUse {
	seen := make(map[ast.Account]bool)
	var uses []accountUse
	for _, directive := range directives {
		used, ok := directive.(ast.WithAccounts)
		if !ok {
			continue
		}
		for _, account := range used.Accounts() {
			if known[account] || seen[account] {
				continue
			}
			seen[account] = true
			uses = append(uses, accountUse{account: account, date: directive.Date()})
		}
	}
	return uses
}

// MissingOpens returns an open directive, without currencies or booking
// method, for every account the processed tree uses that has neither an
// open nor a close directive, dated at its first use, like bean-doctor
// missing_open. They come in order of first use, which is already
// beancount's data.sorted order: dates ascend with the tree, and same-date
// opens all have lineno 0, so the stable sort keeps their order.
func MissingOpens(tree *ast.AST) []*ast.Open {
	known := make(map[ast.Account]bool)
	for _, directive := range tree.Directives {
		switch directive := directive.(type) {
		case *ast.Open:
			known[directive.Account] = true
		case *ast.Close:
			known[directive.Account] = true
		}
	}

	// Loading drops a balance on an account without an open or close
	// (beancount's ops/balance.py), so no balance is the first use of a
	// missing account.
	loaded := slices.DeleteFunc(slices.Clone(tree.Directives), func(directive ast.Directive) bool {
		_, ok := directive.(*ast.Balance)
		return ok
	})

	uses := firstUses(loaded, known)
	opens := make([]*ast.Open, len(uses))
	for i, use := range uses {
		opens[i] = ast.NewOpen(use.date, use.account, nil, "")
	}
	return opens
}
