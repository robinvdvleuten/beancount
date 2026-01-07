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
	} else {
		// Attach config to context for use throughout processing
		ctx = cfg.WithContext(ctx)
	}

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

// GetAccountsByType returns all accounts of the specified type, sorted by name.
func (l *Ledger) GetAccountsByType(accountType ast.AccountType) []*Account {
	var accounts []*Account

	l.forEachAccount(func(acc *Account) bool {
		if acc.Type == accountType {
			accounts = append(accounts, acc)
		}
		return true
	})

	// Sort by name for deterministic output
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Name < accounts[j].Name
	})

	return accounts
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

// HasPrice returns true if a price exists for the given currency pair on or before the date.
func (l *Ledger) HasPrice(date *ast.Date, fromCurrency, toCurrency string) bool {
	_, found := l.GetPrice(date, fromCurrency, toCurrency)
	return found
}

// ConvertAmount converts an amount from one currency to another at a given date.
// Uses pathfinding to find a conversion route if a direct edge doesn't exist.
// Returns (converted amount, error). Same-currency conversions always return 1.0.
func (l *Ledger) ConvertAmount(amount decimal.Decimal, fromCurrency, toCurrency string, date *ast.Date) (decimal.Decimal, error) {
	if fromCurrency == toCurrency {
		return amount, nil
	}

	rate, found := l.GetPrice(date, fromCurrency, toCurrency)
	if !found {
		return decimal.Zero, fmt.Errorf("no price found for %s→%s on %s", fromCurrency, toCurrency, date.String())
	}

	return amount.Mul(rate), nil
}

// ConvertBalance converts a multi-currency balance map to a single currency by summing all amounts
// using the exchange rates on the given date. Uses forward-fill semantics for price lookups.
//
// Returns the consolidated amount, or an error if any currency conversion fails.
// Same-currency amounts are added directly without conversion overhead.
//
// Example: ConvertBalance({"USD": 100, "EUR": 50}, "USD", date) returns
// 100 + (50 * EUR→USD rate) = consolidated USD amount
func (l *Ledger) ConvertBalance(balance map[string]decimal.Decimal, targetCurrency string, date *ast.Date) (decimal.Decimal, error) {
	if len(balance) == 0 {
		return decimal.Zero, nil
	}

	// If only one currency and it's the target, return directly
	if len(balance) == 1 {
		if amount, ok := balance[targetCurrency]; ok {
			return amount, nil
		}
	}

	result := decimal.Zero

	// Sum amounts, converting each currency to target
	for currency, amount := range balance {
		if amount.IsZero() {
			continue
		}

		// Same currency - add directly
		if currency == targetCurrency {
			result = result.Add(amount)
			continue
		}

		// Convert currency to target currency
		rate, found := l.GetPrice(date, currency, targetCurrency)
		if !found {
			return decimal.Zero, fmt.Errorf(
				"no price found to convert %s to %s on %s",
				currency, targetCurrency, date.String(),
			)
		}

		converted := amount.Mul(rate)
		result = result.Add(converted)
	}

	return result, nil
}

