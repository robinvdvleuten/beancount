package ledger

import (
	"sort"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// AccountBalance represents the balance of a single account.
// Useful for balance sheet and income statement reporting.
type AccountBalance struct {
	Account  string                     // Account name (e.g., "Assets:Cash")
	Balances map[string]decimal.Decimal // Currency → amount
}

// IsZero returns true if all balances are zero.
func (ab *AccountBalance) IsZero() bool {
	for _, amt := range ab.Balances {
		if !amt.IsZero() {
			return false
		}
	}
	return true
}

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
	Type                 ast.AccountType
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

// HasMetadata returns true if the account has metadata
func (a *Account) HasMetadata() bool {
	return len(a.Metadata) > 0
}

// GetParent returns the parent account.
// For example, parent of "Assets:US:Checking" is "Assets:US".
// Returns nil if the account has no parent.
// Note: Returns nil for implicit parents (nodes without Account metadata).
func (a *Account) GetParent(l *Ledger) *Account {
	parentNode := l.Graph().GetParent(string(a.Name))
	if parentNode == nil || parentNode.Meta == nil {
		return nil
	}
	if parent, ok := parentNode.Meta.(*Account); ok {
		return parent
	}
	return nil
}

// GetBalance returns the balance for this account (not including children).
// Returns a map of commodity to decimal amount.
func (a *Account) GetBalance() map[string]decimal.Decimal {
	result := make(map[string]decimal.Decimal)
	for _, currency := range a.Inventory.Currencies() {
		result[currency] = a.Inventory.Get(currency)
	}
	return result
}

// GetChildren returns direct child accounts.
// For example, if this account is "Assets", returns child accounts like "Assets:US" and "Assets:Investments".
func (a *Account) GetChildren(l *Ledger) []*Account {
	childNodes := l.Graph().GetChildren(string(a.Name))

	// Extract Account objects from nodes, sort by name
	var children []*Account
	for _, node := range childNodes {
		if node.Kind == "account" {
			if acc, ok := node.Meta.(*Account); ok {
				children = append(children, acc)
			}
		}
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].Name < children[j].Name
	})

	return children
}

// GetSubtreeBalance returns the aggregated balance for this account and all its descendants.
// Useful for balance sheet reporting where parent balances sum their children.
// Returns a map of commodity to total decimal amount.
func (a *Account) GetSubtreeBalance(l *Ledger) map[string]decimal.Decimal {
	result := make(map[string]decimal.Decimal)

	// Traverse all descendants via graph
	descendantNodes := l.Graph().GetDescendants(string(a.Name))
	allAccounts := map[string]*Account{string(a.Name): a}

	// Add all child accounts to lookup map
	for _, node := range descendantNodes {
		if node.Kind == "account" {
			if acc, ok := node.Meta.(*Account); ok {
				allAccounts[node.ID] = acc
			}
		}
	}

	// Sum balances from this account and all descendants
	for _, acc := range allAccounts {
		for currency, amount := range acc.GetBalance() {
			result[currency] = result[currency].Add(amount)
		}
	}

	return result
}

// GetPostingsBefore returns postings up to and including the given date (chronological).
// Used to compute account balance as of a specific point in time.
func (a *Account) GetPostingsBefore(date *ast.Date) []*AccountPosting {
	var result []*AccountPosting
	for _, posting := range a.Postings {
		if !posting.Transaction.Date.After(date.Time) {
			result = append(result, posting)
		}
	}
	return result
}

// GetPostingsInPeriod returns postings within [start, end] inclusive.
// Used to compute period changes for income statements.
func (a *Account) GetPostingsInPeriod(start, end *ast.Date) []*AccountPosting {
	var result []*AccountPosting
	for _, posting := range a.Postings {
		txnDate := posting.Transaction.Date
		if !txnDate.Before(start.Time) && !txnDate.After(end.Time) {
			result = append(result, posting)
		}
	}
	return result
}

// GetBalanceAsOf returns the account balance as of a specific date.
// Reconstructs balance from postings up to and including the given date.
// Returns AccountBalance with empty map if no postings exist before the date.
func (a *Account) GetBalanceAsOf(date *ast.Date) *AccountBalance {
	balance := make(map[string]decimal.Decimal)
	postings := a.GetPostingsBefore(date)

	for _, posting := range postings {
		if posting.Posting.Amount == nil {
			continue
		}

		amount, err := ParseAmount(posting.Posting.Amount)
		if err != nil {
			continue
		}
		currency := posting.Posting.Amount.Currency

		balance[currency] = balance[currency].Add(amount)
	}

	return &AccountBalance{
		Account:  string(a.Name),
		Balances: balance,
	}
}

// GetBalanceInPeriod returns the net balance change for this account within [start, end].
// Used to compute period changes for income statements.
// Returns AccountBalance with empty map if no postings exist in the period.
func (a *Account) GetBalanceInPeriod(start, end *ast.Date) *AccountBalance {
	balance := make(map[string]decimal.Decimal)
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

		balance[currency] = balance[currency].Add(amount)
	}

	return &AccountBalance{
		Account:  string(a.Name),
		Balances: balance,
	}
}
