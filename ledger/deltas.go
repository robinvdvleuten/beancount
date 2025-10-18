package ledger

import (
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Delta Architecture
//
// This file defines lightweight "delta" structs that represent the mutations
// to be applied to the ledger state. Validators return these deltas instead of
// directly mutating state, keeping validation pure and making mutations explicit.
//
// Benefits:
//   - Pure validation: validators compute changes without side effects
//   - Inspectable: deltas are plain Go structs that can be logged/debugged
//   - Testable: can validate without applying, test deltas independently
//   - Replayable: can store deltas and replay them later
//   - Consistent: same pattern across all directive types

// InventoryOperation represents the type of inventory mutation
type InventoryOperation int

const (
	// OpAdd adds to inventory (augmentation)
	OpAdd InventoryOperation = iota
	// OpReduce removes from inventory (reduction)
	OpReduce
)

// String returns the string representation of the operation
func (op InventoryOperation) String() string {
	switch op {
	case OpAdd:
		return "Add"
	case OpReduce:
		return "Reduce"
	default:
		return "Unknown"
	}
}

// InventoryChange represents a single change to an account's inventory
type InventoryChange struct {
	Account   string             // Account name
	Currency  string             // Currency/commodity
	Amount    decimal.Decimal    // Amount to add/remove (ALWAYS POSITIVE - operation indicates direction)
	LotSpec   *lotSpec           // Lot specification (nil for simple amounts)
	Operation InventoryOperation // Add or Reduce (determines sign)
}

// String returns a human-readable representation of the inventory change
func (ic *InventoryChange) String() string {
	var sb strings.Builder
	sb.WriteString(ic.Operation.String())
	sb.WriteString(" ")
	sb.WriteString(ic.Amount.String())
	sb.WriteString(" ")
	sb.WriteString(ic.Currency)

	if ic.LotSpec != nil && !ic.LotSpec.IsEmpty() {
		sb.WriteString(" ")
		sb.WriteString(ic.LotSpec.String())
	}

	sb.WriteString(" ")
	if ic.Operation == OpAdd {
		sb.WriteString("to")
	} else {
		sb.WriteString("from")
	}
	sb.WriteString(" ")
	sb.WriteString(ic.Account)

	return sb.String()
}

// TransactionDelta represents the mutations to be applied from a transaction.
// It contains both inferred values (amounts/costs) and the explicit list of
// inventory changes to be made.
type TransactionDelta struct {
	Transaction      *ast.Transaction             // Original transaction
	InferredAmounts  map[*ast.Posting]*ast.Amount // Amounts inferred for postings without explicit amounts
	InferredCosts    map[*ast.Posting]*ast.Amount // Costs inferred from balance residuals
	InventoryChanges []InventoryChange            // Explicit list of inventory mutations
}

// String returns a human-readable representation of the transaction delta
func (td *TransactionDelta) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Transaction on %s:\n", td.Transaction.Date.Format("2006-01-02")))

	if len(td.InferredAmounts) > 0 {
		sb.WriteString("  Inferred amounts:\n")
		for posting, amount := range td.InferredAmounts {
			sb.WriteString(fmt.Sprintf("    %s: %s %s\n", posting.Account, amount.Value, amount.Currency))
		}
	}

	if len(td.InferredCosts) > 0 {
		sb.WriteString("  Inferred costs:\n")
		for posting, cost := range td.InferredCosts {
			sb.WriteString(fmt.Sprintf("    %s: {%s %s}\n", posting.Account, cost.Value, cost.Currency))
		}
	}

	if len(td.InventoryChanges) > 0 {
		sb.WriteString("  Inventory changes:\n")
		for _, change := range td.InventoryChanges {
			sb.WriteString(fmt.Sprintf("    %s\n", change.String()))
		}
	}

	return sb.String()
}

// BalanceDelta represents the mutations to be applied from a balance assertion.
// It includes padding information if a pad directive is active for the account.
type BalanceDelta struct {
	Balance         *ast.Balance    // Original balance directive
	ActualAmount    decimal.Decimal // Actual amount in the account (computed during validation)
	PadRequired     bool            // Whether padding is needed
	PadAmount       decimal.Decimal // Amount to pad (if padding required)
	PadCurrency     string          // Currency being padded
	PadAccount      string          // Pad account to use (from pad directive)
	BalanceMismatch bool            // Whether balance doesn't match after padding (validation result)
	ExpectedAmount  decimal.Decimal // Expected amount from directive
	FinalAmount     decimal.Decimal // Final amount after padding
}

// String returns a human-readable representation of the balance delta
func (bd *BalanceDelta) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Balance on %s for %s:\n", bd.Balance.Date.Format("2006-01-02"), bd.Balance.Account))
	sb.WriteString(fmt.Sprintf("  Expected: %s %s\n", bd.Balance.Amount.Value, bd.Balance.Amount.Currency))
	sb.WriteString(fmt.Sprintf("  Actual: %s %s\n", bd.ActualAmount.String(), bd.Balance.Amount.Currency))

	if bd.PadRequired {
		sb.WriteString(fmt.Sprintf("  Padding: %s %s from %s\n", bd.PadAmount.String(), bd.PadCurrency, bd.PadAccount))
	}

	return sb.String()
}

// PadDelta represents storing a pad directive for later use.
// Pad directives are stored and applied when the next balance assertion is encountered.
type PadDelta struct {
	Pad         *ast.Pad // Original pad directive
	AccountName string   // Account name (for map key)
}

// String returns a human-readable representation of the pad delta
func (pd *PadDelta) String() string {
	return fmt.Sprintf("Store pad for %s (will pad from %s)", pd.Pad.Account, pd.Pad.AccountPad)
}

// OpenDelta represents opening an account.
// The account is pre-created during validation for consistency.
type OpenDelta struct {
	Open    *ast.Open // Original open directive
	Account *Account  // Pre-created account to be added to ledger
}

// String returns a human-readable representation of the open delta
func (od *OpenDelta) String() string {
	return fmt.Sprintf("Open account %s on %s", od.Open.Account, od.Open.Date.Format("2006-01-02"))
}

// CloseDelta represents closing an account.
type CloseDelta struct {
	Close       *ast.Close // Original close directive
	AccountName string     // Account name (for map lookup)
}

// String returns a human-readable representation of the close delta
func (cd *CloseDelta) String() string {
	return fmt.Sprintf("Close account %s on %s", cd.Close.Account, cd.Close.Date.Format("2006-01-02"))
}

// NoteDelta represents a note directive.
// Notes have no state mutations - they're for documentation only.
type NoteDelta struct {
	Note *ast.Note // Original note directive
}

// String returns a human-readable representation of the note delta
func (nd *NoteDelta) String() string {
	return fmt.Sprintf("Note for %s: %s", nd.Note.Account, nd.Note.Description)
}

// DocumentDelta represents a document directive.
// Documents have no state mutations - they're for documentation only.
type DocumentDelta struct {
	Document *ast.Document // Original document directive
}

// String returns a human-readable representation of the document delta
func (dd *DocumentDelta) String() string {
	return fmt.Sprintf("Document for %s: %s", dd.Document.Account, dd.Document.PathToDocument)
}
