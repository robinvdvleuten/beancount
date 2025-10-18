package ledger

import (
	"fmt"
	"sort"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// Error types for ledger validation errors

// AccountNotOpenError is returned when a directive references an account that hasn't been opened
type AccountNotOpenError struct {
	Account   ast.Account
	Date      *ast.Date
	Pos       ast.Position  // Position in source file (includes filename)
	Directive ast.Directive // The directive that referenced the closed account
}

func (e *AccountNotOpenError) Error() string {
	// Format: filename:line: message
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Invalid reference to unknown account '%s'", location, e.Account)
}

func (e *AccountNotOpenError) GetPosition() ast.Position {
	return e.Pos
}

func (e *AccountNotOpenError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *AccountNotOpenError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountNotOpenError) GetDate() *ast.Date {
	return e.Date
}

// AccountAlreadyOpenError is returned when trying to open an account that's already open
type AccountAlreadyOpenError struct {
	Account    ast.Account
	Date       *ast.Date
	OpenedDate *ast.Date
	Pos        ast.Position
	Directive  ast.Directive
}

func (e *AccountAlreadyOpenError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Account %s is already open (opened on %s)",
		location, e.Account, e.OpenedDate.Format("2006-01-02"))
}

func (e *AccountAlreadyOpenError) GetPosition() ast.Position {
	return e.Pos
}

func (e *AccountAlreadyOpenError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *AccountAlreadyOpenError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountAlreadyOpenError) GetDate() *ast.Date {
	return e.Date
}

// AccountAlreadyClosedError is returned when trying to use or close an account that's already closed
type AccountAlreadyClosedError struct {
	Account    ast.Account
	Date       *ast.Date
	ClosedDate *ast.Date
	Pos        ast.Position
	Directive  ast.Directive
}

func (e *AccountAlreadyClosedError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Account %s is already closed (closed on %s)",
		location, e.Account, e.ClosedDate.Format("2006-01-02"))
}

func (e *AccountAlreadyClosedError) GetPosition() ast.Position {
	return e.Pos
}

func (e *AccountAlreadyClosedError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *AccountAlreadyClosedError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountAlreadyClosedError) GetDate() *ast.Date {
	return e.Date
}

// AccountNotClosedError is returned when trying to close an account that was never opened
type AccountNotClosedError struct {
	Account   ast.Account
	Date      *ast.Date
	Pos       ast.Position
	Directive ast.Directive
}

func (e *AccountNotClosedError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Cannot close account %s that was never opened",
		location, e.Account)
}

func (e *AccountNotClosedError) GetPosition() ast.Position {
	return e.Pos
}

func (e *AccountNotClosedError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *AccountNotClosedError) GetAccount() ast.Account {
	return e.Account
}

func (e *AccountNotClosedError) GetDate() *ast.Date {
	return e.Date
}

// TransactionNotBalancedError is returned when a transaction doesn't balance
type TransactionNotBalancedError struct {
	Pos         ast.Position      // Position in source file (includes filename)
	Date        *ast.Date         // Transaction date
	Narration   string            // Transaction narration
	Residuals   map[string]string // currency -> amount string (unbalanced amounts)
	Transaction *ast.Transaction  // Full transaction for context rendering
}

// Error returns a bean-check style error message with filename:line prefix.
func (e *TransactionNotBalancedError) Error() string {
	// Format the residual amounts
	residualStr := e.formatResiduals()

	// Format: filename:line: message (residual)
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Transaction does not balance: %s", location, residualStr)
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

func (e *TransactionNotBalancedError) GetPosition() ast.Position {
	return e.Pos
}

func (e *TransactionNotBalancedError) GetDirective() ast.Directive {
	return e.Transaction
}

func (e *TransactionNotBalancedError) GetDate() *ast.Date {
	return e.Date
}

// InvalidAmountError is returned when an amount cannot be parsed
type InvalidAmountError struct {
	Date       *ast.Date
	Account    ast.Account
	Value      string
	Underlying error
	Pos        ast.Position
	Directive  ast.Directive
}

func (e *InvalidAmountError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Invalid amount %q for account %s: %v",
		location, e.Value, e.Account, e.Underlying)
}

func (e *InvalidAmountError) GetPosition() ast.Position {
	return e.Pos
}

func (e *InvalidAmountError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *InvalidAmountError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidAmountError) GetDate() *ast.Date {
	return e.Date
}

// BalanceMismatchError is returned when a balance assertion fails
type BalanceMismatchError struct {
	Date      *ast.Date
	Account   ast.Account
	Expected  string // Expected amount
	Actual    string // Actual amount in inventory
	Currency  string
	Pos       ast.Position
	Directive ast.Directive
}

func (e *BalanceMismatchError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Balance mismatch for %s:\n  Expected: %s %s\n  Actual:   %s %s",
		location, e.Account,
		e.Expected, e.Currency,
		e.Actual, e.Currency)
}

