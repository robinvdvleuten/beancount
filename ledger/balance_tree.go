package ledger

import (
	"fmt"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
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

	// StartDate and EndDate define the period for the balance calculation.
	// When StartDate == EndDate, this is a point-in-time balance (balance sheet).
	// When StartDate < EndDate, this is a period change (income statement).
	// When both are nil, this represents the current inventory state.
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
//
// Parameters:
//   - types: Account types to include (e.g., Assets, Liabilities). Empty means all types (trial balance).
//   - startDate, endDate: Date range for balance calculation.
//   - Both nil: Current inventory state (all postings).
//   - startDate == endDate: Point-in-time balance (balance sheet).
//   - startDate < endDate: Period change (income statement).
//
// Returns an error if only one date is provided or startDate > endDate.
//
// The tree is organized with account types as virtual root nodes. Balances are
// aggregated bottom-up so parent nodes include the sum of all their descendants.
func (l *Ledger) GetBalanceTree(types []ast.AccountType, startDate, endDate *ast.Date) (*BalanceTree, error) {
	return newBalanceTree(l.accounts, l.config, types, startDate, endDate)
}

// newBalanceTree builds the balance tree GetBalanceTree returns from the
// accounts, with the account-type roots cfg names.
func newBalanceTree(accounts map[string]*Account, cfg *Config, types []ast.AccountType, startDate, endDate *ast.Date) (*BalanceTree, error) {
	// Validate date range
	if (startDate == nil) != (endDate == nil) {
		return nil, fmt.Errorf("startDate and endDate must both be set or both be nil")
	}
	if startDate != nil && endDate != nil && startDate.After(endDate.Time) {
		return nil, fmt.Errorf("startDate %s is after endDate %s", startDate.String(), endDate.String())
	}

	// Build type filter from enum to configured names
	typeFilter := make(map[string]bool)
	for _, t := range types {
		typeFilter[cfg.ToAccountTypeName(t)] = true
	}

	// Collect all accounts with their balances
	var entries []balanceTreeEntry
	currencySet := make(map[string]bool)

	for _, account := range accounts {
		// Skip if type filter is set and account doesn't match
		if len(typeFilter) > 0 && !typeFilter[account.Type] {
			continue
		}

		// Calculate balance for the period
		var balance *Balance
		if startDate == nil && endDate == nil {
			// Current inventory state
			balance = currentBalance(account)
		} else {
			// Use GetBalanceInPeriod with the dates
			start := *startDate
			end := *endDate
			balance = account.GetBalanceInPeriod(start, end)
		}

		entries = append(entries, balanceTreeEntry{account: account, balance: balance})

		// Track currencies
		for _, currency := range balance.Currencies() {
			currencySet[currency] = true
		}
	}

	// Build sorted currency list
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

// currentBalance returns the current inventory balance for an account.
func currentBalance(account *Account) *Balance {
	if account.Inventory == nil {
		return NewBalance()
	}

	balance := NewBalance()
	for _, currency := range account.Inventory.Currencies() {
		balance.Set(currency, account.Inventory.Get(currency))
	}
	return balance
}

// buildBalanceTree constructs the hierarchical tree structure from account entries.
// balanceTreeEntry is used internally by GetBalanceTree.
type balanceTreeEntry struct {
	account *Account
	balance *Balance
}

func buildBalanceTree(cfg *Config, entries []balanceTreeEntry, typeFilter map[string]bool) *BalanceTree {
	// Group accounts by type
	accountsByType := make(map[string][]balanceTreeEntry)
	for _, entry := range entries {
		accountsByType[entry.account.Type] = append(accountsByType[entry.account.Type], entry)
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
		accountName := string(entry.account.Name)
		nodeMap[accountName] = &BalanceNode{
			Name:     accountName,
			Account:  accountName,
			Depth:    strings.Count(accountName, ":"),
			Balance:  entry.balance.Copy(),
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
			Balance:  NewBalance(),
			Children: nil,
		}
		nodeMap[path] = n
		return n
	}

	// Link every parent-child pair along each account's path, creating
	// both ends first so an intermediate node is linked to its own parent
	// even when no other account passes through it.
	for _, entry := range entries {
		accountName := string(entry.account.Name)
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
			node.Balance.Merge(child.Balance)
		}
	}

	// Create the type root node
	root := &BalanceNode{
		Name:     typeName,
		Account:  "", // Virtual root, not an actual account
		Depth:    0,
		Balance:  NewBalance(),
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
		root.Balance.Merge(child.Balance)
	}

	return root
}
