package ledger

import (
	"context"
	"fmt"
	"maps"

	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/telemetry"
	"github.com/shopspring/decimal"
)

// Booking completes each transaction's postings before Plugins and
// validation run, one Currency group at a time: it interpolates missing
// numbers, matches reductions to the lots they reduce, and splits an
// amount-less posting per group. Like beancount's booking_full, it keeps its
// own inventory per account, independent of open directives, and drops a
// group it cannot book while booking the transaction's other groups.

// booker books transactions in date order. Its embedded validator carries
// only the configuration, for the tolerance and well-formedness checks.
type booker struct {
	*validator
	inventories map[string]*Inventory
	methods     map[string]BookingMethod
	fallback    BookingMethod
}

// bookedTransaction is Booking's result for one transaction.
type bookedTransaction struct {
	residuals map[string]decimal.Decimal // Non-empty when it does not balance
	postings  []bookedPosting
}

// bookedPosting is a posting's change to its account's inventory.
type bookedPosting struct {
	posting   *ast.Posting
	commodity string
	changes   []lotChange
	lots      []BookedLot // The lots a reduction was booked against
}

// newBooker takes each account's booking method from its open directive,
// wherever it is dated, and the configured method otherwise, as beancount
// does.
func newBooker(cfg *Config, directives []ast.Directive) *booker {
	b := &booker{
		validator:   newValidator(nil, cfg),
		inventories: make(map[string]*Inventory),
		methods:     make(map[string]BookingMethod),
		fallback:    BookingMethod(cfg.BookingMethod),
	}
	for _, directive := range directives {
		if open, ok := directive.(*ast.Open); ok && sharedconfig.IsBookingMethod(open.BookingMethod) {
			b.methods[string(open.Account)] = BookingMethod(open.BookingMethod)
		}
	}
	return b
}

func (b *booker) inventory(account ast.Account) *Inventory {
	inv, ok := b.inventories[string(account)]
	if !ok {
		inv = NewInventory()
		b.inventories[string(account)] = inv
	}
	return inv
}

func (b *booker) method(account ast.Account) BookingMethod {
	if method, ok := b.methods[string(account)]; ok {
		return method
	}
	return defaultBookingMethod(b.fallback)
}

// book runs Booking over the sorted directives, leaving out Dropped
// transactions.
func (l *Ledger) book(ctx context.Context, tree *ast.AST) error {
	timer := telemetry.FromContext(ctx).Start("ledger.booking")
	defer timer.End()

	l.booker = newBooker(l.config, tree.Directives)
	kept := tree.Directives[:0]
	for _, directive := range tree.Directives {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if txn, ok := directive.(*ast.Transaction); ok && !l.bookTransaction(txn) {
			continue
		}
		kept = append(kept, directive)
	}
	clear(tree.Directives[len(kept):])
	tree.Directives = kept
	return nil
}

// bookTransaction books txn and reports whether it stays in the ledger.
func (l *Ledger) bookTransaction(txn *ast.Transaction) bool {
	// Beancount v2 reports a merge cost {*} while parsing, so it is reported
	// whether or not the transaction books.
	for _, posting := range txn.Postings {
		if posting.Cost.IsMergeCost() {
			l.errors = append(l.errors, NewMergeCostError(txn, posting))
		}
	}
	booked, errs := l.booker.book(txn)
	l.errors = append(l.errors, errs...)
	if booked == nil {
		return false
	}
	l.booked[txn] = booked
	for _, bp := range booked.postings {
		if len(bp.lots) > 0 {
			l.bookedLots[bp.posting] = bp.lots
		}
	}
	return true
}

