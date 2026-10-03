package ledger

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// BalanceTree represents a hierarchical view of account balances.
// Used for generating balance sheets, income statements, and trial balances.
//
// The tree is organized by account type (Assets, Liabilities, etc.) with
// each type serving as a virtual root node. Balances are aggregated bottom-up
// so parent nodes include the sum of all their descendants.
type BalanceTree struct {
	// Roots contains the top-level nodes (account type roots like "Assets", "Liabilities").
	// Each root's Balance is the aggregated total of all accounts under that type.
	Roots []*BalanceNode

	// Currencies lists all currencies present in the tree, sorted alphabetically.
	Currencies []string

	// StartDate and EndDate bound the period of the balance calculation, both
	// inclusive; a nil bound leaves that side open. With only EndDate, this is
	// the balance as of that date (balance sheet). When both are nil, this
	// represents the current inventory state.
	StartDate *string
	EndDate   *string
}

// BalanceNode represents a single node in the balance tree hierarchy.
// Can be either an account type root (e.g., "Assets") or an actual account.
type BalanceNode struct {
	// Name is the display name for this node.
	// For account type roots: "Assets", "Liabilities", etc.
	// For accounts: the full account name like "Assets:US:Checking".
	Name string

	// Account is the full account path, empty for virtual root nodes.
	Account string

	// Depth indicates the nesting level (0 for roots, 1+ for accounts).
	Depth int

	// Balance is the aggregated balance for this node and all descendants.
	// For leaf accounts, this is the account's own balance.
	// For parent accounts and roots, this includes all children's balances.
	Balance *Balance

	// Children contains direct child nodes, sorted by name.
	Children []*BalanceNode
}

// GetBalanceTree returns a hierarchical view of account balances for reporting.
// It holds every account opened or posted to: an account posted to but
// never opened has no open date, and its postings count all the same.
//
// Parameters:
//   - types: Account types to include (e.g., Assets, Liabilities). Empty means all types (trial balance).
//   - startDate, endDate: Date range for balance calculation, both inclusive;
//     a nil bound leaves that side open.
//   - Both nil: Current inventory state (all postings).
//   - Only endDate: Balance as of endDate (balance sheet).
//   - Only startDate: Change from startDate on.
//   - Both set: Change within the period (income statement); a period of
//     one day, startDate == endDate, holds that day's postings.
//   - valuation: How each account's balance is stated. The positions the
//     account's postings in the period booked (BookedPositions) are summed
//     per lot and valued once, on endDate or else today, then summed per
//     currency.
//   - closed: Return Closed balances, a balance sheet's: Equity gains the
//     Current earnings and Current conversions accounts the
//     account_current_* options name and, At market value or Converted to
//     a currency, Unrealized gains (Equity:Earnings:Unrealized), so that
//     Assets, Liabilities and Equity sum to zero. A computed amount adds to
//     a real account of the same name, and a computed account that comes
//     out empty is left out. It needs a balance as of endDate (no
//     startDate) and types that leave out Income and Expenses.
//
// Returns an error if startDate > endDate, or for closed balances of a
// period or with Income or Expenses among the types.
//
// The tree is organized with account types as virtual root nodes. Balances are
// aggregated bottom-up so parent nodes include the sum of all their descendants.
func (l *Ledger) GetBalanceTree(types []ast.AccountType, startDate, endDate *ast.Date, valuation Valuation, closed bool) (*BalanceTree, error) {
	// Like beancount, a posting to an account never opened is reported and
	// still applied, so the account counts too, or the tree would not sum
	// to zero.
	accounts := make(map[string]*Account, len(l.accounts)+len(l.unopened))
	maps.Copy(accounts, l.accounts)
	maps.Copy(accounts, l.unopened)
	return l.newBalanceTree(accounts, l.config, types, startDate, endDate, valuation, closed)
}

// unrealizedGainsLeaf is the leaf, under the equity root, of the account
// Closed balances hold Unrealized gains in. Like fava's, it is fixed:
// beancount has no option naming it.
const unrealizedGainsLeaf = "Earnings:Unrealized"

