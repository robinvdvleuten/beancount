package ledger

import (
	"context"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/telemetry"
	"github.com/shopspring/decimal"
)

// Plugin is a Built-in Plugin: it rewrites the booked ledger's directives
// before validation and returns the errors to report.
type Plugin func(ctx context.Context, l *Ledger, tree *ast.AST) []error

// pluginRegistry maps a plugin directive's module name to the Built-in
// Plugin that reproduces it.
var pluginRegistry = map[string]Plugin{
	"beancount.plugins.auto_accounts":   autoAccounts,
	"beancount.plugins.implicit_prices": implicitPrices,
}

// ignoredPlugins are the other plugin modules beancount v2 ships: they
// import, so naming one is not an error, but they do not run here.
var ignoredPlugins = map[string]bool{
	"beancount.plugins.auto":               true,
	"beancount.plugins.book_conversions":   true,
	"beancount.plugins.check_average_cost": true,
	"beancount.plugins.check_closing":      true,
	"beancount.plugins.check_commodity":    true,
	"beancount.plugins.check_drained":      true,
	"beancount.plugins.close_tree":         true,
	"beancount.plugins.coherent_cost":      true,
	"beancount.plugins.commodity_attr":     true,
	"beancount.plugins.currency_accounts":  true,
	"beancount.plugins.divert_expenses":    true,
	"beancount.plugins.exclude_tag":        true,
	"beancount.plugins.fill_account":       true,
	"beancount.plugins.fix_payees":         true,
	"beancount.plugins.forecast":           true,
	"beancount.plugins.ira_contribs":       true,
	"beancount.plugins.leafonly":           true,
	"beancount.plugins.mark_unverified":    true,
	"beancount.plugins.merge_meta":         true,
	"beancount.plugins.noduplicates":       true,
	"beancount.plugins.nounused":           true,
	"beancount.plugins.onecommodity":       true,
	"beancount.plugins.pedantic":           true,
	"beancount.plugins.sellgains":          true,
	"beancount.plugins.split_expenses":     true,
	"beancount.plugins.tag_pending":        true,
	"beancount.plugins.unique_prices":      true,
	"beancount.plugins.unrealized":         true,
}

// runPlugins runs the Built-in Plugins named by plugin directives, in the
// order the directives appear. A name under beancount.plugins that v2 does
// not ship fails to import there, and is reported; other names are
// ignored, since a module outside beancount may import.
func (l *Ledger) runPlugins(ctx context.Context, tree *ast.AST) {
	for _, directive := range tree.Plugins {
		name := directive.Name.String()
		plugin, ok := pluginRegistry[name]
		if !ok {
			if strings.HasPrefix(name, "beancount.plugins.") && !ignoredPlugins[name] {
				l.errors = append(l.errors, NewPluginImportError(directive))
			}
			continue
		}
		// Neither Built-in Plugin takes a configuration; beancount fails
		// to apply one that is given it.
		if !directive.Config.IsEmpty() {
			l.errors = append(l.errors, NewPluginConfigError(directive))
			continue
		}

		timer := telemetry.FromContext(ctx).Start("ledger.plugin " + name)
		l.errors = append(l.errors, plugin(ctx, l, tree)...)
		timer.End()
	}
}

// autoAccounts is beancount.plugins.auto_accounts: it opens every account a
// directive uses without an open directive, dated at its first use.
func autoAccounts(ctx context.Context, l *Ledger, tree *ast.AST) []error {
	opened := make(map[ast.Account]bool)
	for _, directive := range tree.Directives {
		if open, ok := directive.(*ast.Open); ok {
			opened[open.Account] = true
		}
	}

	firstUse := make(map[ast.Account]*ast.Date)
	for _, directive := range tree.Directives {
		used, ok := directive.(ast.WithAccounts)
		if !ok {
			continue
		}
		for _, account := range used.Accounts() {
			if _, seen := firstUse[account]; !seen && !opened[account] {
				firstUse[account] = directive.Date()
			}
		}
	}
	if len(firstUse) == 0 {
		return nil
	}

	// Like beancount, the inserted opens are numbered in account order.
	accounts := make([]ast.Account, 0, len(firstUse))
	for account := range firstUse {
		accounts = append(accounts, account)
	}
	slices.Sort(accounts)
	for i, account := range accounts {
		open := ast.NewOpen(firstUse[account], account, nil, "")
		open.SetPosition(ast.Position{Filename: "<auto_accounts>", Line: i})
		tree.Directives = append(tree.Directives, open)
	}
	_ = ast.SortDirectives(tree)
	return nil
}