// book books txn one Currency group at a time. A nil result with errors is a
// Dropped transaction: a date out of range, a malformed number, cost or
// price, or postings that cannot be sorted into groups. Otherwise the errors
// are the Dropped groups (missing numbers that cannot be interpolated, a
// reduction that matches no lot or several), whose postings leave txn while
// the other groups are booked.
func (b *booker) book(txn *ast.Transaction) (*bookedTransaction, []error) {
	if err := validateDateRange(txn.Date()); err != nil {
		return nil, []error{err}
	}
	malformed := b.validateAmounts(txn)
	malformed = append(malformed, b.validateCosts(txn)...)
	malformed = append(malformed, b.validatePrices(txn)...)
	if len(malformed) > 0 {
		return nil, malformed
	}

	groups, errs := b.categorize(txn)
	if len(errs) > 0 {
		return nil, errs
	}

	delta := &TransactionDelta{
		InferredAmounts: make(map[*ast.Posting]*ast.Amount),
		InferredCosts:   make(map[*ast.Posting]*ast.Amount),
		InferredPrices:  make(map[*ast.Posting]*ast.Amount),
		Postings:        make([]*ast.Posting, 0, len(txn.Postings)),
	}
	residuals := make(map[string]decimal.Decimal)
	autoBooked := false
	for _, group := range groups {
		groupDelta, balance, autoAmounts, groupErrs := b.calculateBalance(txn, group)
		if len(groupErrs) == 0 && groupDelta == nil {
			// Missing numbers could not be interpolated.
			groupErrs = []error{newNotBalancedError(txn, balance.residuals)}
		}
		if len(groupErrs) == 0 {
			groupErrs = b.checkLots(txn, group.postings, groupDelta)
		}
		if len(groupErrs) > 0 {
			errs = append(errs, groupErrs...)
			continue
		}

		// The amount-less posting belongs to every group: it is booked once
		// per amount, as itself the first time and as a copy after that.
		for _, posting := range group.postings {
			if posting.Amount != nil || posting.Price != nil {
				delta.Postings = append(delta.Postings, posting)
				continue
			}
			for _, amount := range autoAmounts {
				if !autoBooked {
					autoBooked = true
					delta.InferredAmounts[posting] = amount
					delta.Postings = append(delta.Postings, posting)
					continue
				}
				copied := *posting
				copied.Amount = amount
				copied.Inferred = true
				delta.Postings = append(delta.Postings, &copied)
			}
			delete(groupDelta.InferredAmounts, posting)
		}
		maps.Copy(delta.InferredAmounts, groupDelta.InferredAmounts)
		maps.Copy(delta.InferredCosts, groupDelta.InferredCosts)
		maps.Copy(delta.InferredPrices, groupDelta.InferredPrices)
		for currency, residual := range balance.residuals {
			residuals[currency] = residuals[currency].Add(residual)
		}
	}
	commitDelta(txn, delta)

	booked := &bookedTransaction{residuals: residuals}
	for _, posting := range txn.Postings {
		if posting.Amount == nil || isIncompleteAmount(posting.Amount) {
			continue
		}
		amount, err := ParseAmount(posting.Amount)
		if err != nil {
			continue
		}
		currency := posting.Amount.Currency
		inv := b.inventory(posting.Account)

		bp := bookedPosting{posting: posting, commodity: currency}
		if posting.Cost == nil {
			bp.changes = []lotChange{{amount: amount}}
			inv.Add(currency, amount)
		} else {
			// An augmentation whose cost could not be inferred holds no lot.
			if !posting.Cost.HasNumber() && !inv.isReducedBy(currency, amount) {
				continue
			}
			spec, err := ParseLotSpec(posting.Cost)
			if err != nil {
				continue
			}
			if err := normalizeLotSpecForPosting(spec, posting); err != nil {
				continue
			}
			// Beancount records an acquisition date on every new lot,
			// defaulting to the transaction date; LIFO/FIFO ordering and
			// dated lot specs depend on it.
			lots, changes, err := inv.book(currency, amount, spec, b.method(posting.Account), txn.Date())
			if err != nil {
				// Two reductions of the same lots in one transaction can
				// each pass checkLots and still fail together.
				return nil, append(errs, newBookingError(txn, posting.Account, err))
			}
			bp.changes = changes
			bp.lots = lots
		}
		booked.postings = append(booked.postings, bp)
	}
	return booked, errs
}

// commitDelta writes Booking's results onto the transaction. The processed
// AST carries booked postings, like beancount's booked entries; the source
// layout (BodyItems) is left as written.
func commitDelta(txn *ast.Transaction, delta *TransactionDelta) {
	txn.Postings = delta.Postings
	for posting, amount := range delta.InferredAmounts {
		posting.Amount = amount
		posting.Inferred = true
	}
	for posting, amount := range delta.InferredCosts {
		posting.Cost.Amount = amount
		posting.Cost.Inferred = true
	}
	for posting, price := range delta.InferredPrices {
		posting.Price = price
	}
}

