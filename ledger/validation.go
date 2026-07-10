package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Validation computes directive-specific deltas without mutating ledger state.
// Handlers apply those deltas only when validation succeeds.

// validator provides transaction validation with read-only access to ledger state.
// This is a separate type from Ledger to ensure validation cannot mutate state.
type validator struct {
	accounts map[string]*Account
	config   *Config
}

// newValidator creates a validator with a read-only view of the current ledger state
func newValidator(accounts map[string]*Account, config *Config) *validator {
	return &validator{
		accounts: accounts,
		config:   config,
	}
}

// validateDateRange checks if a date is within the valid Beancount range (1-9999).
// Follows official Beancount behavior which rejects year 0 and year >= 10000.
func validateDateRange(date *ast.Date) error {
	if date == nil {
		return nil
	}

	year := date.Year()
	if year < 1 || year > 9999 {
		return fmt.Errorf("ValueError: year %d is out of range", year)
	}

	return nil
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
//	errs := v.validateAccountsOpen(txn)
//	if len(errs) > 0 {
//	    // txn references closed or non-existent accounts
//	    for _, err := range errs {
//	        fmt.Printf("Account error: %v\n", err)
//	    }
//	}
func (v *validator) validateAccountsOpen(txn *ast.Transaction) []error {
	var errs []error
	for _, posting := range txn.Postings {
		accountName := string(posting.Account)
		acc, exists := v.accounts[accountName]
		if !exists {
			errs = append(errs, NewAccountNotOpenError(txn, posting.Account))
			continue
		}
		if !acc.IsOpen(txn.Date()) {
			errs = append(errs, NewAccountNotOpenError(txn, posting.Account))
		}
	}
	return errs
}

// validateAmounts checks all amounts can be parsed
func (v *validator) validateAmounts(txn *ast.Transaction) []error {
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
//	errs := v.validateCosts(txn)
//	if len(errs) > 0 {
//	    // Found invalid cost specifications
//	    for _, err := range errs {
//	        fmt.Printf("Cost error: %v\n", err)
//	        // Example: "2024-01-15: Invalid cost specification (Posting #1: Assets:Stock): {abc USD}: invalid decimal"
//	    }
//	}
func (v *validator) validateCosts(txn *ast.Transaction) []error {
	var errs []error
	for i, posting := range txn.Postings {
		if posting.Cost == nil {
			continue // No cost specification
		}

		// Empty cost {} is valid
		if posting.Cost.IsEmpty() {
			continue
		}

		// Validate total cost {{}} requirements
		if posting.Cost.IsTotal {
			if posting.Amount == nil {
				errs = append(errs, &TotalCostError{
					directiveError: newDirectiveError(txn),
					Posting:        posting,
					Message:        "total cost requires a quantity",
				})
				continue
			}

			if posting.Cost.Amount == nil {
				errs = append(errs, &TotalCostError{
					directiveError: newDirectiveError(txn),
					Posting:        posting,
					Message:        "total cost requires an amount",
				})
				continue
			}

			quantity, err := decimal.NewFromString(posting.Amount.Value)
			if err != nil {
				errs = append(errs, &TotalCostError{
					directiveError: newDirectiveError(txn),
					Posting:        posting,
					Message:        fmt.Sprintf("invalid quantity %q: %v", posting.Amount.Value, err),
				})
				continue
			}

			_, err = decimal.NewFromString(posting.Cost.Amount.Value)
			if err != nil {
				errs = append(errs, &TotalCostError{
					directiveError: newDirectiveError(txn),
					Posting:        posting,
					Message:        fmt.Sprintf("invalid total cost %q: %v", posting.Cost.Amount.Value, err),
				})
				continue
			}

			if quantity.IsZero() {
				errs = append(errs, &TotalCostError{
					directiveError: newDirectiveError(txn),
					Posting:        posting,
					Message:        "cannot use total cost with zero quantity",
				})
				continue
			}
		}

		// Validate cost amount if present
		if posting.Cost.Amount != nil {
			if _, err := ParseAmount(posting.Cost.Amount); err != nil {
				costSpec := fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
			}
		}

		// Validate ParseLotSpec can parse the cost
		if _, err := ParseLotSpec(posting.Cost); err != nil {
			costSpec := "{...}"
			if posting.Cost.Amount != nil {
				costSpec = fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
			}
			errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
		}

		// Validate cost date if present
		if posting.Cost.Date != nil {
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
			if strings.TrimSpace(posting.Cost.Label) == "" {
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
//	errs := v.validatePrices(txn)
//	if len(errs) > 0 {
//	    // Found invalid price specifications
//	    for _, err := range errs {
//	        fmt.Printf("Price error: %v\n", err)
//	        // Example: "2024-01-15: Invalid price specification (Posting #2: Expenses:Foreign): @ abc USD: invalid decimal"
//	    }
//	}
func (v *validator) validatePrices(txn *ast.Transaction) []error {
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
//	errs := v.validateMetadata(txn)
//	if len(errs) > 0 {
//	    // Found invalid or duplicate metadata
//	    for _, err := range errs {
//	        fmt.Printf("Metadata error: %v\n", err)
//	        // Example: "2024-01-15: Invalid metadata: key="invoice", value="": empty value"
//	        // Example: "2024-01-15: Invalid metadata (account Assets:Checking): key="note", value="xyz": duplicate key"
//	    }
//	}
func (v *validator) validateMetadata(txn *ast.Transaction) []error {
	errs := validateMetadataEntries(txn, txn.Metadata, "")
	for _, posting := range txn.Postings {
		errs = append(errs, validateMetadataEntries(txn, posting.Metadata, posting.Account)...)
	}
	return errs
}

func validateMetadataEntries(
	txn *ast.Transaction,
	metadata []*ast.Metadata,
	account ast.Account,
) []error {
	var errs []error
	seen := make(map[string]bool, len(metadata))
	for _, meta := range metadata {
		if seen[meta.Key] {
			errs = append(errs, NewInvalidMetadataError(
				txn, account, meta.Key, meta.Value, "duplicate key",
			))
			continue
		}
		seen[meta.Key] = true

		if meta.Value != nil && meta.Value.StringValue != nil && meta.Value.StringValue.IsEmpty() {
			errs = append(errs, NewInvalidMetadataError(
				txn, account, meta.Key, meta.Value, "empty value",
			))
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

			// Cost specs without an amount (empty {} or date/label-only)
			// need their cost resolved from booked lots or inferred.
			if posting.Cost != nil && !posting.Cost.IsMergeCost() {
				if posting.Cost.Amount == nil {
					pc.withEmptyCosts = append(pc.withEmptyCosts, posting)
				} else {
					pc.withExplicitCost = append(pc.withExplicitCost, posting)
				}
			}
		}
	}
	return pc
}

// calculateBalance computes weights, infers amounts/costs, and checks if transaction balances.
// Returns delta (mutations), validation (balance state), and errors.
// This is the core transaction validation logic.
func (v *validator) calculateBalance(txn *ast.Transaction) (*TransactionDelta, *balanceValidation, []error) {
	var errs []error
	pc := classifyPostings(txn.Postings)

	// Calculate weights for postings with amounts
	var allWeights []weightSet
	// Negative empty-cost postings whose lot cost could not be resolved via
	// booking (e.g. NONE booking); their cost must be inferred from the residual.
	unresolvedEmptyCosts := make(map[*ast.Posting]bool)
	for _, posting := range pc.withAmounts {
		weights, err := calculateWeights(posting)
		if err != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
			continue
		}

		// Check if this is a cost spec without an amount (returns empty weights)
		if len(weights) == 0 && posting.Cost != nil && posting.Cost.Amount == nil && !posting.Cost.IsMergeCost() {
			// Reductions resolve their weight from the booked lots' cost basis,
			// matching beancount, which books lots before interpolation. The
			// spec's date/label (if any) narrows which lots are booked.
			// Augmentations are handled in cost inference below.
			amount, aerr := ParseAmount(posting.Amount)
			if aerr == nil && amount.IsNegative() {
				if booked, ok := v.bookedReductionWeights(posting.Account, posting.Cost, posting.Amount.Currency, amount); ok {
					allWeights = append(allWeights, booked)
				} else {
					unresolvedEmptyCosts[posting] = true
				}
			}
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

	// An unresolved cost and a missing amount are two unknowns; beancount
	// reports "too many missing numbers" and cannot interpolate.
	if len(unresolvedEmptyCosts) > 0 && len(pc.withoutAmounts) > 0 {
		return delta, unbalancedValidation(balance), nil
	}

	// Infer missing amounts if possible
	// Beancount allows at most 1 posting without amount per transaction
	// It's automatically balanced to make the transaction sum to zero
	if len(pc.withoutAmounts) == 1 {
		posting := pc.withoutAmounts[0]

		if len(balance) == 1 {
			// Exactly 1 currency - can infer the amount uniquely
			for currency, residual := range balance {
				needed := residual.Neg()
				delta.InferredAmounts[posting] = &ast.Amount{
					Value:    needed.String(),
					Currency: currency,
				}
				// Update balance to reflect the inferred amount
				balance[currency] = balance[currency].Add(needed)
			}
		} else if len(balance) > 1 {
			// Multiple currencies - infer posting must balance all of them
			// Create multi-currency amount (not supported, so fail)
			// This matches official beancount: "cannot infer multi-currency amount"
			return delta, unbalancedValidation(balance), nil
		}
		// else: len(balance) == 0 means already balanced, no inference needed
	} else if len(pc.withoutAmounts) > 1 {
		// Multiple postings without amounts - ambiguous, can't infer
		return delta, unbalancedValidation(balance), nil
	}

	// Infer costs for empty cost specs {}
	if len(pc.withEmptyCosts) > 0 {
		// Count empty costs that need inference from the residual: augmentations
		// plus reductions whose lot cost could not be resolved via booking.
		inferableEmptyCosts := 0
		for _, posting := range pc.withEmptyCosts {
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue
			}
			if !amount.IsNegative() || unresolvedEmptyCosts[posting] {
				inferableEmptyCosts++
			}
		}

		// Beancount compliance: Cannot infer costs when multiple postings have empty cost specs
		// This is ambiguous - which posting gets which portion of the residual?
		if inferableEmptyCosts > 1 {
			return delta, unbalancedValidation(balance), nil
		}

		for _, posting := range pc.withEmptyCosts {
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue
			}

			// Infer cost for augmentations, and for reductions whose cost was
			// not resolved from booked lots (e.g. NONE booking)
			if amount.IsZero() || (amount.IsNegative() && !unresolvedEmptyCosts[posting]) {
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
				return delta, unbalancedValidation(balance), nil
			}
		}
	}

	// Check if balanced (within tolerance) after inference
	amountsByCurrency := make(map[string][]decimal.Decimal)

	// Collect all amounts (explicit and inferred) for tolerance calculation
	for _, posting := range txn.Postings {
		if amountValue := delta.amountFor(posting); amountValue != nil {
			amount, err := ParseAmount(amountValue)
			if err != nil {
				continue
			}
			currency := amountValue.Currency
			amountsByCurrency[currency] = append(amountsByCurrency[currency], amount)
		}
	}

	// Check each currency balance with inferred tolerance
	residuals := make(map[string]decimal.Decimal)
	for currency, residual := range balance {
		amounts := amountsByCurrency[currency]
		tolerance := InferTolerance(amounts, currency, v.config.Tolerance)

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

func unbalancedValidation(balance map[string]decimal.Decimal) *balanceValidation {
	residuals := make(map[string]decimal.Decimal, len(balance))
	for currency, residual := range balance {
		residuals[currency] = residual
	}
	return &balanceValidation{
		isBalanced: false,
		residuals:  residuals,
	}
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
//	// Validation passed - inferred amounts/costs are stored on postings directly
//	// Check posting.Inferred and posting.Cost.Inferred for inferred values
func (v *validator) validateTransaction(ctx context.Context, txn *ast.Transaction) ([]error, *TransactionDelta) {
	var allErrors []error

	// 0. Validate transaction date is in valid range
	if err := validateDateRange(txn.Date()); err != nil {
		allErrors = append(allErrors, err)
		return allErrors, nil
	}

	// 1. Validate accounts are open
	if errs := v.validateAccountsOpen(txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 2. Validate amounts are parseable
	if errs := v.validateAmounts(txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 3. Validate cost specifications
	if errs := v.validateCosts(txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 4. Validate price specifications
	if errs := v.validatePrices(txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 5. Validate metadata
	if errs := v.validateMetadata(txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// If basic validation failed, don't proceed to balance calculation
	if len(allErrors) > 0 {
		return allErrors, nil
	}

	// 6. Calculate balance and infer amounts
	delta, validation, errs := v.calculateBalance(txn)
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
	if errs := v.validateConstraintCurrencies(txn, delta); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// 9. Validate inventory operations
	if errs := v.validateInventoryOperations(txn, delta); len(errs) > 0 {
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
//	errs := v.validateBalance(balance)
//	if len(errs) > 0 {
//	    // Account doesn't exist or amount is invalid
//	    for _, err := range errs {
//	        fmt.Printf("Balance validation error: %v\n", err)
//	    }
//	}
func (v *validator) validateBalance(balance *ast.Balance) []error {
	var errs []error

	// 0. Validate balance date is in valid range
	if err := validateDateRange(balance.Date()); err != nil {
		errs = append(errs, err)
		return errs
	}

	// 1. Validate account is open
	accountName := string(balance.Account)
	acc, exists := v.accounts[accountName]
	if !exists {
		errs = append(errs, NewAccountNotOpenError(balance, balance.Account))
		return errs
	}

	if !acc.IsOpen(balance.Date()) {
		errs = append(errs, NewAccountNotOpenError(balance, balance.Account))
		return errs
	}

	// 2. Validate amount is parseable
	if _, err := ParseAmount(balance.Amount); err != nil {
		errs = append(errs, NewInvalidAmountError(balance, balance.Account, balance.Amount.Value, err))
		return errs
	}
	if balance.Tolerance != nil {
		if _, err := ParseAmount(balance.Tolerance); err != nil {
			errs = append(errs, NewInvalidAmountError(balance, balance.Account, balance.Tolerance.Value, err))
			return errs
		}
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
//	errs := v.validatePad(pad)
//	if len(errs) > 0 {
//	    // One or both accounts don't exist or are closed
//	    for _, err := range errs {
//	        fmt.Printf("Pad validation error: %v\n", err)
//	    }
//	}
func (v *validator) validatePad(pad *ast.Pad) []error {
	var errs []error

	// 0. Validate pad date is in valid range
	if err := validateDateRange(pad.Date()); err != nil {
		errs = append(errs, err)
		return errs
	}

	// 1. Validate main account is open
	if !v.isAccountOpen(pad.Account, pad.Date()) {
		errs = append(errs, NewAccountNotOpenError(pad, pad.Account))
	}

	// 2. Validate pad account is open
	if !v.isAccountOpen(pad.AccountPad, pad.Date()) {
		errs = append(errs, NewAccountNotOpenError(pad, pad.AccountPad))
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
//	errs := v.validateNote(note)
//	if len(errs) > 0 {
//	    // Account doesn't exist or is closed
//	    for _, err := range errs {
//	        fmt.Printf("Note validation error: %v\n", err)
//	    }
//	}
func (v *validator) validateNote(note *ast.Note) []error {
	var errs []error

	// 0. Validate note date is in valid range
	if err := validateDateRange(note.Date()); err != nil {
		errs = append(errs, err)
		return errs
	}

	// 1. Validate account is open
	if !v.isAccountOpen(note.Account, note.Date()) {
		errs = append(errs, NewAccountNotOpenError(note, note.Account))
	}

	// 2. Validate description is non-empty
	if note.Description.IsEmpty() {
		// This is already enforced by the parser, but check anyway
		errs = append(errs, fmt.Errorf("note description cannot be empty"))
	}

	return errs
}

// validateDocument checks if a document directive is valid.
//
// Validates that the account exists and is open at the document date.
// Document directives link external files to accounts for audit trails.
func (v *validator) validateDocument(doc *ast.Document) []error {
	var errs []error

	// 0. Validate document date is in valid range
	if err := validateDateRange(doc.Date()); err != nil {
		errs = append(errs, err)
		return errs
	}

	// 1. Validate account is open
	if !v.isAccountOpen(doc.Account, doc.Date()) {
		errs = append(errs, NewAccountNotOpenError(doc, doc.Account))
	}

	// 2. Validate path is non-empty
	if doc.PathToDocument.IsEmpty() {
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

	// 0. Validate open date is in valid range
	if err := validateDateRange(open.Date()); err != nil {
		errs = append(errs, err)
		return errs, nil
	}

	// 1. Validate account root name is configured
	if !v.config.IsValidAccountName(open.Account) {
		errs = append(errs, NewInvalidAccountNameError(open, v.config))
		return errs, nil
	}

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

	bookingMethod := BookingMethod(open.BookingMethod)
	if bookingMethod == "" {
		bookingMethod = BookingMethod(v.config.BookingMethod)
	}

	// Build delta with account properties (avoid allocating Inventory during validation)
	delta := &OpenDelta{
		Account:              open.Account,
		OpenDate:             open.Date(),
		ConstraintCurrencies: constraintCurrenciesCopy,
		BookingMethod:        bookingMethod,
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

	// 0. Validate close date is in valid range
	if err := validateDateRange(close.Date()); err != nil {
		errs = append(errs, err)
		return errs, nil
	}

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
		CloseDate:   close.Date(),
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
	if strings.HasPrefix(differenceStr, "-") {
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
//	delta, err := v.calculateBalanceDelta(balance, padEntry)
//	if err != nil {
//	    // Validation failed
//	}
func (v *validator) calculateBalanceDelta(balance *ast.Balance, padEntry *ast.Pad) (*BalanceDelta, error) {
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
		if !padEntry.Date().Time.Before(balance.Date().Time) { //nolint:staticcheck
			return nil, fmt.Errorf("pad directive dated %s must come before balance assertion dated %s",
				padEntry.Date().String(), balance.Date().String())
		}

		difference := expectedAmount.Sub(actualAmount)
		tolerance, err := v.balanceTolerance(balance)
		if err != nil {
			return nil, err
		}

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
				padEntry.Date(),                       // Use pad date, not balance date
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
	tolerance, err := v.balanceTolerance(balance)
	if err != nil {
		return nil, err
	}
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

func (v *validator) balanceTolerance(balance *ast.Balance) (decimal.Decimal, error) {
	if balance.Tolerance == nil {
		amount, err := ParseAmount(balance.Amount)
		if err != nil {
			return decimal.Zero, err
		}
		exp := amount.Exponent()
		if exp >= 0 {
			return decimal.Zero, nil
		}
		// Beancount allows twice the multiplier on balance and pad assertions,
		// as user-provided balances may be rounded further off than the amounts
		// within a single transaction (see beancount ops/balance.py).
		return decimal.New(1, exp).Mul(v.config.Tolerance.Multiplier).Mul(decimal.NewFromInt(2)), nil
	}
	return ParseAmount(balance.Tolerance)
}

// bookedReductionWeights resolves the balancing weights of an amount-less
// cost spec reduction (empty {} or date/label-only) from the lots selected by
// the account's booking method, matching beancount, which books lots before
// interpolation.
//
// The second return value reports whether the posting's cost is considered
// resolved. It is false only when the cost must instead be inferred from the
// transaction residual (NONE booking, or booked lots without a cost basis).
// Booking failures (ambiguous matches, insufficient lots) return true with no
// weights: they are reported separately by validateInventoryOperations, and
// the posting must not additionally participate in cost inference.
func (v *validator) bookedReductionWeights(accountName ast.Account, cost *ast.Cost, commodity string, amount decimal.Decimal) (weightSet, bool) {
	account, ok := v.accounts[string(accountName)]
	if !ok {
		return nil, true // Unopened account; reported by validateAccountsOpen
	}

	bookingMethod := defaultBookingMethod(account.BookingMethod)
	if bookingMethod == BookingNONE {
		return nil, false
	}

	spec, err := ParseLotSpec(cost)
	if err != nil {
		return nil, true // Invalid cost spec; reported by validateCosts
	}

	plan, err := account.Inventory.planReduction(commodity, amount, spec, bookingMethod)
	if err != nil {
		return nil, true // Booking error; reported by validateInventoryOperations
	}
	if plan == nil || len(plan.reductions) == 0 {
		return nil, false
	}

	var weights weightSet
	for _, reduction := range plan.reductions {
		spec := reduction.lot.Spec
		if spec == nil || spec.Cost == nil {
			return nil, false // Lot held without cost basis; infer from residual
		}
		weights = append(weights, weight{
			Amount:   reduction.amount.Mul(*spec.Cost).Neg(),
			Currency: spec.CostCurrency,
		})
	}

	return weights, true
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
//	errs := v.validateInventoryOperations(txn, delta)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateInventoryOperations(txn *ast.Transaction, delta *TransactionDelta) []error {

	var errs []error

	for _, posting := range txn.Postings {
		// Skip postings without amounts (should not happen after inference)
		amountValue := delta.amountFor(posting)
		if amountValue == nil {
			continue
		}

		amount, _ := ParseAmount(amountValue)
		currency := amountValue.Currency

		// Check if this is a lot reduction
		costValue := delta.costFor(posting)
		if costValue != nil && amount.IsNegative() {
			accountName := string(posting.Account)
			account := v.accounts[accountName]

			lotSpec, err := ParseLotSpec(costValue)
			if err != nil {
				// Should already be validated by validateCosts
				continue
			}

			bookingMethod := defaultBookingMethod(account.BookingMethod)

			// Check if reduction is possible (read-only)
			if err := account.Inventory.CanReduceLot(currency, amount, lotSpec, bookingMethod); err != nil {
				var ambiguousErr *ambiguousBookingMatchError
				if errors.As(err, &ambiguousErr) {
					errs = append(errs, NewAmbiguousBookingError(txn, posting.Account, ambiguousErr))
					continue
				}
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
//	errs := v.validateConstraintCurrencies(txn, delta)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateConstraintCurrencies(txn *ast.Transaction, delta *TransactionDelta) []error {

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

		// Get currency (amount is always set after inference)
		amount := delta.amountFor(posting)
		if amount == nil {
			continue
		}
		currency := amount.Currency

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

// validatePrice validates a Price directive for semantic correctness
func validatePrice(price *ast.Price) []error {
	var errs []error

	// Validate commodity is non-empty
	if price.Commodity == "" {
		errs = append(errs, NewInvalidDirectivePriceError("price commodity cannot be empty", price))
	}

	// Validate amount is present
	if price.Amount == nil {
		errs = append(errs, NewInvalidDirectivePriceError("price amount is required", price))
		return errs
	}

	// Validate currency is non-empty
	if price.Amount.Currency == "" {
		errs = append(errs, NewInvalidDirectivePriceError("price currency cannot be empty", price))
	}

	// Validate amount value is non-empty and parseable
	if price.Amount.Value == "" {
		errs = append(errs, NewInvalidDirectivePriceError("price amount value cannot be empty", price))
		return errs
	}

	// Parse and validate amount is non-zero
	amount, err := ParseAmount(price.Amount)
	if err != nil {
		errs = append(errs, NewInvalidDirectivePriceError(fmt.Sprintf("invalid price amount: %v", err), price))
		return errs
	}

	if amount.IsZero() {
		errs = append(errs, NewInvalidDirectivePriceError("price amount cannot be zero", price))
	}

	return errs
}

// validateCommodity validates a commodity directive.
// Per Beancount spec and Parser → Validate separation:
//   - Parser ensures: non-empty currency code (via parseIdent requirement)
//   - Parser ensures: valid IDENT format (via lexer tokenization)
//   - Validator ensures: semantic constraints (future: duplicate detection)
//
// Currently, the parser already enforces all syntactic requirements for
// commodity directives, so validateCommodity is a pass-through.
//
// Reference: https://beancount.github.io/docs/beancount_language_syntax.html#commodities-currencies
func (v *validator) validateCommodity(commodity *ast.Commodity) []error {
	// Parser enforces:
	// - Currency code is non-empty (parseIdent fails otherwise)
	// - Currency code is valid IDENT (lexer validates format)
	//
	// No additional validation needed at this stage.
	// Future: Duplicate detection would go here.
	return nil
}