// implicitPricesMeta marks a price inserted by implicit_prices, as
// beancount does.
const implicitPricesMeta = "__implicit_prices__"

// implicitPrices is beancount.plugins.implicit_prices: it inserts a price
// directive after each transaction for every posting with a price, and for
// every posting held at cost that does not reduce a lot. A price repeating
// another's date, commodity and amount is inserted once.
func implicitPrices(ctx context.Context, l *Ledger, tree *ast.AST) []error {
	type priceKey struct {
		date      string
		commodity string
		number    string
		currency  string
	}
	seen := make(map[priceKey]bool)
	// Like beancount, a posting at cost reduces when its account holds
	// the same lot with the opposite sign.
	held := make(map[string]map[lotKey]decimal.Decimal)

	directives := make(ast.Directives, 0, len(tree.Directives))
	for _, directive := range tree.Directives {
		directives = append(directives, directive)
		txn, ok := directive.(*ast.Transaction)
		if !ok {
			continue
		}

		for _, posting := range txn.Postings {
			units, err := ParseAmount(posting.Amount)
			if posting.Amount == nil || err != nil {
				continue
			}

			reduced := false
			if posting.Cost != nil {
				lots := held[string(posting.Account)]
				if lots == nil {
					lots = make(map[lotKey]decimal.Decimal)
					held[string(posting.Account)] = lots
				}
				reduced = trackLots(lots, l.BookedLots(posting), posting, units, txn.Date())
			}

			var price *ast.Price
			switch {
			case posting.Price != nil:
				number, currency, ok := PerUnitPrice(posting)
				if !ok {
					continue
				}
				price = newImplicitPrice(txn, posting, number, currency, "from_price")
			case posting.Cost != nil && !reduced:
				perUnit, currency, ok := PerUnitCost(posting)
				if !ok {
					continue
				}
				price = newImplicitPrice(txn, posting, perUnit, currency, "from_cost")
			default:
				continue
			}

			key := priceKey{txn.Date().String(), price.Commodity, priceNumber(price), price.Amount.Currency}
			if seen[key] {
				continue
			}
			seen[key] = true
			directives = append(directives, price)
		}
	}
	tree.Directives = directives
	return nil
}

func newImplicitPrice(txn *ast.Transaction, posting *ast.Posting, number decimal.Decimal, currency, source string) *ast.Price {
	price := ast.NewPrice(txn.Date(), posting.Amount.Currency, ast.NewAmount(number.String(), currency))
	price.Metadata = []*ast.Metadata{ast.NewMetadata(implicitPricesMeta, source)}
	price.SetPosition(txn.Position())
	return price
}

func priceNumber(price *ast.Price) string {
	number, err := ParseAmount(price.Amount)
	if err != nil {
		return price.Amount.Value
	}
	return number.String()
}

// lotKey identifies a lot the way beancount's Inventory does: by commodity
// and cost.
type lotKey struct {
	commodity    string
	cost         string
	costCurrency string
	date         string
	label        string
}

// trackLots adds a posting held at cost to its account's lots and reports
// whether it reduced one, like beancount's Inventory.add_position. A
// reduction that Booking matched to lots reduces each of them.
func trackLots(lots map[lotKey]decimal.Decimal, booked []BookedLot, posting *ast.Posting, units decimal.Decimal, date *ast.Date) bool {
	if len(booked) > 0 {
		for _, lot := range booked {
			key := lotKey{commodity: posting.Amount.Currency, costCurrency: lot.CostCurrency, label: lot.Label}
			if lot.Cost != nil {
				key.cost = lot.Cost.String()
			}
			if lot.Date != nil {
				key.date = lot.Date.String()
			}
			addToLot(lots, key, lot.Units)
		}
		return true
	}

	perUnit, costCurrency, ok := PerUnitCost(posting)
	if !ok {
		return false
	}
	key := lotKey{commodity: posting.Amount.Currency, cost: perUnit.String(), costCurrency: costCurrency, date: date.String()}
	if spec, err := ParseLotSpec(posting.Cost); err == nil {
		if spec.Date != nil {
			key.date = spec.Date.String()
		}
		key.label = spec.Label
	}
	return addToLot(lots, key, units)
}

// addToLot adds units to a lot and reports whether they reduced it.
func addToLot(lots map[lotKey]decimal.Decimal, key lotKey, units decimal.Decimal) bool {
	held, ok := lots[key]
	sum := pydecimal.Add(held, units)
	if sum.IsZero() {
		delete(lots, key)
	} else {
		lots[key] = sum
	}
	return ok && held.Sign() != units.Sign()
}
