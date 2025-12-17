package ledger

import "github.com/robinvdvleuten/beancount/ast"

// Account represents an account in the ledger
type Account struct {
	Name                 ast.Account
	Type                 ast.AccountType
	OpenDate             *ast.Date
	CloseDate            *ast.Date
	ConstraintCurrencies []string
	BookingMethod        string
	Metadata             []*ast.Metadata
	Inventory            *Inventory // Inventory with lot tracking
}

// IsOpen returns true if the account is open at the given date
func (a *Account) IsOpen(date *ast.Date) bool {
	if a.OpenDate == nil {
		return false
	}

	// Account must be opened before or on the date
	if a.OpenDate.After(date.Time) {
		return false
	}

	// If there's a close date, check that the date is not after closing
	// Transactions are allowed ON the close date, but not AFTER
	if a.CloseDate != nil && date.After(a.CloseDate.Time) {
		return false
	}

	return true
}

// IsClosed returns true if the account has been closed
func (a *Account) IsClosed() bool {
	return a.CloseDate != nil
}

// HasMetadata returns true if the account has metadata
func (a *Account) HasMetadata() bool {
	return len(a.Metadata) > 0
}
