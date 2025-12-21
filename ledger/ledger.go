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
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
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
	accounts              map[string]*Account
	errors                []error
	options               map[string][]string
	padEntries            map[string]*ast.Pad // account -> pad directive
	usedPads              map[string]bool     // account -> whether pad was used
	syntheticTransactions []*ast.Transaction  // Padding transactions to insert into AST
	toleranceConfig       *ToleranceConfig
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
		accounts:        make(map[string]*Account),
		errors:          make([]error, 0),
		options:         make(map[string][]string),
		padEntries:      make(map[string]*ast.Pad),
		usedPads:        make(map[string]bool),
		toleranceConfig: NewToleranceConfig(),
	}
}

// GetOption returns the first value for the option key, or empty string if not found.
// For options that can have multiple values (e.g., inferred_tolerance_default),
// use GetOptions instead.
//
// Options that typically have single values:
//   - title
//   - render_commas
//   - booking_method
//
// Options that can have multiple values:
//   - inferred_tolerance_default (per-currency: "USD:0.01", "EUR:0.01")
//   - operating_currency (multiple currencies: "USD", "EUR")
func (l *Ledger) GetOption(key string) (string, bool) {
	values := l.options[key]
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// GetOptions returns all values for the option key.
// Use this for options that can have multiple values like
// inferred_tolerance_default (per-currency tolerances) or
// operating_currency (multiple operating currencies).
func (l *Ledger) GetOptions(key string) []string {
	return l.options[key]
}

// Process processes an AST and builds the ledger state
func (l *Ledger) Process(ctx context.Context, tree *ast.AST) error {
	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

	// Process options first
	for _, opt := range tree.Options {
		l.options[opt.Name.Value] = append(l.options[opt.Name.Value], opt.Value.Value)
	}

	// Parse tolerance configuration from options
	if config, err := ParseToleranceConfig(l.options); err != nil {
		l.errors = append(l.errors, err)
	} else {
		l.toleranceConfig = config
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
			l.processTransaction(ctx, txn)
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
	acc, ok := l.accounts[name]
	return acc, ok
}

// Accounts returns all accounts
func (l *Ledger) Accounts() map[string]*Account {
	return l.accounts
}

// processDirective processes a single directive
func (l *Ledger) processDirective(ctx context.Context, directive ast.Directive) {
	switch d := directive.(type) {
	case *ast.Open:
		l.processOpen(ctx, d)
	case *ast.Close:
		l.processClose(ctx, d)
	case *ast.Transaction:
		l.processTransaction(ctx, d)
	case *ast.Balance:
		l.processBalance(ctx, d)
	case *ast.Pad:
		l.processPad(ctx, d)
	case *ast.Note:
		l.processNote(ctx, d)
	case *ast.Document:
		l.processDocument(ctx, d)
	default:
		// Unknown directive type - ignore for now
		// Note: Price, Commodity, and Event directives are intentionally not processed
		// as they don't affect ledger state or require validation
	}
}

// processOpen processes an Open directive with validation
func (l *Ledger) processOpen(ctx context.Context, open *ast.Open) {
	v := newValidator(l.accounts, l.toleranceConfig)
	errs, delta := v.validateOpen(ctx, open)

	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	l.applyOpen(open, delta)
}

// applyOpen applies the open delta to the ledger (mutation only)
func (l *Ledger) applyOpen(open *ast.Open, delta *OpenDelta) {
	// Build account from delta (avoid allocating during validation)
	account := &Account{
		Name:                 delta.Account,
		Type:                 delta.Account.Type(),
		OpenDate:             delta.OpenDate,
		ConstraintCurrencies: delta.ConstraintCurrencies,
		BookingMethod:        delta.BookingMethod,
		Metadata:             delta.Metadata,
		Inventory:            NewInventory(), // Create inventory only at mutation time
	}

	l.accounts[string(delta.Account)] = account
}

// processClose processes a Close directive with validation
func (l *Ledger) processClose(ctx context.Context, close *ast.Close) {
	v := newValidator(l.accounts, l.toleranceConfig)
	errs, delta := v.validateClose(ctx, close)

	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	l.applyClose(delta)
}

// applyClose applies the close delta to the ledger (mutation only)
func (l *Ledger) applyClose(delta *CloseDelta) {
	account := l.accounts[delta.AccountName]
	account.CloseDate = delta.CloseDate
}

// processTransaction processes a Transaction directive
func (l *Ledger) processTransaction(ctx context.Context, txn *ast.Transaction) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs, delta := v.validateTransaction(ctx, txn)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply effects
	l.applyTransaction(txn, delta)
}

// applyTransaction mutates ledger state (inventory updates)
// Only called after validation passes. Panics on bugs (invariant violations).
func (l *Ledger) applyTransaction(txn *ast.Transaction, delta *TransactionDelta) {
	for _, posting := range txn.Postings {
		// Amount is always set after inference (either explicit or inferred)
		if posting.Amount == nil {
			continue
		}

		accountName := string(posting.Account)
		account, ok := l.accounts[accountName]
		if !ok {
			// This should never happen after validation - panic to catch bugs
			panic(fmt.Sprintf("BUG: account %s not found after validation", accountName))
		}

		amount, err := ParseAmount(posting.Amount)
		if err != nil {
			// This should never happen after validation - panic to catch bugs
			panic(fmt.Sprintf("BUG: amount parsing failed after validation: %v", err))
		}
		currency := posting.Amount.Currency

		// Determine cost - inferred costs are now stored directly on posting.Cost
		var costToUse *ast.Cost
		hasExplicitCost := posting.Cost != nil && !posting.Cost.IsEmpty() && !posting.Cost.IsMergeCost() && !posting.Cost.Inferred
		hasEmptyCost := posting.Cost != nil && posting.Cost.IsEmpty()
		hasInferredCost := posting.Cost != nil && posting.Cost.Inferred
		hasMergeCost := posting.Cost != nil && posting.Cost.IsMergeCost()

		if hasExplicitCost || hasEmptyCost || hasMergeCost || hasInferredCost {
			costToUse = posting.Cost
		}

		// Update inventory
		if hasExplicitCost || hasEmptyCost || hasInferredCost || hasMergeCost {
			lotSpec, err := ParseLotSpec(costToUse)
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
	}
}

// processBalance processes a Balance directive with validation and delta calculation
func (l *Ledger) processBalance(ctx context.Context, balance *ast.Balance) {
	v := newValidator(l.accounts, l.toleranceConfig)

	// Basic validation
	errs := v.validateBalance(balance)
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Get pad entry if exists
	accountName := string(balance.Account)
	padEntry := l.padEntries[accountName]

	// Mark pad as used if it existed (even if validation fails)
	// A pad is "used" if any balance assertion references it
	if padEntry != nil {
		l.usedPads[accountName] = true
	}

	// Calculate delta (returns error separately, not in delta)
	delta, err := v.calculateBalanceDelta(balance, padEntry)
	if err != nil {
		l.errors = append(l.errors, err)
		return
	}

	// Store synthetic transaction for AST insertion
	if delta.SyntheticTransaction != nil {
		l.syntheticTransactions = append(l.syntheticTransactions, delta.SyntheticTransaction)
	}

	// Apply mutations (but don't remove pad yet)
	l.applyBalance(delta)
}

// applyBalance applies the balance delta to the ledger (mutation only)
func (l *Ledger) applyBalance(delta *BalanceDelta) {
	// Note: Padding adjustments are applied by processing synthetic transactions
	// (not here, to avoid double-application)
	// Pad removal happens at end of processing to support multiple currencies
}

// processPad processes a Pad directive
func (l *Ledger) processPad(ctx context.Context, pad *ast.Pad) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs := v.validatePad(pad)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - store pad directive
	// Will be applied when next balance assertion is encountered
	accountName := string(pad.Account)
	l.padEntries[accountName] = pad
}

// processNote processes a Note directive
func (l *Ledger) processNote(ctx context.Context, note *ast.Note) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs := v.validateNote(note)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
	}

	// Note has no state mutation - just validation
}

// processDocument processes a Document directive
func (l *Ledger) processDocument(ctx context.Context, doc *ast.Document) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs := v.validateDocument(doc)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
	}

	// Document has no state mutation - just validation
}
