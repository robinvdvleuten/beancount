package ledger

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// Diagnostic is a ledger error: an error found while booking, running
// Plugins or validating. Every kind shares this shape, so the CLI and the
// web API render them without knowing the kind. Its text starts with the
// Error line, path:line: (or the date when there is no source position).
type Diagnostic struct {
	kind      string
	message   string
	account   ast.Account
	pos       ast.Position
	date      *ast.Date
	directive ast.Directive
}

// newError creates an error of kind about directive d, positioned at d.
func newError(kind string, d ast.Directive, account ast.Account, format string, args ...any) *Diagnostic {
	return &Diagnostic{
		kind:      kind,
		message:   fmt.Sprintf(format, args...),
		account:   account,
		pos:       d.Position(),
		date:      d.Date(),
		directive: d,
	}
}

// atPosting moves the error to the posting's line, which beancount blames
// for errors about a single posting.
func (e *Diagnostic) atPosting(posting *ast.Posting) *Diagnostic {
	e.pos = posting.Position()
	return e
}

// Kind names the kind of error, e.g. "AccountNotOpenError".
func (e *Diagnostic) Kind() string { return e.kind }

// Message is the error's text without its location.
func (e *Diagnostic) Message() string { return e.message }

func (e *Diagnostic) Error() string { return e.location() + ": " + e.message }

// GetPosition returns where the error is reported.
func (e *Diagnostic) GetPosition() ast.Position { return e.pos }

// GetDirective returns the directive the error is about, or nil.
func (e *Diagnostic) GetDirective() ast.Directive { return e.directive }

// GetAccount returns the account the error is about, or "".
func (e *Diagnostic) GetAccount() ast.Account { return e.account }

// GetDate returns the date of the directive the error is about, or nil.
func (e *Diagnostic) GetDate() *ast.Date { return e.date }

// Severity is fatal for every ledger error, like bean-check's.
func (e *Diagnostic) Severity() diagnostic.Severity { return diagnostic.SeverityError }

// MarshalJSON renders the error for the web API.
func (e *Diagnostic) MarshalJSON() ([]byte, error) {
	data := map[string]any{
		"type":     e.kind,
		"message":  e.Error(),
		"position": e.pos,
	}
	if e.account != "" {
		data["account"] = string(e.account)
	}
	if e.date != nil {
		data["date"] = e.date.String()
	}
	return json.Marshal(data)
}

// location returns path:line, or the date when there is no source position.
func (e *Diagnostic) location() string {
	if e.pos.Filename != "" {
		return fmt.Sprintf("%s:%d", e.pos.Filename, e.pos.Line)
	}
	if e.date != nil {
		return e.date.String()
	}
	return "unknown"
}

// BalanceMismatchError is returned when a balance assertion fails. It keeps
// its amounts, which print reads to show the difference.
type BalanceMismatchError struct {
	Diagnostic
	Expected string // Expected amount
	Actual   string // Actual amount in inventory
	// Difference is the actual amount less the expected one, like
	// beancount's diff_amount, with the exponent the subtraction leaves.
	Difference decimal.Decimal
}

// NewBalanceMismatchError creates an error for a balance assertion whose
// account holds actual instead of the expected amount.
func NewBalanceMismatchError(balance *ast.Balance, expected, actual decimal.Decimal) *BalanceMismatchError {
	currency := balance.Amount.Currency
	return &BalanceMismatchError{
		Diagnostic: *newError("BalanceMismatchError", balance, balance.Account,
			"Balance mismatch for %s:\n  Expected: %s %s\n  Actual:   %s %s",
			balance.Account, expected.String(), currency, actual.String(), currency),
		Expected:   expected.String(),
		Actual:     actual.String(),
		Difference: pydecimal.Sub(actual, expected),
	}
}

// NewAccountNotOpenError creates an error for a directive that references an
// account that is not open.
func NewAccountNotOpenError(d ast.Directive, account ast.Account) *Diagnostic {
	return newError("AccountNotOpenError", d, account, "Invalid reference to unknown account '%s'", account)
}

// NewAccountAlreadyOpenError creates an error for opening an account that is
// already open.
func NewAccountAlreadyOpenError(open *ast.Open, openedDate *ast.Date) *Diagnostic {
	return newError("AccountAlreadyOpenError", open, open.Account,
		"Account %s is already open (opened on %s)", open.Account, openedDate.String())
}

// NewInvalidAccountNameError creates an error for an account whose type is
// not one of the configured account types.
func NewInvalidAccountNameError(open *ast.Open, cfg *Config) *Diagnostic {
	validAccountTypes := []string{
		cfg.AccountNames.Assets,
		cfg.AccountNames.Liabilities,
		cfg.AccountNames.Equity,
		cfg.AccountNames.Income,
		cfg.AccountNames.Expenses,
	}
	accountType, _, found := strings.Cut(string(open.Account), ":")
	if !found {
		accountType = "?"
	}
	return newError("InvalidAccountNameError", open, open.Account,
		"Account %q uses invalid type %q, expected one of: %s",
		open.Account, accountType, strings.Join(validAccountTypes, ", "))
}

