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
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/telemetry"
	"github.com/shopspring/decimal"
)

// Ledger represents the state of the accounting ledger with account balances,
// transaction validation, and error tracking. It processes directives in date order
// and maintains the complete state of all accounts including their inventory positions.
//
// The ledger validates all transactions for balance, ensures accounts are opened before
// use, verifies balance assertions, and processes pad directives. All validation errors
// are collected and returned together after processing.
type Ledger struct {
	accounts map[string]*Account
	// Prices by currency pair, for GetPrice
	prices priceIndex
	// Currencies declared by a commodity directive
	commodities map[string]bool
	config      *Config
	// The tolerances Booking and balance assertions check against, from
	// the config's tolerance options
	tolerances tolerances
	errors     []error
	pads       *pads
	// Every balance assertion, passing or not, by account, currency and
	// date, in directive order
	balances map[balanceKey][]*ast.Balance
	// Balance assertions repeating an earlier one with a different amount
	duplicateBalances map[*ast.Balance]bool
	// The first applied transaction per Import ID; a Dropped transaction
	// is not in it
	importIDs map[string]*ast.Transaction
	// The positions each booked posting holds, Booking's published record
	bookedPositions map[*ast.Posting][]BookedPosition
	booker          *booker
	booked          map[*ast.Transaction]*bookedTransaction
	unopened        map[string]*Account // Accounts posted to before any open
	opened          map[string]bool     // Accounts an open directive names, at any date
	display         *DisplayContext
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
	fmt.Fprintf(&buf, "\n\n%d validation error(s) found", len(e.Errors))
	return buf.String()
}

// Unwrap returns the underlying errors for error unwrapping
func (e *ValidationErrors) Unwrap() []error {
	return e.Errors
}

