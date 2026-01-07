package ledger

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// directiveError embeds common fields from directives, providing a consistent
// base for all ledger validation errors. It extracts position and date from the
// directive interface, eliminating redundant field extraction in constructors.
type directiveError struct {
	pos       ast.Position
	directive ast.Directive
}

// newDirectiveError creates a directiveError from any directive type.
// This replaces all type-specific field extraction with a single constructor.
func newDirectiveError(d ast.Directive) directiveError {
	return directiveError{
		pos:       d.Position(),
		directive: d,
	}
}

func (e *directiveError) GetPosition() ast.Position   { return e.pos }
func (e *directiveError) GetDirective() ast.Directive { return e.directive }
func (e *directiveError) GetDate() *ast.Date          { return e.directive.GetDate() }

// formatLocation returns a standard location string for error messages.
// Uses filename:line if available, falls back to date string.
func (e *directiveError) formatLocation() string {
	if e.pos.Filename != "" {
		return fmt.Sprintf("%s:%d", e.pos.Filename, e.pos.Line)
	}
	if date := e.directive.GetDate(); date != nil {
		return date.String()
	}
	return "unknown"
}

// Error types for ledger validation errors

// AccountNotOpenError is returned when a directive references an account that hasn't been opened
type AccountNotOpenError struct {
	directiveError
	Account ast.Account
}

func (e *AccountNotOpenError) Error() string {
	return fmt.Sprintf("%s: Invalid reference to unknown account '%s'", e.formatLocation(), e.Account)
}

func (e *AccountNotOpenError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountNotOpenError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "AccountNotOpenError",
		"message":  e.Error(),
		"position": e.pos,
		"account":  string(e.Account),
		"date":     e.GetDate().String(),
	})
}

// AccountAlreadyOpenError is returned when trying to open an account that's already open
type AccountAlreadyOpenError struct {
	directiveError
	Account    ast.Account
	OpenedDate *ast.Date
}

func (e *AccountAlreadyOpenError) Error() string {
	return fmt.Sprintf("%s: Account %s is already open (opened on %s)",
		e.formatLocation(), e.Account, e.OpenedDate.String())
}

func (e *AccountAlreadyOpenError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountAlreadyOpenError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":        "AccountAlreadyOpenError",
		"message":     e.Error(),
		"position":    e.pos,
		"account":     string(e.Account),
		"date":        e.GetDate().String(),
		"opened_date": e.OpenedDate.String(),
	})
}

// InvalidAccountNameError is returned when an account name uses an invalid account type
type InvalidAccountNameError struct {
	directiveError
	Account           ast.Account
	ValidAccountTypes []string // The configured valid account types
}

func (e *InvalidAccountNameError) Error() string {
	// Extract account type from account name
	idx := strings.IndexByte(string(e.Account), ':')
	accountType := "?"
	if idx != -1 {
		accountType = string(e.Account)[:idx]
	}

	return fmt.Sprintf("%s: Account %q uses invalid type %q, expected one of: %s",
		e.formatLocation(), e.Account, accountType, strings.Join(e.ValidAccountTypes, ", "))
}

func (e *InvalidAccountNameError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidAccountNameError) MarshalJSON() ([]byte, error) {
	// Extract account type from account name
	idx := strings.IndexByte(string(e.Account), ':')
	accountType := "?"
	if idx != -1 {
		accountType = string(e.Account)[:idx]
	}

	return json.Marshal(map[string]any{
		"type":                "InvalidAccountNameError",
		"message":             e.Error(),
		"position":            e.pos,
		"account":             string(e.Account),
		"account_type":        accountType,
		"valid_account_types": e.ValidAccountTypes,
	})
}

// AccountAlreadyClosedError is returned when trying to use or close an account that's already closed
type AccountAlreadyClosedError struct {
	directiveError
	Account    ast.Account
	ClosedDate *ast.Date
}

func (e *AccountAlreadyClosedError) Error() string {
	return fmt.Sprintf("%s: Account %s is already closed (closed on %s)",
		e.formatLocation(), e.Account, e.ClosedDate.String())
}

func (e *AccountAlreadyClosedError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountAlreadyClosedError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":        "AccountAlreadyClosedError",
		"message":     e.Error(),
		"position":    e.pos,
		"account":     string(e.Account),
		"date":        e.GetDate().String(),
		"closed_date": e.ClosedDate.String(),
	})
}

// AccountNotClosedError is returned when trying to close an account that was never opened
type AccountNotClosedError struct {
	directiveError
	Account ast.Account
}

