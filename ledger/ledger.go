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
		l.processOpen(d)
	case *ast.Close:
		l.processClose(d)
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
func (l *Ledger) processOpen(open *ast.Open) {
	accountName := string(open.Account)

	// Check if account already exists
	if existing, ok := l.accounts[accountName]; ok {
		// Check if it's already open
		if !existing.IsClosed() {
			l.addError(NewAccountAlreadyOpenError(open, existing.OpenDate))
			return
		}
		// Account was closed before, allow reopening
	}

	// Create new account
	account := &Account{
		Name:                 open.Account,
		Type:                 ParseAccountType(open.Account),
		OpenDate:             open.Date,
		ConstraintCurrencies: open.ConstraintCurrencies,
		BookingMethod:        open.BookingMethod,
		Metadata:             open.Metadata,
		Inventory:            NewInventory(),
	}

	l.accounts[accountName] = account
}

// processClose processes a Close directive
func (l *Ledger) processClose(close *ast.Close) {
	accountName := string(close.Account)

	// Check if account exists
	account, ok := l.accounts[accountName]
	if !ok {
		l.addError(NewAccountNotClosedError(close))
		return
	}

	// Check if already closed
	if account.IsClosed() {
		l.addError(NewAccountAlreadyClosedError(close, account.CloseDate))
		return
	}

	// Close the account
	account.CloseDate = close.Date
}

// processTransaction processes a Transaction directive
func (l *Ledger) processTransaction(ctx context.Context, txn *ast.Transaction) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs, balanceResult := v.validateTransaction(ctx, txn)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply effects
	l.applyTransaction(txn, balanceResult)
}

// applyTransaction mutates ledger state (inventory updates)
// Only called after validation passes
func (l *Ledger) applyTransaction(txn *ast.Transaction, result *balanceResult) {
	for _, posting := range txn.Postings {
		// Get the amount (either explicit or inferred)
		var amountToUse *ast.Amount
		if posting.Amount != nil {
			amountToUse = posting.Amount
		} else if inferredAmount, ok := result.inferredAmounts[posting]; ok {
			amountToUse = inferredAmount
		} else {
			// No amount and couldn't infer - skip
			continue
		}

		accountName := string(posting.Account)
		account, ok := l.accounts[accountName]
		if !ok {
			continue
		}

		// Parse amount
		amount, _ := ParseAmount(amountToUse) // We know it's valid from validation
		currency := amountToUse.Currency

		// Check if we have a cost (explicit, inferred, or empty)
		hasExplicitCost := posting.Cost != nil && !posting.Cost.IsEmpty() && !posting.Cost.IsMergeCost()
		hasEmptyCost := posting.Cost != nil && posting.Cost.IsEmpty()
		hasInferredCost := false
		var costToUse *ast.Cost

		if hasExplicitCost {
			costToUse = posting.Cost
		} else if hasEmptyCost {
			// Empty cost spec - use it directly for lot tracking
			// For reductions, this triggers FIFO/LIFO booking
			// For augmentations, the cost was inferred earlier
			costToUse = posting.Cost
		} else if inferredCost, ok := result.inferredCosts[posting]; ok {
			// Use inferred cost - create a temporary Cost structure
			hasInferredCost = true
			costToUse = &ast.Cost{
				Amount: inferredCost,
			}
		}

		// Update lot inventory
		if hasExplicitCost || hasEmptyCost || hasInferredCost {
			// Has cost basis - add/reduce with lot tracking
			lotSpec, err := ParseLotSpec(costToUse)
			if err != nil {
				l.addError(NewInvalidAmountError(txn, posting.Account, "cost", err))
				continue
			}

			if amount.GreaterThan(decimal.Zero) {
				// Adding to inventory
				account.Inventory.AddLot(currency, amount, lotSpec)
			} else {
				// Reducing from inventory
				bookingMethod := account.BookingMethod
				if bookingMethod == "" {
					bookingMethod = "FIFO" // Default
				}
				err := account.Inventory.ReduceLot(currency, amount, lotSpec, bookingMethod)
				if err != nil {
					l.addError(NewInvalidAmountError(txn, posting.Account, "lot reduction", err))
				}
			}
		} else {
			// No cost basis - simple add
			account.Inventory.Add(currency, amount)
		}
	}
}

// processBalance processes a Balance directive
func (l *Ledger) processBalance(ctx context.Context, balance *ast.Balance) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs := v.validateBalance(ctx, balance)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
		return
	}

	// Validation passed - now apply effects
	l.applyBalance(balance)
}

// applyBalance applies balance assertion and padding (mutation only)
func (l *Ledger) applyBalance(balance *ast.Balance) {
	// Parse expected amount (we know it's valid from validation)
	expectedAmount, _ := ParseAmount(balance.Amount)
	currency := balance.Amount.Currency

	// Get account inventory
	accountName := string(balance.Account)
	account := l.accounts[accountName] // We know it exists from validation

	// Get actual amount from inventory
	actualAmount := account.Inventory.Get(currency)

	// Check if there's a pad directive for this account
	if padEntry, hasPad := l.padEntries[accountName]; hasPad {
		// Calculate the difference needed to reach expected balance
		difference := expectedAmount.Sub(actualAmount)

		// Apply padding if difference is significant
		tolerance := l.toleranceConfig.GetDefaultTolerance(currency)
		if difference.Abs().GreaterThan(tolerance) {
			// Add difference to the account
			account.Inventory.Add(currency, difference)

			// Subtract from the pad account
			padAccountName := string(padEntry.AccountPad)
			if padAccount, ok := l.accounts[padAccountName]; ok {
				padAccount.Inventory.Add(currency, difference.Neg())
			}
		}

		// Remove the pad entry after applying
		delete(l.padEntries, accountName)

		// Update actual amount after padding
		actualAmount = account.Inventory.Get(currency)
	}

	// Check if amounts match within tolerance
	tolerance := l.toleranceConfig.GetDefaultTolerance(currency)
	if !AmountEqual(expectedAmount, actualAmount, tolerance) {
		l.addError(NewBalanceMismatchError(balance, expectedAmount.String(), actualAmount.String(), currency))
	}
}

// processPad processes a Pad directive
func (l *Ledger) processPad(ctx context.Context, pad *ast.Pad) {
	// Create validator with read-only view of current state
	v := newValidator(l.accounts, l.toleranceConfig)

	// Run pure validation
	errs := v.validatePad(ctx, pad)

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
	errs := v.validateNote(ctx, note)

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
	errs := v.validateDocument(ctx, doc)

	// Collect validation errors
	if len(errs) > 0 {
		l.errors = append(l.errors, errs...)
	}

	// Document has no state mutation - just validation
}

// addError adds an error to the error collection
func (l *Ledger) addError(err error) {
	l.errors = append(l.errors, err)
}
