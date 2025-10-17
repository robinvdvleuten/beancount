package ledger

import (
	"strings"

	"github.com/robinvdvleuten/beancount/parser"
)

// AccountType represents the type of account
type AccountType int

const (
	AccountTypeUnknown AccountType = iota
	AccountTypeAssets
	AccountTypeLiabilities
	AccountTypeEquity
	AccountTypeIncome
	AccountTypeExpenses
)

// String returns the string representation of the account type
func (t AccountType) String() string {
	switch t {
	case AccountTypeAssets:
		return "Assets"
	case AccountTypeLiabilities:
		return "Liabilities"
	case AccountTypeEquity:
		return "Equity"
	case AccountTypeIncome:
		return "Income"
	case AccountTypeExpenses:
		return "Expenses"
	default:
		return "Unknown"
	}
}

// Account represents an account in the ledger
type Account struct {
	Name                 parser.Account
	Type                 AccountType
	OpenDate             *parser.Date
	CloseDate            *parser.Date
	ConstraintCurrencies []string
	BookingMethod        string
	Metadata             []*parser.Metadata
	Inventory            *Inventory // Inventory with lot tracking
}

// IsOpen returns true if the account is open at the given date
func (a *Account) IsOpen(date *parser.Date) bool {
	if a.OpenDate == nil {
		return false
	}

	// Account must be opened before or on the date
	if a.OpenDate.After(date.Time) {
		return false
	}

	// If there's a close date, check that the date is before closing
	if a.CloseDate != nil && !a.CloseDate.After(date.Time) {
		return false
	}

	return true
}

// IsClosed returns true if the account has been closed
func (a *Account) IsClosed() bool {
	return a.CloseDate != nil
}

// ParseAccountType parses the account type from the account name
func ParseAccountType(account parser.Account) AccountType {
	parts := strings.Split(string(account), ":")
	if len(parts) == 0 {
		return AccountTypeUnknown
	}

	switch parts[0] {
	case "Assets":
		return AccountTypeAssets
	case "Liabilities":
		return AccountTypeLiabilities
	case "Equity":
		return AccountTypeEquity
	case "Income":
		return AccountTypeIncome
	case "Expenses":
		return AccountTypeExpenses
	default:
		return AccountTypeUnknown
	}
}
