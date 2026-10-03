package ledger

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
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

var _ diagnostic.Positioned = (*Diagnostic)(nil)

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
	expected string // Expected amount
	actual   string // Actual amount in inventory
	// Difference is the actual amount less the expected one, like
	// beancount's diff_amount, with the exponent the subtraction leaves.
	Difference decimal.Decimal
}

// newBalanceMismatchError creates an error for a balance assertion whose
// account holds actual instead of the expected amount.
func newBalanceMismatchError(balance *ast.Balance, expected, actual decimal.Decimal) *BalanceMismatchError {
	currency := balance.Amount.Currency
	return &BalanceMismatchError{
		Diagnostic: *newError("BalanceMismatchError", balance, balance.Account,
			"Balance mismatch for %s:\n  Expected: %s %s\n  Actual:   %s %s",
			balance.Account, expected.String(), currency, actual.String(), currency),
		expected:   expected.String(),
		actual:     actual.String(),
		Difference: pydecimal.Sub(actual, expected),
	}
}

// newAccountNotOpenError creates an error for a directive that references an
// account the ledger never opens.
func newAccountNotOpenError(d ast.Directive, account ast.Account) *Diagnostic {
	return newError("AccountNotOpenError", d, account, "Invalid reference to unknown account '%s'", account)
}

// newInactiveAccountError creates an error for a directive that references an
// account the ledger opens, but not over the directive's date: before its
// open or after its close.
func newInactiveAccountError(d ast.Directive, account ast.Account) *Diagnostic {
	return newError("AccountNotOpenError", d, account, "Invalid reference to inactive account '%s'", account)
}

// newAccountAlreadyOpenError creates an error for opening an account that is
// already open.
func newAccountAlreadyOpenError(open *ast.Open, openedDate *ast.Date) *Diagnostic {
	return newError("AccountAlreadyOpenError", open, open.Account,
		"Account %s is already open (opened on %s)", open.Account, openedDate.String())
}

// newInvalidAccountNameError creates an error for an account whose type is
// not one of the configured account types.
func newInvalidAccountNameError(open *ast.Open, cfg *sharedconfig.Config) *Diagnostic {
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

// newAccountAlreadyClosedError creates an error for closing an account that
// is already closed.
func newAccountAlreadyClosedError(close *ast.Close, closedDate *ast.Date) *Diagnostic {
	return newError("AccountAlreadyClosedError", close, close.Account,
		"Account %s is already closed (closed on %s)", close.Account, closedDate.String())
}

// newAccountNotClosedError creates an error for closing an account that was
// never opened.
func newAccountNotClosedError(close *ast.Close) *Diagnostic {
	return newError("AccountNotClosedError", close, close.Account,
		"Cannot close account %s that was never opened", close.Account)
}

// newDuplicateCommodityError creates an error for a repeated commodity
// directive.
func newDuplicateCommodityError(commodity *ast.Commodity) *Diagnostic {
	return newError("DuplicateCommodityError", commodity, "",
		"Duplicate commodity directives for '%s'", commodity.Currency)
}

// newBalanceCurrencyError creates an error for a balance assertion in a
// currency its account does not allow.
func newBalanceCurrencyError(balance *ast.Balance) *Diagnostic {
	return newError("BalanceCurrencyError", balance, balance.Account,
		"Invalid currency '%s' for Balance directive: ", balance.Amount.Currency)
}

// newDuplicateBalanceError creates an error for a balance assertion that
// repeats an earlier one with a different amount.
func newDuplicateBalanceError(balance *ast.Balance) *Diagnostic {
	return newError("DuplicateBalanceError", balance, balance.Account,
		"Duplicate balance assertion with different amounts")
}

// newNegativeCostError creates an error for a posting booked at a negative
// cost. Like beancount, it blames the posting's line.
func newNegativeCostError(txn *ast.Transaction, posting *ast.Posting, cost decimal.Decimal, currency string) *Diagnostic {
	return newError("NegativeCostError", txn, posting.Account,
		"Cost is negative: %s %s (account %s)", cost.String(), currency, posting.Account).atPosting(posting)
}

// newZeroAmountError creates an error for a posting booked at cost with zero
// units. Like beancount, it blames the posting's line.
func newZeroAmountError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("ZeroAmountError", txn, posting.Account,
		"Amount is zero: \"%s %s\"", posting.Amount.Value, posting.Amount.Currency).atPosting(posting)
}

