package ledger

import (
	"fmt"
	"sort"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/robinvdvleuten/beancount/parser"
)

// Error types for ledger validation errors

// AccountNotOpenError is returned when a directive references an account that hasn't been opened
type AccountNotOpenError struct {
	Account   parser.Account
	Date      *parser.Date
	Pos       lexer.Position   // Position in source file (includes filename)
	Directive parser.Directive // The directive that referenced the closed account
}

func (e *AccountNotOpenError) Error() string {
	// Format: filename:line: message
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Invalid reference to unknown account '%s'", location, e.Account)
}

func (e *AccountNotOpenError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *AccountNotOpenError) GetDirective() parser.Directive {
	return e.Directive
}

func (e *AccountNotOpenError) GetAccount() parser.Account {
	return e.Account
}

func (e *AccountNotOpenError) GetDate() *parser.Date {
	return e.Date
}

// AccountAlreadyOpenError is returned when trying to open an account that's already open
type AccountAlreadyOpenError struct {
	Account    parser.Account
	Date       *parser.Date
	OpenedDate *parser.Date
	Pos        lexer.Position
	Directive  parser.Directive
}

func (e *AccountAlreadyOpenError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Account %s is already open (opened on %s)",
		location, e.Account, e.OpenedDate.Format("2006-01-02"))
}

func (e *AccountAlreadyOpenError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *AccountAlreadyOpenError) GetDirective() parser.Directive {
	return e.Directive
}

func (e *AccountAlreadyOpenError) GetAccount() parser.Account {
	return e.Account
}

func (e *AccountAlreadyOpenError) GetDate() *parser.Date {
	return e.Date
}

// AccountAlreadyClosedError is returned when trying to use or close an account that's already closed
type AccountAlreadyClosedError struct {
	Account    parser.Account
	Date       *parser.Date
	ClosedDate *parser.Date
	Pos        lexer.Position
	Directive  parser.Directive
}

func (e *AccountAlreadyClosedError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Account %s is already closed (closed on %s)",
		location, e.Account, e.ClosedDate.Format("2006-01-02"))
}

func (e *AccountAlreadyClosedError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *AccountAlreadyClosedError) GetDirective() parser.Directive {
	return e.Directive
}

func (e *AccountAlreadyClosedError) GetAccount() parser.Account {
	return e.Account
}

func (e *AccountAlreadyClosedError) GetDate() *parser.Date {
	return e.Date
}

// AccountNotClosedError is returned when trying to close an account that was never opened
type AccountNotClosedError struct {
	Account   parser.Account
	Date      *parser.Date
	Pos       lexer.Position
	Directive parser.Directive
}

func (e *AccountNotClosedError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Cannot close account %s that was never opened",
		location, e.Account)
}

func (e *AccountNotClosedError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *AccountNotClosedError) GetDirective() parser.Directive {
	return e.Directive
}

func (e *AccountNotClosedError) GetAccount() parser.Account {
	return e.Account
}

func (e *AccountNotClosedError) GetDate() *parser.Date {
	return e.Date
}

// TransactionNotBalancedError is returned when a transaction doesn't balance
type TransactionNotBalancedError struct {
	Pos         lexer.Position      // Position in source file (includes filename)
	Date        *parser.Date        // Transaction date
	Narration   string              // Transaction narration
	Residuals   map[string]string   // currency -> amount string (unbalanced amounts)
	Transaction *parser.Transaction // Full transaction for context rendering
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
	result := "("
	for i, currency := range currencies {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s %s", e.Residuals[currency], currency)
	}
	result += ")"

	return result
}

func (e *TransactionNotBalancedError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *TransactionNotBalancedError) GetDirective() parser.Directive {
	return e.Transaction
}

func (e *TransactionNotBalancedError) GetDate() *parser.Date {
	return e.Date
}

// InvalidAmountError is returned when an amount cannot be parsed
type InvalidAmountError struct {
	Date       *parser.Date
	Account    parser.Account
	Value      string
	Underlying error
	Pos        lexer.Position
	Directive  parser.Directive
}

func (e *InvalidAmountError) Error() string {
	location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
	if e.Pos.Filename == "" {
		location = e.Date.Format("2006-01-02")
	}

	return fmt.Sprintf("%s: Invalid amount %q for account %s: %v",
		location, e.Value, e.Account, e.Underlying)
}

func (e *InvalidAmountError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *InvalidAmountError) GetDirective() parser.Directive {
	return e.Directive
}

