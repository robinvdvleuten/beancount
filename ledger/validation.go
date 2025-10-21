package ledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Validation Architecture
//
// The ledger uses a two-phase approach for processing directives:
//
// 1. Validation Phase (Pure Functions)
//   - Checks all business rules without side effects
//   - Uses the validator type with read-only access to ledger state
//   - Returns all errors found (doesn't short-circuit)
//   - Produces delta objects describing planned mutations
//   - Runs with telemetry instrumentation for performance monitoring
//
// 2. Mutation Phase (State Changes)
//   - Only executes if validation passes
//   - Applies deltas to update account state
//   - Can assume all inputs are valid
//
// Validation Flow:
//
// TRANSACTIONS:
//   Ledger.processTransaction(txn)
//     ↓
//   validator.validateTransaction(ctx, txn) → ([]error, *TransactionDelta)
//     ├─ validateAccountsOpen()           // Check accounts exist and are open
//     ├─ validateAmounts()                // Check amounts are parseable
//     ├─ validateCosts()                  // Check cost specifications
//     ├─ validatePrices()                 // Check price specifications
//     ├─ validateMetadata()               // Check metadata entries
//     ├─ calculateBalance()               // Calculate weights, check balance
//     ├─ validateInventoryOperations()    // Check lot operations
//     └─ validateConstraintCurrencies()   // Check currency restrictions
//     ↓
//   Ledger.applyTransaction(txn, delta)
//     └─ Apply inferred amounts/costs, update inventories
//
// OPEN DIRECTIVES:
//   Ledger.processOpen(open)
//     ↓
//   validator.validateOpen(ctx, open) → ([]error, *OpenDelta)
//     ├─ Check account doesn't already exist
//     └─ Parse account type, copy metadata
//     ↓
//   Ledger.applyOpen(delta)
//     └─ Create account with properties
//
// CLOSE DIRECTIVES:
//   Ledger.processClose(close)
//     ↓
//   validator.validateClose(ctx, close) → ([]error, *CloseDelta)
//     ├─ Check account exists
//     └─ Check account is not already closed
//     ↓
//   Ledger.applyClose(delta)
//     └─ Set account close date
//
// BALANCE ASSERTIONS:
//   Ledger.processBalance(balance)
//     ↓
//   validator.validateBalance(ctx, balance) → []error
//     ├─ Check account exists and is open
//     └─ Check amount is parseable
//     ↓
//   validator.calculateBalanceDelta(ctx, balance, pad) → (*BalanceDelta, error)
//     ├─ Validate pad timing (must come before balance)
//     ├─ Calculate padding adjustments if needed
//     └─ Check balance matches within tolerance
//     ↓
//   Ledger.applyBalance(delta)
//     └─ Apply padding transaction, remove pad entry
//
// PAD DIRECTIVES:
//   Ledger.processPad(pad)
//     ↓
//   validator.validatePad(ctx, pad) → ([]error, *PadDelta)
//     ├─ Check main account exists and is open
//     └─ Check pad account exists and is open
//     ↓
//   Ledger.applyPad(delta)
//     └─ Store pad entry for next balance assertion
//
// NOTE/DOCUMENT DIRECTIVES:
//   validator.validateNote(ctx, note) → []error
//   validator.validateDocument(ctx, doc) → []error
//     └─ Check account exists and is open
//     (No mutations needed - validation only)
//
// Delta Types:
//   - TransactionDelta: InferredAmounts, InferredCosts
//   - OpenDelta: Account properties (name, type, currencies, metadata)
//   - CloseDelta: Account name, close date
//   - BalanceDelta: PaddingAdjustments, pad account, removal flag
//   - PadDelta: Stores pad entry for later use
//
// Validation Methods:
//   Transaction: validateTransaction, validateAccountsOpen, validateAmounts,
//                validateCosts, validatePrices, validateMetadata, calculateBalance,
//                validateInventoryOperations, validateConstraintCurrencies
//   Open:        validateOpen
//   Close:       validateClose
//   Balance:     validateBalance, calculateBalanceDelta
//   Pad:         validatePad
//   Note:        validateNote
//   Document:    validateDocument
//
// This separation provides several benefits:
//   - Individual validation rules can be tested in isolation
//   - Validation can be performed without mutating state (dry-run mode)
//   - Clear separation of concerns (validation vs mutation)
//   - Better error reporting (all errors found, not just first)
//   - Delta inspection allows observing planned changes before applying
//   - Easier to understand and maintain
//
// Example: Normal usage
//
//   v := newValidator(ledger.accounts, ledger.toleranceConfig)
//   errs, delta := v.validateTransaction(ctx, txn)
//   if len(errs) > 0 {
//       // Handle validation errors
//       for _, err := range errs {
//           fmt.Println(err)
//       }
//       return
//   }
//   // Validation passed - safe to mutate state
//   ledger.applyTransaction(txn, delta)
//
// Example: Dry-run mode (inspect deltas without applying)
//
//   v := newValidator(ledger.accounts, ledger.toleranceConfig)
//   errs, delta := v.validateTransaction(ctx, txn)
//   if len(errs) == 0 {
//       // Inspect planned mutations without applying
//       log.Printf("Would infer %d amounts", len(delta.InferredAmounts))
//       log.Printf("Would infer %d costs", len(delta.InferredCosts))
//       for posting, amount := range delta.InferredAmounts {
//           log.Printf("  %s: %s %s", posting.Account, amount.Value, amount.Currency)
//       }
//       // Don't call applyTransaction - just inspect!
//   }
//
//   // Balance assertions with padding
//   errs := v.validateBalance(ctx, balance)
//   if len(errs) == 0 {
//       delta, err := v.calculateBalanceDelta(ctx, balance, padEntry)
//       if err == nil && delta.HasMutations() {
//           log.Printf("Would add padding: %v", delta.PaddingAdjustments)
//           log.Printf("Pad from account: %s", delta.PadAccountName)
//           // Don't call applyBalance - just inspect!
//       }
//   }

