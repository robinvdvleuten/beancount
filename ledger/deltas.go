package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
)

// Deltas are what a directive's handler plans in Validate and hands to its
// Apply: the changes the directive makes to the ledger's state. They hold
// no errors, which Validate returns beside them. A transaction has none
// here: its handler applies what Booking recorded for it (bookedTransaction,
// booking.go).

// OpenDelta describes changes from opening an account.
// Stores account properties directly to avoid unnecessary allocations.
type OpenDelta struct {
	Account              ast.Account
	OpenDate             *ast.Date
	ConstraintCurrencies []string
	Metadata             []*ast.Metadata
}

// HasMetadata returns true if the delta has metadata
func (d *OpenDelta) HasMetadata() bool {
	return len(d.Metadata) > 0
}

// CloseDelta describes changes from closing an account
type CloseDelta struct {
	AccountName string
	CloseDate   *ast.Date
}

// BalanceDelta describes changes from a balance assertion.
// Does NOT include validation errors - those are returned separately.
type BalanceDelta struct {
	AccountName string
	Currency    string
	Padding     *ast.Transaction // Padding the assertion's pad inserts; nil when none
}

// CommodityDelta describes changes from a commodity declaration.
type CommodityDelta struct {
	CommodityID string // Currency/commodity code
}

// NoteDelta - no mutations needed (validation only)
// DocumentDelta - no mutations needed (validation only)
