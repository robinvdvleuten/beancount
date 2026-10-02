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
// A bare CLOSE truncates nothing, but still adds the conversion entry.
// OPEN ON, CLOSE and CLEAR create transactions, and the returned context
// knows their postings' positions.
func applyFromTransforms(qctx *Context, entries []ast.Directive, from *compiledFrom) (*Context, []ast.Directive) {
	if from.OpenOn != nil || from.CloseOn != nil || from.Close || from.Clear {
		summarizing := *qctx
		summarizing.summarized = make(map[*ast.Posting]*positionValue)
		qctx = &summarizing
	}

	if from.OpenOn != nil {
		entries = openTransform(qctx, entries, from.OpenOn)
	}
	if from.CloseOn != nil || from.Close {
		entries = closeTransform(qctx, entries, from.CloseOn)
	}
	if from.Clear {
		entries = clearTransform(qctx, entries)
	}
	return qctx, entries
}

// openTransform summarizes all transactions before the open date: their
// conversion entry books into the previous-conversions account, income and
// expenses balances collapse into the previous-earnings account, and every
// balance-sheet account's inventory becomes an S-flagged opening transaction
// at the day before the open date, posted against the previous-balances
// account (Config.PreviousAccounts, Equity:Opening-Balances by default).
// Of the other directives before the open date only the active opens and
// the last prices stay, sorted in among the opening transactions.
func openTransform(qctx *Context, entries []ast.Directive, openDate *ast.Date) []ast.Directive {
	var before, after []ast.Directive
	accounts := make(map[string]*inventoryValue)

	for _, entry := range entries {
		if !entry.Date().Before(openDate.Time) {
			after = append(after, entry)
			continue
		}
		before = append(before, entry)
		if txn, ok := entry.(*ast.Transaction); ok {
			bookTransaction(qctx, accounts, txn)
		}
	}

	earnings, opening, conversions := qctx.config().PreviousAccounts()
	openingDate := &ast.Date{Time: openDate.AddDate(0, 0, -1)}
	if conversion := conversionTransaction(qctx, before, openingDate, conversions); conversion != nil {
		bookTransaction(qctx, accounts, conversion)
	}

	// Collapse income and expenses into the previous-earnings account, like
	// beancount's transfer entries: accounts in sorted order, each position
	// adding its cost value, so the earnings positions keep beancount's order.
	for _, account := range sortedAccounts(accounts) {
		typ, ok := accountType(qctx, account)
		if !ok || (typ != ast.AccountTypeIncome && typ != ast.AccountTypeExpenses) {
			continue
		}
		target, ok := accounts[earnings]
		if !ok {
			target = newInventory()
			accounts[earnings] = target
		}
		for _, p := range accounts[account].Positions() {
			target.AddAmount(positionCost(p))
		}
		delete(accounts, account)
	}

	summary := append(activeOpens(before), lastPrices(before)...)
	for _, account := range sortedAccounts(accounts) {
		inventory := accounts[account]
		if inventory.IsEmpty() {
			continue
		}
		narration := fmt.Sprintf("Opening balance for '%s' (Summarization)", account)
		summary = append(summary, balanceTransaction(qctx, openingDate, narration, "S", account, opening, inventory, false, "<summarize>"))
	}
	// Like beancount's entry_sortkey: the opening transactions have no
	// source line, so they sort before a price on the same date.
	sort.Stable(ast.Directives(summary))

	return append(summary, after...)
}