func (e *AccountNotClosedError) Error() string {
	return fmt.Sprintf("%s: Cannot close account %s that was never opened",
		e.formatLocation(), e.Account)
}

func (e *AccountNotClosedError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountNotClosedError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "AccountNotClosedError",
		"message":  e.Error(),
		"position": e.pos,
		"account":  string(e.Account),
		"date":     e.GetDate().String(),
	})
}

// TransactionNotBalancedError is returned when a transaction doesn't balance
type TransactionNotBalancedError struct {
	directiveError
	Narration string            // Transaction narration
	Residuals map[string]string // currency -> amount string (unbalanced amounts)
}

// Error returns a bean-check style error message with filename:line prefix.
func (e *TransactionNotBalancedError) Error() string {
	return fmt.Sprintf("%s: Transaction does not balance: %s", e.formatLocation(), e.formatResiduals())
}

// formatResiduals formats the residual amounts in a consistent order.
func (e *TransactionNotBalancedError) formatResiduals() string {
	if len(e.Residuals) == 0 {
		return ""
	}

	// Sort currencies for consistent output
	currencies := make([]string, 0, len(e.Residuals))
	for currency := range e.Residuals {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)

	// Format as "(amount1 CUR1, amount2 CUR2, ...)"
	var buf strings.Builder
	buf.WriteByte('(')
	for i, currency := range currencies {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(e.Residuals[currency])
		buf.WriteByte(' ')
		buf.WriteString(currency)
	}
	buf.WriteByte(')')

	return buf.String()
}

func (e *TransactionNotBalancedError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":      "TransactionNotBalancedError",
		"message":   e.Error(),
		"position":  e.pos,
		"date":      e.GetDate().String(),
		"narration": e.Narration,
		"residuals": e.Residuals,
	})
}

// InvalidAmountError is returned when an amount cannot be parsed
type InvalidAmountError struct {
	directiveError
	Account    ast.Account
	Value      string
	Underlying error
}

func (e *InvalidAmountError) Error() string {
	return fmt.Sprintf("%s: Invalid amount %q for account %s: %v",
		e.formatLocation(), e.Value, e.Account, e.Underlying)
}

func (e *InvalidAmountError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidAmountError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "InvalidAmountError",
		"message":  e.Error(),
		"position": e.pos,
		"account":  string(e.Account),
		"date":     e.GetDate().String(),
		"value":    e.Value,
	})
}

// BalanceMismatchError is returned when a balance assertion fails
type BalanceMismatchError struct {
	directiveError
	Account  ast.Account
	Expected string // Expected amount
	Actual   string // Actual amount in inventory
	Currency string
}

func (e *BalanceMismatchError) Error() string {
	return fmt.Sprintf("%s: Balance mismatch for %s:\n  Expected: %s %s\n  Actual:   %s %s",
		e.formatLocation(), e.Account,
		e.Expected, e.Currency,
		e.Actual, e.Currency)
}

func (e *BalanceMismatchError) GetAccount() ast.Account {
	return e.Account
}

func (e *BalanceMismatchError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "BalanceMismatchError",
		"message":  e.Error(),
		"position": e.pos,
		"account":  string(e.Account),
		"date":     e.GetDate().String(),
		"expected": e.Expected,
		"actual":   e.Actual,
	})
}

// Constructor functions for ledger errors.
// These provide a cleaner API and ensure consistent field initialization.

// NewAccountNotOpenError creates an error for when a directive references an unopened account.
// Works with any directive type (Transaction, Balance, Pad, Note, Document, etc.).
func NewAccountNotOpenError(d ast.Directive, account ast.Account) *AccountNotOpenError {
	return &AccountNotOpenError{
		directiveError: newDirectiveError(d),
		Account:        account,
	}
}

// NewAccountAlreadyOpenError creates an error for when trying to open an already-open account.
func NewAccountAlreadyOpenError(open *ast.Open, openedDate *ast.Date) *AccountAlreadyOpenError {
	return &AccountAlreadyOpenError{
		directiveError: newDirectiveError(open),
		Account:        open.Account,
		OpenedDate:     openedDate,
	}
}

// NewAccountAlreadyClosedError creates an error for when trying to use or close an already-closed account.
func NewAccountAlreadyClosedError(close *ast.Close, closedDate *ast.Date) *AccountAlreadyClosedError {
	return &AccountAlreadyClosedError{
		directiveError: newDirectiveError(close),
		Account:        close.Account,
		ClosedDate:     closedDate,
	}
}

