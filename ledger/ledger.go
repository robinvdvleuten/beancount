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

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// Ledger represents the state of the accounting ledger with account balances,
// transaction validation, and error tracking. It processes directives in date order
// and maintains the complete state of all accounts including their inventory positions.
//
// The ledger validates all transactions for balance, ensures accounts are opened before
// use, verifies balance assertions, and processes pad directives. All validation errors
// are collected and returned together after processing.
type Ledger struct {
	accounts        map[string]*Account
	errors          []error
	options         map[string]string
	padEntries      map[string]*ast.Pad // account -> pad directive
	toleranceConfig *ToleranceConfig
}

// ValidationErrors wraps multiple validation errors
type ValidationErrors struct {
	Errors []error
}

func (e *ValidationErrors) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("%d validation errors occurred", len(e.Errors))
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
		options:         make(map[string]string),
		padEntries:      make(map[string]*ast.Pad),
		toleranceConfig: NewToleranceConfig(),
	}
}

// Process processes an AST and builds the ledger state
func (l *Ledger) Process(ctx context.Context, tree *ast.AST) error {
	// Process options first
	for _, opt := range tree.Options {
		l.options[opt.Name] = opt.Value
	}

	// Parse tolerance configuration from options
	if config, err := ParseToleranceConfig(l.options); err != nil {
		l.errors = append(l.errors, err)
	} else {
		l.toleranceConfig = config
	}

	// Process directives in order (they're already sorted by date)
	processTimer := telemetry.StartTimer(ctx, fmt.Sprintf("ledger.processing (%d directives)", len(tree.Directives)))
	for _, directive := range tree.Directives {
		// Check for cancellation
		select {
		case <-ctx.Done():
			processTimer.End()
			return ctx.Err()
		default:
		}

		l.processDirective(ctx, directive)
	}
	processTimer.End()

	// Return collected errors
	if len(l.errors) > 0 {
		return &ValidationErrors{Errors: l.errors}
	}

	return nil
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

// processOpen processes an Open directive
func (l *Ledger) processOpen(ctx context.Context, open *ast.Open) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, delta := v.validateOpen(ctx, open)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply delta
	l.ApplyOpenDelta(delta)
}

// processClose processes a Close directive
func (l *Ledger) processClose(ctx context.Context, close *ast.Close) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, delta := v.validateClose(ctx, close)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply delta
	l.ApplyCloseDelta(delta)
}

// processTransaction processes a Transaction directive
func (l *Ledger) processTransaction(ctx context.Context, txn *ast.Transaction) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, delta := v.validateTransaction(ctx, txn)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply delta
	l.ApplyTransactionDelta(delta)
}

// ApplyTransactionDelta mutates ledger state by applying a transaction delta.
// The delta contains all the inventory changes computed during validation.
func (l *Ledger) ApplyTransactionDelta(delta *TransactionDelta) {
	for _, change := range delta.InventoryChanges {
		account, ok := l.accounts[change.Account]
		if !ok {
			// This shouldn't happen as validation checks account exists
			continue
		}

		switch change.Operation {
		case OpAdd:
			if change.LotSpec != nil {
				// Add with lot tracking
				account.Inventory.AddLot(change.Currency, change.Amount, change.LotSpec)
			} else {
				// Simple add (amount is always positive)
				account.Inventory.Add(change.Currency, change.Amount)
			}

		case OpReduce:
			if change.LotSpec != nil {
				// Reduce from lot-tracked inventory using booking method
				// Validator has already checked this will succeed
				bookingMethod := account.BookingMethod
				if bookingMethod == "" {
					bookingMethod = "FIFO" // Default
				}
				// For OpReduce, amount is always positive, but ReduceLot expects negative
				err := account.Inventory.ReduceLot(change.Currency, change.Amount.Neg(), change.LotSpec, bookingMethod)
				if err != nil {
					// This should never happen - validator checks sufficiency
					// If it does, it's a bug in the validator
					panic(fmt.Sprintf("ReduceLot failed after validation: %v", err))
				}
			} else {
				// Simple reduction (no lot tracking) - just add negative amount
				account.Inventory.Add(change.Currency, change.Amount.Neg())
			}
		}
	}
}

// processBalance processes a Balance directive
func (l *Ledger) processBalance(ctx context.Context, balance *ast.Balance) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, delta := v.validateBalance(ctx, balance)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply delta
	l.ApplyBalanceDelta(delta)
}

// ApplyOpenDelta mutates ledger state by adding a new account.
func (l *Ledger) ApplyOpenDelta(delta *OpenDelta) {
	accountName := string(delta.Open.Account)
	l.accounts[accountName] = delta.Account
}

// ApplyCloseDelta mutates ledger state by closing an account.
func (l *Ledger) ApplyCloseDelta(delta *CloseDelta) {
	account := l.accounts[delta.AccountName] // We know it exists from validation
	account.CloseDate = delta.Close.Date
}

// ApplyBalanceDelta mutates ledger state by applying a balance delta.
// This includes applying padding if required and adding errors if balance doesn't match.
// All checking is done in the validator - this just executes the delta.
func (l *Ledger) ApplyBalanceDelta(delta *BalanceDelta) {
	accountName := string(delta.Balance.Account)
	account := l.accounts[accountName] // We know it exists from validation
	currency := delta.Balance.Amount.Currency

	// Apply padding if required
	if delta.PadRequired {
		// Add padding to the account
		account.Inventory.Add(delta.PadCurrency, delta.PadAmount)

		// Subtract from the pad account
		if padAccount, ok := l.accounts[delta.PadAccount]; ok {
			padAccount.Inventory.Add(delta.PadCurrency, delta.PadAmount.Neg())
		}

		// Remove the pad entry after applying
		delete(l.padEntries, accountName)
	}

	// Add error if validator determined balance doesn't match
	if delta.BalanceMismatch {
		l.addError(NewBalanceMismatchError(delta.Balance,
			delta.ExpectedAmount.String(),
			delta.FinalAmount.String(),
			currency))
	}
}

// ApplyPadDelta mutates ledger state by storing a pad directive.
// The pad will be applied when the next balance assertion is encountered.
// Assumes validation has already checked for duplicates.
func (l *Ledger) ApplyPadDelta(delta *PadDelta) {
	l.padEntries[delta.AccountName] = delta.Pad
}

// processPad processes a Pad directive
func (l *Ledger) processPad(ctx context.Context, pad *ast.Pad) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, delta := v.validatePad(ctx, pad)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply delta
	l.ApplyPadDelta(delta)
}

// processNote processes a Note directive
func (l *Ledger) processNote(ctx context.Context, note *ast.Note) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, _ := v.validateNote(ctx, note)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
	}

	// Note has no state mutation - no delta to apply
}

// processDocument processes a Document directive
func (l *Ledger) processDocument(ctx context.Context, doc *ast.Document) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.padEntries, l.toleranceConfig)

	// Run pure validation
	errs, _ := v.validateDocument(ctx, doc)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
	}

	// Document has no state mutation - no delta to apply
}

// addError adds an error to the error collection
func (l *Ledger) addError(err error) {
	l.errors = append(l.errors, err)
}