// activeOpens returns the open directives of the accounts still open after
// entries, in stream order, like beancount's get_open_entries: an account's
// earliest open counts, and a close drops it.
func activeOpens(entries []ast.Directive) []ast.Directive {
	type indexedOpen struct {
		index int
		open  *ast.Open
	}
	opens := make(map[ast.Account]indexedOpen)
	for i, entry := range entries {
		switch d := entry.(type) {
		case *ast.Open:
			if existing, ok := opens[d.Account]; !ok || d.Date().Before(existing.open.Date().Time) {
				opens[d.Account] = indexedOpen{i, d}
			}
		case *ast.Close:
			delete(opens, d.Account)
		}
	}

	active := make([]indexedOpen, 0, len(opens))
	for _, open := range opens {
		active = append(active, open)
	}
	sort.Slice(active, func(i, j int) bool { return active[i].index < active[j].index })
	result := make([]ast.Directive, len(active))
	for i, open := range active {
		result[i] = open.open
	}
	return result
}

// lastPrices returns the last price directive per (commodity, quote
// currency) pair in entries, like beancount's get_last_price_entries: in the
// order each pair first appeared, which a stable sort keeps among prices on
// the same date and line.
func lastPrices(entries []ast.Directive) []ast.Directive {
	type pair struct{ commodity, currency string }
	index := make(map[pair]int)
	var prices []ast.Directive
	for _, entry := range entries {
		price, ok := entry.(*ast.Price)
		if !ok || price.Amount == nil {
			continue
		}
		key := pair{price.Commodity, price.Amount.Currency}
		if i, ok := index[key]; ok {
			prices[i] = price
			continue
		}
		index[key] = len(prices)
		prices = append(prices, price)
	}
	return prices
}

// closeTransform truncates the stream at the close date, keeping entries
// strictly before it, and appends the conversion entry for what is left,
// into the current-conversions account (Config.CurrentAccounts):
// at the day before the close date, or at the last entry's date for a bare
// CLOSE (nil closeDate).
func closeTransform(qctx *Context, entries []ast.Directive, closeDate *ast.Date) []ast.Directive {
	kept := entries
	if closeDate != nil {
		kept = nil
		for _, entry := range entries {
			if entry.Date().Before(closeDate.Time) {
				kept = append(kept, entry)
			}
		}
	}
	if len(kept) == 0 {
		return kept
	}

	date := kept[len(kept)-1].Date()
	if closeDate != nil {
		date = &ast.Date{Time: closeDate.AddDate(0, 0, -1)}
	}
	_, conversions := qctx.config().CurrentAccounts()
	if conversion := conversionTransaction(qctx, kept, date, conversions); conversion != nil {
		kept = append(kept[:len(kept):len(kept)], conversion)
	}
	return kept
}

// conversionTransaction builds beancount's conversion entry for entries: a
// C-flagged transaction at date posting the negated cost balance of their
// transactions to the conversions account. Entries only leave a cost balance
// when a price converts between currencies; each leg is priced at zero in
// the conversion currency so the entry still balances. It records each
// leg's position in qctx and returns nil when the cost balance is empty.
func conversionTransaction(qctx *Context, entries []ast.Directive, date *ast.Date, account string) *ast.Transaction {
	balance := newInventory()
	for _, entry := range entries {
		txn, ok := entry.(*ast.Transaction)
		if !ok {
			continue
		}
		for _, posting := range txn.Postings {
			for _, position := range postingPositions(qctx, posting) {
				balance.AddPosition(position)
			}
		}
	}

	costs := newInventory()
	for _, p := range balance.Positions() {
		costs.AddAmount(positionCost(p))
	}
	if costs.IsEmpty() {
		return nil
	}

	price := ast.NewAmount("0", conversionCurrency)
	var postings []*ast.Posting
	for _, p := range costs.Positions() {
		units := amountValue{Number: p.Units.Number.Neg(), Currency: p.Units.Currency}
		leg := ast.NewPosting(ast.Account(account),
			ast.WithAmount(numberString(units.Number), units.Currency),
			ast.WithPrice(price))
		qctx.summarized[leg] = &positionValue{Units: units}
		postings = append(postings, leg)
	}
	narration := "Conversion for " + objectString(balance)
	txn := ast.NewTransaction(date, narration, ast.WithFlag("C"), ast.WithPostings(postings...))
	// Like beancount's, the conversion entry's meta names "<conversions>".
	txn.SetPosition(ast.Position{Filename: "<conversions>", Line: -1})
	return txn
}