// newMergeCostError creates an error for a posting with a merge cost {*},
// which beancount v2 rejects and then books like an empty cost {}. Like
// beancount, it blames the posting's line and uses its words.
func newMergeCostError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("MergeCostError", txn, posting.Account,
		"Cost merging is not supported yet").atPosting(posting)
}

// newNegativePriceError creates an error for a posting with a negative
// price, which beancount books at its absolute value. Like beancount, it
// blames the posting's line.
func newNegativePriceError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("NegativePriceError", txn, posting.Account,
		"Negative prices are not allowed: %s %s", posting.Price.Value, posting.Price.Currency).atPosting(posting)
}

// newTotalPriceWithoutUnitsError creates an error for a total price (@@) on
// a posting without units, which beancount drops. Like beancount, it blames
// the posting's line.
func newTotalPriceWithoutUnitsError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("TotalPriceWithoutUnitsError", txn, posting.Account,
		"Total price on a posting without units: %s %s", posting.Price.Value, posting.Price.Currency).atPosting(posting)
}

// newCurrencyGroupError creates an error for a posting that Booking cannot
// sort into a Currency group, or whose group's missing numbers it cannot
// complete. Like beancount, it blames the posting's line and prints the
// message alone.
func newCurrencyGroupError(txn *ast.Transaction, posting *ast.Posting, message string) *Diagnostic {
	return newError("CurrencyGroupError", txn, posting.Account, "%s", message).atPosting(posting)
}

// newInvalidBookingMethodError creates an error for an open directive with an
// unknown booking method.
func newInvalidBookingMethodError(open *ast.Open) *Diagnostic {
	return newError("InvalidBookingMethodError", open, open.Account,
		"Invalid booking method: %s", open.BookingMethod)
}

// newUnbookedTransactionError creates an error for a transaction that
// reached validation without a booking result, because a Plugin added it
// after Booking ran.
func newUnbookedTransactionError(txn *ast.Transaction) *Diagnostic {
	return newError("UnbookedTransactionError", txn, "",
		"Transaction was not booked: it was added after Booking")
}

