package ledger

import (
	"context"
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
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
//   - Runs with telemetry instrumentation for performance monitoring
//
// 2. Mutation Phase (State Changes)
//   - Only executes if validation passes
//   - Updates account inventories, balances, and other state
//   - Can assume all inputs are valid
//
// Validation Flow for Transactions:
//
//   Ledger.processTransaction(txn)
//     ↓
//   validator.validateTransaction(ctx, txn)
//     ├─ validateAccountsOpen()    // Check accounts exist and are open
//     ├─ validateAmounts()          // Check amounts are parseable
//     ├─ validateCosts()            // Check cost specifications
//     ├─ validatePrices()           // Check price specifications
//     ├─ validateMetadata()         // Check metadata entries
//     └─ calculateBalance()         // Calculate weights and check balance
//     ↓
//   Ledger.applyTransaction(txn, balanceResult)
//     └─ Update account inventories
//
// This separation provides several benefits:
//   - Individual validation rules can be tested in isolation
//   - Validation can be performed without mutating state
//   - Clear separation of concerns (validation vs mutation)
//   - Better error reporting (all errors found, not just first)
//   - Easier to understand and maintain
//
// Example usage:
//
//   v := newValidator(ledger.accounts)
//   errs, result := v.validateTransaction(ctx, txn)
//   if len(errs) > 0 {
//       // Handle validation errors
//       for _, err := range errs {
//           fmt.Println(err)
//       }
//       return
//   }
//   // Validation passed - safe to mutate state
//   ledger.applyTransaction(txn, result)

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

// balanceResult contains the result of transaction balancing
type balanceResult struct {
	isBalanced      bool
	residuals       map[string]string // currency -> residual amount
	inferredAmounts map[*ast.Posting]*ast.Amount
	inferredCosts   map[*ast.Posting]*ast.Amount
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

		// Merge cost {*} is not yet implemented
		if posting.Cost.IsMergeCost() {
			errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{*}",
				fmt.Errorf("merge cost {*} not yet implemented")))
			continue
		}

		// Validate cost amount if present
		if posting.Cost.Amount != nil {
			if _, err := ParseAmount(posting.Cost.Amount); err != nil {
				costSpec := fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
			}
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

			// Check for empty values (though parser might prevent this)
			if meta.Value == "" {
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

				// Check for empty values
				if meta.Value == "" {
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

// calculateBalance computes weights and determines if transaction balances
// This is a pure function - no side effects
func (v *validator) calculateBalance(ctx context.Context, txn *ast.Transaction) (*balanceResult, []error) {
	timer := telemetry.StartTimer(ctx, "validator.calculate_balance")
	defer timer.End()
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
		return nil, errs
	}

	// Balance the weights
	balance := balanceWeights(allWeights)
	defer putBalanceMap(balance)

	result := &balanceResult{
		inferredAmounts: make(map[*ast.Posting]*ast.Amount),
		inferredCosts:   make(map[*ast.Posting]*ast.Amount),
		residuals:       make(map[string]string),
	}

	// Infer missing amounts if possible
	if len(pc.withoutAmounts) > 0 {
		// Only infer if there's exactly ONE missing posting AND exactly ONE unbalanced currency
		// Otherwise it's ambiguous
		if len(pc.withoutAmounts) == 1 && len(balance) == 1 {
			// Single missing posting and single unbalanced currency - we can infer
			for currency, residual := range balance {
				// Need to negate the residual to balance
				needed := residual.Neg()

				// Create the inferred amount
				result.inferredAmounts[pc.withoutAmounts[0]] = &ast.Amount{
					Value:    needed.String(),
					Currency: currency,
				}
			}
		} else if len(pc.withoutAmounts) > 1 || len(balance) > 1 {
			// Multiple missing postings OR multiple unbalanced currencies - ambiguous
			result.residuals = make(map[string]string)
			for currency, residual := range balance {
				result.residuals[currency] = residual.String()
			}
			result.isBalanced = false
			return result, nil
		}
	}

	// Infer costs for empty cost specs {}
	// Note: Only infer costs for AUGMENTATIONS (positive amounts)
	// For REDUCTIONS (negative amounts), empty cost spec means "use booking method"
	if len(pc.withEmptyCosts) > 0 {
		// For each posting with empty cost spec, infer the cost from the residual
		for _, posting := range pc.withEmptyCosts {
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
					result.inferredCosts[posting] = &ast.Amount{
						Value:    costPerUnit.String(),
						Currency: currency,
					}

					// Add this weight to the balance
					totalCost := amount.Mul(costPerUnit)
					balance[currency] = balance[currency].Add(totalCost)
				}
			} else if len(balance) > 1 {
				// Multiple currencies - ambiguous
				result.residuals = make(map[string]string)
				for currency, residual := range balance {
					result.residuals[currency] = residual.String()
				}
				result.isBalanced = false
				return result, nil
			}
		}
	}

	// Check if balanced (within tolerance) after inference
	// Collect amounts per currency for tolerance inference
	amountsByCurrency := make(map[string][]decimal.Decimal)
	for _, posting := range txn.Postings {
		if posting.Amount == nil {
			continue
		}
		amount, err := ParseAmount(posting.Amount)
		if err != nil {
			continue // Already validated earlier
		}
		currency := posting.Amount.Currency
		amountsByCurrency[currency] = append(amountsByCurrency[currency], amount)
	}

	// Add inferred amounts to balance for verification
	for _, inferredAmount := range result.inferredAmounts {
		amount, err := ParseAmount(inferredAmount)
		if err != nil {
			continue // Shouldn't happen, but be safe
		}
		currency := inferredAmount.Currency

		// Add inferred amount to the balance
		current := balance[currency]
		balance[currency] = current.Add(amount)

		// Also add to amounts for tolerance calculation
		amountsByCurrency[currency] = append(amountsByCurrency[currency], amount)
	}

	// Check each currency balance with inferred tolerance
	for currency, residual := range balance {
		// Infer tolerance from the amounts in this transaction
		amounts := amountsByCurrency[currency]
		tolerance := InferTolerance(amounts, currency, v.toleranceConfig)

		// Check if residual is within tolerance (applies to all currencies)
		if residual.Abs().GreaterThan(tolerance) {
			result.residuals[currency] = residual.String()
		}
	}

	result.isBalanced = len(result.residuals) == 0
	return result, nil
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
//   - *balanceResult: Balance calculation result (nil if validation failed)
//
// Performance: ~955ns/op with telemetry instrumentation enabled.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs, result := v.validateTransaction(ctx, txn)
//	if len(errs) > 0 {
//	    // Validation failed
//	    fmt.Printf("Found %d validation errors:\n", len(errs))
//	    for _, err := range errs {
//	        fmt.Printf("  - %v\n", err)
//	    }
//	    return
//	}
//	// Validation passed - result contains balance info and inferred amounts
//	fmt.Printf("Transaction balanced: %v\n", result.isBalanced)
func (v *validator) validateTransaction(ctx context.Context, txn *ast.Transaction) ([]error, *balanceResult) {
	timer := telemetry.StartTimer(ctx, "validator.transaction")
	defer timer.End()

	var allErrors []error

	// 1. Validate accounts are open
	accountsTimer := timer.Child("validator.accounts")
	if errs := v.validateAccountsOpen(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	accountsTimer.End()

	// 2. Validate amounts are parseable
	amountsTimer := timer.Child("validator.amounts")
	if errs := v.validateAmounts(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	amountsTimer.End()

	// 3. Validate cost specifications
	costsTimer := timer.Child("validator.costs")
	if errs := v.validateCosts(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	costsTimer.End()

	// 4. Validate price specifications
	pricesTimer := timer.Child("validator.prices")
	if errs := v.validatePrices(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	pricesTimer.End()

	// 5. Validate metadata
	metadataTimer := timer.Child("validator.metadata")
	if errs := v.validateMetadata(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	metadataTimer.End()

	// If basic validation failed, don't proceed to balance calculation
	if len(allErrors) > 0 {
		return allErrors, nil
	}

	// 6. Calculate balance and infer amounts
	balanceResult, errs := v.calculateBalance(ctx, txn)
	if len(errs) > 0 {
		allErrors = append(allErrors, errs...)
		return allErrors, nil
	}

	// 7. Check if balanced
	if !balanceResult.isBalanced {
		allErrors = append(allErrors, NewTransactionNotBalancedError(txn, balanceResult.residuals))
	}

	return allErrors, balanceResult
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