// NewAccountNotClosedError creates an error for when trying to close an account that was never opened.
func NewAccountNotClosedError(close *ast.Close) *AccountNotClosedError {
	return &AccountNotClosedError{
		directiveError: newDirectiveError(close),
		Account:        close.Account,
	}
}

// NewTransactionNotBalancedError creates an error for when a transaction doesn't balance.
func NewTransactionNotBalancedError(txn *ast.Transaction, residuals map[string]string) *TransactionNotBalancedError {
	return &TransactionNotBalancedError{
		directiveError: newDirectiveError(txn),
		Narration:      txn.Narration.Value,
		Residuals:      residuals,
	}
}

// NewInvalidAmountError creates an error for when an amount cannot be parsed or is invalid.
// Works with any directive type (Transaction, Balance, etc.).
func NewInvalidAmountError(d ast.Directive, account ast.Account, value string, err error) *InvalidAmountError {
	return &InvalidAmountError{
		directiveError: newDirectiveError(d),
		Account:        account,
		Value:          value,
		Underlying:     err,
	}
}

// NewBalanceMismatchError creates an error for when a balance assertion fails.
func NewBalanceMismatchError(balance *ast.Balance, expected, actual, currency string) *BalanceMismatchError {
	return &BalanceMismatchError{
		directiveError: newDirectiveError(balance),
		Account:        balance.Account,
		Expected:       expected,
		Actual:         actual,
		Currency:       currency,
	}
}

// InvalidCostError is returned when a cost specification is invalid.
//
// Cost specifications define the acquisition cost of commodities, used for
// lot-based inventory tracking and capital gains calculations.
//
// Common causes:
//   - Invalid decimal in cost amount (e.g., {abc USD})
//   - Zero or invalid cost date
//   - Merge costs {*} not yet implemented
//
// Example error message:
//
//	"file.bean:15: Invalid cost specification (Posting #1: Assets:Stock): {500.x USD}: invalid decimal"
type InvalidCostError struct {
	directiveError
	Account      ast.Account
	PostingIndex int    // Index of posting in transaction (0-based)
	CostSpec     string // String representation of the cost spec
	Underlying   error
}

func (e *InvalidCostError) Error() string {
	postingInfo := ""
	if e.PostingIndex >= 0 {
		postingInfo = fmt.Sprintf(" (Posting #%d: %s)", e.PostingIndex+1, e.Account)
	}

	return fmt.Sprintf("%s: Invalid cost specification%s: %s: %v",
		e.formatLocation(), postingInfo, e.CostSpec, e.Underlying)
}

func (e *InvalidCostError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidCostError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":          "InvalidCostError",
		"message":       e.Error(),
		"position":      e.pos,
		"account":       string(e.Account),
		"date":          e.GetDate().String(),
		"cost_spec":     e.CostSpec,
		"posting_index": e.PostingIndex,
	})
}

// NewInvalidCostError creates an error for when a cost specification is invalid
func NewInvalidCostError(txn *ast.Transaction, account ast.Account, postingIndex int, costSpec string, err error) *InvalidCostError {
	return &InvalidCostError{
		directiveError: newDirectiveError(txn),
		Account:        account,
		PostingIndex:   postingIndex,
		CostSpec:       costSpec,
		Underlying:     err,
	}
}

// TotalCostError is returned when a total cost specification {{}} is invalid.
//
// Total cost syntax allows specifying the total cost for a lot instead of per-unit cost.
// The per-unit cost is calculated by dividing the total by the quantity.
//
// Common causes:
//   - Total cost with zero quantity
//   - Total cost without amount
//   - Total cost without quantity
//   - Invalid decimal in total cost amount
//
// Example error message:
//
//	"file.bean:15: Invalid total cost specification: cannot use total cost with zero quantity"
type TotalCostError struct {
	directiveError
	Posting *ast.Posting
	Message string
}

func (e *TotalCostError) Error() string {
	return fmt.Sprintf("%s: Invalid total cost specification: %s", e.formatLocation(), e.Message)
}

func (e *TotalCostError) GetAccount() ast.Account {
	if e.Posting != nil {
		return e.Posting.Account
	}
	return ""
}

func (e *TotalCostError) MarshalJSON() ([]byte, error) {
	data := map[string]any{
		"type":     "TotalCostError",
		"message":  e.Error(),
		"position": e.pos,
	}
	if date := e.GetDate(); date != nil {
		data["date"] = date.String()
	}
	if e.Posting != nil {
		data["account"] = string(e.Posting.Account)
	}
	return json.Marshal(data)
}