// NewAccountAlreadyClosedError creates an error for closing an account that
// is already closed.
func NewAccountAlreadyClosedError(close *ast.Close, closedDate *ast.Date) *Diagnostic {
	return newError("AccountAlreadyClosedError", close, close.Account,
		"Account %s is already closed (closed on %s)", close.Account, closedDate.String())
}

// NewAccountNotClosedError creates an error for closing an account that was
// never opened.
func NewAccountNotClosedError(close *ast.Close) *Diagnostic {
	return newError("AccountNotClosedError", close, close.Account,
		"Cannot close account %s that was never opened", close.Account)
}

// NewDuplicateCommodityError creates an error for a repeated commodity
// directive.
func NewDuplicateCommodityError(commodity *ast.Commodity) *Diagnostic {
	return newError("DuplicateCommodityError", commodity, "",
		"Duplicate commodity directives for '%s'", commodity.Currency)
}

// NewBalanceCurrencyError creates an error for a balance assertion in a
// currency its account does not allow.
func NewBalanceCurrencyError(balance *ast.Balance) *Diagnostic {
	return newError("BalanceCurrencyError", balance, balance.Account,
		"Invalid currency '%s' for Balance directive", balance.Amount.Currency)
}

// NewDuplicateBalanceError creates an error for a balance assertion that
// repeats an earlier one with a different amount.
func NewDuplicateBalanceError(balance *ast.Balance) *Diagnostic {
	return newError("DuplicateBalanceError", balance, balance.Account,
		"Duplicate balance assertion with different amounts")
}

// NewNegativeCostError creates an error for a posting booked at a negative
// cost. Like beancount, it blames the posting's line.
func NewNegativeCostError(txn *ast.Transaction, posting *ast.Posting, cost decimal.Decimal, currency string) *Diagnostic {
	return newError("NegativeCostError", txn, posting.Account,
		"Cost is negative: %s %s (account %s)", cost.String(), currency, posting.Account).atPosting(posting)
}

// NewMergeCostError creates an error for a posting with a merge cost {*},
// which beancount v2 rejects and then books like an empty cost {}. Like
// beancount, it blames the posting's line.
func NewMergeCostError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("MergeCostError", txn, posting.Account,
		"Cost merging is not supported yet (account %s)", posting.Account).atPosting(posting)
}

// NewNegativePriceError creates an error for a posting with a negative
// price, which beancount books at its absolute value. Like beancount, it
// blames the posting's line.
func NewNegativePriceError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("NegativePriceError", txn, posting.Account,
		"Negative prices are not allowed: %s %s", posting.Price.Value, posting.Price.Currency).atPosting(posting)
}

// NewTotalPriceWithoutUnitsError creates an error for a total price (@@) on
// a posting without units, which beancount drops. Like beancount, it blames
// the posting's line.
func NewTotalPriceWithoutUnitsError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("TotalPriceWithoutUnitsError", txn, posting.Account,
		"Total price on a posting without units: %s %s", posting.Price.Value, posting.Price.Currency).atPosting(posting)
}

// NewCurrencyGroupError creates an error for a posting that Booking cannot
// sort into a Currency group, or whose group's missing numbers it cannot
// complete. Like beancount, it blames the posting's line.
func NewCurrencyGroupError(txn *ast.Transaction, posting *ast.Posting, message string) *Diagnostic {
	return newError("CurrencyGroupError", txn, posting.Account,
		"%s (account %s)", message, posting.Account).atPosting(posting)
}

// NewInvalidBookingMethodError creates an error for an open directive with an
// unknown booking method.
func NewInvalidBookingMethodError(open *ast.Open) *Diagnostic {
	return newError("InvalidBookingMethodError", open, open.Account,
		"Invalid booking method: %s", open.BookingMethod)
}

// NewTransactionNotBalancedError creates an error for a transaction that does
// not balance, listing its residuals by currency.
func NewTransactionNotBalancedError(txn *ast.Transaction, residuals map[string]string) *Diagnostic {
	var buf strings.Builder
	if len(residuals) > 0 {
		currencies := make([]string, 0, len(residuals))
		for currency := range residuals {
			currencies = append(currencies, currency)
		}
		slices.Sort(currencies)
		buf.WriteByte('(')
		for i, currency := range currencies {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(residuals[currency])
			buf.WriteByte(' ')
			buf.WriteString(currency)
		}
		buf.WriteByte(')')
	}
	return newError("TransactionNotBalancedError", txn, "", "Transaction does not balance: %s", buf.String())
}