// validator provides transaction validation with read-only access to ledger state.
// This is a separate type from Ledger to ensure validation cannot mutate state.
type validator struct {
	accounts        map[string]*Account
	toleranceConfig *ToleranceConfig
}

// newValidator creates a validator with a read-only view of the current ledger state
func newValidator(accounts map[string]*Account, toleranceConfig *ToleranceConfig) *validator {
	return &validator{
		accounts:        accounts,
		toleranceConfig: toleranceConfig,
	}
}

// postingClassification groups postings by their characteristics
// This makes the processing logic clearer and prevents misclassification
type postingClassification struct {
	withAmounts      []*ast.Posting
	withoutAmounts   []*ast.Posting
	withEmptyCosts   []*ast.Posting
	withExplicitCost []*ast.Posting
}

// validateAccountsOpen checks all posting accounts are open at transaction date.
//
// It validates that:
//   - Each account referenced in postings exists in the ledger
//   - Each account is open on or before the transaction date
//   - Each account is not closed before the transaction date
//
// Returns a slice of AccountNotOpenError for any accounts that fail validation.
// An empty slice indicates all accounts are valid.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs := v.validateAccountsOpen(ctx, txn)
//	if len(errs) > 0 {
//	    // txn references closed or non-existent accounts
//	    for _, err := range errs {
//	        fmt.Printf("Account error: %v\n", err)
//	    }
//	}
func (v *validator) validateAccountsOpen(ctx context.Context, txn *ast.Transaction) []error {
	var errs []error
	for _, posting := range txn.Postings {
		accountName := string(posting.Account)
		acc, exists := v.accounts[accountName]
		if !exists {
			errs = append(errs, NewAccountNotOpenError(txn, posting.Account))
			continue
		}
		if !acc.IsOpen(txn.Date) {
			errs = append(errs, NewAccountNotOpenError(txn, posting.Account))
		}
	}
	return errs
}

// validateAmounts checks all amounts can be parsed
func (v *validator) validateAmounts(ctx context.Context, txn *ast.Transaction) []error {
	var errs []error
	for _, posting := range txn.Postings {
		if posting.Amount == nil {
			continue // Will be inferred, checked later
		}
		if _, err := ParseAmount(posting.Amount); err != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
		}
	}
	return errs
}

// validateCosts checks all cost specifications are valid.
//
// It validates that:
//   - Cost amounts are parseable as decimal numbers
//   - Cost dates are valid (not zero dates)
//   - Cost labels are non-empty if present
//   - Merge costs {*} are flagged as not yet implemented
//   - Empty costs {} are accepted (for automatic lot selection)
//   - ParseLotSpec can parse the cost specification
//
// Returns a slice of InvalidCostError for any invalid cost specifications.
// Includes posting index and cost spec string for clear error messages.
//
// Example:
//
//	// Valid cost: 10 HOOL {500.00 USD}
//	v := newValidator(ledger.accounts)
//	errs := v.validateCosts(ctx, txn)
//	if len(errs) > 0 {
//	    // Found invalid cost specifications
//	    for _, err := range errs {
//	        fmt.Printf("Cost error: %v\n", err)
//	        // Example: "2024-01-15: Invalid cost specification (Posting #1: Assets:Stock): {abc USD}: invalid decimal"
//	    }
//	}
func (v *validator) validateCosts(ctx context.Context, txn *ast.Transaction) []error {
	var errs []error
	for i, posting := range txn.Postings {
		if posting.Cost == nil {
			continue // No cost specification
		}

		// Empty cost {} is valid
		if posting.Cost.IsEmpty() {
			continue
		}

		// Validate cost amount if present
		if posting.Cost.Amount != nil {
			if _, err := ParseAmount(posting.Cost.Amount); err != nil {
				costSpec := fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
			}
		}

		// NEW: Validate ParseLotSpec can parse the cost
		if _, err := ParseLotSpec(posting.Cost); err != nil {
			costSpec := "{...}"
			if posting.Cost.Amount != nil {
				costSpec = fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
			}
			errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
		}

		// Validate cost date if present
		if posting.Cost.Date != nil {
			// Date is already validated by the parser, but we can add additional checks
			// For now, just ensure it's not a zero date
			if posting.Cost.Date.IsZero() {
				costSpec := "{...}"
				if posting.Cost.Amount != nil {
					costSpec = fmt.Sprintf("{%s %s, ...}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				}
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec,
					fmt.Errorf("cost date cannot be zero")))
			}
		}

		// Validate cost label if present
		if posting.Cost.Label != "" {
			// Labels must be non-empty strings (already guaranteed by parser)
			// But we can add validation for label format if needed
			if len(posting.Cost.Label) == 0 {
				costSpec := "{...}"
				if posting.Cost.Amount != nil {
					costSpec = fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				}
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec,
					fmt.Errorf("cost label cannot be empty")))
			}
		}
	}
	return errs
}

