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
type TransactionDelta struct {
	InferredAmounts map[*ast.Posting]*ast.Amount
	InferredCosts   map[*ast.Posting]*ast.Amount
	InferredPrices  map[*ast.Posting]*ast.Amount
	// Postings, when set, replaces the transaction's postings with the booked
	// ones. Beancount books an amount-less posting once per currency with a
	// non-zero residual, as a copy of the posting per extra currency, and
	// drops it when every residual is zero.
	Postings []*ast.Posting
	// NOTE: LotOps is reserved for future enhancement - not populated in this implementation
	// LotOps []LotOperation  // Pre-calculated, validated inventory operations
}

// balanceValidation holds validation results from balance calculation.
// Separated from TransactionDelta to keep deltas pure (only mutations, no validation state).
type balanceValidation struct {
	isBalanced bool
	residuals  map[string]decimal.Decimal
}

func (d *TransactionDelta) amountFor(posting *ast.Posting) *ast.Amount {
	if amount := d.InferredAmounts[posting]; amount != nil {
		return amount
	}
	return posting.Amount
}

func (d *TransactionDelta) costFor(posting *ast.Posting) *ast.Cost {
	if amount := d.InferredCosts[posting]; amount != nil {
		cost := *posting.Cost
		cost.Amount = amount
		cost.Inferred = true
		return &cost
	}
	return posting.Cost
}

// OpenDelta describes changes from opening an account.
// Stores account properties directly to avoid unnecessary allocations.
type OpenDelta struct {
	Account              ast.Account
	OpenDate             *ast.Date
	ConstraintCurrencies []string
	BookingMethod        BookingMethod
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
// Stores commodity metadata for graph node creation.
type CommodityDelta struct {
	CommodityID string          // Currency/commodity code
	Date        *ast.Date       // Effective date
	Metadata    []*ast.Metadata // Commodity metadata
}

// NoteDelta - no mutations needed (validation only)
// DocumentDelta - no mutations needed (validation only)
