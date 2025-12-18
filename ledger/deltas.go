package ledger

import (
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Delta Types
//
// Deltas are pure data structures describing WHAT TO CHANGE, not validation results.
// They represent the planned mutations resulting from processing directives.
//
// Key principles:
//   - Deltas are immutable after creation
//   - Deltas contain only mutation plans (no validation state)
//   - Validation errors are returned separately from deltas
//   - Deltas can be inspected, logged, or discarded without applying

// LotOpType describes the type of inventory operation
type LotOpType int

const (
	LotOpAdd LotOpType = iota
	LotOpReduceSpecific
	LotOpReduceFIFO
	LotOpReduceLIFO
)

// LotOperation describes a single inventory mutation to perform.
// NOTE: This is reserved for future enhancement - not populated in current implementation.
type LotOperation struct {
	Posting   *ast.Posting
	Account   string
	Currency  string
	Amount    decimal.Decimal
	Operation LotOpType
	LotSpec   *lotSpec
}

// TransactionDelta describes mutations from a transaction.
// This is a pure data structure describing WHAT TO CHANGE, not validation results.
//
// Note: Inferred amounts and costs are now stored directly on the AST nodes
// (Posting.Inferred and Cost.Inferred flags) rather than in separate maps.
type TransactionDelta struct {
	// NOTE: LotOps is reserved for future enhancement - not populated in this implementation
	// LotOps []LotOperation  // Pre-calculated, validated inventory operations
}

// balanceValidation holds validation results from balance calculation.
// Separated from TransactionDelta to keep deltas pure (only mutations, no validation state).
type balanceValidation struct {
	isBalanced bool
	residuals  map[string]decimal.Decimal
}

// HasMutations returns true if delta requires state changes.
// Since inferred amounts/costs are now stored directly on AST nodes,
// this always returns false for TransactionDelta.
func (d *TransactionDelta) HasMutations() bool {
	return false
}

// OpenDelta describes changes from opening an account.
// Stores account properties directly to avoid unnecessary allocations.
type OpenDelta struct {
	Account              ast.Account
	OpenDate             *ast.Date
	ConstraintCurrencies []string
	BookingMethod        string
	Metadata             []*ast.Metadata
}

// HasMutations returns true if delta requires state changes
func (d *OpenDelta) HasMutations() bool {
	return true // Opening always creates/modifies account
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

// HasMutations returns true if delta requires state changes
func (d *CloseDelta) HasMutations() bool {
	return true // Closing always modifies account
}

// BalanceDelta describes changes from a balance assertion.
// Does NOT include validation errors - those are returned separately.
type BalanceDelta struct {
	AccountName          string
	Currency             string
	ExpectedAmount       decimal.Decimal
	ActualAmount         decimal.Decimal            // Before padding
	PaddingAdjustments   map[string]decimal.Decimal // currency -> amount
	PadAccountName       string                     // Where padding comes from
	ShouldRemovePad      bool                       // True if pad was consumed
	SyntheticTransaction *ast.Transaction           // Padding transaction to insert into AST (nil if no padding)
}

// HasMutations returns true if delta requires state changes
func (d *BalanceDelta) HasMutations() bool {
	return len(d.PaddingAdjustments) > 0 || d.ShouldRemovePad || d.SyntheticTransaction != nil
}

// PadDelta describes storing a pad directive (no immediate mutation)
type PadDelta struct {
	AccountName string
	PadEntry    *ast.Pad // Store for next balance
}

// HasMutations returns true if delta requires state changes
func (d *PadDelta) HasMutations() bool {
	return true // Always stores the pad entry
}

// NoteDelta - no mutations needed (validation only)
// DocumentDelta - no mutations needed (validation only)
