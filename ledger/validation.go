package ledger

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/shopspring/decimal"
)

// Validation computes directive-specific deltas without mutating ledger state.
// Handlers apply those deltas only when validation succeeds.

// validator provides transaction validation with read-only access to ledger state.
// This is a separate type from Ledger to ensure validation cannot mutate state.
type validator struct {
	accounts map[string]*Account
	// Every account an open directive names anywhere in the ledger, with
	// that open, which tells an inactive account from an unknown one
	opened map[string]*ast.Open
	config *sharedconfig.Config
}

// newValidator creates a validator with a read-only view of the current ledger state
func newValidator(accounts map[string]*Account, opened map[string]*ast.Open, config *sharedconfig.Config) *validator {
	return &validator{
		accounts: accounts,
		opened:   opened,
		config:   config,
	}
}

// accountNotOpenError reports a reference to an account outside its open
// interval. Like v2's validate_active_accounts, the account is inactive when
// the ledger opens it anywhere, before or after the directive, and unknown
// otherwise.
func (v *validator) accountNotOpenError(d ast.Directive, account ast.Account) error {
	if v.opened[string(account)] != nil {
		return newInactiveAccountError(d, account)
	}
	return newAccountNotOpenError(d, account)
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
			errs = append(errs, v.accountNotOpenError(txn, posting.Account))
			continue
		}
		if !acc.isOpen(txn.Date()) {
			errs = append(errs, v.accountNotOpenError(txn, posting.Account))
		}
	}
	return errs
}

// validateTransaction checks a transaction that Booking kept, with only the
// Currency groups it booked. Booking has already reported the transactions
// and groups it dropped (a date out of range, a malformed number, cost or
// price, postings it cannot sort into groups, missing numbers that cannot be
// interpolated, a reduction that matches no lot or several).
//
// A transaction without a booking result, which only a Plugin that adds one
// after Booking can produce, is reported as an UnbookedTransactionError and
// not applied.
//
// Like beancount, which books every transaction before it checks it, the
// booked transaction is returned for Apply even when it is reported for
// posting to an unopened or inactive account, not balancing, zero units or a negative cost at cost, or a currency its account
// does not allow; later directives then see its effects instead of reporting
// follow-on errors.
func (v *validator) validateTransaction(ctx context.Context, txn *ast.Transaction, booked *bookedTransaction) ([]error, *bookedTransaction) {
	if booked == nil {
		return []error{newUnbookedTransactionError(txn)}, nil
	}

	var errs []error
	errs = append(errs, v.validateAccountsOpen(txn)...)
	if len(booked.residuals) > 0 {
		errs = append(errs, newTransactionNotBalancedError(txn, booked.residuals))
	}
	errs = append(errs, v.validateBookedCosts(txn)...)
	errs = append(errs, v.validateConstraintCurrencies(txn)...)
	return errs, booked
}

// validateBalance checks that a balance directive's date is in range and
// its amount and tolerance parse; checkBalance checks the assertion itself.
func (v *validator) validateBalance(balance *ast.Balance) []error {
	if err := validateDateRange(balance.Date()); err != nil {
		return []error{err}
	}
	if _, err := ParseAmount(balance.Amount); err != nil {
		return []error{newInvalidAmountError(balance, balance.Account, balance.Amount.Value, err)}
	}
	if balance.Tolerance != nil {
		if _, err := ParseAmount(balance.Tolerance); err != nil {
			return []error{newInvalidAmountError(balance, balance.Account, balance.Tolerance.Value, err)}
		}
	}
	return nil
}

// checkBalance checks a balance assertion against the amount its account
// holds, padding included. An assertion on an account the ledger never
// opens is reported as an unknown account, once, and not checked; on an
// account outside its open interval it is reported and still checked,
// against the postings made before the open too (assertions are allowed
// after close).
func (v *validator) checkBalance(balance *ast.Balance, held, tolerance decimal.Decimal) []error {
	if v.opened[string(balance.Account)] == nil {
		return []error{newAccountNotOpenError(balance, balance.Account)}
	}

	var errs []error
	if !v.isAccountActiveAllowingClose(balance.Account, balance.Date()) {
		errs = append(errs, newInactiveAccountError(balance, balance.Account))
	}
	if err := v.validateBalanceCurrency(balance); err != nil {
		errs = append(errs, err)
	}
	expected, _ := ParseAmount(balance.Amount)
	if !amountEqual(expected, held, tolerance) {
		errs = append(errs, newBalanceMismatchError(balance, expected, held))
	}
	return errs
}