// validatePrices checks all price specifications are valid.
//
// It validates that:
//   - Price amounts are parseable as decimal numbers
//   - Per-unit prices (@) and total prices (@@) are correctly formatted
//
// Returns a slice of InvalidPriceError for any invalid price specifications.
// Includes posting index and price spec string for clear error messages.
//
// Example:
//
//	// Valid price: 100 EUR @ 1.20 USD
//	v := newValidator(ledger.accounts)
//	errs := v.validatePrices(ctx, txn)
//	if len(errs) > 0 {
//	    // Found invalid price specifications
//	    for _, err := range errs {
//	        fmt.Printf("Price error: %v\n", err)
//	        // Example: "2024-01-15: Invalid price specification (Posting #2: Expenses:Foreign): @ abc USD: invalid decimal"
//	    }
//	}
func (v *validator) validatePrices(ctx context.Context, txn *ast.Transaction) []error {
	var errs []error
	for i, posting := range txn.Postings {
		if posting.Price == nil {
			continue // No price specification
		}

		// Validate price amount
		if _, err := ParseAmount(posting.Price); err != nil {
			priceSpec := fmt.Sprintf("@ %s %s", posting.Price.Value, posting.Price.Currency)
			if posting.PriceTotal {
				priceSpec = fmt.Sprintf("@@ %s %s", posting.Price.Value, posting.Price.Currency)
			}
			errs = append(errs, NewInvalidPriceError(txn, posting.Account, i, priceSpec, err))
			continue
		}

		// Validate that price currency differs from posting currency
		// (It's valid but unusual to have the same currency)
		// For now, we'll allow it but could add a warning system later
	}
	return errs
}

// validateMetadata checks metadata entries are valid.
//
// It validates that:
//   - Metadata keys are not duplicated within a directive
//   - Metadata keys are not duplicated within a posting
//   - Metadata values are non-empty
//
// Checks both transaction-level and posting-level metadata.
//
// Returns a slice of InvalidMetadataError for any invalid metadata entries.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs := v.validateMetadata(ctx, txn)
//	if len(errs) > 0 {
//	    // Found invalid or duplicate metadata
//	    for _, err := range errs {
//	        fmt.Printf("Metadata error: %v\n", err)
//	        // Example: "2024-01-15: Invalid metadata: key="invoice", value="": empty value"
//	        // Example: "2024-01-15: Invalid metadata (account Assets:Checking): key="note", value="xyz": duplicate key"
//	    }
//	}
func (v *validator) validateMetadata(ctx context.Context, txn *ast.Transaction) []error {
	var errs []error

	// Validate transaction-level metadata
	if len(txn.Metadata) > 0 {
		seen := make(map[string]bool)
		for _, meta := range txn.Metadata {
			// Check for duplicate keys
			if seen[meta.Key] {
				errs = append(errs, NewInvalidMetadataError(txn, "", meta.Key, meta.Value, "duplicate key"))
				continue
			}
			seen[meta.Key] = true

			// Check for empty values (empty string values)
			if meta.Value != nil && meta.Value.StringValue != nil && *meta.Value.StringValue == "" {
				errs = append(errs, NewInvalidMetadataError(txn, "", meta.Key, meta.Value, "empty value"))
			}
		}
	}

	// Validate posting-level metadata
	for _, posting := range txn.Postings {
		if len(posting.Metadata) > 0 {
			seen := make(map[string]bool)
			for _, meta := range posting.Metadata {
				// Check for duplicate keys
				if seen[meta.Key] {
					errs = append(errs, NewInvalidMetadataError(txn, posting.Account, meta.Key, meta.Value, "duplicate key"))
					continue
				}
				seen[meta.Key] = true

				// Check for empty values (empty string values)
				if meta.Value != nil && meta.Value.StringValue != nil && *meta.Value.StringValue == "" {
					errs = append(errs, NewInvalidMetadataError(txn, posting.Account, meta.Key, meta.Value, "empty value"))
				}
			}
		}
	}

	return errs
}