// newTransactionNotBalancedError creates an error for a transaction that does
// not balance, listing its residuals by currency.
func newTransactionNotBalancedError(txn *ast.Transaction, residuals map[string]string) *Diagnostic {
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

// newInvalidAmountError creates an error for an amount that cannot be parsed.
func newInvalidAmountError(d ast.Directive, account ast.Account, value string, err error) *Diagnostic {
	return newError("InvalidAmountError", d, account, "Invalid amount %q for account %s: %v", value, account, err)
}

// newInvalidCostError creates an error for an invalid cost specification.
func newInvalidCostError(txn *ast.Transaction, account ast.Account, postingIndex int, costSpec string, err error) *Diagnostic {
	return newError("InvalidCostError", txn, account,
		"Invalid cost specification%s: %s: %v", postingInfo(postingIndex, account), costSpec, err)
}

// newTotalCostError creates an error for an invalid total cost {{}}, such as
// one on zero units.
func newTotalCostError(txn *ast.Transaction, posting *ast.Posting, message string) *Diagnostic {
	return newError("TotalCostError", txn, posting.Account, "Invalid total cost specification: %s", message)
}

// newTotalCompoundCostError creates an error for a compound cost inside
// total braces ({{5 # 3 USD}}), whose per-unit number beancount ignores. Like
// beancount, it blames the posting's line and quotes the compound amount's
// Python repr.
func newTotalCompoundCostError(txn *ast.Transaction, posting *ast.Posting) *Diagnostic {
	return newError("TotalCostError", txn, posting.Account,
		"Per-unit cost may not be specified using total cost syntax: '%s'; ignoring per-unit cost",
		compoundAmountRepr(posting.Cost)).atPosting(posting)
}

// newDuplicateCostComponentError creates an error for a component a cost
// spec repeats (a Cost of ast.Cost.Duplicates), which beancount reports and
// ignores, keeping the first. Like beancount, it blames the posting's line
// and uses its words.
func newDuplicateCostComponentError(txn *ast.Transaction, posting *ast.Posting, duplicate *ast.Cost) *Diagnostic {
	var message string
	switch {
	case duplicate.Amount != nil:
		message = "Duplicate cost: '" + compoundAmountRepr(duplicate) + "'."
	case duplicate.Date != nil:
		message = "Duplicate date: '" + duplicate.Date.Format("2006-01-02") + "'."
	case duplicate.IsMerge:
		message = "Duplicate merge-cost spec"
	default:
		message = "Duplicate label: '" + duplicate.Label + "'."
	}
	return newError("DuplicateCostComponentError", txn, posting.Account, "%s", message).atPosting(posting)
}

// compoundAmountRepr is the Python repr of a cost's amount as beancount's
// CompoundAmount holds it: a number left out is MISSING, and a total None
// unless the amount is a compound.
func compoundAmountRepr(cost *ast.Cost) string {
	const missing = "<class 'beancount.core.number.MISSING'>"
	number := func(amount *ast.Amount) string {
		if amount == nil {
			return "None"
		}
		n, err := ParseAmount(amount)
		if amount.Value == "" || err != nil {
			return missing
		}
		return "Decimal(" + pyrepr.String(pydecimal.String(n)) + ")"
	}
	currency := missing
	if cost.Amount.Currency != "" {
		currency = pyrepr.String(cost.Amount.Currency)
	}
	return fmt.Sprintf("CompoundAmount(number_per=%s, number_total=%s, currency=%s)",
		number(cost.Amount), number(cost.Total), currency)
}

// newInvalidPriceError creates an error for an invalid price specification.
func newInvalidPriceError(txn *ast.Transaction, account ast.Account, postingIndex int, priceSpec string, err error) *Diagnostic {
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

// newInvalidMetadataError creates an error for invalid metadata, on a
// directive or, with an account, on one of its postings.
func newInvalidMetadataError(directive ast.Directive, account ast.Account, key string, value *ast.MetadataValue, reason string) *Diagnostic {
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

// newInsufficientInventoryError creates an error for a reduction the
// account's lots cannot cover, or that matches none of them, worded by
// details as beancount words it.
func newInsufficientInventoryError(txn *ast.Transaction, account ast.Account, details error) *Diagnostic {
	return newError("InsufficientInventoryError", txn, account, "%v", details)
}

// newAmbiguousBookingError creates an error for a reduction that matches
// several lots under STRICT booking, worded by details as beancount words
// it.
func newAmbiguousBookingError(txn *ast.Transaction, account ast.Account, details error) *Diagnostic {
	return newError("AmbiguousBookingError", txn, account, "%v", details)
}

// newCurrencyConstraintError creates an error for a posting in a currency its
// account does not allow, worded as beancount words it.
func newCurrencyConstraintError(txn *ast.Transaction, account ast.Account, currency string) *Diagnostic {
	return newError("CurrencyConstraintError", txn, account,
		"Invalid currency %s for account '%s'", currency, account)
}

// newPadCostError creates an error for a pad that fills a currency its
// account holds at cost. Like beancount's, it is reported on the balance
// assertion's line, shows the pad, and lists the account's inventory as it is
// before the padding.
func newPadCostError(balance *ast.Balance, pad *ast.Pad, inv *inventory) *Diagnostic {
	e := newError("PadError", pad, pad.Account, "Attempt to pad an entry with cost for balance: %s", inv.String())
	e.pos = balance.Position()
	e.date = balance.Date()
	return e
}

// newUnusedPadWarning creates an error for a pad that inserted no padding.
// It is fatal, as official bean-check rejects unused pad entries.
func newUnusedPadWarning(pad *ast.Pad) *Diagnostic {
	return newError("UnusedPadWarning", pad, pad.Account, "Unused Pad entry")
}

// newDocumentFileError creates an error for a document directive referencing
// a file that does not exist, matching beancount's
// verify_document_files_exist.
func newDocumentFileError(doc *ast.Document) *Diagnostic {
	return newError("DocumentFileError", doc, doc.Account, "File does not exist: %q", doc.ResolvedPath())
}

// newInvalidDirectivePriceError creates an error for a price directive with
// invalid data.
func newInvalidDirectivePriceError(message string, price *ast.Price) *Diagnostic {
	return newError("InvalidDirectivePriceError", price, "", "%s", message)
}

// newPluginConfigError creates an error for a plugin directive that passes a
// configuration to a Built-in Plugin that takes none. Beancount fails to
// apply such a plugin; so do we.
func newPluginConfigError(plugin *ast.Plugin) *Diagnostic {
	return &Diagnostic{
		kind:    "PluginConfigError",
		message: fmt.Sprintf("Plugin %q takes no configuration", plugin.Name.String()),
		pos:     plugin.Position(),
	}
}

// newPluginImportError creates an error for a plugin directive naming a
// module under beancount.plugins that beancount v2 does not ship, which it
// fails to import.
func newPluginImportError(plugin *ast.Plugin) *Diagnostic {
	return &Diagnostic{
		kind:    "PluginImportError",
		message: fmt.Sprintf("Error importing %q", plugin.Name.String()),
		pos:     plugin.Position(),
	}
}