// calculateBalance computes a Currency group's weights, infers its missing
// numbers, and checks whether it balances. It returns the delta (mutations),
// the balance state, and the amounts the group books its amount-less posting
// at, which the caller records; a nil delta without errors means the missing
// numbers could not be interpolated.
func (b *booker) calculateBalance(txn *ast.Transaction, group currencyGroup) (*TransactionDelta, *balanceValidation, []*ast.Amount, []error) {
	var errs []error
	pc := classifyPostings(group.postings)

	// Calculate weights for postings with amounts
	var allWeights []weightSet
	// Empty-cost postings that reduce their account's inventory, and the
	// subset whose lot cost could not be resolved via booking (e.g. NONE
	// booking); the latter's cost must be inferred from the residual.
	reducingEmptyCosts := make(map[*ast.Posting]bool)
	unresolvedEmptyCosts := make(map[*ast.Posting]bool)
	// Lots that reductions with an amount-less cost spec are booked against.
	bookedLots := make(map[*ast.Posting][]lotReduction)
	for _, posting := range pc.withAmounts {
		// A partial price annotation leaves the posting's weight unknown;
		// it is resolved from the residual during interpolation below.
		if posting.Price != nil && isIncompleteAmount(posting.Price) {
			continue
		}

		weights, err := calculateWeights(posting)
		if err != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
			continue
		}

		// Check if this is a cost spec without an amount (returns empty weights)
		if len(weights) == 0 && posting.Cost != nil && !posting.Cost.HasNumber() {
			// Reductions resolve their weight from the booked lots' cost basis,
			// matching beancount, which books lots before interpolation. The
			// spec's date/label (if any) narrows which lots are booked.
			// Augmentations are handled in cost inference below.
			amount, aerr := ParseAmount(posting.Amount)
			if aerr == nil && b.reducesInventory(posting.Account, posting.Amount.Currency, amount) {
				reducingEmptyCosts[posting] = true
				lots, ok, berr := b.bookedReductions(posting.Account, posting.Cost, posting.Amount.Currency, amount)
				if berr != nil {
					// Beancount stops at the booking error; the posting's
					// weight is unknown, so the balance is not checked.
					errs = append(errs, newBookingError(txn, posting.Account, berr))
					continue
				}
				if ok {
					var booked weightSet
					for _, lot := range lots {
						booked = append(booked, weight{
							Amount:   lot.amount.Mul(*lot.lot.Spec.Cost),
							Currency: lot.lot.Spec.CostCurrency,
						})
					}
					allWeights = append(allWeights, booked)
					bookedLots[posting] = lots
				} else {
					unresolvedEmptyCosts[posting] = true
				}
			}
		} else {
			allWeights = append(allWeights, weights)
		}
	}

	if len(errs) > 0 {
		return nil, nil, nil, errs
	}
	if first := tooManyMissing(group, reducingEmptyCosts, unresolvedEmptyCosts); first != nil {
		return nil, nil, nil, []error{NewCurrencyGroupError(txn, first,
			fmt.Sprintf("Too many missing numbers for currency group '%s'", group.currency))}
	}

	// Balance the weights
	balance := balanceWeights(allWeights)
	defer putBalanceMap(balance)

	delta := &TransactionDelta{
		InferredAmounts: make(map[*ast.Posting]*ast.Amount),
		InferredCosts:   make(map[*ast.Posting]*ast.Amount),
		InferredPrices:  make(map[*ast.Posting]*ast.Amount),
	}

	// A number-only amount or price is not a missing number in beancount:
	// only its currency is inferred, from the single currency in use. Their
	// weights are known, so resolve them before counting unknowns.
	var currencyOnlyAmounts []*ast.Posting
	for _, posting := range pc.incompleteAmounts {
		if posting.Amount.Value == "" {
			currencyOnlyAmounts = append(currencyOnlyAmounts, posting)
			continue
		}
		if len(balance) != 1 {
			return nil, unbalancedValidation(balance), nil, nil
		}
		number, nerr := decimal.NewFromString(posting.Amount.Value)
		if nerr != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, nerr))
			return nil, nil, nil, errs
		}
		for currency := range balance {
			delta.InferredAmounts[posting] = &ast.Amount{
				Value:    posting.Amount.Value,
				Currency: currency,
			}
			balance[currency] = balance[currency].Add(number)
		}
	}

	var valuelessPrices []*ast.Posting
	for _, posting := range pc.incompletePrices {
		if posting.Price.Value == "" {
			valuelessPrices = append(valuelessPrices, posting)
			continue
		}
		units, uerr := ParseAmount(posting.Amount)
		if uerr != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, uerr))
			return nil, nil, nil, errs
		}
		priceNumber, perr := decimal.NewFromString(posting.Price.Value)
		if perr != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Price.Value, perr))
			return nil, nil, nil, errs
		}
		currency := posting.Price.Currency
		if currency == "" {
			if len(balance) != 1 {
				return nil, unbalancedValidation(balance), nil, nil
			}
			for c := range balance {
				currency = c
			}
		}
		weight := units.Mul(priceNumber)
		if posting.PriceTotal {
			weight = perUnitWeight(units, priceNumber)
		}
		delta.InferredPrices[posting] = &ast.Amount{Value: posting.Price.Value, Currency: currency}
		balance[currency] = balance[currency].Add(weight)
	}

	// Beancount interpolates at most one missing number per transaction;
	// more unknowns (missing amounts, currency-only amounts, value-less
	// prices, or an unresolved cost) are "too many missing numbers".
	unknowns := len(pc.withoutAmounts) + len(currencyOnlyAmounts) + len(valuelessPrices)
	if unknowns > 0 && len(unresolvedEmptyCosts) > 0 {
		return nil, unbalancedValidation(balance), nil, nil
	}
	if unknowns > 1 {
		return nil, unbalancedValidation(balance), nil, nil
	}

	// Beancount allows at most one posting without an amount per
	// transaction. It absorbs the residual of every weight currency, so it
	// is booked once per currency with a non-zero residual, in the order the
	// currencies first appear.
	// Like beancount, tolerances come from the whole transaction.
	withAmounts := classifyPostings(txn.Postings).withAmounts
	stated := statedUnits(withAmounts)
	specCostTolerances := b.costTolerances(specToleranceShares(withAmounts))
	var autoPosting *ast.Posting
	var autoAmounts []*ast.Amount
	if len(pc.withoutAmounts) == 1 {
		autoPosting = pc.withoutAmounts[0]

		for _, currency := range residualCurrencies(allWeights, balance) {
			needed := roundInterpolated(balance[currency].Neg(), b.transactionTolerance(currency, stated[currency], specCostTolerances))
			autoAmounts = append(autoAmounts, &ast.Amount{
				Value:    formatInferredNumber(needed),
				Currency: currency,
			})
			balance[currency] = balance[currency].Add(needed)
		}

		if len(autoAmounts) > 0 {
			delta.InferredAmounts[autoPosting] = autoAmounts[0]
		}
	}

	// Complete a currency-only amount: the missing number is the residual
	// of the currency it balances in. Held at a per-unit cost or at a price,
	// the units are the residual of that currency divided by it (less a
	// compound cost's total part), like beancount's interpolate_group.
	if len(currencyOnlyAmounts) == 1 {
		posting := currencyOnlyAmounts[0]
		currency := posting.Amount.Currency
		weightCurrency, perUnit, total, ok := unitsWeightTerms(posting)
		if !ok {
			return nil, unbalancedValidation(balance), nil, nil
		}

		weight := balance[weightCurrency].Neg()
		needed := weight
		if weightCurrency != currency {
			needed = pydecimal.Quo(weight.Sub(total), perUnit)
		}
		needed = roundInterpolated(needed, b.transactionTolerance(currency, stated[currency], specCostTolerances))
		delta.InferredAmounts[posting] = &ast.Amount{
			Value:    formatInferredNumber(needed),
			Currency: currency,
		}
		if weightCurrency == currency {
			balance[currency] = balance[currency].Add(needed)
		} else {
			balance[weightCurrency] = balance[weightCurrency].Add(needed.Mul(perUnit).Add(total))
		}
	}

	// Complete a value-less price annotation (bare @ or currency-only): the
	// posting's weight is whatever zeroes the residual, and the price is
	// derived from it.
	if len(valuelessPrices) == 1 {
		posting := valuelessPrices[0]
		units, err := ParseAmount(posting.Amount)
		if err != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
			return nil, nil, nil, errs
		}

		currency := posting.Price.Currency
		if currency == "" {
			if len(balance) != 1 {
				return nil, unbalancedValidation(balance), nil, nil
			}
			for c := range balance {
				currency = c
			}
		}

		weight := balance[currency].Neg()
		priceNumber := weight.Abs()
		if !posting.PriceTotal && !units.IsZero() {
			priceNumber = pydecimal.Quo(weight, units).Abs()
		}
		delta.InferredPrices[posting] = &ast.Amount{Value: priceNumber.String(), Currency: currency}
		balance[currency] = balance[currency].Add(weight)
	}

	// Infer costs for empty cost specs {}
	if len(pc.withEmptyCosts) > 0 {
		// Count empty costs that need inference from the residual: augmentations
		// plus reductions whose lot cost could not be resolved via booking.
		inferableEmptyCosts := 0
		for _, posting := range pc.withEmptyCosts {
			if _, err := ParseAmount(posting.Amount); err != nil {
				continue
			}
			if !reducingEmptyCosts[posting] || unresolvedEmptyCosts[posting] {
				inferableEmptyCosts++
			}
		}

		// Beancount compliance: Cannot infer costs when multiple postings have empty cost specs
		// This is ambiguous - which posting gets which portion of the residual?
		if inferableEmptyCosts > 1 {
			return nil, unbalancedValidation(balance), nil, nil
		}

		for _, posting := range pc.withEmptyCosts {
			amount, err := ParseAmount(posting.Amount)
			if err != nil {
				continue
			}

			// Infer cost for augmentations, and for reductions whose cost was
			// not resolved from booked lots (e.g. NONE booking)
			if amount.IsZero() || (reducingEmptyCosts[posting] && !unresolvedEmptyCosts[posting]) {
				continue
			}

			if len(balance) > 1 {
				// Multiple currencies - ambiguous
				return nil, unbalancedValidation(balance), nil, nil
			}
			// The group's residual, zero when nothing else in the group
			// weighs, as for {USD} next to postings in other currencies.
			currency := group.currency
			residual := balance[currency]
			// A total cost {{USD}} completes its total, like beancount's
			// number_total; the lot's per-unit cost follows from it.
			number := residual.Neg()
			weight := perUnitWeight(amount, number)
			if !posting.Cost.IsTotal {
				number = pydecimal.Quo(number, amount)
				weight = amount.Mul(number)
			}
			delta.InferredCosts[posting] = &ast.Amount{
				Value:    number.String(),
				Currency: currency,
			}
			balance[currency] = residual.Add(weight)
		}
	}

	// Check if balanced (within tolerance) after inference
	amountsByCurrency := make(map[string][]decimal.Decimal)

	// Collect all amounts (explicit and inferred) for tolerance calculation
	for _, posting := range txn.Postings {
		if amountValue := delta.amountFor(posting); amountValue != nil {
			amount, err := ParseAmount(amountValue)
			if err != nil {
				continue
			}
			currency := amountValue.Currency
			amountsByCurrency[currency] = append(amountsByCurrency[currency], amount)
		}
	}

	// Check each currency balance with inferred tolerance. Costs count as
	// booked: per unit, with inferred cost numbers resolved.
	bookedCostTolerances := b.costTolerances(bookedToleranceShares(txn.Postings, delta, bookedLots))
	residuals := make(map[string]decimal.Decimal)
	for currency, residual := range balance {
		tolerance := b.transactionTolerance(currency, amountsByCurrency[currency], bookedCostTolerances)

		// Always check residuals against tolerance (even with inferred amounts)
		if residual.Abs().GreaterThan(tolerance) {
			residuals[currency] = residual
		}
	}

	validation := &balanceValidation{
		isBalanced: len(residuals) == 0,
		residuals:  residuals,
	}

	return delta, validation, autoAmounts, nil
}

