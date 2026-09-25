package ledger

import (
	"context"

	"github.com/robinvdvleuten/beancount/ast"
)

// Handler defines the interface for processing directives.
// Each directive type has a corresponding handler that validates and applies mutations.
//
// Validate reports every error it finds and returns a delta describing the
// mutations to apply, or nil when nothing can be applied. The two are
// independent: like beancount, a directive can be reported and still applied
// (an unbalanced transaction, a posting to a closed account), so later
// directives see its effects instead of reporting follow-on errors.
//
// Apply runs whenever Validate returned a delta, errors or not.
type Handler interface {
	// Validate checks a directive without mutating state. It returns the
	// errors found and the delta to apply (nil when the directive must not
	// be applied). The delta type is specific to each handler (OpenDelta,
	// TransactionDelta, etc.).
	Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any)

	// Apply mutates ledger state with the delta Validate returned.
	Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any)
}

// deltaOf returns d as a handler delta, keeping a nil pointer nil rather
// than a non-nil interface holding one.
func deltaOf[T any](d *T) any {
	if d == nil {
		return nil
	}
	return d
}

// OpenHandler processes Open directives.
type OpenHandler struct{}

func (h *OpenHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	open := d.(*ast.Open)
	cfg := l.config
	v := newValidator(l.accounts, cfg)
	errs, delta := v.validateOpen(ctx, open)
	return errs, deltaOf(delta)
}

func (h *OpenHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	open := d.(*ast.Open)
	openDelta := delta.(*OpenDelta)
	cfg := l.config
	l.applyOpen(open, openDelta, cfg)
}

// CloseHandler processes Close directives.
type CloseHandler struct{}

func (h *CloseHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	close := d.(*ast.Close)
	cfg := l.config
	v := newValidator(l.accounts, cfg)
	errs, delta := v.validateClose(ctx, close)
	return errs, deltaOf(delta)
}

func (h *CloseHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	closeDelta := delta.(*CloseDelta)
	l.applyClose(closeDelta)
}

// TransactionHandler processes Transaction directives.
type TransactionHandler struct{}

func (h *TransactionHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	txn := d.(*ast.Transaction)
	cfg := l.config
	v := newValidator(l.accounts, cfg)
	errs, booked := v.validateTransaction(ctx, txn, l.booked[txn])
	return errs, deltaOf(booked)
}

func (h *TransactionHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	txn := d.(*ast.Transaction)
	l.applyTransaction(txn, delta.(*bookedTransaction))
}

// BalanceHandler processes Balance directives.
type BalanceHandler struct{}

func (h *BalanceHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	balance := d.(*ast.Balance)
	cfg := l.config
	v := newValidator(l.accounts, cfg)

	// Basic validation
	errs := v.validateBalance(balance)
	if len(errs) > 0 {
		return errs, nil
	}

	// Get pad entry if exists
	accountName := string(balance.Account)
	padEntry := l.activePad(accountName, balance.Amount.Currency)

	// A failed assertion is reported and its padding still applies.
	delta, err := v.calculateBalanceDelta(balance, padEntry)
	switch {
	case delta == nil:
		return []error{err}, nil
	case err != nil:
		return []error{err}, delta
	}
	return nil, delta
}

func (h *BalanceHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	balanceDelta := delta.(*BalanceDelta)
	l.applyBalance(balanceDelta)

	// The first assertion per currency after a pad consumes it for that
	// currency; the pad is used once it inserts padding.
	if state, ok := l.pads[balanceDelta.AccountName]; ok && !state.consumed[balanceDelta.Currency] {
		state.consumed[balanceDelta.Currency] = true
		if balanceDelta.SyntheticTransaction != nil {
			state.used = true
		}
	}

	// Store synthetic transaction for AST insertion if it exists
	if balanceDelta.SyntheticTransaction != nil {
		l.syntheticTransactions = append(l.syntheticTransactions, balanceDelta.SyntheticTransaction)
	}
}

// PadHandler processes Pad directives.
type PadHandler struct{}

func (h *PadHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	pad := d.(*ast.Pad)
	cfg := l.config
	v := newValidator(l.accounts, cfg)
	errs := v.validatePad(pad)
	if len(errs) > 0 {
		return errs, nil
	}
	return nil, pad
}

func (h *PadHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	pad := delta.(*ast.Pad)
	accountName := string(pad.Account)
	// A new pad supersedes the previous one; beancount reports that one as
	// unused if it never inserted padding.
	if previous, ok := l.pads[accountName]; ok && !previous.used {
		l.unusedPads = append(l.unusedPads, previous.pad)
	}
	l.pads[accountName] = &padState{pad: pad, consumed: make(map[string]bool)}
}

// NoteHandler processes Note directives.
type NoteHandler struct{}

func (h *NoteHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	note := d.(*ast.Note)
	cfg := l.config
	v := newValidator(l.accounts, cfg)
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
	cfg := l.config
	v := newValidator(l.accounts, cfg)
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
	if errs := validatePrice(price); len(errs) > 0 {
		return errs, nil
	}
	return nil, price
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
	cfg := l.config
	v := newValidator(l.accounts, cfg)
	errs := v.validateCommodity(commodity)
	// Like beancount, a currency may be declared only once.
	if node := l.graph.GetNode(commodity.Currency); node != nil && node.Kind == NodeCommodity {
		errs = append(errs, NewDuplicateCommodityError(commodity))
	}
	if len(errs) > 0 {
		return errs, nil
	}

	// Create delta with commodity metadata for graph node creation
	delta := &CommodityDelta{
		CommodityID: commodity.Currency,
		Date:        commodity.Date(),
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

// QueryHandler processes Query directives.
// Query directives are informational and don't affect ledger state.
type QueryHandler struct{}

func (h *QueryHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	return nil, nil
}

func (h *QueryHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Query directives don't mutate state
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
	ast.KindQuery:       &QueryHandler{},
	ast.KindCustom:      &CustomHandler{},
}

// GetHandler returns the handler for a given directive kind.
// Returns nil if no handler is registered for the directive kind.
func GetHandler(kind ast.DirectiveKind) Handler {
	return handlerRegistry[kind]
}