// clearTransform appends T-flagged transactions at the last entry date that
// transfer every income and expenses balance to the current-earnings
// account (Config.CurrentAccounts, Equity:Earnings:Current by default).
func clearTransform(qctx *Context, entries []ast.Directive) []ast.Directive {
	if len(entries) == 0 {
		return entries
	}

	accounts := make(map[string]*inventoryValue)
	var lastDate time.Time
	for _, entry := range entries {
		if entry.Date().After(lastDate) {
			lastDate = entry.Date().Time
		}
		if txn, ok := entry.(*ast.Transaction); ok {
			bookTransaction(qctx, accounts, txn)
		}
	}

	earnings, _ := qctx.config().CurrentAccounts()
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
		result = append(result, balanceTransaction(qctx, transferDate, narration, "T", account, earnings, inventory, true, "<transfer_balances>"))
	}
	return result
}

// bookTransaction books a transaction's postings into the per-account
// inventories, using the same booked positions as row generation.
func bookTransaction(qctx *Context, accounts map[string]*inventoryValue, txn *ast.Transaction) {
	for _, posting := range txn.Postings {
		inventory, ok := accounts[string(posting.Account)]
		if !ok {
			inventory = newInventory()
			accounts[string(posting.Account)] = inventory
		}
		for _, position := range postingPositions(qctx, posting) {
			inventory.AddPosition(position)
		}
	}
}

// balanceTransaction builds a synthetic transaction moving an account's
// inventory to (or from) an equity account, like beancount's
// create_entries_from_balances: every position gets an account leg followed
// directly by an equity leg for that position's cost value. When negate is
// set the account legs carry the negated balance (transfers); otherwise they
// restate it (opening balances). It records each leg's position in qctx.
// filename is what the entry's meta names, as beancount's: "<summarize>"
// or "<transfer_balances>", at line 0.
func balanceTransaction(qctx *Context, date *ast.Date, narration, flag, account, equity string, inventory *inventoryValue, negate bool, filename string) *ast.Transaction {
	positions := inventory.Positions()
	postings := make([]*ast.Posting, 0, 2*len(positions))

	for _, p := range positions {
		units := p.Units
		if negate {
			units.Number = units.Number.Neg()
		}
		opts := []ast.PostingOption{ast.WithAmount(numberString(units.Number), units.Currency)}
		if p.Cost != nil {
			cost := ast.NewCostWithDate(
				ast.NewAmount(numberString(p.Cost.Number), p.Cost.Currency), p.Cost.Date)
			cost.Label = p.Cost.Label
			opts = append(opts, ast.WithCost(cost))
		}
		leg := ast.NewPosting(ast.Account(account), opts...)
		qctx.summarized[leg] = &positionValue{Units: units, Cost: p.Cost}
		postings = append(postings, leg)

		value := *positionCost(p)
		if !negate {
			value.Number = value.Number.Neg()
		}
		leg = ast.NewPosting(ast.Account(equity), ast.WithAmount(numberString(value.Number), value.Currency))
		qctx.summarized[leg] = &positionValue{Units: value}
		postings = append(postings, leg)
	}

	txn := ast.NewTransaction(date, narration, ast.WithFlag(flag), ast.WithPostings(postings...))
	txn.SetPosition(ast.Position{Filename: filename})
	return txn
}

func sortedAccounts(accounts map[string]*inventoryValue) []string {
	names := make([]string, 0, len(accounts))
	for name := range accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// conversionCurrency is beancount's default conversion_currency option, the
// imaginary currency pricing conversion entries at zero.
const conversionCurrency = "NOTHING"

// numberString renders a decimal preserving its scale.
func numberString(d decimal.Decimal) string {
	return d.StringFixed(max(-d.Exponent(), 0))
}