// tooManyMissing returns the first posting of a Currency group with a missing
// number when the group has more than one, which beancount cannot
// interpolate: a missing units number (or no amount at all), a missing cost
// number other than on a reduction booked against lots, or a missing price
// number.
func tooManyMissing(group currencyGroup, reducingEmptyCosts, unresolvedEmptyCosts map[*ast.Posting]bool) *ast.Posting {
	var first *ast.Posting
	missing := 0
	for _, posting := range group.postings {
		n := 0
		if posting.Amount == nil || posting.Amount.Value == "" {
			n++
		}
		if posting.Cost != nil && !posting.Cost.HasNumber() && (!reducingEmptyCosts[posting] || unresolvedEmptyCosts[posting]) {
			n++
		}
		if posting.Price != nil && posting.Price.Value == "" {
			n++
		}
		if n > 0 && first == nil {
			first = posting
		}
		missing += n
	}
	if missing > 1 {
		return first
	}
	return nil
}

func unbalancedValidation(balance map[string]decimal.Decimal) *balanceValidation {
	residuals := make(map[string]decimal.Decimal, len(balance))
	for currency, residual := range balance {
		residuals[currency] = residual
	}
	return &balanceValidation{
		isBalanced: false,
		residuals:  residuals,
	}
}

// reducesInventory reports whether a posting of amount commodity reduces the
// account's inventory rather than augmenting it (see Inventory.isReducedBy).
func (b *booker) reducesInventory(account ast.Account, commodity string, amount decimal.Decimal) bool {
	return b.inventory(account).isReducedBy(commodity, amount)
}

