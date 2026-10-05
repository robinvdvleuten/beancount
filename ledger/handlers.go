package ledger

import (
	"context"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
)

// handler defines the interface for processing directives.
// Each directive type has a corresponding handler that validates and applies mutations.
//
// validate reports every error it finds and returns a delta describing the
// mutations to apply, or nil when nothing can be applied. The two are
// independent: like beancount, a directive can be reported and still applied
// (an unbalanced transaction, a posting to a closed account), so later
// directives see its effects instead of reporting follow-on errors.
//
// apply runs whenever validate returned a delta, errors or not.
type handler interface {
	// validate checks a directive without mutating state. It returns the
	// errors found and the delta to apply (nil when the directive must not
	// be applied). The delta type is specific to each handler (openDelta,
	// balanceDelta, etc.).
	validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any)

	// apply mutates ledger state with the delta validate returned.
	apply(ctx context.Context, l *Ledger, d ast.Directive, delta any)
}

// deltaOf returns d as a handler delta, keeping a nil pointer nil rather
// than a non-nil interface holding one.
func deltaOf[T any](d *T) any {
	if d == nil {
		return nil
	}
	return d
}

// openHandler processes Open directives.
type openHandler struct{}

func (h *openHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	open := d.(*ast.Open)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	errs, delta := v.validateOpen(ctx, open)
	return errs, deltaOf(delta)
}

func (h *openHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	open := d.(*ast.Open)
	l.applyOpen(open, delta.(*openDelta), l.config)
}

// closeHandler processes Close directives.
type closeHandler struct{}

func (h *closeHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	close := d.(*ast.Close)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	errs, delta := v.validateClose(ctx, close)
	return errs, deltaOf(delta)
}

func (h *closeHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	l.applyClose(delta.(*closeDelta))
}

// transactionHandler processes Transaction directives.
type transactionHandler struct{}

func (h *transactionHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	txn := d.(*ast.Transaction)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	errs, booked := v.validateTransaction(ctx, txn, l.booked[txn])
	return errs, deltaOf(booked)
}

func (h *transactionHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	txn := d.(*ast.Transaction)
	l.applyTransaction(txn, delta.(*bookedTransaction))
}

// balanceHandler processes Balance directives.
type balanceHandler struct{}

func (h *balanceHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	balance := d.(*ast.Balance)
	v := newValidator(l.accounts, l.opens, l.config)

	errs := v.validateBalance(balance)
	if len(errs) == 0 {
		if tolerance, err := l.tolerances.balance(balance); err != nil {
			errs = append(errs, err)
		} else {
			// The paddings are applied at their pads, so the subtree
			// holds them by now.
			held := l.subtree(balance.Account, balance.Amount.Currency)
			errs = append(slices.Clone(l.pads.costErrs[balance]), v.checkBalance(balance, held, tolerance)...)
		}
	}
	if l.duplicateBalances[balance] {
		errs = append(errs, newDuplicateBalanceError(balance))
	}
	return errs, nil
}

// apply does nothing: an assertion changes no state.
func (h *balanceHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {}

// padHandler processes Pad directives.
type padHandler struct{}

func (h *padHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	pad := d.(*ast.Pad)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	// Like any directive, a pad on accounts outside their interval is
	// reported and still pads.
	return v.validatePad(pad), pad
}

// apply applies the paddings planPads planned for the pad, which take
// effect at the pad.
func (h *padHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	for _, padding := range l.pads.planned[delta.(*ast.Pad)] {
		l.pads.padding = append(l.pads.padding, padding)
		l.applyPadding(ctx, padding)
	}
}

// noteHandler processes Note directives.
type noteHandler struct{}

func (h *noteHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	note := d.(*ast.Note)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	errs := v.validateNote(note)
	return errs, nil
}

func (h *noteHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Note has no state mutation - just validation
}

// documentHandler processes Document directives.
type documentHandler struct{}

func (h *documentHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	doc := d.(*ast.Document)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	errs := v.validateDocument(doc)
	return errs, nil
}

func (h *documentHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Document has no state mutation - just validation
}

// priceHandler processes Price directives.
type priceHandler struct{}

func (h *priceHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	price := d.(*ast.Price)
	if errs := validatePrice(price); len(errs) > 0 {
		return errs, nil
	}
	return nil, price
}

func (h *priceHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	price := delta.(*ast.Price)
	l.applyPrice(price)
}

// commodityHandler processes Commodity directives.
// Records the declared commodity.
type commodityHandler struct{}

func (h *commodityHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	commodity := d.(*ast.Commodity)
	cfg := l.config
	v := newValidator(l.accounts, l.opens, cfg)
	errs := v.validateCommodity(commodity)
	// Like beancount, a currency may be declared only once.
	if l.commodities[commodity.Currency] {
		errs = append(errs, newDuplicateCommodityError(commodity))
	}
	if len(errs) > 0 {
		return errs, nil
	}

	return nil, &commodityDelta{commodityID: commodity.Currency}
}

func (h *commodityHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	l.applyCommodity(delta.(*commodityDelta))
}

// eventHandler processes Event directives.
// Currently, events are not validated or stored - they're informational only.
type eventHandler struct{}

func (h *eventHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	// Event directives are currently informational and don't require validation
	return nil, nil
}

func (h *eventHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Event directives don't mutate state
}

// queryHandler processes Query directives.
// Query directives are informational and don't affect ledger state.
type queryHandler struct{}

func (h *queryHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	return nil, nil
}

func (h *queryHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Query directives don't mutate state
}

// customHandler processes Custom directives.
// Currently, custom directives are not validated or stored - they're informational only.
type customHandler struct{}

func (h *customHandler) validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
	// Custom directives are currently informational and don't require validation
	return nil, nil
}

func (h *customHandler) apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
	// Custom directives don't mutate state
}

// handlerRegistry maps directive kinds to their handlers.
var handlerRegistry = map[ast.DirectiveKind]handler{
	ast.KindOpen:        &openHandler{},
	ast.KindClose:       &closeHandler{},
	ast.KindTransaction: &transactionHandler{},
	ast.KindBalance:     &balanceHandler{},
	ast.KindPad:         &padHandler{},
	ast.KindNote:        &noteHandler{},
	ast.KindDocument:    &documentHandler{},
	ast.KindPrice:       &priceHandler{},
	ast.KindCommodity:   &commodityHandler{},
	ast.KindEvent:       &eventHandler{},
	ast.KindQuery:       &queryHandler{},
	ast.KindCustom:      &customHandler{},
}

// getHandler returns the handler for a given directive kind.
// Returns nil if no handler is registered for the directive kind.
func getHandler(kind ast.DirectiveKind) handler {
	return handlerRegistry[kind]
}
