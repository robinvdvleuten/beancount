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

// GetPostingsInPeriod returns postings within [start, end] inclusive.
// When start == end, returns all postings up to and including that date (point-in-time).
// When start < end, returns postings within the period (for income statements).
func (a *Account) GetPostingsInPeriod(start, end ast.Date) []*AccountPosting {
	var result []*AccountPosting

	// Point-in-time: return all postings up to and including the date
	if start.Equal(end.Time) {
		for _, posting := range a.Postings {
			if !posting.Transaction.Date().After(end.Time) {
				result = append(result, posting)
			}
		}
		return result
	}

	// Period: return postings within [start, end]
	for _, posting := range a.Postings {
		txnDate := posting.Transaction.Date()
		if !txnDate.Before(start.Time) && !txnDate.After(end.Time) {
			result = append(result, posting)
		}
	}
	return result
}

// GetBalanceInPeriod returns the balance for this account within [start, end].
// When start == end, returns point-in-time balance (all postings up to that date).
// When start < end, returns net change within the period.
func (a *Account) GetBalanceInPeriod(start, end ast.Date) *Balance {
	balance := NewBalance()
	postings := a.GetPostingsInPeriod(start, end)

	for _, posting := range postings {
		if posting.Posting.Amount == nil {
			continue
		}

		amount, err := ParseAmount(posting.Posting.Amount)
		if err != nil {
			continue
		}
		currency := posting.Posting.Amount.Currency

		balance.Add(currency, amount)
	}

	return balance
}