// New creates a new empty ledger
func New() *Ledger {
	cfg := NewConfig()
	return &Ledger{
		accounts:        make(map[string]*Account),
		config:          cfg,
		tolerances:      newTolerances(cfg.Tolerance),
		errors:          make([]error, 0),
		pads:            newPads(),
		commodities:     make(map[string]bool),
		bookedPositions: make(map[*ast.Posting][]BookedPosition),
		booked:          make(map[*ast.Transaction]*bookedTransaction),
		unopened:        make(map[string]*Account),
		importIDs:       make(map[string]*ast.Transaction),
		display:         newDisplayContext(),
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

	prepareTimer := collector.Start("ledger.prepare_ast")
	// Unbalanced pushes and pops are reported like validation errors; the
	// ledger is still processed.
	l.errors = append(l.errors, ast.ApplyPushPopDirectives(tree)...)
	if err := ast.SortDirectives(tree); err != nil {
		prepareTimer.End()
		return err
	}
	prepareTimer.End()

	// Parse configuration from AST options; an invalid option is reported
	// and keeps its default while the others apply.
	cfg, optionErrs := configFromAST(tree)
	l.errors = append(l.errors, optionErrs...)
	l.config = cfg
	l.tolerances = newTolerances(cfg.Tolerance)

	// Process directives in semantic date order.
	processTimer := collector.StartStructured(telemetry.TimerConfig{
		Name:  "ledger.processing",
		Count: len(tree.Directives),
		Unit:  "directives",
	})

	// Count transactions, and record the source amounts' precisions before
	// interpolation writes inferred amounts into the AST.
	transactionCount := 0
	for _, directive := range tree.Directives {
		if _, ok := directive.(*ast.Transaction); ok {
			transactionCount++
		}
		l.display.updateFromDirective(directive)
	}

	if err := l.book(ctx, tree); err != nil {
		processTimer.End()
		return err
	}
	l.runPlugins(ctx, tree)
	l.opened = openedAccounts(tree.Directives)
	l.balances = balancesByKey(tree.Directives)
	l.duplicateBalances = duplicateBalances(l.balances)

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

	// The padding transactions join the AST at their pads' dates.
	if len(l.pads.padding) > 0 {
		for _, padding := range l.pads.padding {
			tree.Directives = append(tree.Directives, padding)
		}
		_ = ast.SortDirectives(tree)
	}
	for _, pad := range l.pads.unusedPads() {
		l.errors = append(l.errors, NewUnusedPadWarning(pad))
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
	return diagnostic.Errors(l.errors)
}

// Warnings returns non-fatal diagnostics collected while processing.
func (l *Ledger) Warnings() []error {
	return diagnostic.Warnings(l.errors)
}

// Diagnostics returns all diagnostics in processing order.
func (l *Ledger) Diagnostics() []error {
	return slices.Clone(l.errors)
}

// BookedPositions returns the positions a posting of an Applied transaction
// booked. A reduction booked one per lot it was booked against, in booking
// order, as beancount replaces it with one booked posting per lot; any
// other posting booked its own: at its per-unit cost and lot date, or its
// units alone without cost. A posting of a Dropped group has none.
func (l *Ledger) BookedPositions(posting *ast.Posting) []BookedPosition {
	return l.bookedPositions[posting]
}

// DisplayContext returns the per-currency display precision of the
// ledger's source amounts.
func (l *Ledger) DisplayContext() *DisplayContext {
	return l.display
}

// GetAccount returns an account by name
func (l *Ledger) GetAccount(name string) (*Account, bool) {
	account, ok := l.accounts[name]
	return account, ok
}

// Accounts returns all accounts in the ledger
func (l *Ledger) Accounts() map[string]*Account {
	result := make(map[string]*Account, len(l.accounts))
	for name, account := range l.accounts {
		result[name] = account
	}
	return result
}

// GetPrice returns the exchange rate from one currency to another at a given
// date, using forward-fill semantics (most recent price on or before the
// date). Like beancount's get_price, only the pair itself or its inverse
// counts: prices are not chained through other currencies. found is false
// when neither exists.
//
// Same-currency conversions always return 1.0.
func (l *Ledger) GetPrice(date *ast.Date, fromCurrency, toCurrency string) (decimal.Decimal, bool) {
	return l.prices.rate(date, fromCurrency, toCurrency)
}

// processDirective validates a directive, records its errors, and applies
// its delta if Validate returned one.
func (l *Ledger) processDirective(ctx context.Context, directive ast.Directive) {
	handler := GetHandler(directive.Kind())
	if handler == nil {
		// Unknown directive kind - ignore
		return
	}

	errs, delta := handler.Validate(ctx, l, directive)
	l.errors = append(l.errors, errs...)
	if delta != nil {
		handler.Apply(ctx, l, directive, delta)
	}
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
		Metadata:             delta.Metadata,
		Inventory:            NewInventory(),
	}
	// Postings made before the open keep counting, as in beancount.
	if early, ok := l.unopened[accountName]; ok {
		account.Inventory = early.Inventory
		account.Postings = early.Postings
		delete(l.unopened, accountName)
	}
	l.accounts[accountName] = account
}

// inventory returns what an account holds, posted to before its open too,
// or an empty inventory for an account nothing was posted to.
func (l *Ledger) inventory(account ast.Account) *Inventory {
	if acc, ok := l.accounts[string(account)]; ok {
		return acc.Inventory
	}
	if acc, ok := l.unopened[string(account)]; ok {
		return acc.Inventory
	}
	return NewInventory()
}

// applyClose applies the close delta to the ledger (mutation only)
func (l *Ledger) applyClose(delta *CloseDelta) {
	if account, ok := l.accounts[delta.AccountName]; ok {
		account.CloseDate = delta.CloseDate
	}
}

// applyTransaction replays a booked transaction's positions onto its
// accounts' inventories and records the posting history and Import ID. A
// posting to an account that is not open yet is kept for the account's open.
func (l *Ledger) applyTransaction(txn *ast.Transaction, booked *bookedTransaction) {
	if id, ok := importID(txn); ok && id != "" {
		if _, seen := l.importIDs[id]; !seen {
			l.importIDs[id] = txn
		}
	}
	for _, bp := range booked.postings {
		accountName := string(bp.posting.Account)
		account, ok := l.accounts[accountName]
		if !ok {
			account, ok = l.unopened[accountName]
			if !ok {
				account = &Account{Name: bp.posting.Account, Inventory: NewInventory()}
				l.unopened[accountName] = account
			}
		}
		for _, position := range bp.positions {
			account.Inventory.AddLot(bp.commodity, position.Units, position.lotSpec())
		}
		account.Postings = append(account.Postings, &AccountPosting{
			Transaction: txn,
			Posting:     bp.posting,
		})
	}
}

// applyPrice adds a price to the price index (mutation only)
func (l *Ledger) applyPrice(price *ast.Price) {
	amount, err := ParseAmount(price.Amount)
	if err != nil {
		panic(fmt.Sprintf("BUG: amount parsing failed after validation: %v", err))
	}
	l.prices.add(price.Date(), string(price.Commodity), price.Amount.Currency, amount)
}

// applyCommodity records a declared commodity (mutation only)
func (l *Ledger) applyCommodity(delta *CommodityDelta) {
	l.commodities[delta.CommodityID] = true
}