// newBalanceTree builds the balance tree GetBalanceTree returns from the
// accounts, with the account-type roots cfg names.
func (l *Ledger) newBalanceTree(accounts map[string]*Account, cfg *sharedconfig.Config, types []ast.AccountType, startDate, endDate *ast.Date, valuation Valuation, closed bool) (*BalanceTree, error) {
	// Validate date range
	if startDate != nil && endDate != nil && startDate.After(endDate.Time) {
		return nil, fmt.Errorf("startDate %s is after endDate %s", startDate.String(), endDate.String())
	}
	if closed {
		if startDate != nil {
			return nil, fmt.Errorf("closed balances are a balance as of a date and take no startDate")
		}
		if len(types) == 0 || slices.Contains(types, ast.AccountTypeIncome) || slices.Contains(types, ast.AccountTypeExpenses) {
			return nil, fmt.Errorf("closed balances close %s and %s into %s, so they cannot include them",
				cfg.AccountNames.Income, cfg.AccountNames.Expenses, cfg.AccountNames.Equity)
		}
	}
	valuationDate := endDate
	if valuationDate == nil {
		valuationDate = today()
	}

	// Build type filter from enum to configured names
	typeFilter := make(map[string]bool)
	for _, t := range types {
		typeFilter[cfg.ToAccountTypeName(t)] = true
	}

	// Collect all accounts with their balances
	var entries []balanceTreeEntry
	var closing *closingBalances
	if closed {
		closing = newClosingBalances(cfg, endDate)
	}
	// In account order, so that the closing sums, rounded to 28 digits,
	// come out the same every time.
	for _, name := range slices.Sorted(maps.Keys(accounts)) {
		account := accounts[name]
		included := len(typeFilter) == 0 || typeFilter[account.Type]
		if !included && closing == nil {
			continue
		}

		positions := l.positionsBetween(account.postings, startDate, endDate)
		balance := l.valuePositions(positions, valuation, valuationDate)
		if closing != nil {
			closing.add(account, positions, balance)
		}
		if included {
			entries = append(entries, balanceTreeEntry{name: string(account.name), accountType: account.Type, balance: balance})
		}
	}
	if closing != nil {
		entries = l.closeEntries(entries, closing, typeFilter, valuation, valuationDate)
	}

	// Build sorted currency list
	currencySet := make(map[string]bool)
	for _, entry := range entries {
		for _, currency := range entry.balance.currencies() {
			currencySet[currency] = true
		}
	}
	currencies := make([]string, 0, len(currencySet))
	for currency := range currencySet {
		currencies = append(currencies, currency)
	}
	slices.Sort(currencies)

	// Build the tree structure
	tree := buildBalanceTree(cfg, entries, typeFilter)

	// Set metadata
	if startDate != nil {
		s := startDate.String()
		tree.StartDate = &s
	}
	if endDate != nil {
		e := endDate.String()
		tree.EndDate = &e
	}
	tree.Currencies = currencies

	return tree, nil
}

// closingBalances gathers, account by account in account order, what
// Closed balances add under Equity: the Current earnings positions, the
// postings Current conversions is computed from, and the valued total of
// the balance sheet's own accounts.
type closingBalances struct {
	cfg      *sharedconfig.Config
	end      *ast.Date                  // the date the balances are as of, nil for all
	earnings lotSums                    // Income and Expenses
	postings []*accountPosting          // every account's
	total    map[string]decimal.Decimal // Assets, Liabilities and Equity, valued
}

func newClosingBalances(cfg *sharedconfig.Config, end *ast.Date) *closingBalances {
	return &closingBalances{
		cfg:      cfg,
		end:      end,
		earnings: newLotSums(),
		total:    make(map[string]decimal.Decimal),
	}
}

// add takes in an account, its positions in the period and their valued
// balance. Like beancount's CLEAR, which transfers each account's balance
// in account order, Current earnings sums the accounts' positions in that
// order.
func (c *closingBalances) add(account *Account, positions []Position, valued *Balance) {
	c.postings = append(c.postings, account.postings...)
	switch account.Type {
	case c.cfg.AccountNames.Income, c.cfg.AccountNames.Expenses:
		for _, position := range positions {
			c.earnings.add(position)
		}
	default:
		c.addTotal(valued)
	}
}

