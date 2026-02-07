// Package ledger provides accounting ledger validation and processing for Beancount files.
// It validates transactions, maintains account states, tracks inventory with lot-based cost
// basis, and performs balance assertions.
//
// The ledger validates that:
//   - All transactions balance to zero across all currencies
//   - Accounts are opened before use and closed accounts are not used
//   - Balance assertions match actual inventory balances
//   - Pad directives correctly balance accounts
//
// The ledger tracks inventory using lot-based accounting with support for different booking
// methods (FIFO, LIFO). It uses decimal arithmetic for all monetary amounts to avoid floating
// point precision issues.
//
// Example usage:
//
//	// Parse a Beancount file
//	ast, err := parser.ParseBytes([]byte(source))
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create and process ledger
//	ledger := ledger.New()
//	err = ledger.Process(ast)
//	if err != nil {
//	    // Handle validation errors
//	    if verr, ok := err.(*ledger.ValidationErrors); ok {
//	        for _, e := range verr.Errors {
//	            fmt.Println(e)
//	        }
//	    }
//	}
package ledger

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
	"github.com/shopspring/decimal"
)

// Ledger represents the state of the accounting ledger with account balances,
// transaction validation, and error tracking. It processes directives in date order
// and maintains the complete state of all accounts including their inventory positions.
//
// The ledger is implemented as a unified graph where:
//   - Nodes represent accounts and currencies
//   - Edges represent prices (currency conversions) and account state changes
//   - Temporal queries use forward-fill semantics (most recent price on or before date)
//
// The ledger validates all transactions for balance, ensures accounts are opened before
// use, verifies balance assertions, and processes pad directives. All validation errors
// are collected and returned together after processing.
type Ledger struct {
	graph                 *Graph // Unified graph of accounts, currencies, and relationships
	config                *Config
	errors                []error
	padEntries            map[string]*ast.Pad // account -> pad directive
	usedPads              map[string]bool     // account -> whether pad was used
	syntheticTransactions []*ast.Transaction  // Padding transactions to insert into AST
}

// ValidationErrors wraps multiple validation errors
type ValidationErrors struct {
	Errors []error
}

func (e *ValidationErrors) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}

	// Show all errors plus summary
	var buf strings.Builder
	for i, err := range e.Errors {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(err.Error())
	}
	buf.WriteString(fmt.Sprintf("\n\n%d validation error(s) found", len(e.Errors)))
	return buf.String()
}

// Unwrap returns the underlying errors for error unwrapping
func (e *ValidationErrors) Unwrap() []error {
	return e.Errors
}

// New creates a new empty ledger
func New() *Ledger {
	return &Ledger{
		graph:      NewGraph(),
		errors:     make([]error, 0),
		padEntries: make(map[string]*ast.Pad),
		usedPads:   make(map[string]bool),
	}
}

// GetAccountTypeFromName converts an account type name to its enum value.
// Returns (0, false) if the name doesn't match any configured account type.
func (l *Ledger) GetAccountTypeFromName(name string) (ast.AccountType, bool) {
	cfg := l.config
	if cfg == nil {
		cfg = NewConfig()
	}
	return cfg.GetAccountTypeFromName(name)
}

