package ledger

import (
	"sort"

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
	BookingMethod        string
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

// GetParent returns the parent account.
// For example, parent of "Assets:US:Checking" is "Assets:US".
// Returns nil if the account has no parent.
// Includes both explicitly opened accounts and implicit parent accounts.
func (a *Account) GetParent(l *Ledger) *Account {
	parentNode := l.Graph().GetParent(string(a.Name))
	if parentNode == nil {
		return nil
	}

	// If parent has Account metadata, use it
	if parentNode.Meta != nil {
		if parent, ok := parentNode.Meta.(*Account); ok {
			return parent
		}
	}

	// For implicit parent accounts (created by ensureAccountHierarchy),
	// reconstruct Account from node ID
	name := ast.Account(parentNode.ID)
	parent := &Account{
		Name: name,
		Type: name.Root(),
	}
	return parent
}

// GetChildren returns direct child accounts.
// For example, if this account is "Assets", returns child accounts like "Assets:US" and "Assets:Investments".
// Includes both explicitly opened accounts and implicit parent accounts (which have no Account metadata).
func (a *Account) GetChildren(l *Ledger) []*Account {
	childNodes := l.Graph().GetChildren(string(a.Name))

	// Extract Account objects from nodes, sort by name
	var children []*Account
	for _, node := range childNodes {
		if node.Kind == "account" {
			var acc *Account
			if a, ok := node.Meta.(*Account); ok {
				// Explicitly opened account
				acc = a
			} else {
				// Implicit parent account (created by ensureAccountHierarchy)
				// Reconstruct Account from node ID (the account name)
				name := ast.Account(node.ID)
				acc = &Account{
					Name: name,
					Type: name.Root(),
				}
			}
			children = append(children, acc)
		}
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].Name < children[j].Name
	})

	return children
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
