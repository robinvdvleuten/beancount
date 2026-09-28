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
func applyFromTransforms(qctx *Context, entries []ast.Directive, from *CompiledFrom) []ast.Directive {
	if from.OpenOn != nil {
		entries = openTransform(qctx, entries, from.OpenOn)
	}
	if from.CloseOn != nil || from.Close {
		entries = closeTransform(qctx, entries, from.CloseOn)
	}
	if from.Clear {
		entries = clearTransform(qctx, entries)
	}
	return entries
}

// openTransform summarizes all transactions before the open date: their
// conversion entry books into Equity:Conversions:Previous, income and
// expenses balances collapse into Equity:Earnings:Previous, and every
// balance-sheet account's inventory becomes an S-flagged opening transaction
// at the day before the open date, posted against Equity:Opening-Balances.
// Of the other directives before the open date only the active opens and
// the last prices stay, sorted in among the opening transactions.
func openTransform(qctx *Context, entries []ast.Directive, openDate *ast.Date) []ast.Directive {
	var before, after []ast.Directive
	accounts := make(map[string]*Inventory)

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

	openingDate := &ast.Date{Time: openDate.AddDate(0, 0, -1)}
	if conversion := conversionTransaction(qctx, before, openingDate, equityAccount(qctx, "Conversions:Previous")); conversion != nil {
		bookTransaction(qctx, accounts, conversion)
	}

	// Collapse income and expenses into the previous-earnings account, like
	// beancount's transfer entries: accounts in sorted order, each position
	// adding its cost value, so the earnings positions keep beancount's order.
	earnings := equityAccount(qctx, "Earnings:Previous")
	for _, account := range sortedAccounts(accounts) {
		typ, ok := accountType(qctx, account)
		if !ok || (typ != ast.AccountTypeIncome && typ != ast.AccountTypeExpenses) {
			continue
		}
		target, ok := accounts[earnings]
		if !ok {
			target = NewInventory()
			accounts[earnings] = target
		}
		for _, p := range accounts[account].Positions() {
			target.AddAmount(positionCost(p))
		}
		delete(accounts, account)
	}

	opening := equityAccount(qctx, "Opening-Balances")
	summary := append(activeOpens(before), lastPrices(before)...)
	for _, account := range sortedAccounts(accounts) {
		inventory := accounts[account]
		if inventory.IsEmpty() {
			continue
		}
		narration := fmt.Sprintf("Opening balance for '%s' (Summarization)", account)
		summary = append(summary, balanceTransaction(openingDate, narration, "S", account, opening, inventory, false))
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
// strictly before it, and appends the conversion entry for what is left:
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
	if conversion := conversionTransaction(qctx, kept, date, equityAccount(qctx, "Conversions:Current")); conversion != nil {
		kept = append(kept[:len(kept):len(kept)], conversion)
	}
	return kept
}

// conversionTransaction builds beancount's conversion entry for entries: a
// C-flagged transaction at date posting the negated cost balance of their
// transactions to the conversions account. Entries only leave a cost balance
// when a price converts between currencies; each leg is priced at zero in
// the conversion currency so the entry still balances. It returns nil when
// the cost balance is empty.
func conversionTransaction(qctx *Context, entries []ast.Directive, date *ast.Date, account string) *ast.Transaction {
	balance := NewInventory()
	for _, entry := range entries {
		txn, ok := entry.(*ast.Transaction)
		if !ok {
			continue
		}
		for _, posting := range txn.Postings {
			for _, position := range postingPositions(qctx, posting, txn.Date()) {
				balance.AddPosition(position)
			}
		}
	}

	costs := NewInventory()
	for _, p := range balance.Positions() {
		costs.AddAmount(positionCost(p))
	}
	if costs.IsEmpty() {
		return nil
	}

	price := ast.NewAmount("0", conversionCurrency)
	var postings []*ast.Posting
	for _, p := range costs.Positions() {
		postings = append(postings, ast.NewPosting(ast.Account(account),
			ast.WithAmount(numberString(p.Units.Number.Neg()), p.Units.Currency),
			ast.WithPrice(price)))
	}
	narration := "Conversion for " + objectString(balance)
	return ast.NewTransaction(date, narration, ast.WithFlag("C"), ast.WithPostings(postings...))
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
			bookTransaction(qctx, accounts, txn)
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
// inventories, using the same booked positions as row generation.
func bookTransaction(qctx *Context, accounts map[string]*Inventory, txn *ast.Transaction) {
	for _, posting := range txn.Postings {
		inventory, ok := accounts[string(posting.Account)]
		if !ok {
			inventory = NewInventory()
			accounts[string(posting.Account)] = inventory
		}
		for _, position := range postingPositions(qctx, posting, txn.Date()) {
			inventory.AddPosition(position)
		}
	}
}

// balanceTransaction builds a synthetic transaction moving an account's
// inventory to (or from) an equity account, like beancount's
// create_entries_from_balances: every position gets an account leg followed
// directly by an equity leg for that position's cost value. When negate is
// set the account legs carry the negated balance (transfers); otherwise they
// restate it (opening balances).
func balanceTransaction(date *ast.Date, narration, flag, account, equity string, inventory *Inventory, negate bool) *ast.Transaction {
	positions := inventory.Positions()
	postings := make([]*ast.Posting, 0, 2*len(positions))

	for _, p := range positions {
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
		postings = append(postings, ast.NewPosting(ast.Account(equity),
			ast.WithAmount(numberString(number), value.Currency)))
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

// conversionCurrency is beancount's default conversion_currency option, the
// imaginary currency pricing conversion entries at zero.
const conversionCurrency = "NOTHING"

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