// InvalidPriceError is returned when a price specification is invalid.
//
// Price specifications define the market value of commodities at transaction time,
// used for conversion rates and reporting.
//
// Common causes:
//   - Invalid decimal in price amount (e.g., @ abc USD)
//   - Invalid total price specification (@@)
//
// Example error message:
//
//	"file.bean:20: Invalid price specification (Posting #2: Expenses:Foreign): @ 1.x USD: invalid decimal"
type InvalidPriceError struct {
	directiveError
	Account      ast.Account
	PostingIndex int    // Index of posting in transaction (0-based)
	PriceSpec    string // String representation of the price spec
	Underlying   error
}

func (e *InvalidPriceError) Error() string {
	postingInfo := ""
	if e.PostingIndex >= 0 {
		postingInfo = fmt.Sprintf(" (Posting #%d: %s)", e.PostingIndex+1, e.Account)
	}

	return fmt.Sprintf("%s: Invalid price specification%s: %s: %v",
		e.formatLocation(), postingInfo, e.PriceSpec, e.Underlying)
}

func (e *InvalidPriceError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidPriceError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":          "InvalidPriceError",
		"message":       e.Error(),
		"position":      e.pos,
		"account":       string(e.Account),
		"date":          e.GetDate().String(),
		"price_spec":    e.PriceSpec,
		"posting_index": e.PostingIndex,
	})
}

// NewInvalidPriceError creates an error for when a price specification is invalid
func NewInvalidPriceError(txn *ast.Transaction, account ast.Account, postingIndex int, priceSpec string, err error) *InvalidPriceError {
	return &InvalidPriceError{
		directiveError: newDirectiveError(txn),
		Account:        account,
		PostingIndex:   postingIndex,
		PriceSpec:      priceSpec,
		Underlying:     err,
	}
}

// InvalidMetadataError is returned when metadata is invalid.
//
// Metadata provides key-value annotations on directives and postings for
// additional context like invoice numbers, confirmation codes, etc.
//
// Common causes:
//   - Duplicate metadata keys within same directive/posting
//   - Empty metadata values
//
// Example error messages:
//
//	"file.bean:10: Invalid metadata: key="invoice", value="": empty value"
//	"file.bean:12: Invalid metadata (account Assets:Checking): key="note", value="xyz": duplicate key"
type InvalidMetadataError struct {
	directiveError
	Account ast.Account // Empty if directive-level metadata
	Key     string
	Value   *ast.MetadataValue
	Reason  string // Why it's invalid (e.g., "duplicate key", "empty value")
}

func (e *InvalidMetadataError) Error() string {
	accountInfo := ""
	if e.Account != "" {
		accountInfo = fmt.Sprintf(" (account %s)", e.Account)
	}

	valueStr := ""
	if e.Value != nil {
		valueStr = e.Value.String()
	}

	return fmt.Sprintf("%s: Invalid metadata%s: key=%q, value=%q: %s",
		e.formatLocation(), accountInfo, e.Key, valueStr, e.Reason)
}

func (e *InvalidMetadataError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidMetadataError) MarshalJSON() ([]byte, error) {
	data := map[string]any{
		"type":     "InvalidMetadataError",
		"message":  e.Error(),
		"position": e.pos,
		"account":  string(e.Account),
		"key":      e.Key,
		"reason":   e.Reason,
	}
	if date := e.GetDate(); date != nil {
		data["date"] = date.String()
	}
	return json.Marshal(data)
}

// NewInvalidMetadataError creates an error for when metadata is invalid.
// Works with any directive type - no type switch needed.
func NewInvalidMetadataError(directive ast.Directive, account ast.Account, key string, value *ast.MetadataValue, reason string) *InvalidMetadataError {
	return &InvalidMetadataError{
		directiveError: newDirectiveError(directive),
		Account:        account,
		Key:            key,
		Value:          value,
		Reason:         reason,
	}
}

// InsufficientInventoryError is returned when a transaction tries to reduce inventory but lacks enough lots
type InsufficientInventoryError struct {
	directiveError
	Payee   string
	Account ast.Account
	Details error
}

func (e *InsufficientInventoryError) Error() string {
	return fmt.Sprintf("%s: Insufficient inventory (account %s): %v",
		e.formatLocation(), e.Account, e.Details)
}

func (e *InsufficientInventoryError) GetAccount() ast.Account {
	return e.Account
}

func (e *InsufficientInventoryError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "InsufficientInventoryError",
		"message":  e.Error(),
		"position": e.pos,
		"account":  string(e.Account),
		"date":     e.GetDate().String(),
		"payee":    e.Payee,
	})
}