func (c *closingBalances) addTotal(valued *Balance) {
	for _, entry := range valued.Entries() {
		c.total[entry.Currency] = pydecimal.Add(c.total[entry.Currency], entry.Amount)
	}
}

// conversions returns the Current conversions positions: the negated cost
// balance of every posting through the closing date. It sums as beancount's
// conversions does, so that 28-digit rounding gives its amount: the
// postings' positions per lot in the order the ledger applied them, then
// each lot's cost per currency in the order the lots first appear,
// negated once summed.
func (l *Ledger) conversions(closing *closingBalances) []Position {
	postings := closing.postings
	slices.SortFunc(postings, func(a, b *accountPosting) int { return cmp.Compare(a.seq, b.seq) })
	var currencies []string
	costs := make(map[string]decimal.Decimal)
	for _, position := range l.positionsBetween(postings, nil, closing.end) {
		cost := position.AtCost()
		sum, seen := costs[cost.Currency]
		if !seen {
			currencies = append(currencies, cost.Currency)
		}
		costs[cost.Currency] = pydecimal.Add(sum, cost.Amount)
	}
	positions := make([]Position, 0, len(currencies))
	for _, currency := range currencies {
		positions = append(positions, Position{Number: costs[currency].Neg(), Currency: currency})
	}
	return positions
}

// closeEntries adds Closed balances' computed accounts to entries: each
// merges into the entry of the same name or, unless it comes out empty,
// becomes an entry of its own. Unrealized gains, At market value or
// Converted to a currency only, is what leaves the valued Assets,
// Liabilities and Equity summing to zero, every other computed account
// included.
func (l *Ledger) closeEntries(entries []balanceTreeEntry, closing *closingBalances, typeFilter map[string]bool, v Valuation, date *ast.Date) []balanceTreeEntry {
	equity := closing.cfg.AccountNames.Equity
	add := func(name string, balance *Balance) {
		if balance.IsZero() {
			return
		}
		if !typeFilter[equity] {
			return
		}
		for i := range entries {
			if entries[i].name == name {
				merged := entries[i].balance.copy()
				merged.merge(balance)
				entries[i].balance = merged
				return
			}
		}
		entries = append(entries, balanceTreeEntry{name: name, accountType: equity, balance: balance})
	}

	earningsAccount, conversionsAccount := closing.cfg.CurrentAccounts()
	earnings := l.valuePositions(closing.earnings.positions, v, date)
	conversions := l.valuePositions(l.conversions(closing), v, date)
	closing.addTotal(earnings)
	closing.addTotal(conversions)
	add(earningsAccount, earnings)
	add(conversionsAccount, conversions)

	if v.kind == valuationAtMarket || v.kind == valuationConverted {
		unrealized := make(map[string]decimal.Decimal, len(closing.total))
		for currency, number := range closing.total {
			if !number.IsZero() {
				unrealized[currency] = number.Neg()
			}
		}
		add(equity+":"+unrealizedGainsLeaf, newBalanceFromMap(unrealized))
	}
	return entries
}

// buildBalanceTree constructs the hierarchical tree structure from account entries.
// balanceTreeEntry is used internally by GetBalanceTree.
type balanceTreeEntry struct {
	name        string
	accountType string // the account type root name
	balance     *Balance
}

