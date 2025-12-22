package ledger

import (
	"sort"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

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

// GetParent returns the parent account path.
// For example, GetParent("Assets:US:Checking") returns "Assets:US".
// Returns empty string if the account has no parent (only one segment).
func (a *Account) GetParent() string {
	parts := strings.Split(string(a.Name), ":")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], ":")
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
	parentPath := string(a.Name)
	prefix := parentPath + ":"
	seen := make(map[string]bool)
	var childPaths []string

	for accountName := range l.accounts {
		if strings.HasPrefix(accountName, prefix) {
			remainder := strings.TrimPrefix(accountName, prefix)
			// Extract only the first segment (direct child)
			firstSegment := strings.Split(remainder, ":")[0]
			childPath := parentPath + ":" + firstSegment

			if !seen[childPath] {
				childPaths = append(childPaths, childPath)
				seen[childPath] = true
			}
		}
	}

	// Return Account structs, sorted by name
	sort.Strings(childPaths)
	var children []*Account
	for _, path := range childPaths {
		if child, ok := l.accounts[path]; ok {
			children = append(children, child)
		}
	}
	return children
}

// GetSubtreeBalance returns the aggregated balance for this account and all its descendants.
// Useful for balance sheet reporting where parent balances sum their children.
// Returns a map of commodity to total decimal amount.
func (a *Account) GetSubtreeBalance(l *Ledger) map[string]decimal.Decimal {
	result := make(map[string]decimal.Decimal)

	// Add this account's direct balance
	for currency, amount := range a.GetBalance() {
		result[currency] = amount
	}

	// Add all descendants recursively
	a.addDescendantBalances(l, result)
	return result
}

// addDescendantBalances recursively accumulates balances from all descendant accounts.
func (a *Account) addDescendantBalances(l *Ledger, result map[string]decimal.Decimal) {
	for _, child := range a.GetChildren(l) {
		// Add child's direct balance
		for currency, amount := range child.GetBalance() {
			result[currency] = result[currency].Add(amount)
		}
		// Recursively add child's descendants
		child.addDescendantBalances(l, result)
	}
}