// validateBalanceCurrency reports a balance assertion in a currency its
// account's open does not allow. Like beancount, the constraints are the
// open's wherever it is dated, and the assertion is still checked.
func (v *validator) validateBalanceCurrency(balance *ast.Balance) error {
	open := v.opened[string(balance.Account)]
	if open == nil || len(open.ConstraintCurrencies) == 0 || slices.Contains(open.ConstraintCurrencies, balance.Amount.Currency) {
		return nil
	}
	return newBalanceCurrencyError(balance)
}

// balanceKey is what makes two balance assertions assert the same thing.
type balanceKey struct {
	account  ast.Account
	currency string
	date     string
}

func balanceKeyOf(balance *ast.Balance) balanceKey {
	return balanceKey{balance.Account, balance.Amount.Currency, balance.Date().String()}
}

// openedAccounts returns every account an open directive names, with its
// open: like beancount's get_account_open_close, the earliest one when an
// account is opened more than once.
func openedAccounts(directives []ast.Directive) map[string]*ast.Open {
	opened := make(map[string]*ast.Open)
	for _, directive := range directives {
		open, ok := directive.(*ast.Open)
		if !ok {
			continue
		}
		if first := opened[string(open.Account)]; first == nil || open.Date().Before(first.Date().Time) {
			opened[string(open.Account)] = open
		}
	}
	return opened
}

// balancesByKey groups the balance assertions by account, currency and
// date, each group in directive order.
func balancesByKey(directives []ast.Directive) map[balanceKey][]*ast.Balance {
	balances := make(map[balanceKey][]*ast.Balance)
	for _, directive := range directives {
		if balance, ok := directive.(*ast.Balance); ok {
			k := balanceKeyOf(balance)
			balances[k] = append(balances[k], balance)
		}
	}
	return balances
}

// duplicateBalances returns the balance assertions whose account, currency
// and date repeat an earlier one's with a different number, like beancount's
// validate_duplicate_balances: each is compared with the first assertion
// for its key, in directive order, whatever else is reported about either.
// The tolerance is not compared.
func duplicateBalances(balances map[balanceKey][]*ast.Balance) map[*ast.Balance]bool {
	duplicates := make(map[*ast.Balance]bool)
	for _, group := range balances {
		for _, balance := range group[1:] {
			if !sameNumber(group[0].Amount, balance.Amount) {
				duplicates[balance] = true
			}
		}
	}
	return duplicates
}

// sameNumber reports whether two amounts have equal numbers, comparing
// their text when either does not parse.
func sameNumber(a, b *ast.Amount) bool {
	x, errA := ParseAmount(a)
	y, errB := ParseAmount(b)
	if errA != nil || errB != nil {
		return a.Value == b.Value
	}
	return x.Equal(y)
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
		errs = append(errs, v.accountNotOpenError(pad, pad.Account))
	}

	// 2. Validate pad account is open
	if !v.isAccountOpen(pad.AccountPad, pad.Date()) {
		errs = append(errs, v.accountNotOpenError(pad, pad.AccountPad))
	}

	return errs
}

// validateNote checks if a note directive is valid.
//
// It validates that:
//   - The account exists and is open at the note date
//
// Like bean-check, any description is accepted, including an empty string.
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
		errs = append(errs, v.accountNotOpenError(note, note.Account))
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
		errs = append(errs, v.accountNotOpenError(doc, doc.Account))
	}

	// 2. Validate the referenced file exists, matching beancount's
	// verify_document_files_exist plugin. Relative paths resolve against
	// the directory of the file declaring the directive, so an empty path
	// names that directory and passes, as in bean-check.
	if _, err := os.Stat(doc.ResolvedPath()); err != nil {
		errs = append(errs, newDocumentFileError(doc))
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
	return acc.isOpen(date)
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
// Returns validation errors and openDelta for the mutations to apply.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs, delta := v.validateOpen(ctx, openDirective)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateOpen(ctx context.Context, open *ast.Open) ([]error, *openDelta) {
	var errs []error
	accountName := string(open.Account)

	// 0. Validate open date is in valid range
	if err := validateDateRange(open.Date()); err != nil {
		errs = append(errs, err)
		return errs, nil
	}

	// 1. Validate account root name is configured
	if !v.config.IsValidAccountName(open.Account) {
		errs = append(errs, newInvalidAccountNameError(open, v.config))
		return errs, nil
	}

	// Check if account already exists - duplicate open is always an error
	if existing, ok := v.accounts[accountName]; ok {
		errs = append(errs, newAccountAlreadyOpenError(open, existing.OpenDate))
		return errs, nil
	}

	// Copy metadata and constraint currencies to avoid shared references with AST
	metadataCopy := make([]*ast.Metadata, len(open.Metadata))
	copy(metadataCopy, open.Metadata)

	constraintCurrenciesCopy := make([]string, len(open.ConstraintCurrencies))
	copy(constraintCurrenciesCopy, open.ConstraintCurrencies)

	// Build delta with account properties (avoid allocating Inventory during validation)
	delta := &openDelta{
		account:              open.Account,
		openDate:             open.Date(),
		constraintCurrencies: constraintCurrenciesCopy,
		metadata:             metadataCopy,
	}

	return errs, delta
}

