package query

import (
	"fmt"
	"sort"
	"time"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// applyFromTransforms applies the FROM clause's summarization transforms to
// the directive stream, in grammar order: OPEN ON, CLOSE [ON], CLEAR. The
// FROM filter expression runs after the transforms (official behavior).
// A bare CLOSE truncates nothing here: its conversion entries only exist for
// multi-currency conversion tracking, which the official tool also skips for
// ledgers without a conversion imbalance.
func applyFromTransforms(qctx *Context, entries []ast.Directive, from *CompiledFrom) []ast.Directive {
	if from.OpenOn != nil {
		entries = openTransform(qctx, entries, from.OpenOn)
	}
	if from.CloseOn != nil {
		entries = closeTransform(entries, from.CloseOn)
	}
	if from.Clear {
		entries = clearTransform(qctx, entries)
	}
	return entries
}

// openTransform summarizes all transactions before the open date: income and
// expenses balances collapse into Equity:Earnings:Previous, and every
// balance-sheet account's inventory becomes an S-flagged opening transaction
// at the day before the open date, posted against Equity:Opening-Balances.
func openTransform(qctx *Context, entries []ast.Directive, openDate *ast.Date) []ast.Directive {
	var kept []ast.Directive
	accounts := make(map[string]*Inventory)

	for _, entry := range entries {
		txn, isTxn := entry.(*ast.Transaction)
		if entry.Date().Before(openDate.Time) {
			if !isTxn {
				kept = append(kept, entry)
				continue
			}
			bookTransaction(accounts, txn)
			continue
		}
		kept = append(kept, entry)
	}

	// Collapse income and expenses into the previous-earnings account.
	earnings := equityAccount(qctx, "Earnings:Previous")
	for account, inventory := range accounts {
		if typ, ok := accountType(qctx, account); ok &&
			(typ == ast.AccountTypeIncome || typ == ast.AccountTypeExpenses) {
			target, ok := accounts[earnings]
			if !ok {
				target = NewInventory()
				accounts[earnings] = target
			}
			target.AddInventory(inventory)
			delete(accounts, account)
		}
	}

	openingDate := &ast.Date{Time: openDate.AddDate(0, 0, -1)}
	opening := equityAccount(qctx, "Opening-Balances")
	var txns []ast.Directive
	for _, account := range sortedAccounts(accounts) {
		inventory := accounts[account]
		if inventory.IsEmpty() {
			continue
		}
		narration := fmt.Sprintf("Opening balance for '%s' (Summarization)", account)
		txns = append(txns, balanceTransaction(openingDate, narration, "S", account, opening, inventory, false))
	}

	result := make([]ast.Directive, 0, len(txns)+len(kept))
	result = append(result, txns...)
	result = append(result, kept...)
	return result
}

// closeTransform truncates the stream at the close date, keeping entries
// strictly before it.
func closeTransform(entries []ast.Directive, closeDate *ast.Date) []ast.Directive {
	var kept []ast.Directive
	for _, entry := range entries {
		if entry.Date().Before(closeDate.Time) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// clearTransform appends T-flagged transactions at the last entry date that
// transfer every income and expenses balance to Equity:Earnings:Current.
func clearTransform(qctx *Context, entries []ast.Directive) []ast.Directive {
	if len(entries) == 0 {
		return entries
	}

	accounts := make(map[string]*Inventory)
	var lastDate time.Time
	for _, entry := range entries {
		if entry.Date().After(lastDate) {
			lastDate = entry.Date().Time
		}
		if txn, ok := entry.(*ast.Transaction); ok {
			bookTransaction(accounts, txn)
		}
	}

	earnings := equityAccount(qctx, "Earnings:Current")
	transferDate := &ast.Date{Time: lastDate}
	result := entries
	for _, account := range sortedAccounts(accounts) {
		typ, ok := accountType(qctx, account)
		if !ok || (typ != ast.AccountTypeIncome && typ != ast.AccountTypeExpenses) {
			continue
		}
		inventory := accounts[account]
		if inventory.IsEmpty() {
			continue
		}
		narration := fmt.Sprintf("Transfer balance for '%s' (Transfer balance)", account)
		result = append(result, balanceTransaction(transferDate, narration, "T", account, earnings, inventory, true))
	}
	return result
}

// bookTransaction books a transaction's postings into the per-account
// inventories with the same lot-date semantics as row generation.
func bookTransaction(accounts map[string]*Inventory, txn *ast.Transaction) {
	for _, posting := range txn.Postings {
		inventory, ok := accounts[string(posting.Account)]
		if !ok {
			inventory = NewInventory()
			accounts[string(posting.Account)] = inventory
		}
		position := postingPosition(posting, txn.Date())
		if position == nil {
			continue
		}
		if position.Cost != nil && (posting.Cost == nil || posting.Cost.Date == nil) {
			if inherited := inventory.matchLotDate(position); inherited != nil {
				position.Cost.Date = inherited
			}
		}
		inventory.AddPosition(position)
	}
}

// balanceTransaction builds a synthetic two-legged transaction moving an
// account's inventory to (or from) an equity account. When negate is set the
// account leg carries the negated balance (transfers); otherwise the account
// leg restates the balance (opening balances). The equity legs are the cost
// value of the opposite side, one posting per currency.
func balanceTransaction(date *ast.Date, narration, flag, account, equity string, inventory *Inventory, negate bool) *ast.Transaction {
	var postings []*ast.Posting
	equityTotals := make(map[string]decimal.Decimal)
	var equityOrder []string

	for _, p := range inventory.Positions() {
		units := p.Units.Number
		if negate {
			units = units.Neg()
		}
		opts := []ast.PostingOption{ast.WithAmount(numberString(units), p.Units.Currency)}
		if p.Cost != nil {
			cost := ast.NewCostWithDate(
				ast.NewAmount(numberString(p.Cost.Number), p.Cost.Currency), p.Cost.Date)
			cost.Label = p.Cost.Label
			opts = append(opts, ast.WithCost(cost))
		}
		postings = append(postings, ast.NewPosting(ast.Account(account), opts...))

		value := positionCost(p)
		number := value.Number
		if !negate {
			number = number.Neg()
		}
		if _, ok := equityTotals[value.Currency]; !ok {
			equityOrder = append(equityOrder, value.Currency)
		}
		equityTotals[value.Currency] = equityTotals[value.Currency].Add(number)
	}

	for _, currency := range equityOrder {
		postings = append(postings, ast.NewPosting(ast.Account(equity),
			ast.WithAmount(numberString(equityTotals[currency]), currency)))
	}

	return ast.NewTransaction(date, narration, ast.WithFlag(flag), ast.WithPostings(postings...))
}

func sortedAccounts(accounts map[string]*Inventory) []string {
	names := make([]string, 0, len(accounts))
	for name := range accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// equityAccount joins the configured equity root with a sub-account name.
func equityAccount(qctx *Context, sub string) string {
	root := "Equity"
	if qctx != nil && qctx.Config != nil && qctx.Config.AccountNames != nil {
		root = qctx.Config.AccountNames.Equity
	}
	return root + ":" + sub
}

// numberString renders a decimal preserving its scale.
func numberString(d decimal.Decimal) string {
	return d.StringFixed(max(-d.Exponent(), 0))
}