// bookedReductions resolves the lots an amount-less cost spec reduction
// (empty {} or date/label-only) is booked against, selected by the account's
// booking method; their costs give the posting's balancing weights, matching
// beancount, which books lots before interpolation.
//
// The second return value reports whether the posting's cost is considered
// resolved. It is false only when the cost must instead be inferred from the
// transaction residual (NONE booking, or booked lots without a cost basis).
// A booking failure (ambiguous match, not enough lots) is returned as the
// error; the caller reports it instead of checking the balance, as beancount
// stops at booking errors.
func (b *booker) bookedReductions(account ast.Account, cost *ast.Cost, commodity string, amount decimal.Decimal) ([]lotReduction, bool, error) {
	bookingMethod := b.method(account)
	if bookingMethod == BookingNONE {
		return nil, false, nil
	}

	spec, err := ParseLotSpec(cost)
	if err != nil {
		return nil, true, nil // Invalid cost spec; reported by validateCosts
	}

	plan, err := b.inventory(account).planBooking(commodity, amount, spec, bookingMethod, nil)
	if err != nil {
		return nil, true, err
	}
	if plan == nil || len(plan.reductions) == 0 {
		return nil, false, nil
	}

	for _, reduction := range plan.reductions {
		if spec := reduction.lot.Spec; spec == nil || spec.Cost == nil {
			return nil, false, nil // Lot held without cost basis; infer from residual
		}
	}

	return plan.reductions, true, nil
}

// checkLots reports a group's cost postings, completed by delta, whose
// reduction the account's lots cannot cover: no matching lot, several, or
// too few units.
func (b *booker) checkLots(txn *ast.Transaction, postings []*ast.Posting, delta *TransactionDelta) []error {
	var errs []error
	for _, posting := range postings {
		units := delta.amountFor(posting)
		if units == nil || posting.Cost == nil || isIncompleteAmount(units) {
			continue
		}
		amount, err := ParseAmount(units)
		if err != nil {
			continue
		}
		lotSpec, err := ParseLotSpec(delta.costFor(posting))
		if err != nil {
			continue
		}
		if err := b.inventory(posting.Account).CanBook(units.Currency, amount, lotSpec, b.method(posting.Account)); err != nil {
			errs = append(errs, newBookingError(txn, posting.Account, err))
		}
	}
	return errs
}
