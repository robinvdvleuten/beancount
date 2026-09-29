package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
)

// AccountPosting records a single posting's impact on an account.
// Used to trace balance mutations and enable reconciliation.
//
// Store postings in chronological order (enforced by transaction processing order).
// Balances are reconstructed on demand from the posting sequence rather than stored
// (DRY principle: avoids snapshot duplication and synchronization issues).
type AccountPosting struct {
	// The transaction this posting belongs to
	Transaction *ast.Transaction

	// The posting itself
	Posting *ast.Posting
}

// Account represents an account in the ledger
type Account struct {
	Name                 ast.Account
	Type                 string // Account type root name (e.g., "Assets", "Vermoegen")
	OpenDate             *ast.Date
	CloseDate            *ast.Date
	ConstraintCurrencies []string
	Metadata             []*ast.Metadata
	Inventory            *Inventory        // Inventory with lot tracking
	Postings             []*AccountPosting // Transaction history in chronological order
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

// GetPostingsInPeriod returns postings within [start, end] inclusive, so a
// period with start == end holds that one day's postings.
func (a *Account) GetPostingsInPeriod(start, end ast.Date) []*AccountPosting {
	var result []*AccountPosting
	for _, posting := range a.Postings {
		txnDate := posting.Transaction.Date()
		if !txnDate.Before(start.Time) && !txnDate.After(end.Time) {
			result = append(result, posting)
		}
	}
	return result
}

// GetBalanceInPeriod returns this account's net change within [start, end]
// inclusive.
func (a *Account) GetBalanceInPeriod(start, end ast.Date) *Balance {
	return a.GetBalanceBetween(&start, &end)
}

// GetBalanceBetween returns the sum of this account's postings dated within
// [start, end] inclusive, where a nil bound leaves that side open: with only
// end, it is the balance at the end of that day.
func (a *Account) GetBalanceBetween(start, end *ast.Date) *Balance {
	balance := NewBalance()
	for _, posting := range a.Postings {
		date := posting.Transaction.Date()
		if start != nil && date.Before(start.Time) || end != nil && date.After(end.Time) {
			continue
		}
		if posting.Posting.Amount == nil {
			continue
		}

		amount, err := ParseAmount(posting.Posting.Amount)
		if err != nil {
			continue
		}
		balance.Add(posting.Posting.Amount.Currency, amount)
	}
	return balance
}
