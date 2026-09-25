package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
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
	withAmounts       []*ast.Posting
	withoutAmounts    []*ast.Posting
	incompleteAmounts []*ast.Posting // amount present but missing number or currency
	incompletePrices  []*ast.Posting // complete amount, price annotation missing number or currency
	withEmptyCosts    []*ast.Posting
	withExplicitCost  []*ast.Posting
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
		if posting.Amount == nil || isIncompleteAmount(posting.Amount) {
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
		if posting.Cost.Total != nil {
			if posting.Cost.IsTotal {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{{... # ...}}", fmt.Errorf("compound cost cannot use total cost syntax")))
			} else if posting.Cost.Amount == nil {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{# ...}", fmt.Errorf("compound cost requires a per-unit amount")))
			} else if posting.Cost.Total.Currency != posting.Cost.Amount.Currency {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{... # ...}", fmt.Errorf("compound cost currencies must match")))
			} else if _, err := ParseAmount(posting.Cost.Total); err != nil {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{... # ...}", err))
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
		if posting.Price == nil || isIncompleteAmount(posting.Price) {
			continue // Absent or interpolated price specification
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

// isIncompleteAmount reports whether an amount omits its number or currency
// (official grammar: incomplete_amount); interpolation completes it.
func isIncompleteAmount(a *ast.Amount) bool {
	return a != nil && (a.Value == "" || a.Currency == "")
}

// classifyPostings categorizes postings for different processing paths
func classifyPostings(postings []*ast.Posting) postingClassification {
	var pc postingClassification
	for _, posting := range postings {
		switch {
		case posting.Amount == nil:
			pc.withoutAmounts = append(pc.withoutAmounts, posting)
		case isIncompleteAmount(posting.Amount):
			pc.incompleteAmounts = append(pc.incompleteAmounts, posting)
		default:
			pc.withAmounts = append(pc.withAmounts, posting)

			if posting.Price != nil && isIncompleteAmount(posting.Price) {
				pc.incompletePrices = append(pc.incompletePrices, posting)
			}

			// Cost specs without an amount (empty {} or date/label-only)
			// need their cost resolved from booked lots or inferred.
			if posting.Cost != nil {
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

// validateTransaction checks a transaction that Booking kept, with only the
// Currency groups it booked. Booking has already reported the transactions
// and groups it dropped (a date out of range, a malformed number, cost or
// price, postings it cannot sort into groups, missing numbers that cannot be
// interpolated, a reduction that matches no lot or several).
//
// Like beancount, which books every transaction before it checks it, the
// booked transaction is returned for Apply even when it is reported for
// posting to an unopened or inactive account, invalid metadata, not
// balancing, a negative cost, or a currency its account does not allow;
// later directives then see its effects instead of reporting follow-on
// errors.
func (v *validator) validateTransaction(ctx context.Context, txn *ast.Transaction, booked *bookedTransaction) ([]error, *bookedTransaction) {
	if booked == nil {
		return nil, nil
	}

	var errs []error
	errs = append(errs, v.validateAccountsOpen(txn)...)
	errs = append(errs, v.validateMetadata(txn)...)
	if len(booked.residuals) > 0 {
		errs = append(errs, newNotBalancedError(txn, booked.residuals))
	}
	errs = append(errs, v.validateNegativeCosts(txn)...)
	errs = append(errs, v.validateConstraintCurrencies(txn)...)
	return errs, booked
}

// newNotBalancedError reports the residuals of a transaction that does not
// balance.
func newNotBalancedError(txn *ast.Transaction, residuals map[string]decimal.Decimal) error {
	residualStrings := make(map[string]string, len(residuals))
	for currency, amount := range residuals {
		residualStrings[currency] = amount.String()
	}
	return NewTransactionNotBalancedError(txn, residualStrings)
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

	// 1. Validate account is active (assertions are allowed after close)
	if !v.isAccountActiveAllowingClose(balance.Account, balance.Date()) {
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
	if !v.isAccountActiveAllowingClose(note.Account, note.Date()) {
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
	if !v.isAccountActiveAllowingClose(doc.Account, doc.Date()) {
		errs = append(errs, NewAccountNotOpenError(doc, doc.Account))
	}

	// 2. Validate path is non-empty
	if doc.PathToDocument.IsEmpty() {
		// This is already enforced by the parser, but check anyway
		errs = append(errs, fmt.Errorf("document path cannot be empty"))
		return errs
	}

	// 3. Validate the referenced file exists, matching beancount's
	// verify_document_files_exist plugin. Relative paths resolve against
	// the directory of the file declaring the directive.
	docPath := doc.PathToDocument.Value
	if !filepath.IsAbs(docPath) {
		docPath = filepath.Join(filepath.Dir(doc.Position().Filename), docPath)
	}
	if _, err := os.Stat(docPath); err != nil {
		errs = append(errs, NewDocumentFileError(doc, docPath))
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

// isAccountActiveAllowingClose checks that an account exists and was opened
// on or before the date, ignoring its close date. Beancount allows Balance,
// Document, and Note directives after an account closes (ALLOW_AFTER_CLOSE
// in ops/validation.py) — statements and assertions may arrive well after
// closure — but never before the account opens.
func (v *validator) isAccountActiveAllowingClose(account ast.Account, date *ast.Date) bool {
	acc, ok := v.accounts[string(account)]
	if !ok || acc.OpenDate == nil {
		return false
	}
	return !acc.OpenDate.After(date.Time)
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

	// A per-account booking method must name one of beancount's methods,
	// matched case-sensitively. Like beancount, an invalid one is reported
	// and the account still opens, with the default method.
	bookingMethod := BookingMethod(open.BookingMethod)
	if open.BookingMethod != "" && !sharedconfig.IsBookingMethod(open.BookingMethod) {
		errs = append(errs, NewInvalidBookingMethodError(open))
		bookingMethod = ""
	}

	// Copy metadata and constraint currencies to avoid shared references with AST
	metadataCopy := make([]*ast.Metadata, len(open.Metadata))
	copy(metadataCopy, open.Metadata)

	constraintCurrenciesCopy := make([]string, len(open.ConstraintCurrencies))
	copy(constraintCurrenciesCopy, open.ConstraintCurrencies)

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

			// Calculate what actual will be after padding. Padding an
			// account from itself posts both legs to it, so nothing changes
			// and the assertion fails, as in beancount.
			if padEntry.AccountPad != balance.Account {
				actualAmountAfterPadding = actualAmount.Add(difference)
			}
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

// statedUnits collects the units numbers written in full (number and
// currency) per currency, which beancount infers a transaction's tolerances
// from before interpolating.
func statedUnits(postings []*ast.Posting) map[string][]decimal.Decimal {
	stated := make(map[string][]decimal.Decimal)
	for _, posting := range postings {
		if posting.Amount == nil || posting.Amount.Value == "" || posting.Amount.Currency == "" {
			continue
		}
		if number, err := ParseAmount(posting.Amount); err == nil {
			stated[posting.Amount.Currency] = append(stated[posting.Amount.Currency], number)
		}
	}
	return stated
}

// maxQuantumDigits mirrors beancount's MAX_TOLERANCE_DIGITS: a quantum with
// this many significant digits is not a neat, user-like step to round to.
const maxQuantumDigits = 5

// maxCostTolerance mirrors beancount's MAXIMUM_TOLERANCE, the cap on what one
// posting held at cost or price adds to a tolerance.
var maxCostTolerance = decimal.RequireFromString("0.5")

// transactionTolerance returns a transaction's tolerance for currency: the
// one inferred from its units amounts, widened by what postings at cost or
// price add under infer_tolerance_from_cost.
func (v *validator) transactionTolerance(currency string, amounts []decimal.Decimal, costTolerances map[string]decimal.Decimal) decimal.Decimal {
	tolerance := InferTolerance(amounts, currency, v.config.Tolerance)
	if fromCost, ok := costTolerances[currency]; ok && fromCost.GreaterThan(tolerance) {
		return fromCost
	}
	return tolerance
}

// toleranceShare is what one posting, as beancount sees it at some stage,
// can add to a transaction's tolerances under infer_tolerance_from_cost.
type toleranceShare struct {
	units        decimal.Decimal
	hasCost      bool
	costNumbers  []decimal.Decimal // numbers the cost states; none widens by the cap
	costCurrency string
	price        *priceAmount
}

// costTolerances returns what postings held at cost or price add to a
// transaction's tolerances when infer_tolerance_from_cost is set, like
// beancount's infer_tolerances(use_cost=True). A share whose units have
// fractional digits, with units tolerance t, adds min(t × n, 0.5) to its
// cost currency (n the smallest cost number, or just 0.5 without one) and
// to its price currency (n the per-unit price). Contributions sum per
// currency.
func (v *validator) costTolerances(shares []toleranceShare) map[string]decimal.Decimal {
	if !v.config.Tolerance.InferFromCost {
		return nil
	}
	tolerances := make(map[string]decimal.Decimal)
	for _, share := range shares {
		if share.units.Exponent() >= 0 {
			continue
		}
		unitsTolerance := decimal.New(1, share.units.Exponent()).Mul(v.config.Tolerance.Multiplier)
		if share.hasCost && share.costCurrency != "" {
			contribution := maxCostTolerance
			for _, n := range share.costNumbers {
				contribution = decimal.Min(contribution, unitsTolerance.Mul(n))
			}
			tolerances[share.costCurrency] = tolerances[share.costCurrency].Add(contribution)
		}
		if share.price != nil {
			contribution := decimal.Min(maxCostTolerance, unitsTolerance.Mul(share.price.number))
			tolerances[share.price.currency] = tolerances[share.price.currency].Add(contribution)
		}
	}
	return tolerances
}

// statedUnitsNumber returns a posting's units number when the source states
// it with its currency; interpolated amounts never contribute tolerance.
func statedUnitsNumber(posting *ast.Posting) (decimal.Decimal, bool) {
	if posting.Inferred || posting.Amount == nil || posting.Amount.Value == "" || posting.Amount.Currency == "" {
		return decimal.Decimal{}, false
	}
	units, err := ParseAmount(posting.Amount)
	return units, err == nil
}

// specToleranceShares describes postings as beancount sees them before
// booking, when it rounds interpolated amounts: a cost spec contributes the
// numbers it states (per-unit, or total for {{...}}, and a compound total).
func specToleranceShares(postings []*ast.Posting) []toleranceShare {
	var shares []toleranceShare
	for _, posting := range postings {
		units, ok := statedUnitsNumber(posting)
		if !ok {
			continue
		}
		share := toleranceShare{units: units, price: perUnitPrice(posting, units)}
		if posting.Cost != nil {
			share.hasCost = true
			share.costCurrency = costCurrency(posting.Cost)
			for _, amount := range []*ast.Amount{posting.Cost.Amount, posting.Cost.Total} {
				if amount == nil || amount.Value == "" {
					continue
				}
				if n, err := ParseAmount(amount); err == nil {
					share.costNumbers = append(share.costNumbers, n)
				}
			}
		}
		shares = append(shares, share)
	}
	return shares
}

// bookedToleranceShares describes postings as beancount books them, when it
// checks the balance: a cost is its per-unit number, with inferred costs
// resolved, and a reduction against lots becomes one share per lot, like
// the booked postings beancount replaces it with.
func bookedToleranceShares(postings []*ast.Posting, delta *TransactionDelta, bookedLots map[*ast.Posting][]lotReduction) []toleranceShare {
	var shares []toleranceShare
	for _, posting := range postings {
		units, ok := statedUnitsNumber(posting)
		if !ok {
			continue
		}
		price := perUnitPrice(posting, units)

		if lots, ok := bookedLots[posting]; ok {
			for _, lot := range lots {
				shares = append(shares, toleranceShare{
					units:        lot.amount,
					hasCost:      true,
					costNumbers:  []decimal.Decimal{*lot.lot.Spec.Cost},
					costCurrency: lot.lot.Spec.CostCurrency,
					price:        price,
				})
			}
			continue
		}

		share := toleranceShare{units: units, price: price}
		if cost := delta.costFor(posting); cost != nil {
			share.hasCost = true
			if number, currency, ok := PerUnitCost(&ast.Posting{Amount: posting.Amount, Cost: cost}); ok {
				share.costNumbers = []decimal.Decimal{number}
				share.costCurrency = currency
			}
		}
		shares = append(shares, share)
	}
	return shares
}

type priceAmount struct {
	number   decimal.Decimal
	currency string
}

// perUnitPrice returns a posting's stated price per unit, or nil; beancount's
// parser turns a total price (@@) into a per-unit one.
func perUnitPrice(posting *ast.Posting, units decimal.Decimal) *priceAmount {
	if posting.Price == nil || posting.Price.Value == "" || posting.Price.Currency == "" {
		return nil
	}
	number, err := ParseAmount(posting.Price)
	if err != nil {
		return nil
	}
	if posting.PriceTotal {
		if units.IsZero() {
			return nil
		}
		number = pydecimal.Quo(number, units.Abs())
	}
	return &priceAmount{number: number.Abs(), currency: posting.Price.Currency}
}

// roundInterpolated rounds an interpolated units number like beancount's
// quantize_with_tolerance: half-to-even to twice the transaction's tolerance
// for its currency, when that step is a neat number. Without a tolerance
// the number is left as computed, so 10.00 USD - 3.333 USD books -6.67 USD
// while a price conversion keeps all its digits.
func roundInterpolated(number, tolerance decimal.Decimal) decimal.Decimal {
	if !tolerance.IsPositive() {
		return number
	}
	// Normalize the quantum (strip trailing zeros), as beancount does.
	quantum := pydecimal.Normalize(tolerance.Mul(decimal.NewFromInt(2)))
	if len(quantum.Coefficient().String()) >= maxQuantumDigits {
		return number
	}
	return number.RoundBank(-quantum.Exponent())
}

// unitsWeightTerms returns how a posting whose units number is missing
// weighs: in its cost currency at a per-unit cost (plus a compound total),
// in its price currency at a price, or in its own currency. ok is false
// when the units cannot be solved for, as with a total-only or empty cost.
func unitsWeightTerms(posting *ast.Posting) (currency string, perUnit, total decimal.Decimal, ok bool) {
	if cost := posting.Cost; cost != nil {
		if cost.IsTotal || cost.Amount == nil || cost.Amount.Value == "" {
			return "", decimal.Zero, decimal.Zero, false
		}
		perUnit, err := ParseAmount(cost.Amount)
		if err != nil || perUnit.IsZero() {
			return "", decimal.Zero, decimal.Zero, false
		}
		if cost.Total != nil {
			if total, err = ParseAmount(cost.Total); err != nil {
				return "", decimal.Zero, decimal.Zero, false
			}
		}
		return cost.Amount.Currency, perUnit, total, true
	}
	if price := posting.Price; price != nil && price.Value != "" && price.Currency != "" && !posting.PriceTotal {
		perUnit, err := ParseAmount(price)
		if err != nil || perUnit.IsZero() {
			return "", decimal.Zero, decimal.Zero, false
		}
		return price.Currency, perUnit, decimal.Zero, true
	}
	return posting.Amount.Currency, decimal.Zero, decimal.Zero, true
}

// residualCurrencies returns the currencies with a non-zero residual, in the
// order their weights first appear in the transaction (beancount orders its
// currency groups by first posting), then any others sorted.
func residualCurrencies(allWeights []weightSet, balance map[string]decimal.Decimal) []string {
	var currencies []string
	seen := make(map[string]bool, len(balance))
	add := func(currency string) {
		if !seen[currency] && !balance[currency].IsZero() {
			currencies = append(currencies, currency)
		}
		seen[currency] = true
	}
	for _, weights := range allWeights {
		for _, w := range weights {
			add(w.Currency)
		}
	}
	rest := make([]string, 0, len(balance))
	for currency := range balance {
		if !seen[currency] {
			rest = append(rest, currency)
		}
	}
	slices.Sort(rest)
	for _, currency := range rest {
		add(currency)
	}
	return currencies
}

func costCurrency(cost *ast.Cost) string {
	if cost.Amount != nil && cost.Amount.Currency != "" {
		return cost.Amount.Currency
	}
	if cost.Total != nil {
		return cost.Total.Currency
	}
	return ""
}

// newBookingError classifies a failed booking: an ambiguous match (or a
// reduction under AVERAGE, which beancount counts as one), or inventory that
// cannot cover the reduction.
func newBookingError(txn *ast.Transaction, account ast.Account, err error) error {
	var ambiguousErr *ambiguousBookingMatchError
	if errors.As(err, &ambiguousErr) || errors.Is(err, errAverageUnsupported) {
		return NewAmbiguousBookingError(txn, account, err)
	}
	return NewInsufficientInventoryError(txn, account, err)
}

// validateNegativeCosts reports booked cost postings with a negative cost.
// Like beancount, a booked cost may be zero but never negative; the check
// applies per unit, after a total or compound cost is spread over the units.
// Beancount books such a posting anyway, so this does not stop Apply.
func (v *validator) validateNegativeCosts(txn *ast.Transaction) []error {
	var errs []error
	for _, posting := range txn.Postings {
		if posting.Amount == nil || posting.Cost == nil {
			continue
		}
		if perUnit, costCurrency, ok := PerUnitCost(posting); ok && perUnit.IsNegative() {
			errs = append(errs, NewNegativeCostError(txn, posting, perUnit, costCurrency))
		}
	}
	return errs
}

// validateConstraintCurrencies reports postings in a currency their
// account's constraint list does not allow. It checks the booked postings,
// so inferred amounts are checked too.
func (v *validator) validateConstraintCurrencies(txn *ast.Transaction) []error {

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

		// Booking has written inferred amounts onto the postings.
		amount := posting.Amount
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
//   - The commodity handler rejects a repeated declaration (it needs the graph)
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
	return nil
}