func (e *InvalidAmountError) GetAccount() parser.Account {
	return e.Account
}

func (e *InvalidAmountError) GetDate() *parser.Date {
	return e.Date
}

// BalanceMismatchError is returned when a balance assertion fails
type BalanceMismatchError struct {
	Date      *parser.Date
	Account   parser.Account
	Expected  string // Expected amount
	Actual    string // Actual amount in inventory
	Currency  string
	Pos       lexer.Position
	Directive parser.Directive
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

func (e *BalanceMismatchError) GetPosition() lexer.Position {
	return e.Pos
}

func (e *BalanceMismatchError) GetDirective() parser.Directive {
	return e.Directive
}

func (e *BalanceMismatchError) GetAccount() parser.Account {
	return e.Account
}

func (e *BalanceMismatchError) GetDate() *parser.Date {
	return e.Date
}

// Constructor functions for ledger errors.
// These provide a cleaner API and ensure consistent field initialization.

// NewAccountNotOpenError creates an error for when a directive references an unopened account.
// Use this for transactions where the account comes from a posting.
func NewAccountNotOpenError(txn *parser.Transaction, account parser.Account) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   account,
		Date:      txn.Date,
		Pos:       txn.Pos,
		Directive: txn,
	}
}

// NewAccountNotOpenErrorFromBalance creates an error for a balance directive referencing an unopened account.
func NewAccountNotOpenErrorFromBalance(balance *parser.Balance) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   balance.Account,
		Date:      balance.Date,
		Pos:       balance.Pos,
		Directive: balance,
	}
}

// NewAccountNotOpenErrorFromPad creates an error for a pad directive referencing an unopened account.
func NewAccountNotOpenErrorFromPad(pad *parser.Pad, account parser.Account) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   account,
		Date:      pad.Date,
		Pos:       pad.Pos,
		Directive: pad,
	}
}

// NewAccountNotOpenErrorFromNote creates an error for a note directive referencing an unopened account.
func NewAccountNotOpenErrorFromNote(note *parser.Note) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   note.Account,
		Date:      note.Date,
		Pos:       note.Pos,
		Directive: note,
	}
}

// NewAccountNotOpenErrorFromDocument creates an error for a document directive referencing an unopened account.
func NewAccountNotOpenErrorFromDocument(doc *parser.Document) *AccountNotOpenError {
	return &AccountNotOpenError{
		Account:   doc.Account,
		Date:      doc.Date,
		Pos:       doc.Pos,
		Directive: doc,
	}
}

// NewAccountAlreadyOpenError creates an error for when trying to open an already-open account.
func NewAccountAlreadyOpenError(open *parser.Open, openedDate *parser.Date) *AccountAlreadyOpenError {
	return &AccountAlreadyOpenError{
		Account:    open.Account,
		Date:       open.Date,
		OpenedDate: openedDate,
		Pos:        open.Pos,
		Directive:  open,
	}
}

// NewAccountAlreadyClosedError creates an error for when trying to use or close an already-closed account.
func NewAccountAlreadyClosedError(close *parser.Close, closedDate *parser.Date) *AccountAlreadyClosedError {
	return &AccountAlreadyClosedError{
		Account:    close.Account,
		Date:       close.Date,
		ClosedDate: closedDate,
		Pos:        close.Pos,
		Directive:  close,
	}
}

// NewAccountNotClosedError creates an error for when trying to close an account that was never opened.
func NewAccountNotClosedError(close *parser.Close) *AccountNotClosedError {
	return &AccountNotClosedError{
		Account:   close.Account,
		Date:      close.Date,
		Pos:       close.Pos,
		Directive: close,
	}
}

// NewTransactionNotBalancedError creates an error for when a transaction doesn't balance.
func NewTransactionNotBalancedError(txn *parser.Transaction, residuals map[string]string) *TransactionNotBalancedError {
	return &TransactionNotBalancedError{
		Pos:         txn.Pos,
		Date:        txn.Date,
		Narration:   txn.Narration,
		Residuals:   residuals,
		Transaction: txn,
	}
}

// NewInvalidAmountError creates an error for when an amount in a transaction cannot be parsed or is invalid.
func NewInvalidAmountError(txn *parser.Transaction, account parser.Account, value string, err error) *InvalidAmountError {
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
func NewInvalidAmountErrorFromBalance(balance *parser.Balance, err error) *InvalidAmountError {
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
func NewBalanceMismatchError(balance *parser.Balance, expected, actual, currency string) *BalanceMismatchError {
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