// classifyPostings categorizes postings for different processing paths
func classifyPostings(postings []*ast.Posting) postingClassification {
	var pc postingClassification
	for _, posting := range postings {
		if posting.Amount == nil {
			pc.withoutAmounts = append(pc.withoutAmounts, posting)
		} else {
			pc.withAmounts = append(pc.withAmounts, posting)

			if posting.Cost != nil && posting.Cost.IsEmpty() {
				pc.withEmptyCosts = append(pc.withEmptyCosts, posting)
			} else if posting.Cost != nil && !posting.Cost.IsEmpty() && !posting.Cost.IsMergeCost() {
				pc.withExplicitCost = append(pc.withExplicitCost, posting)
			}
		}
	}
	return pc
}

// calculateBalance computes weights, infers amounts/costs, and checks if transaction balances.
// Returns delta (mutations), validation (balance state), and errors.
// This is the core transaction validation logic.
func (v *validator) calculateBalance(ctx context.Context, txn *ast.Transaction) (*TransactionDelta, *balanceValidation, []error) {
	var errs []error
	pc := classifyPostings(txn.Postings)

	// Calculate weights for postings with amounts
	var allWeights []weightSet
	for _, posting := range pc.withAmounts {
		weights, err := calculateWeights(posting)
		if err != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
			continue
		}

		// Check if this is an empty cost spec (returns empty weights)
		if len(weights) == 0 && posting.Cost != nil && posting.Cost.IsEmpty() {
			// Don't add to allWeights yet - will be handled in inference
		} else {
			allWeights = append(allWeights, weights)
		}
	}

	if len(errs) > 0 {
		return nil, nil, errs
	}

	// Balance the weights
	balance := balanceWeights(allWeights)
	defer putBalanceMap(balance)

	delta := &TransactionDelta{
		InferredAmounts: make(map[*ast.Posting]*ast.Amount),
		InferredCosts:   make(map[*ast.Posting]*ast.Amount),
	}

	// Infer missing amounts if possible
	// Can only infer if exactly 1 posting without amount AND exactly 1 currency with residual
	if len(pc.withoutAmounts) == 1 && len(balance) == 1 {
		for currency, residual := range balance {
			needed := residual.Neg()
			delta.InferredAmounts[pc.withoutAmounts[0]] = &ast.Amount{
				Value:    needed.String(),
				Currency: currency,
			}
			// Update balance to reflect the inferred amount
			balance[currency] = balance[currency].Add(needed)
		}
	} else if len(pc.withoutAmounts) > 1 {
		// Multiple postings without amounts - ambiguous, can't infer
		residuals := make(map[string]decimal.Decimal)
		for currency, residual := range balance {
			residuals[currency] = residual
		}
		validation := &balanceValidation{
			isBalanced: false,
			residuals:  residuals,
		}
		return delta, validation, nil
	} else if len(pc.withoutAmounts) == 1 && len(balance) > 1 {
		// One posting without amount but multiple currencies - ambiguous
		residuals := make(map[string]decimal.Decimal)
		for currency, residual := range balance {
			residuals[currency] = residual
		}
		validation := &balanceValidation{
			isBalanced: false,
			residuals:  residuals,
		}
		return delta, validation, nil
	}

	// Infer costs for empty cost specs {}
	if len(pc.withEmptyCosts) > 0 {
		// Count positive amount empty costs (augmentations that need cost inference)
		positiveEmptyCosts := 0
		for _, posting := range pc.withEmptyCosts {
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue
			}
			if !amount.IsNegative() {
				positiveEmptyCosts++
			}
		}

		// Beancount compliance: Cannot infer costs when multiple postings have empty cost specs
		// This is ambiguous - which posting gets which portion of the residual?
		if positiveEmptyCosts > 1 {
			residuals := make(map[string]decimal.Decimal)
			for currency, residual := range balance {
				residuals[currency] = residual
			}
			validation := &balanceValidation{
				isBalanced: false,
				residuals:  residuals,
			}
			return delta, validation, nil
		}

		for _, posting := range pc.withEmptyCosts {
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue
			}

			// Only infer cost for augmentations (positive amounts)
			if amount.IsNegative() {
				continue
			}

			if len(balance) == 1 {
				for currency, residual := range balance {
					costPerUnit := residual.Neg().Div(amount)
					delta.InferredCosts[posting] = &ast.Amount{
						Value:    costPerUnit.String(),
						Currency: currency,
					}

					totalCost := amount.Mul(costPerUnit)
					balance[currency] = balance[currency].Add(totalCost)
				}
			} else if len(balance) > 1 {
				// Multiple currencies - ambiguous
				residuals := make(map[string]decimal.Decimal)
				for currency, residual := range balance {
					residuals[currency] = residual
				}
				validation := &balanceValidation{
					isBalanced: false,
					residuals:  residuals,
				}
				return delta, validation, nil
			}
		}
	}

	// Check if balanced (within tolerance) after inference
	amountsByCurrency := make(map[string][]decimal.Decimal)

	// Collect explicit amounts
	for _, posting := range txn.Postings {
		if posting.Amount != nil {
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue
			}
			currency := posting.Amount.Currency
			amountsByCurrency[currency] = append(amountsByCurrency[currency], amount)
		}
	}

	// Include inferred amounts for tolerance calculation
	for _, inferredAmount := range delta.InferredAmounts {
		amount, _ := ParseAmount(inferredAmount)
		currency := inferredAmount.Currency
		amountsByCurrency[currency] = append(amountsByCurrency[currency], amount)
	}

	// Check each currency balance with inferred tolerance
	residuals := make(map[string]decimal.Decimal)
	for currency, residual := range balance {
		amounts := amountsByCurrency[currency]
		tolerance := InferTolerance(amounts, currency, v.toleranceConfig)

		// Always check residuals against tolerance (even with inferred amounts)
		if residual.Abs().GreaterThan(tolerance) {
			residuals[currency] = residual
		}
	}

	validation := &balanceValidation{
		isBalanced: len(residuals) == 0,
		residuals:  residuals,
	}

	return delta, validation, nil
}