// Process processes an AST and builds the ledger state
func (l *Ledger) Process(ctx context.Context, tree *ast.AST) error {
	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

	// Enrich AST with semantic information (currencies, accounts)
	enriched := tree.Enrich()

	// Pre-populate graph with currency nodes (they're not explicitly opened)
	// Account nodes are created by Open directives with full metadata
	for currency := range enriched.Currencies {
		l.graph.AddNode(currency, "currency", nil)
	}

	// Parse configuration from AST options
	cfg, err := configFromAST(tree)
	if err != nil {
		l.errors = append(l.errors, err)
		cfg = NewConfig() // Use defaults if parsing fails
	}
	l.config = cfg
	// Attach config to context for use throughout processing
	ctx = cfg.WithContext(ctx)

	// Process directives in order (they're already sorted by date)
	processTimer := collector.StartStructured(telemetry.TimerConfig{
		Name:  "ledger.processing",
		Count: len(tree.Directives),
		Unit:  "directives",
	})

	// Count transactions and create validation summary timer
	transactionCount := 0
	for _, directive := range tree.Directives {
		if _, ok := directive.(*ast.Transaction); ok {
			transactionCount++
		}
	}

	var validationTimer telemetry.Timer
	if transactionCount > 0 {
		validationTimer = collector.StartStructured(telemetry.TimerConfig{
			Name:  "validation.transactions",
			Count: transactionCount,
			Unit:  "transactions",
		})
	}

	for _, directive := range tree.Directives {
		// Check for cancellation
		select {
		case <-ctx.Done():
			if validationTimer != nil {
				validationTimer.End()
			}
			processTimer.End()
			return ctx.Err()
		default:
		}

		l.processDirective(ctx, directive)
	}

	if validationTimer != nil {
		validationTimer.End()
	}
	processTimer.End()

	// Insert synthetic padding transactions into AST and process them
	if len(l.syntheticTransactions) > 0 {
		insertTimer := collector.StartStructured(telemetry.TimerConfig{
			Name:  "ledger.synthetic_txn_insertion",
			Count: len(l.syntheticTransactions),
			Unit:  "transactions",
		})

		// Add synthetic transactions to AST
		for _, txn := range l.syntheticTransactions {
			tree.Directives = append(tree.Directives, txn)
		}

		// Re-sort to maintain chronological order
		// Use stable sort to preserve original ordering for same-date directives
		_ = ast.SortDirectives(tree)

		// Process synthetic transactions to update inventory
		// Note: These transactions are pre-validated and always balance
		for _, txn := range l.syntheticTransactions {
			// Synthetic transactions skip validation - they're pre-validated by padding calculation
			handler := GetHandler(txn.Kind())
			if handler != nil {
				_, delta := handler.Validate(ctx, l, txn)
				handler.Apply(ctx, l, txn, delta)
			}
		}

		insertTimer.End()
	}

	// Check for unused pad directives (pads that were never referenced by any balance)
	for accountName, pad := range l.padEntries {
		if !l.usedPads[accountName] {
			l.errors = append(l.errors, NewUnusedPadWarning(pad))
		}
	}

	// Return collected errors
	if len(l.errors) > 0 {
		return &ValidationErrors{Errors: l.errors}
	}

	return nil
}

// MustProcess processes an AST, panicking on validation errors.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	ledger := ledger.New()
//	ledger.MustProcess(context.Background(), ast)
func (l *Ledger) MustProcess(ctx context.Context, tree *ast.AST) {
	err := l.Process(ctx, tree)
	if err != nil {
		panic(err)
	}
}

// Errors returns all collected errors
func (l *Ledger) Errors() []error {
	return l.errors
}

// GetAccount returns an account by name
func (l *Ledger) GetAccount(name string) (*Account, bool) {
	node := l.graph.GetNode(name)
	if node == nil || node.Kind != "account" {
		return nil, false
	}
	acc, ok := node.Meta.(*Account)
	return acc, ok
}

// Accounts returns all accounts in the ledger
func (l *Ledger) Accounts() map[string]*Account {
	result := make(map[string]*Account)
	l.forEachAccount(func(acc *Account) bool {
		result[string(acc.Name)] = acc
		return true
	})
	return result
}

// GetPrice returns the exchange rate from one currency to another at a given date,
// using forward-fill semantics (most recent price on or before the date).
// Returns (rate, found) where found is false if no price exists.
//
// Same-currency conversions always return 1.0.
func (l *Ledger) GetPrice(date *ast.Date, fromCurrency, toCurrency string) (decimal.Decimal, bool) {
	// Same currency always returns 1.0
	if fromCurrency == toCurrency {
		return decimal.NewFromInt(1), true
	}

	// Build temporary graph with most recent edges per currency pair
	tempGraph := l.buildForwardFillGraph(date)

	// Find path using the filtered edges
	path, err := tempGraph.FindPath(fromCurrency, toCurrency, date)
	if err != nil {
		return decimal.Zero, false
	}

	// Multiply rates along the path
	result := decimal.NewFromInt(1)
	for _, edge := range path {
		if edge.Kind == "price" && !edge.Weight.IsZero() {
			result = result.Mul(edge.Weight)
		}
	}

	return result, true
}

