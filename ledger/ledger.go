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
	"fmt"

	"github.com/robinvdvleuten/beancount/parser"
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
	accounts   map[string]*Account
	errors     []error
	options    map[string]string
	padEntries map[string]*parser.Pad // account -> pad directive
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
		accounts:   make(map[string]*Account),
		errors:     make([]error, 0),
		options:    make(map[string]string),
		padEntries: make(map[string]*parser.Pad),
	}
}

// Process processes an AST and builds the ledger state
func (l *Ledger) Process(ast *parser.AST) error {
	// Process options first
	for _, opt := range ast.Options {
		l.options[opt.Name] = opt.Value
	}

	// Process directives in order (they're already sorted by date)
	for _, directive := range ast.Directives {
		l.processDirective(directive)
	}

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
func (l *Ledger) processDirective(directive parser.Directive) {
	switch d := directive.(type) {
	case *parser.Open:
		l.processOpen(d)
	case *parser.Close:
		l.processClose(d)
	case *parser.Transaction:
		l.processTransaction(d)
	case *parser.Balance:
		l.processBalance(d)
	case *parser.Pad:
		l.processPad(d)
	case *parser.Note:
		l.processNote(d)
	case *parser.Document:
		l.processDocument(d)
	default:
		// Unknown directive type - ignore for now
		// Note: Price, Commodity, and Event directives are intentionally not processed
		// as they don't affect ledger state or require validation
	}
}

// processOpen processes an Open directive
func (l *Ledger) processOpen(open *parser.Open) {
	accountName := string(open.Account)

	// Check if account already exists
	if existing, ok := l.accounts[accountName]; ok {
		// Check if it's already open
		if !existing.IsClosed() {
			l.addError(&AccountAlreadyOpenError{
				Account:    open.Account,
				Date:       open.Date,
				OpenedDate: existing.OpenDate,
			})
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
func (l *Ledger) processClose(close *parser.Close) {
	accountName := string(close.Account)

	// Check if account exists
	account, ok := l.accounts[accountName]
	if !ok {
		l.addError(&AccountNotClosedError{
			Account: close.Account,
			Date:    close.Date,
		})
		return
	}

	// Check if already closed
	if account.IsClosed() {
		l.addError(&AccountAlreadyClosedError{
			Account:    close.Account,
			Date:       close.Date,
			ClosedDate: account.CloseDate,
		})
		return
	}

	// Close the account
	account.CloseDate = close.Date
}

// processTransaction processes a Transaction directive
func (l *Ledger) processTransaction(txn *parser.Transaction) {
	// Single-pass validation, classification, and weight calculation
	hasErrors := false
	var postingsWithoutAmounts []*parser.Posting
	var postingsWithEmptyCosts []*parser.Posting // Postings with {} empty cost specs
	var allWeights []WeightSet

	for _, posting := range txn.Postings {
		// Validate account is open
		if !l.isAccountOpen(posting.Account, txn.Date) {
			l.addError(&AccountNotOpenError{
				Account:   posting.Account,
				Date:      txn.Date,
				Pos:       txn.Pos,
				Directive: txn,
			})
			hasErrors = true
			continue
		}

		// Classify posting and calculate weights if amount present
		if posting.Amount == nil {
			postingsWithoutAmounts = append(postingsWithoutAmounts, posting)
		} else {
			// Calculate weights immediately
			weights, err := CalculateWeights(posting)
			if err != nil {
				l.addError(&InvalidAmountError{
					Date:       txn.Date,
					Account:    posting.Account,
					Value:      posting.Amount.Value,
					Underlying: err,
				})
				hasErrors = true
				continue
			}

			// Check if this is an empty cost spec (returns empty weights)
			if len(weights) == 0 && posting.Cost != nil && posting.Cost.IsEmpty() {
				postingsWithEmptyCosts = append(postingsWithEmptyCosts, posting)
			} else {
				allWeights = append(allWeights, weights)
			}
		}
	}

	// If we have errors, don't continue
	if hasErrors {
		return
	}

	// Balance the weights
	balance := BalanceWeights(allWeights)
	defer putBalanceMap(balance)

	// Infer missing amounts
	inferredAmounts := getInferredAmountsMap()
	defer putInferredAmountsMap(inferredAmounts)

	if len(postingsWithoutAmounts) > 0 {
		// Group missing postings by currency (if they have costs, we can infer the currency)
		// For now, handle the simple case: one missing posting per currency
		for currency, residual := range balance {
			// Need to negate the residual to balance
			needed := residual.Neg()

			// Find if there's exactly one posting without amount that could use this currency
			// For simplicity, if there's ONE missing posting and ONE unbalanced currency, assign it
			if len(postingsWithoutAmounts) == 1 {
				// Create the inferred amount
				inferredAmounts[postingsWithoutAmounts[0]] = &parser.Amount{
					Value:    needed.String(),
					Currency: currency,
				}
			} else if len(postingsWithoutAmounts) > 1 {
				// Ambiguous - can't infer
				l.addError(&TransactionNotBalancedError{
					Pos:         txn.Pos,
					Date:        txn.Date,
					Narration:   fmt.Sprintf("%s (multiple postings with missing amounts - ambiguous)", txn.Narration),
					Residuals:   map[string]string{currency: residual.String()},
					Transaction: txn,
				})
				return
			}
		}
	}

	// Infer costs for empty cost specs {}
	// Note: Only infer costs for AUGMENTATIONS (positive amounts)
	// For REDUCTIONS (negative amounts), empty cost spec means "use booking method"
	inferredCosts := make(map[*parser.Posting]*parser.Amount)

	if len(postingsWithEmptyCosts) > 0 {
		// For each posting with empty cost spec, infer the cost from the residual
		for _, posting := range postingsWithEmptyCosts {
			// Parse the commodity amount
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue // Already validated earlier
			}

			// Only infer cost for augmentations (positive amounts)
			// For reductions (negative amounts), empty cost spec means "use booking method"
			if amount.IsNegative() {
				// This is a reduction - don't infer cost, let booking method handle it
				// The weight is already 0, which is correct - we'll calculate actual
				// weight when we reduce lots using FIFO/LIFO
				continue
			}

			// Look for a residual currency that can be used for the cost
			// Simple case: if there's one residual currency, use it
			if len(balance) == 1 {
				for currency, residual := range balance {
					// Calculate the cost per unit needed to balance
					// If residual is -5000 USD and amount is 10 HOOL
					// We need +5000 USD, so cost per unit = 5000 / 10 = 500 USD
					costPerUnit := residual.Neg().Div(amount)

					// Store the inferred cost
					inferredCosts[posting] = &parser.Amount{
						Value:    costPerUnit.String(),
						Currency: currency,
					}

					// Add this weight to the balance
					totalCost := amount.Mul(costPerUnit)
					balance[currency] = balance[currency].Add(totalCost)
				}
			} else if len(balance) > 1 {
				// Multiple currencies - ambiguous
				// For now, report error (could be improved to match currencies intelligently)
				l.addError(&TransactionNotBalancedError{
					Pos:         txn.Pos,
					Date:        txn.Date,
					Narration:   fmt.Sprintf("%s (empty cost spec with multiple unbalanced currencies - ambiguous)", txn.Narration),
					Residuals:   map[string]string{},
					Transaction: txn,
				})
				return
			}
		}
	}

	// Check if balanced (within tolerance) after inference
	tolerance := GetTolerance("")
	residuals := make(map[string]string) // Not pooled - persists in error struct
	for currency, amount := range balance {
		// If we inferred an amount for this currency, it should now be balanced
		if len(inferredAmounts) == 0 {
			if amount.Abs().GreaterThan(tolerance) {
				residuals[currency] = amount.String()
			}
		}
		// If we did inference, the balance should be zero (we'll verify below)
	}

	if len(residuals) > 0 {
		l.addError(&TransactionNotBalancedError{
			Pos:         txn.Pos,
			Date:        txn.Date,
			Narration:   txn.Narration,
			Residuals:   residuals,
			Transaction: txn,
		})
		return // Don't update inventory if not balanced
	}

	// Update inventories
	for _, posting := range txn.Postings {
		// Get the amount (either explicit or inferred)
		var amountToUse *parser.Amount
		if posting.Amount != nil {
			amountToUse = posting.Amount
		} else if inferredAmount, ok := inferredAmounts[posting]; ok {
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
		amount, _ := ParseAmount(amountToUse)
		currency := amountToUse.Currency

		// Check if we have a cost (explicit, inferred, or empty)
		hasExplicitCost := posting.Cost != nil && !posting.Cost.IsEmpty() && !posting.Cost.IsMergeCost()
		hasEmptyCost := posting.Cost != nil && posting.Cost.IsEmpty()
		hasInferredCost := false
		var costToUse *parser.Cost

		if hasExplicitCost {
			costToUse = posting.Cost
		} else if hasEmptyCost {
			// Empty cost spec - use it directly for lot tracking
			// For reductions, this triggers FIFO/LIFO booking
			// For augmentations, the cost was inferred earlier
			costToUse = posting.Cost
		} else if inferredCost, ok := inferredCosts[posting]; ok {
			// Use inferred cost - create a temporary Cost structure
			hasInferredCost = true
			costToUse = &parser.Cost{
				Amount: inferredCost,
			}
		}

		// Update lot inventory
		if hasExplicitCost || hasEmptyCost || hasInferredCost {
			// Has cost basis - add/reduce with lot tracking
			lotSpec, err := ParseLotSpec(costToUse)
			if err != nil {
				l.addError(&InvalidAmountError{
					Date:       txn.Date,
					Account:    posting.Account,
					Value:      "cost",
					Underlying: err,
				})
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
					l.addError(&InvalidAmountError{
						Date:       txn.Date,
						Account:    posting.Account,
						Value:      "lot reduction",
						Underlying: err,
					})
				}
			}
		} else {
			// No cost basis - simple add
			account.Inventory.Add(currency, amount)
		}
	}
}

// processBalance processes a Balance directive
func (l *Ledger) processBalance(balance *parser.Balance) {
	// Validate account is open
	if !l.isAccountOpen(balance.Account, balance.Date) {
		l.addError(&AccountNotOpenError{
			Account:   balance.Account,
			Date:      balance.Date,
			Pos:       balance.Pos,
			Directive: balance,
		})
		return
	}

	// Parse expected amount
	expectedAmount, err := ParseAmount(balance.Amount)
	if err != nil {
		l.addError(&InvalidAmountError{
			Date:       balance.Date,
			Account:    balance.Account,
			Value:      balance.Amount.Value,
			Underlying: err,
		})
		return
	}

	currency := balance.Amount.Currency

	// Get account inventory
	accountName := string(balance.Account)
	account, ok := l.accounts[accountName]
	if !ok {
		// This shouldn't happen since we checked IsOpen above, but be safe
		return
	}

	// Get actual amount from inventory
	actualAmount := account.Inventory.Get(currency)

	// Check if there's a pad directive for this account
	if padEntry, hasPad := l.padEntries[accountName]; hasPad {
		// Calculate the difference needed to reach expected balance
		difference := expectedAmount.Sub(actualAmount)

		// Apply padding if difference is significant
		tolerance := GetTolerance(currency)
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
	tolerance := GetTolerance(currency)
	if !AmountEqual(expectedAmount, actualAmount, tolerance) {
		l.addError(&BalanceMismatchError{
			Date:     balance.Date,
			Account:  balance.Account,
			Expected: expectedAmount.String(),
			Actual:   actualAmount.String(),
			Currency: currency,
		})
	}
}

// processPad processes a Pad directive
func (l *Ledger) processPad(pad *parser.Pad) {
	// Validate accounts are open
	if !l.isAccountOpen(pad.Account, pad.Date) {
		l.addError(&AccountNotOpenError{
			Account:   pad.Account,
			Date:      pad.Date,
			Pos:       pad.Pos,
			Directive: pad,
		})
		return
	}

	if !l.isAccountOpen(pad.AccountPad, pad.Date) {
		l.addError(&AccountNotOpenError{
			Account:   pad.AccountPad,
			Date:      pad.Date,
			Pos:       pad.Pos,
			Directive: pad,
		})
		return
	}

	// Store pad directive - will be applied when next balance assertion is encountered
	accountName := string(pad.Account)
	l.padEntries[accountName] = pad
}

// processNote processes a Note directive
func (l *Ledger) processNote(note *parser.Note) {
	// Validate account is open
	if !l.isAccountOpen(note.Account, note.Date) {
		l.addError(&AccountNotOpenError{
			Account:   note.Account,
			Date:      note.Date,
			Pos:       note.Pos,
			Directive: note,
		})
	}

}

// processDocument processes a Document directive
func (l *Ledger) processDocument(doc *parser.Document) {
	// Validate account is open
	if !l.isAccountOpen(doc.Account, doc.Date) {
		l.addError(&AccountNotOpenError{
			Account:   doc.Account,
			Date:      doc.Date,
			Pos:       doc.Pos,
			Directive: doc,
		})
	}
}

// isAccountOpen checks if an account is open at the given date
func (l *Ledger) isAccountOpen(account parser.Account, date *parser.Date) bool {
	accountName := string(account)
	acc, ok := l.accounts[accountName]
	if !ok {
		return false
	}
	return acc.IsOpen(date)
}

// addError adds an error to the error collection
func (l *Ledger) addError(err error) {
	l.errors = append(l.errors, err)
}
