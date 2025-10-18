package ledger

import (
	"context"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// validator provides transaction validation with read-only access to ledger state.
// This is a separate type from Ledger to ensure validation cannot mutate state.
type validator struct {
	accounts map[string]*Account
}

// newValidator creates a validator with a read-only view of the current ledger state
func newValidator(accounts map[string]*Account) *validator {
	return &validator{accounts: accounts}
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

// validateAccountsOpen checks all posting accounts are open at transaction date
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
	collector := telemetry.FromContext(ctx)
	timer := collector.Start("validation.calculate_balance")
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
		// Group missing postings by currency (if they have costs, we can infer the currency)
		// For now, handle the simple case: one missing posting per currency
		for currency, residual := range balance {
			// Need to negate the residual to balance
			needed := residual.Neg()

			// Find if there's exactly one posting without amount that could use this currency
			// For simplicity, if there's ONE missing posting and ONE unbalanced currency, assign it
			if len(pc.withoutAmounts) == 1 {
				// Create the inferred amount
				result.inferredAmounts[pc.withoutAmounts[0]] = &ast.Amount{
					Value:    needed.String(),
					Currency: currency,
				}
			} else if len(pc.withoutAmounts) > 1 {
				// Ambiguous - can't infer
				result.residuals = map[string]string{currency: residual.String()}
				result.isBalanced = false
				return result, nil
			}
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
	tolerance := GetTolerance("")
	for currency, amount := range balance {
		// If we inferred an amount for this currency, it should now be balanced
		if len(result.inferredAmounts) == 0 {
			if amount.Abs().GreaterThan(tolerance) {
				result.residuals[currency] = amount.String()
			}
		}
		// If we did inference, the balance should be zero (we'll verify below)
	}

	result.isBalanced = len(result.residuals) == 0
	return result, nil
}

// validateTransaction runs all validation checks
// Returns all errors found (doesn't short-circuit) and balance result if successful
func (v *validator) validateTransaction(ctx context.Context, txn *ast.Transaction) ([]error, *balanceResult) {
	collector := telemetry.FromContext(ctx)
	timer := collector.Start("validation.transaction")
	defer timer.End()

	var allErrors []error

	// 1. Validate accounts are open
	accountsTimer := timer.Child("validation.accounts")
	if errs := v.validateAccountsOpen(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	accountsTimer.End()

	// 2. Validate amounts are parseable
	amountsTimer := timer.Child("validation.amounts")
	if errs := v.validateAmounts(ctx, txn); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}
	amountsTimer.End()

	// If basic validation failed, don't proceed to balance calculation
	if len(allErrors) > 0 {
		return allErrors, nil
	}

	// 3. Calculate balance and infer amounts
	balanceResult, errs := v.calculateBalance(ctx, txn)
	if len(errs) > 0 {
		allErrors = append(allErrors, errs...)
		return allErrors, nil
	}

	// 4. Check if balanced
	if !balanceResult.isBalanced {
		allErrors = append(allErrors, NewTransactionNotBalancedError(txn, balanceResult.residuals))
	}

	return allErrors, balanceResult
}