func buildBalanceTree(cfg *sharedconfig.Config, entries []balanceTreeEntry, typeFilter map[string]bool) *BalanceTree {
	// Group accounts by type
	accountsByType := make(map[string][]balanceTreeEntry)
	for _, entry := range entries {
		accountsByType[entry.accountType] = append(accountsByType[entry.accountType], entry)
	}

	// Determine which types to include
	var typeOrder []ast.AccountType
	if len(typeFilter) > 0 {
		// Use filtered types in standard order
		for _, t := range []ast.AccountType{
			ast.AccountTypeAssets,
			ast.AccountTypeLiabilities,
			ast.AccountTypeEquity,
			ast.AccountTypeIncome,
			ast.AccountTypeExpenses,
		} {
			typeName := cfg.ToAccountTypeName(t)
			if typeFilter[typeName] {
				typeOrder = append(typeOrder, t)
			}
		}
	} else {
		// All types in standard order
		typeOrder = []ast.AccountType{
			ast.AccountTypeAssets,
			ast.AccountTypeLiabilities,
			ast.AccountTypeEquity,
			ast.AccountTypeIncome,
			ast.AccountTypeExpenses,
		}
	}

	// Build root nodes for each type
	var roots []*BalanceNode
	for _, accountType := range typeOrder {
		typeName := cfg.ToAccountTypeName(accountType)
		typeEntries := accountsByType[typeName]

		if len(typeEntries) == 0 {
			continue
		}

		// Build subtree for this type
		root := buildTypeSubtree(typeName, typeEntries)
		roots = append(roots, root)
	}

	return &BalanceTree{Roots: roots}
}

// buildTypeSubtree builds a subtree for a single account type.
func buildTypeSubtree(typeName string, entries []balanceTreeEntry) *BalanceNode {
	// Create a map of account name to node for quick lookup
	nodeMap := make(map[string]*BalanceNode)
	childSets := make(map[string]map[string]struct{})

	// Create leaf nodes for all accounts
	for _, entry := range entries {
		accountName := entry.name
		nodeMap[accountName] = &BalanceNode{
			Name:     accountName,
			Account:  accountName,
			Depth:    strings.Count(accountName, ":"),
			Balance:  entry.balance.copy(),
			Children: nil,
		}
	}

	// node returns the node for path, creating an intermediate one if needed.
	node := func(path string, depth int) *BalanceNode {
		if n, exists := nodeMap[path]; exists {
			return n
		}
		n := &BalanceNode{
			Name:     path,
			Account:  path,
			Depth:    depth,
			Balance:  newBalance(),
			Children: nil,
		}
		nodeMap[path] = n
		return n
	}

	// Link every parent-child pair along each account's path, creating
	// both ends first so an intermediate node is linked to its own parent
	// even when no other account passes through it.
	for _, entry := range entries {
		accountName := entry.name
		parts := strings.Split(accountName, ":")

		for i := 1; i < len(parts); i++ {
			parentPath := strings.Join(parts[:i], ":")
			childPath := strings.Join(parts[:i+1], ":")
			parent := node(parentPath, i-1)
			child := node(childPath, i)

			children := childSets[parentPath]
			if children == nil {
				children = make(map[string]struct{})
				childSets[parentPath] = children
			}
			if _, exists := children[childPath]; !exists {
				parent.Children = append(parent.Children, child)
				children[childPath] = struct{}{}
			}
		}
	}

	// Sort children at each level
	for _, node := range nodeMap {
		slices.SortFunc(node.Children, func(a, b *BalanceNode) int {
			if a.Name < b.Name {
				return -1
			}
			if a.Name > b.Name {
				return 1
			}
			return 0
		})
	}

	// Aggregate balances bottom-up using post-order traversal
	var aggregate func(node *BalanceNode)
	aggregate = func(node *BalanceNode) {
		for _, child := range node.Children {
			aggregate(child)
			node.Balance.merge(child.Balance)
		}
	}

	// Create the type root node
	root := &BalanceNode{
		Name:     typeName,
		Account:  "", // Virtual root, not an actual account
		Depth:    0,
		Balance:  newBalance(),
		Children: nil,
	}

	// Find direct children of the type root (depth 1 nodes)
	for name, node := range nodeMap {
		if node.Depth == 1 && strings.HasPrefix(name, typeName+":") {
			root.Children = append(root.Children, node)
		}
	}

	// Sort root's children
	slices.SortFunc(root.Children, func(a, b *BalanceNode) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})

	// Aggregate balances from children to root
	for _, child := range root.Children {
		aggregate(child)
		root.Balance.merge(child.Balance)
	}

	return root
}
