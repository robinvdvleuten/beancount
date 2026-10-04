package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
)

// accountPosting records a single posting's impact on an account.
// Used to trace balance mutations and enable reconciliation.
//
// Store postings in chronological order (enforced by transaction processing order).
// Balances are reconstructed on demand from the posting sequence rather than stored
// (DRY principle: avoids snapshot duplication and synchronization issues).
type accountPosting struct {
	// The transaction this posting belongs to
	transaction *ast.Transaction

	// The posting itself
	posting *ast.Posting

	// The order the ledger applied the posting in, across all accounts
	seq int
}

// Account represents an account in the ledger
type Account struct {
	name      ast.Account
	Type      string // Account type root name (e.g., "Assets", "Vermoegen")
	OpenDate  *ast.Date
	CloseDate *ast.Date
	metadata  []*ast.Metadata
	inventory *inventory        // Lots held, with their cost basis
	postings  []*accountPosting // Transaction history in chronological order
}

// isOpen returns true if the account is open at the given date
func (a *Account) isOpen(date *ast.Date) bool {
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

// isClosed returns true if the account has been closed
func (a *Account) isClosed() bool {
	return a.CloseDate != nil
}
