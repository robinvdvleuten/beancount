package ledger

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/parser"
)

// Error types for ledger validation errors

// AccountNotOpenError is returned when a directive references an account that hasn't been opened
type AccountNotOpenError struct {
	Account parser.Account
	Date    *parser.Date
}

func (e *AccountNotOpenError) Error() string {
	return fmt.Sprintf("%s: Account %s is not open", e.Date.Format("2006-01-02"), e.Account)
}

// AccountAlreadyOpenError is returned when trying to open an account that's already open
type AccountAlreadyOpenError struct {
	Account    parser.Account
	Date       *parser.Date
	OpenedDate *parser.Date
}

func (e *AccountAlreadyOpenError) Error() string {
	return fmt.Sprintf("%s: Account %s is already open (opened on %s)",
		e.Date.Format("2006-01-02"), e.Account, e.OpenedDate.Format("2006-01-02"))
}

// AccountAlreadyClosedError is returned when trying to use or close an account that's already closed
type AccountAlreadyClosedError struct {
	Account    parser.Account
	Date       *parser.Date
	ClosedDate *parser.Date
}

func (e *AccountAlreadyClosedError) Error() string {
	return fmt.Sprintf("%s: Account %s is already closed (closed on %s)",
		e.Date.Format("2006-01-02"), e.Account, e.ClosedDate.Format("2006-01-02"))
}

// AccountNotClosedError is returned when trying to close an account that was never opened
type AccountNotClosedError struct {
	Account parser.Account
	Date    *parser.Date
}

func (e *AccountNotClosedError) Error() string {
	return fmt.Sprintf("%s: Cannot close account %s that was never opened",
		e.Date.Format("2006-01-02"), e.Account)
}

// TransactionNotBalancedError is returned when a transaction doesn't balance
type TransactionNotBalancedError struct {
	Date      *parser.Date
	Narration string
	Residuals map[string]string // currency -> amount string
}

func (e *TransactionNotBalancedError) Error() string {
	msg := fmt.Sprintf("%s: Transaction does not balance", e.Date.Format("2006-01-02"))
	if e.Narration != "" {
		msg += fmt.Sprintf(" (%s)", e.Narration)
	}
	msg += ":"
	for currency, amount := range e.Residuals {
		msg += fmt.Sprintf("\n  %s %s", amount, currency)
	}
	return msg
}

// InvalidAmountError is returned when an amount cannot be parsed
type InvalidAmountError struct {
	Date       *parser.Date
	Account    parser.Account
	Value      string
	Underlying error
}

func (e *InvalidAmountError) Error() string {
	return fmt.Sprintf("%s: Invalid amount %q for account %s: %v",
		e.Date.Format("2006-01-02"), e.Value, e.Account, e.Underlying)
}

// BalanceMismatchError is returned when a balance assertion fails
type BalanceMismatchError struct {
	Date     *parser.Date
	Account  parser.Account
	Expected string // Expected amount
	Actual   string // Actual amount in inventory
	Currency string
}

func (e *BalanceMismatchError) Error() string {
	return fmt.Sprintf("%s: Balance mismatch for %s:\n  Expected: %s %s\n  Actual:   %s %s",
		e.Date.Format("2006-01-02"), e.Account,
		e.Expected, e.Currency,
		e.Actual, e.Currency)
}
