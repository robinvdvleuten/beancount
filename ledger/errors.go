package ledger

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/robinvdvleuten/beancount/formatter"
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

// FormatWithContext formats the full error message including the directive context.
// This produces output similar to bean-check, showing the complete directive.
func (e *AccountNotOpenError) FormatWithContext(f *formatter.Formatter) string {
	var buf bytes.Buffer

	// Write the error message
	buf.WriteString(e.Error())
	buf.WriteString("\n\n")

	// Write the formatted directive with proper indentation
	if e.Directive != nil {
		// For transactions, use the exported FormatTransaction method
		if txn, ok := e.Directive.(*parser.Transaction); ok {
			var txnBuf bytes.Buffer
			directiveFormatter := formatter.New()
			if f != nil && f.CurrencyColumn > 0 {
				directiveFormatter = formatter.New(formatter.WithCurrencyColumn(f.CurrencyColumn))
			}

			if err := directiveFormatter.FormatTransaction(txn, &txnBuf); err == nil {
				// Indent each line with 3 spaces
				lines := bytes.Split(txnBuf.Bytes(), []byte("\n"))
				for _, line := range lines {
					if len(line) > 0 {
						buf.WriteString("   ")
						buf.Write(line)
						buf.WriteByte('\n')
					}
				}
			}
		} else {
			// For other directives, manually format them
			buf.WriteString("   ")

			switch d := e.Directive.(type) {
			case *parser.Balance:
				fmt.Fprintf(&buf, "%s balance %s", d.Date.Format("2006-01-02"), d.Account)
				if d.Amount != nil {
					fmt.Fprintf(&buf, "  %s %s", d.Amount.Value, d.Amount.Currency)
				}
			case *parser.Pad:
				fmt.Fprintf(&buf, "%s pad %s %s", d.Date.Format("2006-01-02"), d.Account, d.AccountPad)
			case *parser.Note:
				fmt.Fprintf(&buf, "%s note %s %q", d.Date.Format("2006-01-02"), d.Account, d.Description)
			case *parser.Document:
				fmt.Fprintf(&buf, "%s document %s %q", d.Date.Format("2006-01-02"), d.Account, d.PathToDocument)
			}
			buf.WriteByte('\n')
		}
	}

	return buf.String()
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

// FormatWithContext formats the full error message including the transaction context.
// This produces output similar to bean-check, showing the complete transaction.
func (e *TransactionNotBalancedError) FormatWithContext(f *formatter.Formatter) string {
	var buf bytes.Buffer

	// Write the error message
	buf.WriteString(e.Error())
	buf.WriteString("\n\n")

	// Write the formatted transaction with proper indentation
	if e.Transaction != nil {
		// Create a new formatter to avoid modifying the passed one
		txnFormatter := formatter.New()
		if f != nil && f.CurrencyColumn > 0 {
			txnFormatter = formatter.New(formatter.WithCurrencyColumn(f.CurrencyColumn))
		}

		// Write indented transaction (3 spaces to match bean-check style)
		var txnBuf bytes.Buffer
		if err := txnFormatter.FormatTransaction(e.Transaction, &txnBuf); err == nil {
			// Indent each line with 3 spaces
			lines := bytes.Split(txnBuf.Bytes(), []byte("\n"))
			for _, line := range lines {
				if len(line) > 0 {
					buf.WriteString("   ")
					buf.Write(line)
					buf.WriteByte('\n')
				}
			}
		}
	}

	return buf.String()
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
