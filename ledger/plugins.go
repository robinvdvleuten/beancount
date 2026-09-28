package ledger

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
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

	uses := firstUses(tree.Directives, opened)
	if len(uses) == 0 {
		return nil
	}

	// Like beancount, the inserted opens are numbered in account order.
	slices.SortFunc(uses, func(a, b accountUse) int { return cmp.Compare(a.account, b.account) })
	for i, use := range uses {
		open := ast.NewOpen(use.date, use.account, nil, "")
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
// every position a posting books at cost without reducing a lot, which
// Booking's record tells (BookedPosition.Reduced). A price repeating
// another's date, commodity and amount is inserted once.
func implicitPrices(ctx context.Context, l *Ledger, tree *ast.AST) []error {
	type priceKey struct {
		date      string
		commodity string
		number    string
		currency  string
	}
	seen := make(map[priceKey]bool)

	directives := make(ast.Directives, 0, len(tree.Directives))
	insert := func(price *ast.Price) {
		key := priceKey{price.Date().String(), price.Commodity, priceNumber(price), price.Amount.Currency}
		if !seen[key] {
			seen[key] = true
			directives = append(directives, price)
		}
	}
	for _, directive := range tree.Directives {
		directives = append(directives, directive)
		txn, ok := directive.(*ast.Transaction)
		if !ok {
			continue
		}

		for _, posting := range txn.Postings {
			if posting.Amount == nil {
				continue
			}
			if posting.Price != nil {
				if number, currency, ok := PerUnitPrice(posting); ok {
					insert(newImplicitPrice(txn, posting, number, currency, "from_price"))
				}
				continue
			}
			for _, position := range l.BookedPositions(posting) {
				if position.Cost != nil && !position.Reduced {
					insert(newImplicitPrice(txn, posting, position.Cost.Number, position.Cost.Currency, "from_cost"))
				}
			}
		}
	}
	tree.Directives = directives
	return nil
}

func newImplicitPrice(txn *ast.Transaction, posting *ast.Posting, number decimal.Decimal, currency, source string) *ast.Price {
	price := ast.NewPrice(txn.Date(), posting.Amount.Currency, ast.NewAmount(formatInferredNumber(number), currency))
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