// FindPath finds a path of price edges from one currency to another at a given date.
// Returns the edges in order, or an error if no path exists.
// Useful for debugging or understanding currency conversion routes.
func (l *Ledger) FindPath(fromCurrency, toCurrency string, date *ast.Date) ([]*Edge, error) {
	return l.graph.FindPath(fromCurrency, toCurrency, date)
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

// GetBalancesAsOf returns the balance of every account as of a given date.
// Accounts with no postings before the date are omitted from the result.
// Balances are returned in no particular order.
func (l *Ledger) GetBalancesAsOf(date *ast.Date) []AccountBalance {
	var result []AccountBalance
	l.forEachAccount(func(account *Account) bool {
		acctBalance := account.GetBalanceAsOf(date)
		if !acctBalance.IsZero() {
			result = append(result, *acctBalance)
		}
		return true
	})
	return result
}

// GetBalancesAsOfInCurrency returns all accounts consolidated to a single
// currency as of a given date. Accounts with no postings before the date are
// omitted from the result.
//
// Returns an error if any currency conversion fails (e.g., missing price).
// Balances are returned in no particular order.
func (l *Ledger) GetBalancesAsOfInCurrency(
	currency string,
	date *ast.Date,
) ([]AccountBalance, error) {
	var result []AccountBalance
	var errs []error

	l.forEachAccount(func(account *Account) bool {
		acctBalance := account.GetBalanceAsOf(date)
		if acctBalance.IsZero() {
			return true // continue to next account
		}

		// Convert to target currency
		amount, err := l.ConvertBalance(acctBalance.Balance.ToMap(), currency, date)
		if err != nil {
			errs = append(errs, err)
			return true // collect error and continue
		}

		converted := NewBalance()
		converted.Set(currency, amount)

		result = append(result, AccountBalance{
			Account: acctBalance.Account,
			Balance: converted,
		})
		return true
	})

	if len(errs) > 0 {
		return nil, &ValidationErrors{Errors: errs}
	}

	return result, nil
}

// GetBalancesInPeriod returns net balance changes for accounts within a date range [start, end].
// Optionally filters by account type (e.g., Income, Expenses for income statement).
// Accounts with no postings in the period are omitted from the result.
// Balances are returned in no particular order.
func (l *Ledger) GetBalancesInPeriod(
	start, end *ast.Date,
	accountTypes ...ast.AccountType,
) []AccountBalance {
	var result []AccountBalance

	// Build type filter if specified
	typeFilter := make(map[ast.AccountType]bool)
	for _, t := range accountTypes {
		typeFilter[t] = true
	}

	l.forEachAccount(func(account *Account) bool {
		// Skip if type filter is set and account doesn't match
		if len(accountTypes) > 0 && !typeFilter[account.Type] {
			return true
		}

		acctBalance := account.GetBalanceInPeriod(start, end)
		if !acctBalance.IsZero() {
			result = append(result, *acctBalance)
		}
		return true
	})

	return result
}

// CloseBooks moves Income and Expense balances to Equity:Earnings:Current.
// Returns synthetic transactions created for closing (for audit trail/AST insertion).
// The closingDate is typically the last day of the accounting period.
func (l *Ledger) CloseBooks(closingDate *ast.Date) []*ast.Transaction {
	var syntheticTxns []*ast.Transaction

	// Get all Income and Expense balances accumulated up to closing date
	incomeExpenses := l.GetBalancesInPeriod(
		&ast.Date{}, // Dummy start date (will use zero)
		closingDate,
		ast.AccountTypeIncome, ast.AccountTypeExpenses,
	)

	// If no activity, nothing to close
	if len(incomeExpenses) == 0 {
		return syntheticTxns
	}

	// Build closing transactions: each account → Equity:Earnings:Current
	earningsAccount, err := ast.NewAccount("Equity:Earnings:Current")
	if err != nil {
		// Should never happen for hardcoded account name
		return syntheticTxns
	}

	for _, accBal := range incomeExpenses {
		accountName, err := ast.NewAccount(accBal.Account)
		if err != nil {
			// Skip malformed account names (shouldn't happen)
			continue
		}

		// Create postings for each currency in the balance
		for _, currencyAmount := range accBal.Balance.Entries() {
			if currencyAmount.Amount.IsZero() {
				continue
			}

			// Create synthetic transaction: close to equity
			txn := ast.NewTransaction(closingDate, "Period closing",
				ast.WithFlag("P"), // Mark as padding/synthetic
				ast.WithPostings(
					ast.NewPosting(accountName, ast.WithAmount(currencyAmount.Amount.Neg().String(), currencyAmount.Currency)),
					ast.NewPosting(earningsAccount), // Inferred amount
				),
			)

			syntheticTxns = append(syntheticTxns, txn)
		}
	}

	return syntheticTxns
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
func (l *Ledger) applyOpen(open *ast.Open, delta *OpenDelta) {
	accountName := string(delta.Account)
	account := &Account{
		Name:                 delta.Account,
		Type:                 delta.Account.Type(),
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

			if amount.GreaterThan(decimal.Zero) {
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
		Date:     price.Date,
		Weight:   amount,
		Meta:     price,
		Inferred: false,
	})

	// Add inverse price edge (bidirectional)
	l.graph.AddEdge(&Edge{
		From:     to,
		To:       from,
		Kind:     "price",
		Date:     price.Date,
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