// validateTransaction runs all validation checks on a transaction.
//
// This is the main entry point for transaction validation. It orchestrates
// all validation steps in sequence and collects all errors found.
//
// Validation steps (in order):
//  1. validateAccountsOpen - Check accounts exist and are open
//  2. validateAmounts - Check amounts are parseable
//  3. validateCosts - Check cost specifications are valid
//  4. validatePrices - Check price specifications are valid
//  5. validateMetadata - Check metadata entries are valid
//  6. calculateBalance - Calculate weights, infer amounts, check balance
//
// The validation does NOT short-circuit on first error. Instead, it collects
// all validation errors to provide comprehensive feedback to the user.
//
// Returns:
//   - []error: All validation errors found (empty if validation passed)
//   - *TransactionDelta: Mutation plan (nil if validation failed)
//
// Performance: ~955ns/op with telemetry instrumentation enabled.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs, delta := v.validateTransaction(ctx, txn)
//	if len(errs) > 0 {
//	    // Validation failed
//	    fmt.Printf("Found %d validation errors:\n", len(errs))
//	    for _, err := range errs {
//	        fmt.Printf("  - %v\n", err)
//	    }
//	    return
//	}
//	// Validation passed - delta contains inferred amounts/costs
//	fmt.Printf("Inferred %d amounts\n", len(delta.InferredAmounts))
func (v *validator) validateTransaction(ctx context.Context, txn *ast.Transaction) ([]error, *TransactionDelta) {
	var allErrors []error

	// 1. Validate accounts are open
	if errs := v.validateAccountsOpen(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 2. Validate amounts are parseable
	if errs := v.validateAmounts(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 3. Validate cost specifications
	if errs := v.validateCosts(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 4. Validate price specifications
	if errs := v.validatePrices(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 5. Validate metadata
	if errs := v.validateMetadata(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// If basic validation failed, don't proceed to balance calculation
	if len(allErrors) > 0 {
		return allErrors, nil
	}

	// 6. Calculate balance and infer amounts
	delta, validation, errs := v.calculateBalance(ctx, txn)
	if len(errs) > 0 {
		allErrors = append(allErrors, errs...)
		return allErrors, nil
	}

	// 7. Check if balanced
	if !validation.isBalanced {
		// Convert decimal.Decimal residuals to string for error reporting
		residualStrings := make(map[string]string)
		for currency, amount := range validation.residuals {
			residualStrings[currency] = amount.String()
		}
		allErrors = append(allErrors, NewTransactionNotBalancedError(txn, residualStrings))
	}

	// If balance check failed, return early (can't validate constraints without valid delta)
	if len(allErrors) > 0 {
		return allErrors, nil
	}

	// 8. Validate constraint currencies (AFTER inference so we can check inferred amounts)
	if errs := v.validateConstraintCurrencies(ctx, txn, delta); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 9. Validate inventory operations
	if errs := v.validateInventoryOperations(ctx, txn, delta); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// If any post-balance validation failed, return errors
	if len(allErrors) > 0 {
		return allErrors, nil
	}

	// All validation passed
	return nil, delta
}

// validateBalance checks if a balance directive is valid.
//
// It validates that:
//   - The account exists and is open at the balance date
//   - The balance amount is parseable as a decimal number
//
// Balance directives assert that an account has a specific balance at a given date.
// This validator only checks the directive syntax and account state, not the actual
// balance (which is checked during the mutation phase).
//
// Returns a slice of errors for validation failures.
//
// Example:
//
//	// Valid: 2024-01-15 balance Assets:Checking 100.00 USD
//	v := newValidator(ledger.accounts)
//	errs := v.validateBalance(ctx, balance)
//	if len(errs) > 0 {
//	    // Account doesn't exist or amount is invalid
//	    for _, err := range errs {
//	        fmt.Printf("Balance validation error: %v\n", err)
//	    }
//	}
func (v *validator) validateBalance(ctx context.Context, balance *ast.Balance) []error {
	var errs []error

	// 1. Validate account is open
	accountName := string(balance.Account)
	acc, exists := v.accounts[accountName]
	if !exists {
		errs = append(errs, NewAccountNotOpenErrorFromBalance(balance))
		return errs
	}

	if !acc.IsOpen(balance.Date) {
		errs = append(errs, NewAccountNotOpenErrorFromBalance(balance))
		return errs
	}

	// 2. Validate amount is parseable
	if _, err := ParseAmount(balance.Amount); err != nil {
		errs = append(errs, NewInvalidAmountErrorFromBalance(balance, err))
		return errs
	}

	return errs
}

// validatePad checks if a pad directive is valid.
//
// It validates that:
//   - The main account exists and is open at the pad date
//   - The pad account exists and is open at the pad date
//
// Pad directives automatically insert transactions to bring an account to a specific
// balance determined by the next balance assertion. Both the account being padded
// and the equity account used for padding must be open.
//
// Returns a slice of errors for validation failures.
//
// Example:
//
//	// Valid: 2024-01-01 pad Assets:Checking Equity:Opening-Balances
//	v := newValidator(ledger.accounts)
//	errs := v.validatePad(ctx, pad)
//	if len(errs) > 0 {
//	    // One or both accounts don't exist or are closed
//	    for _, err := range errs {
//	        fmt.Printf("Pad validation error: %v\n", err)
//	    }
//	}
func (v *validator) validatePad(ctx context.Context, pad *ast.Pad) []error {
	var errs []error

	// 1. Validate main account is open
	if !v.isAccountOpen(pad.Account, pad.Date) {
		errs = append(errs, NewAccountNotOpenErrorFromPad(pad, pad.Account))
	}

	// 2. Validate pad account is open
	if !v.isAccountOpen(pad.AccountPad, pad.Date) {
		errs = append(errs, NewAccountNotOpenErrorFromPad(pad, pad.AccountPad))
	}

	return errs
}

// validateNote checks if a note directive is valid.
//
// It validates that:
//   - The account exists and is open at the note date
//   - The description is non-empty (enforced by parser, checked for safety)
//
// Note directives attach dated comments to accounts for documentation purposes.
//
// Returns a slice of errors for validation failures.
//
// Example:
//
//	// Valid: 2024-07-09 note Assets:Checking "Called bank about pending deposit"
//	v := newValidator(ledger.accounts)
//	errs := v.validateNote(ctx, note)
//	if len(errs) > 0 {
//	    // Account doesn't exist or is closed
//	    for _, err := range errs {
//	        fmt.Printf("Note validation error: %v\n", err)
//	    }
//	}
func (v *validator) validateNote(ctx context.Context, note *ast.Note) []error {
	var errs []error

	// 1. Validate account is open
	if !v.isAccountOpen(note.Account, note.Date) {
		errs = append(errs, NewAccountNotOpenErrorFromNote(note))
	}

	// 2. Validate description is non-empty
	if note.Description == "" {
		// This is already enforced by the parser, but check anyway
		errs = append(errs, fmt.Errorf("note description cannot be empty"))
	}

	return errs
}

// validateDocument checks if a document directive is valid.
//
// Validates that the account exists and is open at the document date.
// Document directives link external files to accounts for audit trails.
func (v *validator) validateDocument(ctx context.Context, doc *ast.Document) []error {
	var errs []error

	// 1. Validate account is open
	if !v.isAccountOpen(doc.Account, doc.Date) {
		errs = append(errs, NewAccountNotOpenErrorFromDocument(doc))
	}

	// 2. Validate path is non-empty
	if doc.PathToDocument == "" {
		// This is already enforced by the parser, but check anyway
		errs = append(errs, fmt.Errorf("document path cannot be empty"))
	}

	return errs
}

// isAccountOpen checks if an account is open at the given date
func (v *validator) isAccountOpen(account ast.Account, date *ast.Date) bool {
	accountName := string(account)
	acc, ok := v.accounts[accountName]
	if !ok {
		return false
	}
	return acc.IsOpen(date)
}

// validateOpen validates an open directive.
//
// It validates that:
//   - Account does not already exist (duplicate open directives are errors)
//   - Account name is valid
//   - Copies metadata and constraint currencies to avoid shared AST references
//
// Beancount compliance: Reopening a closed account is NOT allowed.
// Any duplicate open directive is an error, regardless of whether the account
// was previously closed.
//
// Returns validation errors and OpenDelta for the mutations to apply.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs, delta := v.validateOpen(ctx, openDirective)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateOpen(ctx context.Context, open *ast.Open) ([]error, *OpenDelta) {
	var errs []error
	accountName := string(open.Account)

	// Check if account already exists - duplicate open is always an error
	if existing, ok := v.accounts[accountName]; ok {
		errs = append(errs, NewAccountAlreadyOpenError(open, existing.OpenDate))
		return errs, nil
	}

	// Copy metadata and constraint currencies to avoid shared references with AST
	metadataCopy := make([]*ast.Metadata, len(open.Metadata))
	copy(metadataCopy, open.Metadata)

	constraintCurrenciesCopy := make([]string, len(open.ConstraintCurrencies))
	copy(constraintCurrenciesCopy, open.ConstraintCurrencies)

	// Build delta with account properties (avoid allocating Inventory during validation)
	delta := &OpenDelta{
		AccountName:          accountName,
		AccountType:          ParseAccountType(open.Account),
		OpenDate:             open.Date,
		ConstraintCurrencies: constraintCurrenciesCopy,
		BookingMethod:        open.BookingMethod,
		Metadata:             metadataCopy,
	}

	return errs, delta
}

// validateClose validates a close directive.
//
// It validates that:
//   - Account exists in the ledger
//   - Account is not already closed
//
// Returns validation errors and CloseDelta for the mutations to apply.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs, delta := v.validateClose(ctx, closeDirective)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateClose(ctx context.Context, close *ast.Close) ([]error, *CloseDelta) {
	var errs []error
	accountName := string(close.Account)

	// Check if account exists
	account, ok := v.accounts[accountName]
	if !ok {
		errs = append(errs, NewAccountNotClosedError(close))
		return errs, nil
	}

	// Check if already closed
	if account.IsClosed() {
		errs = append(errs, NewAccountAlreadyClosedError(close, account.CloseDate))
		return errs, nil
	}

	delta := &CloseDelta{
		AccountName: accountName,
		CloseDate:   close.Date,
	}

	return errs, delta
}

// createPaddingTransaction creates a synthetic transaction for pad directive.
// The transaction has flag "P" and narration matching official beancount format.
//
// Example output:
//
//	2020-01-01 P "(Padding inserted for Balance of 1000.00 USD for difference 1000.00 USD)"
//	  Assets:Checking         1000.00 USD
//	  Equity:Opening-Balances -1000.00 USD
func createPaddingTransaction(
	date *ast.Date,
	paddedAccount ast.Account,
	padSourceAccount ast.Account,
	difference decimal.Decimal,
	differenceStr string, // Original string representation for formatting
	currency string,
	expectedAmount decimal.Decimal,
	expectedAmountStr string, // Original string representation for formatting
) *ast.Transaction {
	// Format narration matching official beancount
	// Use strings.Builder for efficient string construction
	var narration strings.Builder
	narration.WriteString("(Padding inserted for Balance of ")
	narration.WriteString(expectedAmountStr)
	narration.WriteString(" ")
	narration.WriteString(currency)
	narration.WriteString(" for difference ")
	narration.WriteString(differenceStr)
	narration.WriteString(" ")
	narration.WriteString(currency)
	narration.WriteString(")")

	// Calculate negative amount string (preserve formatting)
	var negDifferenceStr string
	if differenceStr[0] == '-' {
		negDifferenceStr = differenceStr[1:] // Remove minus sign
	} else {
		negDifferenceStr = "-" + differenceStr // Add minus sign
	}

	// Build transaction using AST builders
	txn := ast.NewTransaction(date, narration.String(),
		ast.WithFlag("P"),
		ast.WithPostings(
			ast.NewPosting(paddedAccount,
				ast.WithAmount(differenceStr, currency),
			),
			ast.NewPosting(padSourceAccount,
				ast.WithAmount(negDifferenceStr, currency),
			),
		),
	)

	return txn
}

// calculateBalanceDelta calculates the balance delta for a balance assertion.
//
// It validates that:
//   - Pad directive (if present) comes chronologically BEFORE the balance assertion
//   - Account balance matches expected balance (within tolerance)
//   - Calculates padding adjustments needed
//   - Generates synthetic padding transaction if needed
//
// Returns BalanceDelta (mutations) and error (validation failure).
// Errors are returned separately from the delta to keep deltas pure.
//
// CRITICAL: Pad timing validation - pad must come BEFORE balance (Beancount compliance).
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	delta, err := v.calculateBalanceDelta(ctx, balance, padEntry)
//	if err != nil {
//	    // Validation failed
//	}
func (v *validator) calculateBalanceDelta(ctx context.Context,
	balance *ast.Balance,
	padEntry *ast.Pad) (*BalanceDelta, error) {

	// Basic validation already done by validateBalance()

	expectedAmount, _ := ParseAmount(balance.Amount)
	currency := balance.Amount.Currency
	accountName := string(balance.Account)
	account := v.accounts[accountName]

	actualAmount := account.Inventory.Get(currency)

	delta := &BalanceDelta{
		AccountName:        accountName,
		Currency:           currency,
		ExpectedAmount:     expectedAmount,
		ActualAmount:       actualAmount,
		PaddingAdjustments: make(map[string]decimal.Decimal),
	}

	// Calculate what the amount will be after padding
	actualAmountAfterPadding := actualAmount

	// Calculate padding if pad directive exists
	if padEntry != nil {
		// BEANCOUNT COMPLIANCE: Pad must come chronologically BEFORE balance
		if !padEntry.Date.Time.Before(balance.Date.Time) { //nolint:staticcheck
			return nil, fmt.Errorf("pad directive dated %s must come before balance assertion dated %s",
				padEntry.Date.Format("2006-01-02"), balance.Date.Format("2006-01-02"))
		}

		difference := expectedAmount.Sub(actualAmount)
		tolerance := v.toleranceConfig.GetDefaultTolerance(currency)

		if difference.Abs().GreaterThan(tolerance) {
			delta.PaddingAdjustments[currency] = difference
			delta.PadAccountName = string(padEntry.AccountPad)

			// Generate synthetic padding transaction
			// Determine decimal places from balance amount
			decimalPlaces := int32(2) // default
			if dotIndex := strings.Index(balance.Amount.Value, "."); dotIndex >= 0 {
				decimalPlaces = int32(len(balance.Amount.Value) - dotIndex - 1)
			}

			delta.SyntheticTransaction = createPaddingTransaction(
				padEntry.Date,                         // Use pad date, not balance date
				balance.Account,                       // Account being padded
				padEntry.AccountPad,                   // Source of padding
				difference,                            // Amount to pad
				difference.StringFixed(decimalPlaces), // Format with same precision as balance
				currency,                              // Currency
				expectedAmount,                        // For narration
				balance.Amount.Value,                  // Original string for expected amount
			)

			// Calculate what actual will be after padding
			actualAmountAfterPadding = actualAmount.Add(difference)
		}

		// Mark pad as used (but don't remove it yet - may be needed for other currencies)
		// Removal happens at end of processing
		delta.ShouldRemovePad = false
	}

	// Check if amounts match within tolerance (after padding)
	tolerance := v.toleranceConfig.GetDefaultTolerance(currency)
	if !AmountEqual(delta.ExpectedAmount, actualAmountAfterPadding, tolerance) {
		// Return error separately, not in delta
		return nil, NewBalanceMismatchError(
			balance,
			delta.ExpectedAmount.String(),
			actualAmountAfterPadding.String(),
			currency,
		)
	}

	return delta, nil
}

// validateInventoryOperations validates that inventory operations (lot reductions) are possible.
//
// It validates that:
//   - For lot reductions (negative amounts with cost specs), sufficient inventory exists
//   - Booking method constraints are satisfied
//   - Both explicit and inferred amounts are checked
//
// Returns validation errors for any failed lot reduction validation.
// Must be called AFTER amount inference to check inferred amounts too.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs := v.validateInventoryOperations(ctx, txn, delta)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateInventoryOperations(ctx context.Context,
	txn *ast.Transaction,
	delta *TransactionDelta) []error {

	var errs []error

	for _, posting := range txn.Postings {
		// Get amount (explicit or inferred)
		var amountToUse *ast.Amount
		if posting.Amount != nil {
			amountToUse = posting.Amount
		} else if inferredAmount, ok := delta.InferredAmounts[posting]; ok {
			amountToUse = inferredAmount
		} else {
			continue
		}

		amount, _ := ParseAmount(amountToUse)
		currency := amountToUse.Currency

		// Check if this is a lot reduction
		if posting.Cost != nil && amount.IsNegative() {
			accountName := string(posting.Account)
			account := v.accounts[accountName]

			lotSpec, err := ParseLotSpec(posting.Cost)
			if err != nil {
				// Should already be validated by validateCosts
				continue
			}

			bookingMethod := account.BookingMethod
			if bookingMethod == "" {
				bookingMethod = "FIFO"
			}

			// Check if reduction is possible (read-only)
			if err := account.Inventory.CanReduceLot(currency, amount, lotSpec, bookingMethod); err != nil {
				errs = append(errs, NewInsufficientInventoryError(txn, posting.Account, err))
			}
		}
	}

	return errs
}

// validateConstraintCurrencies validates that postings only use currencies allowed by account constraints.
//
// It validates that:
//   - Postings use only currencies in the account's constraint list
//   - Both explicit and inferred amounts are checked
//
// Must be called AFTER amount inference to check inferred amounts too.
//
// Returns validation errors for any postings using disallowed currencies.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs := v.validateConstraintCurrencies(ctx, txn, delta)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateConstraintCurrencies(ctx context.Context,
	txn *ast.Transaction,
	delta *TransactionDelta) []error {

	var errs []error

	for _, posting := range txn.Postings {
		accountName := string(posting.Account)
		account, ok := v.accounts[accountName]
		if !ok {
			continue // Will be caught by validateAccountsOpen
		}

		// Only check if account has constraint currencies
		if len(account.ConstraintCurrencies) == 0 {
			continue
		}

		// Get currency (explicit or inferred)
		var currency string
		if posting.Amount != nil {
			currency = posting.Amount.Currency
		} else if inferredAmount, ok := delta.InferredAmounts[posting]; ok {
			currency = inferredAmount.Currency
		} else {
			continue
		}

		// Check if currency is allowed
		allowed := false
		for _, c := range account.ConstraintCurrencies {
			if c == currency {
				allowed = true
				break
			}
		}
		if !allowed {
			errs = append(errs, NewCurrencyConstraintError(
				txn, posting.Account, currency, account.ConstraintCurrencies))
		}
	}

	return errs
}