// buildForwardFillGraph constructs a temporary graph with only the most recent
// price edges for each currency pair on or before the given date.
// This implements forward-fill semantics for price lookups.
func (l *Ledger) buildForwardFillGraph(date *ast.Date) *Graph {
	tempGraph := NewGraph()
	validEdges := l.graph.GetPriceEdgesOnDate(date)
	seenPairs := make(map[string]bool)

	for _, edge := range validEdges {
		// Only add the first (most recent) edge for each currency pair
		pairKey := edge.From + "->" + edge.To
		if !seenPairs[pairKey] {
			tempGraph.AddEdge(edge)
			seenPairs[pairKey] = true
		}

		// Also add inverse if not inferred and not already seen
		if !edge.Inferred {
			inversePairKey := edge.To + "->" + edge.From
			if !seenPairs[inversePairKey] {
				inverseEdge := &Edge{
					From:     edge.To,
					To:       edge.From,
					Kind:     "price",
					Date:     edge.Date,
					Weight:   decimal.NewFromInt(1).Div(edge.Weight),
					Meta:     edge.Meta,
					Inferred: true,
				}
				tempGraph.AddEdge(inverseEdge)
				seenPairs[inversePairKey] = true
			}
		}
	}

	return tempGraph
}

// Graph returns the underlying graph for advanced queries.
func (l *Ledger) Graph() *Graph {
	return l.graph
}