// validateClose validates a close directive.
//
// It validates that:
//   - Account exists in the ledger
//   - Account is not already closed
//
// Returns validation errors and closeDelta for the mutations to apply.
//
// Example:
//
//	v := newValidator(ledger.accounts)
//	errs, delta := v.validateClose(ctx, closeDirective)
//	if len(errs) > 0 {
//	    // Validation failed
//	}
func (v *validator) validateClose(ctx context.Context, close *ast.Close) ([]error, *closeDelta) {
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
		errs = append(errs, newAccountNotClosedError(close))
		return errs, nil
	}

	// Check if already closed
	if account.isClosed() {
		errs = append(errs, newAccountAlreadyClosedError(close, account.CloseDate))
		return errs, nil
	}

	delta := &closeDelta{
		accountName: accountName,
		closeDate:   close.Date(),
	}

	return errs, delta
}

// validateBookedCosts reports booked cost postings with zero units or a
// negative cost, like beancount's interpolate_group. A booked cost may be
// zero but never negative; the check applies per unit, after a total or
// compound cost is spread over the units. Beancount books such a posting
// anyway, so this does not stop Apply.
func (v *validator) validateBookedCosts(txn *ast.Transaction) []error {
	var errs []error
	for _, posting := range txn.Postings {
		if posting.Amount == nil || posting.Cost == nil {
			continue
		}
		if units, err := ParseAmount(posting.Amount); err == nil && units.IsZero() {
			errs = append(errs, newZeroAmountError(txn, posting))
		}
		if perUnit, costCurrency, ok := perUnitCost(posting); ok && perUnit.IsNegative() {
			errs = append(errs, newNegativeCostError(txn, posting, perUnit, costCurrency))
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
		if len(account.constraintCurrencies) == 0 {
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
		for _, c := range account.constraintCurrencies {
			if c == currency {
				allowed = true
				break
			}
		}
		if !allowed {
			errs = append(errs, newCurrencyConstraintError(txn, posting.Account, currency))
		}
	}

	return errs
}

// validatePrice validates a Price directive. The parser never produces a
// price without a commodity, an amount, a currency or a number, so ledger
// text cannot reach these checks: they guard directives built in Go, a
// hand-built AST or directives added with ledgerload.Result.With. The
// Importer protocol carries no price directives, so its host covers none of
// them.
func validatePrice(price *ast.Price) []error {
	var errs []error

	// Validate commodity is non-empty
	if price.Commodity == "" {
		errs = append(errs, newInvalidDirectivePriceError("price commodity cannot be empty", price))
	}

	// Validate amount is present
	if price.Amount == nil {
		errs = append(errs, newInvalidDirectivePriceError("price amount is required", price))
		return errs
	}

	// Validate currency is non-empty
	if price.Amount.Currency == "" {
		errs = append(errs, newInvalidDirectivePriceError("price currency cannot be empty", price))
	}

	// Validate amount value is non-empty and parseable
	if price.Amount.Value == "" {
		errs = append(errs, newInvalidDirectivePriceError("price amount value cannot be empty", price))
		return errs
	}

	// Like beancount, any number is a price, zero and negative included.
	if _, err := ParseAmount(price.Amount); err != nil {
		errs = append(errs, newInvalidDirectivePriceError(fmt.Sprintf("invalid price amount: %v", err), price))
	}

	return errs
}

// validateCommodity validates a commodity directive.
// Per Beancount spec and Parser → Validate separation:
//   - Parser ensures: non-empty currency code (via parseIdent requirement)
//   - Parser ensures: valid IDENT format (via lexer tokenization)
//   - The commodity handler rejects a repeated declaration (it needs the declared commodities)
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