func (e *BalanceMismatchError) GetPosition() ast.Position {
	return e.Pos
}

func (e *BalanceMismatchError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *BalanceMismatchError) GetAccount() ast.Account {
	return e.Account
}

func (e *BalanceMismatchError) GetDate() *ast.Date {
	return e.Date
}

// Constructor functions for ledger errors.
// These provide a cleaner API and ensure consistent field initialization.

// NewAccountNotOpenError creates an error for when a directive references an unopened account.
// Use this for transactions where the account comes from a posting.
func NewAccountNotOpenError(txn *ast.Transaction, account ast.Account) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   account,
		Date:      txn.Date,
		Pos:       txn.Pos,
		Directive: txn,
	}
}

// NewAccountNotOpenErrorFromBalance creates an error for a balance directive referencing an unopened account.
func NewAccountNotOpenErrorFromBalance(balance *ast.Balance) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   balance.Account,
		Date:      balance.Date,
		Pos:       balance.Pos,
		Directive: balance,
	}
}

// NewAccountNotOpenErrorFromPad creates an error for a pad directive referencing an unopened account.
func NewAccountNotOpenErrorFromPad(pad *ast.Pad, account ast.Account) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   account,
		Date:      pad.Date,
		Pos:       pad.Pos,
		Directive: pad,
	}
}

// NewAccountNotOpenErrorFromNote creates an error for a note directive referencing an unopened account.
func NewAccountNotOpenErrorFromNote(note *ast.Note) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   note.Account,
		Date:      note.Date,
		Pos:       note.Pos,
		Directive: note,
	}
}

// NewAccountNotOpenErrorFromDocument creates an error for a document directive referencing an unopened account.
func NewAccountNotOpenErrorFromDocument(doc *ast.Document) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   doc.Account,
		Date:      doc.Date,
		Pos:       doc.Pos,
		Directive: doc,
	}
}

// NewAccountAlreadyOpenError creates an error for when trying to open an already-open account.
func NewAccountAlreadyOpenError(open *ast.Open, openedDate *ast.Date) *AccountAlreadyOpenError {
	return &AccountAlreadyOpenError{
		Account:    open.Account,
		Date:       open.Date,
		OpenedDate: openedDate,
		Pos:        open.Pos,
		Directive:  open,
	}
}

// NewAccountAlreadyClosedError creates an error for when trying to use or close an already-closed account.
func NewAccountAlreadyClosedError(close *ast.Close, closedDate *ast.Date) *AccountAlreadyClosedError {
	return &AccountAlreadyClosedError{
		Account:    close.Account,
		Date:       close.Date,
		ClosedDate: closedDate,
		Pos:        close.Pos,
		Directive:  close,
	}
}

// NewAccountNotClosedError creates an error for when trying to close an account that was never opened.
func NewAccountNotClosedError(close *ast.Close) *AccountNotClosedError {
	return &AccountNotClosedError{
		Account:   close.Account,
		Date:      close.Date,
		Pos:       close.Pos,
		Directive: close,
	}
}

// NewTransactionNotBalancedError creates an error for when a transaction doesn't balance.
func NewTransactionNotBalancedError(txn *ast.Transaction, residuals map[string]string) *TransactionNotBalancedError {
	return &TransactionNotBalancedError{
		Pos:         txn.Pos,
		Date:        txn.Date,
		Narration:   txn.Narration,
		Residuals:   residuals,
		Transaction: txn,
	}
}

// NewInvalidAmountError creates an error for when an amount in a transaction cannot be parsed or is invalid.
func NewInvalidAmountError(txn *ast.Transaction, account ast.Account, value string, err error) *InvalidAmountError {
	return &InvalidAmountError{
		Date:       txn.Date,
		Account:    account,
		Value:      value,
		Underlying: err,
		Pos:        txn.Pos,
		Directive:  txn,
	}
}

// NewInvalidAmountErrorFromBalance creates an error for when a balance amount cannot be parsed.
func NewInvalidAmountErrorFromBalance(balance *ast.Balance, err error) *InvalidAmountError {
	return &InvalidAmountError{
		Date:       balance.Date,
		Account:    balance.Account,
		Value:      balance.Amount.Value,
		Underlying: err,
		Pos:        balance.Pos,
		Directive:  balance,
	}
}

