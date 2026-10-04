package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
)

// Deltas are what a directive's handler plans in validate and hands to its
// apply: the changes the directive makes to the ledger's state. They hold
// no errors, which validate returns beside them. A transaction has none
// here: its handler applies what Booking recorded for it (bookedTransaction,
// booking.go).

// openDelta describes changes from opening an account.
// Stores account properties directly to avoid unnecessary allocations.
type openDelta struct {
	account  ast.Account
	openDate *ast.Date
	metadata []*ast.Metadata
}

// closeDelta describes changes from closing an account
type closeDelta struct {
	accountName string
	closeDate   *ast.Date
}

// balanceDelta describes changes from a balance assertion.
// Does NOT include validation errors - those are returned separately.
type balanceDelta struct {
	accountName string
	currency    string
	padding     *ast.Transaction // Padding the assertion's pad inserts; nil when none
}

// commodityDelta describes changes from a commodity declaration.
type commodityDelta struct {
	commodityID string // Currency/commodity code
}