// forEachAccount iterates over all accounts in the ledger, calling fn for each.
// The callback can return false to break early (not used currently, but enables future filtering).
func (l *Ledger) forEachAccount(fn func(*Account) bool) {
	for _, node := range l.graph.GetNodesByKind("account") {
		if account, ok := node.Meta.(*Account); ok {
			if !fn(account) {
				break
			}
		}
	}
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
// Returns error if startDate > endDate.
//
// The tree is organized with account types as virtual root nodes. Balances are
// aggregated bottom-up so parent nodes include the sum of all their descendants.
func (l *Ledger) GetBalanceTree(types []ast.AccountType, startDate, endDate *ast.Date) (*BalanceTree, error) {
	// Validate date range
	if startDate != nil && endDate != nil && startDate.After(endDate.Time) {
		return nil, fmt.Errorf("startDate %s is after endDate %s", startDate.String(), endDate.String())
	}

	// Build type filter from enum to configured names
	typeFilter := make(map[string]bool)
	for _, t := range types {
		typeFilter[l.config.ToAccountTypeName(t)] = true
	}

	// Collect all accounts with their balances
	var entries []balanceTreeEntry
	currencySet := make(map[string]bool)

	l.forEachAccount(func(account *Account) bool {
		// Skip if type filter is set and account doesn't match
		if len(typeFilter) > 0 && !typeFilter[account.Type] {
			return true
		}

		// Calculate balance for the period
		var balance *Balance
		if startDate == nil && endDate == nil {
			// Current inventory state
			balance = l.getAccountCurrentBalance(account)
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

		return true
	})

	// Build sorted currency list
	currencies := make([]string, 0, len(currencySet))
	for currency := range currencySet {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)

	// Build the tree structure
	tree := l.buildBalanceTree(entries, typeFilter)

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

// getAccountCurrentBalance returns the current inventory balance for an account.
func (l *Ledger) getAccountCurrentBalance(account *Account) *Balance {
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

func (l *Ledger) buildBalanceTree(entries []balanceTreeEntry, typeFilter map[string]bool) *BalanceTree {
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
			typeName := l.config.ToAccountTypeName(t)
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
		typeName := l.config.ToAccountTypeName(accountType)
		typeEntries := accountsByType[typeName]

		if len(typeEntries) == 0 {
			continue
		}

		// Build subtree for this type
		root := l.buildTypeSubtree(typeName, typeEntries)
		roots = append(roots, root)
	}

	return &BalanceTree{Roots: roots}
}

// buildTypeSubtree builds a subtree for a single account type.
func (l *Ledger) buildTypeSubtree(typeName string, entries []balanceTreeEntry) *BalanceNode {
	// Create a map of account name to node for quick lookup
	nodeMap := make(map[string]*BalanceNode)

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

	// Build parent-child relationships and create intermediate nodes
	for _, entry := range entries {
		accountName := string(entry.account.Name)
		parts := strings.Split(accountName, ":")

		// Ensure all parent nodes exist
		for i := 1; i < len(parts); i++ {
			parentPath := strings.Join(parts[:i], ":")
			childPath := strings.Join(parts[:i+1], ":")

			// Create parent node if it doesn't exist
			if _, exists := nodeMap[parentPath]; !exists {
				nodeMap[parentPath] = &BalanceNode{
					Name:     parentPath,
					Account:  parentPath,
					Depth:    i - 1,
					Balance:  NewBalance(),
					Children: nil,
				}
			}

			// Add child to parent if not already added
			parent := nodeMap[parentPath]
			child := nodeMap[childPath]
			if child != nil {
				found := false
				for _, c := range parent.Children {
					if c.Name == child.Name {
						found = true
						break
					}
				}
				if !found {
					parent.Children = append(parent.Children, child)
				}
			}
		}
	}

	// Sort children at each level
	for _, node := range nodeMap {
		sort.Slice(node.Children, func(i, j int) bool {
			return node.Children[i].Name < node.Children[j].Name
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
	sort.Slice(root.Children, func(i, j int) bool {
		return root.Children[i].Name < root.Children[j].Name
	})

	// Aggregate balances from children to root
	for _, child := range root.Children {
		aggregate(child)
		root.Balance.Merge(child.Balance)
	}

	return root
}

// processDirective processes a single directive
func (l *Ledger) processDirective(ctx context.Context, directive ast.Directive) {
	handler := GetHandler(directive.Kind())
	if handler == nil {
		// Unknown directive kind - ignore
		return
	}

	// Validate directive
	errs, delta := handler.Validate(ctx, l, directive)
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - apply mutations
	handler.Apply(ctx, l, directive, delta)
}

// applyOpen applies the open delta to the ledger (mutation only)
func (l *Ledger) applyOpen(open *ast.Open, delta *OpenDelta, cfg *Config) {
	accountName := string(delta.Account)

	// Extract account type root name (e.g., "Assets" from "Assets:Checking")
	idx := strings.IndexByte(string(delta.Account), ':')
	accountTypeRoot := ""
	if idx > 0 {
		accountTypeRoot = string(delta.Account)[:idx]
	}

	account := &Account{
		Name:                 delta.Account,
		Type:                 accountTypeRoot,
		OpenDate:             delta.OpenDate,
		ConstraintCurrencies: delta.ConstraintCurrencies,
		BookingMethod:        delta.BookingMethod,
		Metadata:             delta.Metadata,
		Inventory:            NewInventory(),
	}
	l.graph.AddNode(accountName, "account", account)

	// Create implicit parent nodes and hierarchy edges
	l.ensureAccountHierarchy(accountName)
}

// ensureAccountHierarchy creates parent nodes and hierarchy edges for an account.
// For example, "Assets:US:Checking" creates edges:
//
//	Assets -> Assets:US
//	Assets:US -> Assets:US:Checking
func (l *Ledger) ensureAccountHierarchy(accountName string) {
	parts := strings.Split(accountName, ":")
	for i := 1; i < len(parts); i++ {
		parentPath := strings.Join(parts[:i], ":")
		childPath := strings.Join(parts[:i+1], ":")

		// Ensure parent node exists (implicit if not explicitly opened)
		if l.graph.GetNode(parentPath) == nil {
			l.graph.AddNode(parentPath, "account", nil)
		}

		// Ensure hierarchy edge exists
		existsEdge := false
		for _, edge := range l.graph.GetOutgoingEdges(parentPath) {
			if edge.Kind == "hierarchy" && edge.To == childPath {
				existsEdge = true
				break
			}
		}

		if !existsEdge {
			l.graph.AddEdge(&Edge{
				From:   parentPath,
				To:     childPath,
				Kind:   "hierarchy",
				Date:   nil,
				Weight: decimal.Zero,
				Meta:   nil,
			})
		}
	}
}

// applyClose applies the close delta to the ledger (mutation only)
func (l *Ledger) applyClose(delta *CloseDelta) {
	node := l.graph.GetNode(delta.AccountName)
	if node == nil {
		return
	}
	if account, ok := node.Meta.(*Account); ok {
		account.CloseDate = delta.CloseDate
	}
}

// applyTransaction mutates ledger state (inventory updates) and records posting history.
// Only called after validation passes. Panics on bugs (invariant violations).
func (l *Ledger) applyTransaction(txn *ast.Transaction, delta *TransactionDelta) {
	for _, posting := range txn.Postings {
		if posting.Amount == nil {
			continue
		}

		accountName := string(posting.Account)
		node := l.graph.GetNode(accountName)
		if node == nil {
			panic(fmt.Sprintf("BUG: account %s not found after validation", accountName))
		}

		account, ok := node.Meta.(*Account)
		if !ok {
			panic(fmt.Sprintf("BUG: account %s metadata is not *Account", accountName))
		}

		amount, err := ParseAmount(posting.Amount)
		if err != nil {
			// This should never happen after validation - panic to catch bugs
			panic(fmt.Sprintf("BUG: amount parsing failed after validation: %v", err))
		}
		currency := posting.Amount.Currency

		// Update inventory if posting has cost specification
		if posting.Cost != nil {
			lotSpec, err := ParseLotSpec(posting.Cost)
			if err != nil {
				// This should never happen after validation - panic to catch bugs
				panic(fmt.Sprintf("BUG: lot spec parsing failed after validation: %v", err))
			}

			// Convert total cost to per-unit cost for inventory operations
			err = normalizeLotSpecForPosting(lotSpec, posting)
			if err != nil {
				// This should never happen after validation - panic to catch bugs
				panic(fmt.Sprintf("BUG: lot spec normalization failed after validation: %v", err))
			}

			if amount.IsZero() {
				// Zero amount with cost spec is a no-op for inventory
			} else if amount.GreaterThan(decimal.Zero) {
				account.Inventory.AddLot(currency, amount, lotSpec)
			} else {
				bookingMethod := account.BookingMethod
				if bookingMethod == "" {
					bookingMethod = "FIFO"
				}
				err := account.Inventory.ReduceLot(currency, amount, lotSpec, bookingMethod)
				if err != nil {
					// This should never happen after validateInventoryOperations - panic to catch bugs
					panic(fmt.Sprintf("BUG: lot reduction failed after validation: %v", err))
				}
			}
		} else {
			account.Inventory.Add(currency, amount)
		}

		// Record posting in account history (after mutation for correct ordering)
		account.Postings = append(account.Postings, &AccountPosting{
			Transaction: txn,
			Posting:     posting,
		})
	}
}

// applyBalance applies the balance delta to the ledger (mutation only)
func (l *Ledger) applyBalance(delta *BalanceDelta) {
	// Note: Padding adjustments are applied by processing synthetic transactions
	// (not here, to avoid double-application)
	// Pad removal happens at end of processing to support multiple currencies
}

// applyPrice adds price edges to the ledger's graph (mutation only)
func (l *Ledger) applyPrice(price *ast.Price) {
	amount, err := ParseAmount(price.Amount)
	if err != nil {
		panic(fmt.Sprintf("BUG: amount parsing failed after validation: %v", err))
	}

	from := string(price.Commodity)
	to := price.Amount.Currency

	// Add forward price edge
	l.graph.AddEdge(&Edge{
		From:     from,
		To:       to,
		Kind:     "price",
		Date:     price.Date(),
		Weight:   amount,
		Meta:     price,
		Inferred: false,
	})

	// Add inverse price edge (bidirectional)
	l.graph.AddEdge(&Edge{
		From:     to,
		To:       from,
		Kind:     "price",
		Date:     price.Date(),
		Weight:   decimal.NewFromInt(1).Div(amount),
		Meta:     price,
		Inferred: true,
	})
}

// applyCommodity creates an explicit commodity node in the graph with metadata.
// Commodities are treated as explicit graph nodes rather than implicit currency references.
// This allows tracking commodity-specific metadata, properties, and constraints.
//
// If a currency node was previously created implicitly (e.g., via enrichment or a transaction),
// it is upgraded to an explicit commodity node with kind "commodity" and its metadata.
func (l *Ledger) applyCommodity(commodity *ast.Commodity, delta *CommodityDelta) {
	// Create or upgrade the commodity node with metadata
	// This upgrades implicit "currency" nodes to explicit "commodity" nodes
	node := l.graph.AddNode(delta.CommodityID, "commodity", &CommodityNode{
		ID:       delta.CommodityID,
		Date:     delta.Date,
		Metadata: delta.Metadata,
	})

	// Ensure the node kind is set to "commodity" (not "currency")
	// This handles the case where the node was previously created as "currency"
	node.Kind = "commodity"
}

// CommodityNode represents a commodity or currency as an explicit graph node.
// Stores metadata from the Commodity directive for future queries and constraints.
type CommodityNode struct {
	ID       string          // Currency/commodity code (e.g., "USD", "HOOL")
	Date     *ast.Date       // Effective date of the commodity declaration
	Metadata []*ast.Metadata // Commodity-specific metadata (name, precision, etc.)
}
