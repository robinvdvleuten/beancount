package ledger

import (
	"context"

	"github.com/robinvdvleuten/beancount/ast"
)

// Handler defines the interface for processing directives.
// Each directive type has a corresponding handler that validates and applies mutations.
//
// Validation returns a slice of errors and an optional delta object.
// The delta is directive-specific (e.g., OpenDelta, TransactionDelta) and contains
// mutations to apply if validation passes.
//
// Apply receives the directive, validator context, and delta (if any) and mutates
// the ledger state. Apply is only called if Validate returned no errors.
type Handler interface {
	// Validate checks if a directive is valid without mutating state.
	// Returns a slice of errors (empty if valid) and an optional delta describing mutations.
	// The delta type is specific to each handler (OpenDelta, TransactionDelta, etc.).
	Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any)

	// Apply mutates ledger state after successful validation.
	// It receives the directive, the validator state snapshot, and the delta from Validate.
	Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any)
}

// OpenHandler processes Open directives.
type OpenHandler struct{}

func (h *OpenHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	open := d.(*ast.Open)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	return v.validateOpen(ctx, open)
}

func (h *OpenHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	open := d.(*ast.Open)
	openDelta := delta.(*OpenDelta)
	l.applyOpen(open, openDelta)
}

// CloseHandler processes Close directives.
type CloseHandler struct{}

func (h *CloseHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	close := d.(*ast.Close)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	errs, delta := v.validateClose(ctx, close)
	return errs, delta
}

func (h *CloseHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	closeDelta := delta.(*CloseDelta)
	l.applyClose(closeDelta)
}

// TransactionHandler processes Transaction directives.
type TransactionHandler struct{}

func (h *TransactionHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	txn := d.(*ast.Transaction)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	return v.validateTransaction(ctx, txn)
}

func (h *TransactionHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	txn := d.(*ast.Transaction)
	txnDelta := delta.(*TransactionDelta)
	l.applyTransaction(txn, txnDelta)
}

// BalanceHandler processes Balance directives.
type BalanceHandler struct{}

func (h *BalanceHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	balance := d.(*ast.Balance)
	v := newValidator(l.Accounts(), l.toleranceConfig)

	// Basic validation
	errs := v.validateBalance(balance)
	if len(errs) > 0 {
		return errs, nil
	}

	// Get pad entry if exists
	accountName := string(balance.Account)
	padEntry := l.padEntries[accountName]

	// Mark pad as used if it existed (even if validation fails)
	if padEntry != nil {
		l.usedPads[accountName] = true
	}

	// Calculate delta (returns error separately, not in delta)
	delta, err := v.calculateBalanceDelta(balance, padEntry)
	if err != nil {
		return []error{err}, nil
	}

	return nil, delta
}

func (h *BalanceHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	balanceDelta := delta.(*BalanceDelta)
	l.applyBalance(balanceDelta)

	// Store synthetic transaction for AST insertion if it exists
	if balanceDelta.SyntheticTransaction != nil {
		l.syntheticTransactions = append(l.syntheticTransactions, balanceDelta.SyntheticTransaction)
	}
}

// PadHandler processes Pad directives.
type PadHandler struct{}

func (h *PadHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	pad := d.(*ast.Pad)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	errs := v.validatePad(pad)
	return errs, pad
}

func (h *PadHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	pad := delta.(*ast.Pad)
	accountName := string(pad.Account)
	l.padEntries[accountName] = pad
}

// NoteHandler processes Note directives.
type NoteHandler struct{}

func (h *NoteHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	note := d.(*ast.Note)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	errs := v.validateNote(note)
	return errs, nil
}

func (h *NoteHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Note has no state mutation - just validation
}

// DocumentHandler processes Document directives.
type DocumentHandler struct{}

func (h *DocumentHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	doc := d.(*ast.Document)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	errs := v.validateDocument(doc)
	return errs, nil
}

func (h *DocumentHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Document has no state mutation - just validation
}

// PriceHandler processes Price directives.
type PriceHandler struct{}

func (h *PriceHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	price := d.(*ast.Price)
	errs := validatePrice(price)
	return errs, price
}

func (h *PriceHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	price := delta.(*ast.Price)
	l.applyPrice(price)
}

// CommodityHandler processes Commodity directives.
// Creates explicit commodity nodes in the graph with metadata from the directive.
type CommodityHandler struct{}

func (h *CommodityHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	commodity := d.(*ast.Commodity)
	v := newValidator(l.Accounts(), l.toleranceConfig)
	errs := v.validateCommodity(commodity)
	if len(errs) > 0 {
		return errs, nil
	}

	// Create delta with commodity metadata for graph node creation
	delta := &CommodityDelta{
		CommodityID: commodity.Currency,
		Date:        commodity.Date,
		Metadata:    commodity.Metadata,
	}

	return nil, delta
}

func (h *CommodityHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	commodity := d.(*ast.Commodity)
	commodityDelta := delta.(*CommodityDelta)
	l.applyCommodity(commodity, commodityDelta)
}

// EventHandler processes Event directives.
// Currently, events are not validated or stored - they're informational only.
type EventHandler struct{}

func (h *EventHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	// Event directives are currently informational and don't require validation
	return nil, nil
}

func (h *EventHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Event directives don't mutate state
}

// CustomHandler processes Custom directives.
// Currently, custom directives are not validated or stored - they're informational only.
type CustomHandler struct{}

func (h *CustomHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	// Custom directives are currently informational and don't require validation
	return nil, nil
}

func (h *CustomHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Custom directives don't mutate state
}

// handlerRegistry maps directive kinds to their handlers.
var handlerRegistry = map[ast.DirectiveKind]Handler{
	ast.KindOpen:        &OpenHandler{},
	ast.KindClose:       &CloseHandler{},
	ast.KindTransaction: &TransactionHandler{},
	ast.KindBalance:     &BalanceHandler{},
	ast.KindPad:         &PadHandler{},
	ast.KindNote:        &NoteHandler{},
	ast.KindDocument:    &DocumentHandler{},
	ast.KindPrice:       &PriceHandler{},
	ast.KindCommodity:   &CommodityHandler{},
	ast.KindEvent:       &EventHandler{},
	ast.KindCustom:      &CustomHandler{},
}

// GetHandler returns the handler for a given directive kind.
// Returns nil if no handler is registered for the directive kind.
func GetHandler(kind ast.DirectiveKind) Handler {
	return handlerRegistry[kind]
}