// NewInvalidAmountError creates an error for an amount that cannot be parsed.
func NewInvalidAmountError(d ast.Directive, account ast.Account, value string, err error) *Diagnostic {
	return newError("InvalidAmountError", d, account, "Invalid amount %q for account %s: %v", value, account, err)
}

// NewInvalidCostError creates an error for an invalid cost specification.
func NewInvalidCostError(txn *ast.Transaction, account ast.Account, postingIndex int, costSpec string, err error) *Diagnostic {
	return newError("InvalidCostError", txn, account,
		"Invalid cost specification%s: %s: %v", postingInfo(postingIndex, account), costSpec, err)
}

// NewTotalCostError creates an error for an invalid total cost {{}}, such as
// one on zero units.
func NewTotalCostError(txn *ast.Transaction, posting *ast.Posting, message string) *Diagnostic {
	return newError("TotalCostError", txn, posting.Account, "Invalid total cost specification: %s", message)
}

// NewInvalidPriceError creates an error for an invalid price specification.
func NewInvalidPriceError(txn *ast.Transaction, account ast.Account, postingIndex int, priceSpec string, err error) *Diagnostic {
	return newError("InvalidPriceError", txn, account,
		"Invalid price specification%s: %s: %v", postingInfo(postingIndex, account), priceSpec, err)
}

// postingInfo names a posting by its number and account, or nothing when the
// index is unknown.
func postingInfo(postingIndex int, account ast.Account) string {
	if postingIndex < 0 {
		return ""
	}
	return fmt.Sprintf(" (Posting #%d: %s)", postingIndex+1, account)
}

// NewInvalidMetadataError creates an error for invalid metadata, on a
// directive or, with an account, on one of its postings.
func NewInvalidMetadataError(directive ast.Directive, account ast.Account, key string, value *ast.MetadataValue, reason string) *Diagnostic {
	accountInfo := ""
	if account != "" {
		accountInfo = fmt.Sprintf(" (account %s)", account)
	}
	valueStr := ""
	if value != nil {
		valueStr = value.String()
	}
	return newError("InvalidMetadataError", directive, account,
		"Invalid metadata%s: key=%q, value=%q: %s", accountInfo, key, valueStr, reason)
}

// NewInsufficientInventoryError creates an error for a reduction the
// account's lots cannot cover.
func NewInsufficientInventoryError(txn *ast.Transaction, account ast.Account, details error) *Diagnostic {
	return newError("InsufficientInventoryError", txn, account, "Insufficient inventory (account %s): %v", account, details)
}

// NewAmbiguousBookingError creates an error for a reduction that matches
// several lots under STRICT booking.
func NewAmbiguousBookingError(txn *ast.Transaction, account ast.Account, details error) *Diagnostic {
	return newError("AmbiguousBookingError", txn, account, "Ambiguous booking (account %s): %v", account, details)
}

// NewCurrencyConstraintError creates an error for a posting in a currency its
// account does not allow.
func NewCurrencyConstraintError(txn *ast.Transaction, account ast.Account, currency string, allowedCurrencies []string) *Diagnostic {
	return newError("CurrencyConstraintError", txn, account,
		"Currency %s not allowed for account %s (allowed: %v)", currency, account, allowedCurrencies)
}

// NewUnusedPadWarning creates an error for a pad that inserted no padding.
// It is fatal, as official bean-check rejects unused pad entries.
func NewUnusedPadWarning(pad *ast.Pad) *Diagnostic {
	return newError("UnusedPadWarning", pad, pad.Account, "Unused Pad entry")
}

// NewDocumentFileError creates an error for a document directive referencing
// a file that does not exist, matching beancount's
// verify_document_files_exist.
func NewDocumentFileError(doc *ast.Document, path string) *Diagnostic {
	return newError("DocumentFileError", doc, doc.Account, "File does not exist: %q", path)
}

// NewInvalidDirectivePriceError creates an error for a price directive with
// invalid data.
func NewInvalidDirectivePriceError(message string, price *ast.Price) *Diagnostic {
	return newError("InvalidDirectivePriceError", price, "", "%s", message)
}

// NewPluginConfigError creates an error for a plugin directive that passes a
// configuration to a Built-in Plugin that takes none. Beancount fails to
// apply such a plugin; so do we.
func NewPluginConfigError(plugin *ast.Plugin) *Diagnostic {
	return &Diagnostic{
		kind:    "PluginConfigError",
		message: fmt.Sprintf("Plugin %q takes no configuration", plugin.Name.String()),
		pos:     plugin.Position(),
	}
}

// NewPluginImportError creates an error for a plugin directive naming a
// module under beancount.plugins that beancount v2 does not ship, which it
// fails to import.
func NewPluginImportError(plugin *ast.Plugin) *Diagnostic {
	return &Diagnostic{
		kind:    "PluginImportError",
		message: fmt.Sprintf("Error importing %q", plugin.Name.String()),
		pos:     plugin.Position(),
	}
}