// NewInsufficientInventoryError creates an error for when inventory operations cannot be performed
func NewInsufficientInventoryError(txn *ast.Transaction, account ast.Account, details error) *InsufficientInventoryError {
	return &InsufficientInventoryError{
		directiveError: newDirectiveError(txn),
		Payee:          txn.Payee.Value,
		Account:        account,
		Details:        details,
	}
}

// CurrencyConstraintError is returned when a posting uses a currency not allowed by the account
type CurrencyConstraintError struct {
	directiveError
	Payee             string
	Account           ast.Account
	Currency          string
	AllowedCurrencies []string
}

func (e *CurrencyConstraintError) Error() string {
	return fmt.Sprintf("%s: Currency %s not allowed for account %s (allowed: %v)",
		e.formatLocation(), e.Currency, e.Account, e.AllowedCurrencies)
}

func (e *CurrencyConstraintError) GetAccount() ast.Account {
	return e.Account
}

func (e *CurrencyConstraintError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":               "CurrencyConstraintError",
		"message":            e.Error(),
		"position":           e.pos,
		"account":            string(e.Account),
		"date":               e.GetDate().String(),
		"currency":           e.Currency,
		"allowed_currencies": e.AllowedCurrencies,
	})
}

// NewCurrencyConstraintError creates an error for when a posting violates currency constraints
func NewCurrencyConstraintError(txn *ast.Transaction, account ast.Account,
	currency string, allowedCurrencies []string) *CurrencyConstraintError {
	return &CurrencyConstraintError{
		directiveError:    newDirectiveError(txn),
		Payee:             txn.Payee.Value,
		Account:           account,
		Currency:          currency,
		AllowedCurrencies: allowedCurrencies,
	}
}

// UnusedPadWarning is returned when a pad directive is never consumed by a balance assertion
type UnusedPadWarning struct {
	Pad     *ast.Pad
	Account string
}

func (e *UnusedPadWarning) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pad.Pos.Filename, e.Pad.Pos.Line)
	if e.Pad.Pos.Filename == "" {
		location = e.Pad.Date.String()
	}

	return fmt.Sprintf("%s: Unused Pad entry\n\n   %s pad %s %s",
		location,
		e.Pad.Date.String(),
		e.Pad.Account,
		e.Pad.AccountPad,
	)
}

func (e *UnusedPadWarning) GetPosition() ast.Position {
	return e.Pad.Pos
}

func (e *UnusedPadWarning) GetDirective() ast.Directive {
	return e.Pad
}

func (e *UnusedPadWarning) GetAccount() ast.Account {
	return e.Pad.Account
}

func (e *UnusedPadWarning) GetDate() *ast.Date {
	return e.Pad.Date
}

func (e *UnusedPadWarning) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     "UnusedPadWarning",
		"message":  e.Error(),
		"position": e.Pad.Pos,
		"account":  e.Account,
		"date":     e.Pad.Date.String(),
	})
}

// NewInvalidAccountNameError creates an error for an account with an invalid account type
func NewInvalidAccountNameError(open *ast.Open, cfg *Config) *InvalidAccountNameError {
	validAccountTypes := []string{
		cfg.AccountNames.Assets,
		cfg.AccountNames.Liabilities,
		cfg.AccountNames.Equity,
		cfg.AccountNames.Income,
		cfg.AccountNames.Expenses,
	}
	return &InvalidAccountNameError{
		directiveError:    newDirectiveError(open),
		Account:           open.Account,
		ValidAccountTypes: validAccountTypes,
	}
}

// NewUnusedPadWarning creates a warning for an unused pad directive
func NewUnusedPadWarning(pad *ast.Pad) *UnusedPadWarning {
	return &UnusedPadWarning{
		Pad:     pad,
		Account: string(pad.Account),
	}
}

// InvalidDirectivePriceError indicates a price directive has invalid data
type InvalidDirectivePriceError struct {
	Message   string
	Pos       ast.Position
	Directive *ast.Price
}

func (e *InvalidDirectivePriceError) Error() string {
	return fmt.Sprintf("%s at %s", e.Message, e.Pos)
}

func (e *InvalidDirectivePriceError) GetPosition() ast.Position {
	return e.Pos
}

// NewInvalidDirectivePriceError creates an error for an invalid price directive
func NewInvalidDirectivePriceError(message string, price *ast.Price) *InvalidDirectivePriceError {
	return &InvalidDirectivePriceError{
		Message:   message,
		Pos:       price.Pos,
		Directive: price,
	}
}