// NewBalanceMismatchError creates an error for when a balance assertion fails.
func NewBalanceMismatchError(balance *ast.Balance, expected, actual, currency string) *BalanceMismatchError {
	return &BalanceMismatchError{
		Date:      balance.Date,
		Account:   balance.Account,
		Expected:  expected,
		Actual:    actual,
		Currency:  currency,
		Pos:       balance.Pos,
		Directive: balance,
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
//   "file.bean:15: Invalid cost specification (Posting #1: Assets:Stock): {500.x USD}: invalid decimal"
type InvalidCostError struct {
	Date         *ast.Date
	Account      ast.Account
	PostingIndex int    // Index of posting in transaction (0-based)
	CostSpec     string // String representation of the cost spec
	Underlying   error
	Pos          ast.Position
	Directive    ast.Directive
}

func (e *InvalidCostError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	postingInfo := ""
	if e.PostingIndex >= 0 {
		postingInfo = fmt.Sprintf(" (Posting #%d: %s)", e.PostingIndex+1, e.Account)
	}

	return fmt.Sprintf("%s: Invalid cost specification%s: %s: %v",
		location, postingInfo, e.CostSpec, e.Underlying)
}

func (e *InvalidCostError) GetPosition() ast.Position {
	return e.Pos
}

func (e *InvalidCostError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *InvalidCostError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidCostError) GetDate() *ast.Date {
	return e.Date
}

// NewInvalidCostError creates an error for when a cost specification is invalid
func NewInvalidCostError(txn *ast.Transaction, account ast.Account, postingIndex int, costSpec string, err error) *InvalidCostError {
	return &InvalidCostError{
		Date:         txn.Date,
		Account:      account,
		PostingIndex: postingIndex,
		CostSpec:     costSpec,
		Underlying:   err,
		Pos:          txn.Pos,
		Directive:    txn,
	}
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
//   "file.bean:20: Invalid price specification (Posting #2: Expenses:Foreign): @ 1.x USD: invalid decimal"
type InvalidPriceError struct {
	Date         *ast.Date
	Account      ast.Account
	PostingIndex int    // Index of posting in transaction (0-based)
	PriceSpec    string // String representation of the price spec
	Underlying   error
	Pos          ast.Position
	Directive    ast.Directive
}

func (e *InvalidPriceError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	postingInfo := ""
	if e.PostingIndex >= 0 {
		postingInfo = fmt.Sprintf(" (Posting #%d: %s)", e.PostingIndex+1, e.Account)
	}

	return fmt.Sprintf("%s: Invalid price specification%s: %s: %v",
		location, postingInfo, e.PriceSpec, e.Underlying)
}

func (e *InvalidPriceError) GetPosition() ast.Position {
	return e.Pos
}

func (e *InvalidPriceError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *InvalidPriceError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidPriceError) GetDate() *ast.Date {
	return e.Date
}

// NewInvalidPriceError creates an error for when a price specification is invalid
func NewInvalidPriceError(txn *ast.Transaction, account ast.Account, postingIndex int, priceSpec string, err error) *InvalidPriceError {
	return &InvalidPriceError{
		Date:         txn.Date,
		Account:      account,
		PostingIndex: postingIndex,
		PriceSpec:    priceSpec,
		Underlying:   err,
		Pos:          txn.Pos,
		Directive:    txn,
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
//   "file.bean:10: Invalid metadata: key="invoice", value="": empty value"
//   "file.bean:12: Invalid metadata (account Assets:Checking): key="note", value="xyz": duplicate key"
type InvalidMetadataError struct {
	Date       *ast.Date
	Account    ast.Account // Empty if directive-level metadata
	Key        string
	Value      string
	Reason     string // Why it's invalid (e.g., "duplicate key", "empty value")
	Pos        ast.Position
	Directive  ast.Directive
}

func (e *InvalidMetadataError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" && e.Date != nil {
		location = e.Date.Format("2006-01-02")
	}

	accountInfo := ""
	if e.Account != "" {
		accountInfo = fmt.Sprintf(" (account %s)", e.Account)
	}

	return fmt.Sprintf("%s: Invalid metadata%s: key=%q, value=%q: %s",
		location, accountInfo, e.Key, e.Value, e.Reason)
}

func (e *InvalidMetadataError) GetPosition() ast.Position {
	return e.Pos
}

func (e *InvalidMetadataError) GetDirective() ast.Directive {
	return e.Directive
}

func (e *InvalidMetadataError) GetAccount() ast.Account {
	return e.Account
}

func (e *InvalidMetadataError) GetDate() *ast.Date {
	return e.Date
}

// NewInvalidMetadataError creates an error for when metadata is invalid
func NewInvalidMetadataError(directive ast.Directive, account ast.Account, key, value, reason string) *InvalidMetadataError {
	var date *ast.Date
	var pos ast.Position

	// Extract date and position from directive
	switch d := directive.(type) {
	case *ast.Transaction:
		date = d.Date
		pos = d.Pos
	case *ast.Balance:
		date = d.Date
		pos = d.Pos
	case *ast.Pad:
		date = d.Date
		pos = d.Pos
	case *ast.Note:
		date = d.Date
		pos = d.Pos
	case *ast.Document:
		date = d.Date
		pos = d.Pos
	case *ast.Open:
		date = d.Date
		pos = d.Pos
	case *ast.Close:
		date = d.Date
		pos = d.Pos
	}

	return &InvalidMetadataError{
		Date:      date,
		Account:   account,
		Key:       key,
		Value:     value,
		Reason:    reason,
		Pos:       pos,
		Directive: directive,
	}
}
