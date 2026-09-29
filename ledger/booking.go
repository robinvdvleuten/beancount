package ledger

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

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

// booker books transactions in date order, balancing each within its
// tolerances.
type booker struct {
	tolerances  tolerances
	inventories map[string]*Inventory
	methods     map[string]BookingMethod
	fallback    BookingMethod
}

// bookedTransaction is Booking's result for one transaction.
type bookedTransaction struct {
	residuals map[string]decimal.Decimal // Non-empty when it does not balance
	postings  []bookedPosting
}

// bookedPosting is a posting's change to its account's inventory: the
// positions it booked, which the Ledger publishes and Apply replays.
type bookedPosting struct {
	posting   *ast.Posting
	commodity string
	positions []BookedPosition
}

// newBooker takes each account's booking method from its open directive,
// wherever it is dated, and the configured method otherwise, as beancount
// does.
func newBooker(cfg *Config, tolerances tolerances, directives []ast.Directive) *booker {
	b := &booker{
		tolerances:  tolerances,
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

	l.booker = newBooker(l.config, l.tolerances, tree.Directives)
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
	// Beancount v2 reports these while parsing, so they are reported
	// whether or not the transaction books: a merge cost {*}, and a price
	// that is negative or a total on a posting without units, which it
	// fixes up before booking.
	for _, posting := range txn.Postings {
		if posting.Cost.IsMergeCost() {
			l.errors = append(l.errors, NewMergeCostError(txn, posting))
		}
		l.errors = append(l.errors, fixPrice(txn, posting)...)
	}
	booked, errs := l.booker.book(txn)
	l.errors = append(l.errors, errs...)
	if booked == nil {
		return false
	}
	l.booked[txn] = booked
	for _, bp := range booked.postings {
		l.bookedPositions[bp.posting] = bp.positions
	}
	return true
}

// fixPrice reports and fixes up a posting's price like beancount's parser: a
// negative price is made positive, and a total price (@@) on a posting
// without units is dropped.
func fixPrice(txn *ast.Transaction, posting *ast.Posting) []error {
	price := posting.Price
	if price == nil {
		return nil
	}
	var errs []error
	if price.Value != "" {
		if number, err := ParseAmount(price); err == nil && number.IsNegative() {
			errs = append(errs, NewNegativePriceError(txn, posting))
			posting.Price = &ast.Amount{Value: formatInferredNumber(number.Abs()), Currency: price.Currency}
		}
	}
	if posting.PriceTotal && (posting.Amount == nil || posting.Amount.Value == "") {
		errs = append(errs, NewTotalPriceWithoutUnitsError(txn, posting))
		posting.Price = nil
		posting.PriceTotal = false
	}
	return errs
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
	malformed := validateAmounts(txn)
	malformed = append(malformed, validateCosts(txn)...)
	malformed = append(malformed, validatePrices(txn)...)
	if len(malformed) > 0 {
		return nil, malformed
	}

	groups, errs := b.categorize(txn)
	if len(errs) > 0 {
		return nil, errs
	}
	written := resolveCostCurrencies(txn, groups)

	delta := &TransactionDelta{
		InferredAmounts: make(map[*ast.Posting]*ast.Amount),
		InferredCosts:   make(map[*ast.Posting]*ast.Amount),
		InferredPrices:  make(map[*ast.Posting]*ast.Amount),
		Postings:        make([]*ast.Posting, 0, len(txn.Postings)),
	}
	residuals := make(map[string]decimal.Decimal)
	autoBooked := false
	// Like beancount, a transaction changes the inventories only through
	// the groups it books: each group reduces lots in scratch copies, which
	// are staged once the group is booked.
	staged := make(map[string]*Inventory)
	reductions := make(map[*ast.Posting][]BookedPosition)
	for _, group := range groups {
		scratch := &scratchInventories{booker: b, staged: staged, own: make(map[string]*Inventory)}
		groupReductions, groupErrs := b.bookReductions(txn, group, scratch)
		var groupDelta *TransactionDelta
		var balance *balanceValidation
		var autoAmounts []*ast.Amount
		if len(groupErrs) == 0 {
			groupDelta, balance, autoAmounts, groupErrs = b.calculateBalance(txn, group, groupReductions)
			if len(groupErrs) == 0 && groupDelta == nil {
				// Missing numbers could not be interpolated.
				groupErrs = []error{newNotBalancedError(txn, balance.residuals)}
			}
		}
		if len(groupErrs) > 0 {
			errs = append(errs, groupErrs...)
			continue
		}
		maps.Copy(staged, scratch.own)
		maps.Copy(reductions, groupReductions)

		// The amount-less posting belongs to every group: it is booked once
		// per amount, as itself the first time and as a copy after that.
		for _, posting := range group.postings {
			if groupDelta.Dropped[posting] {
				delete(groupDelta.InferredAmounts, posting)
				continue
			}
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
			residuals[currency] = pydecimal.Add(residuals[currency], residual)
		}
	}
	if len(errs) > 0 {
		// Like beancount's, the errors carry the transaction as written:
		// commitDelta takes the Dropped groups' postings out of txn.
		if written == nil {
			written = unbooked(txn)
		}
		for _, err := range errs {
			if diagnostic, ok := err.(*Diagnostic); ok && diagnostic.directive == txn {
				diagnostic.directive = written
			}
		}
	}
	commitDelta(txn, delta)
	maps.Copy(b.inventories, staged)

	// The booked postings other than the reductions, their numbers now
	// complete, join their accounts' inventories, like beancount's
	// add_position once a transaction is booked.
	booked := &bookedTransaction{residuals: residuals}
	for _, posting := range txn.Postings {
		positions, reduced := reductions[posting]
		if !reduced {
			positions = b.inventory(posting.Account).augment(posting, txn.Date())
		}
		if len(positions) > 0 {
			booked.postings = append(booked.postings, bookedPosting{posting: posting, commodity: posting.Amount.Currency, positions: positions})
		}
	}
	return booked, errs
}

// validateAmounts checks all amounts can be parsed
func validateAmounts(txn *ast.Transaction) []error {
	var errs []error
	for _, posting := range txn.Postings {
		if posting.Amount == nil || isIncompleteAmount(posting.Amount) {
			continue // Will be inferred, checked later
		}
		if _, err := ParseAmount(posting.Amount); err != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
		}
	}
	return errs
}

// validateCosts checks all cost specifications are valid.
//
// It validates that:
//   - Cost amounts are parseable as decimal numbers
//   - Cost dates are valid (not zero dates)
//   - Cost labels are non-empty if present
//   - Empty costs {} are accepted (for automatic lot selection)
//   - ParseLotSpec can parse the cost specification
//
// Returns a slice of InvalidCostError for any invalid cost specifications.
// Includes posting index and cost spec string for clear error messages.
//
// Example:
//
//	// Valid cost: 10 HOOL {500.00 USD}
//	errs := validateCosts(txn)
//	if len(errs) > 0 {
//	    // Found invalid cost specifications
//	    for _, err := range errs {
//	        fmt.Printf("Cost error: %v\n", err)
//	        // Example: "2024-01-15: Invalid cost specification (Posting #1: Assets:Stock): {abc USD}: invalid decimal"
//	    }
//	}
func validateCosts(txn *ast.Transaction) []error {
	var errs []error
	for i, posting := range txn.Postings {
		if posting.Cost == nil {
			continue // No cost specification
		}

		// Empty cost {} is valid
		if posting.Cost.IsEmpty() {
			continue
		}

		// Validate total cost {{}} requirements
		if posting.Cost.IsTotal {
			if posting.Amount == nil {
				errs = append(errs, NewTotalCostError(txn, posting, "total cost requires a quantity"))
				continue
			}

			if posting.Cost.Amount == nil {
				errs = append(errs, NewTotalCostError(txn, posting, "total cost requires an amount"))
				continue
			}

			quantity, err := decimal.NewFromString(posting.Amount.Value)
			if err != nil {
				errs = append(errs, NewTotalCostError(txn, posting, fmt.Sprintf("invalid quantity %q: %v", posting.Amount.Value, err)))
				continue
			}

			if posting.Cost.HasNumber() {
				if _, err := decimal.NewFromString(posting.Cost.Amount.Value); err != nil {
					errs = append(errs, NewTotalCostError(txn, posting, fmt.Sprintf("invalid total cost %q: %v", posting.Cost.Amount.Value, err)))
					continue
				}
			}

			if quantity.IsZero() {
				errs = append(errs, NewTotalCostError(txn, posting, "cannot use total cost with zero quantity"))
				continue
			}
		}

		// Validate cost amount if present
		if posting.Cost.HasNumber() {
			if _, err := ParseAmount(posting.Cost.Amount); err != nil {
				costSpec := fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
			}
		}
		if posting.Cost.Total != nil {
			if posting.Cost.IsTotal {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{{... # ...}}", fmt.Errorf("compound cost cannot use total cost syntax")))
			} else if posting.Cost.Amount == nil {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{# ...}", fmt.Errorf("compound cost requires a per-unit amount")))
			} else if posting.Cost.Total.Currency != posting.Cost.Amount.Currency {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{... # ...}", fmt.Errorf("compound cost currencies must match")))
			} else if _, err := ParseAmount(posting.Cost.Total); err != nil {
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, "{... # ...}", err))
			}
		}

		// Validate ParseLotSpec can parse the cost
		if _, err := ParseLotSpec(posting.Cost); err != nil {
			costSpec := "{...}"
			if posting.Cost.Amount != nil {
				costSpec = fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
			}
			errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec, err))
		}

		// Validate cost date if present
		if posting.Cost.Date != nil {
			if posting.Cost.Date.IsZero() {
				costSpec := "{...}"
				if posting.Cost.Amount != nil {
					costSpec = fmt.Sprintf("{%s %s, ...}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				}
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec,
					fmt.Errorf("cost date cannot be zero")))
			}
		}

		// Validate cost label if present
		if posting.Cost.Label != "" {
			if strings.TrimSpace(posting.Cost.Label) == "" {
				costSpec := "{...}"
				if posting.Cost.Amount != nil {
					costSpec = fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				}
				errs = append(errs, NewInvalidCostError(txn, posting.Account, i, costSpec,
					fmt.Errorf("cost label cannot be empty")))
			}
		}
	}
	return errs
}

// validatePrices checks all price specifications are valid.
//
// It validates that:
//   - Price amounts are parseable as decimal numbers
//   - Per-unit prices (@) and total prices (@@) are correctly formatted
//
// Returns a slice of InvalidPriceError for any invalid price specifications.
// Includes posting index and price spec string for clear error messages.
//
// Example:
//
//	// Valid price: 100 EUR @ 1.20 USD
//	errs := validatePrices(txn)
//	if len(errs) > 0 {
//	    // Found invalid price specifications
//	    for _, err := range errs {
//	        fmt.Printf("Price error: %v\n", err)
//	        // Example: "2024-01-15: Invalid price specification (Posting #2: Expenses:Foreign): @ abc USD: invalid decimal"
//	    }
//	}
func validatePrices(txn *ast.Transaction) []error {
	var errs []error
	for i, posting := range txn.Postings {
		if posting.Price == nil || isIncompleteAmount(posting.Price) {
			continue // Absent or interpolated price specification
		}

		// Validate price amount
		if _, err := ParseAmount(posting.Price); err != nil {
			priceSpec := fmt.Sprintf("@ %s %s", posting.Price.Value, posting.Price.Currency)
			if posting.PriceTotal {
				priceSpec = fmt.Sprintf("@@ %s %s", posting.Price.Value, posting.Price.Currency)
			}
			errs = append(errs, NewInvalidPriceError(txn, posting.Account, i, priceSpec, err))
			continue
		}

		// Validate that price currency differs from posting currency
		// (It's valid but unusual to have the same currency)
		// For now, we'll allow it but could add a warning system later
	}
	return errs
}

// isIncompleteAmount reports whether an amount omits its number or currency
// (official grammar: incomplete_amount); interpolation completes it.
func isIncompleteAmount(a *ast.Amount) bool {
	return a != nil && (a.Value == "" || a.Currency == "")
}

// scratchInventories are the inventories one Currency group books against:
// copies, made on first use, of the inventories staged by the transaction's
// earlier groups or else of the booker's.
type scratchInventories struct {
	booker *booker
	staged map[string]*Inventory
	own    map[string]*Inventory
}

func (s *scratchInventories) get(account ast.Account) *Inventory {
	if inv, ok := s.own[string(account)]; ok {
		return inv
	}
	base, ok := s.staged[string(account)]
	if !ok {
		base = s.booker.inventory(account)
	}
	inv := base.clone()
	s.own[string(account)] = inv
	return inv
}

// bookReductions books a Currency group's postings into its scratch
// inventories in posting order, like beancount's book_reductions: book books
// each reduction at once, so a later posting cannot book the same units
// again, and leaves the augmentations to the end of the transaction. A
// booking failure drops the group.
func (b *booker) bookReductions(txn *ast.Transaction, group currencyGroup, scratch *scratchInventories) (map[*ast.Posting][]BookedPosition, []error) {
	reductions := make(map[*ast.Posting][]BookedPosition)
	for _, posting := range group.postings {
		// Only a posting at cost can reduce. Skipping the rest here spares
		// cloning their accounts' inventories; book still decides.
		if posting.Cost == nil {
			continue
		}
		positions, reduced, err := scratch.get(posting.Account).book(posting, b.method(posting.Account))
		if err != nil {
			return nil, []error{newBookingError(txn, posting.Account, err)}
		}
		if reduced {
			reductions[posting] = positions
		}
	}
	return reductions, nil
}

// newBookingError classifies a failed booking: an ambiguous match (or a
// reduction under AVERAGE, which beancount counts as one), or inventory that
// cannot cover the reduction.
func newBookingError(txn *ast.Transaction, account ast.Account, err error) error {
	var ambiguousErr *ambiguousBookingMatchError
	if errors.As(err, &ambiguousErr) || errors.Is(err, errAverageUnsupported) {
		return NewAmbiguousBookingError(txn, account, err)
	}
	return NewInsufficientInventoryError(txn, account, err)
}

// unbooked returns a copy of txn as written, before resolveCostCurrencies
// and commitDelta rewrite its postings and fill in their numbers, for the
// errors reported on it to carry, so an error shows the postings of the
// group it dropped.
func unbooked(txn *ast.Transaction) *ast.Transaction {
	copied := *txn
	copied.Postings = make([]*ast.Posting, len(txn.Postings))
	for i, posting := range txn.Postings {
		p := *posting
		if posting.Cost != nil {
			cost := *posting.Cost
			p.Cost = &cost
		}
		copied.Postings[i] = &p
	}
	return &copied
}

// resolveCostCurrencies gives a cost that states its number but not its
// currency the currency of its posting's Currency group, which categorize
// resolved it to, like beancount's replace_currencies, so Booking weighs
// and books it in that currency. The cost is replaced, not edited, and
// resolveCostCurrencies returns the transaction as written when it
// replaced one, nil otherwise.
func resolveCostCurrencies(txn *ast.Transaction, groups []currencyGroup) *ast.Transaction {
	var written *ast.Transaction
	for _, group := range groups {
		for _, posting := range group.postings {
			cost := posting.Cost
			if !cost.HasNumber() || cost.Amount.Currency != "" {
				continue
			}
			if written == nil {
				written = unbooked(txn)
			}
			amount := *cost.Amount
			amount.Currency = group.currency
			resolved := *cost
			resolved.Amount = &amount
			posting.Cost = &resolved
		}
	}
	return written
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
func (b *booker) calculateBalance(txn *ast.Transaction, group currencyGroup, reductions map[*ast.Posting][]BookedPosition) (*TransactionDelta, *balanceValidation, []*ast.Amount, []error) {
	var errs []error
	pc := classifyPostings(group.postings)

	// Calculate weights for postings with amounts
	var allWeights []weightSet
	// Positions that reductions with an amount-less cost spec booked, one
	// per lot. Every other amount-less cost spec is an augmentation's, whose
	// cost is inferred from the residual.
	reducedPositions := make(map[*ast.Posting][]BookedPosition)
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
			// Augmentations, NONE's included, are handled in cost inference
			// below.
			if positions, ok := reductions[posting]; ok {
				var weights weightSet
				for _, position := range positions {
					weights = append(weights, weight{
						Amount:   pydecimal.Mul(position.Units, position.Cost.Number),
						Currency: position.Cost.Currency,
					})
				}
				allWeights = append(allWeights, weights)
				reducedPositions[posting] = positions
			}
		} else {
			allWeights = append(allWeights, weights)
		}
	}

	if len(errs) > 0 {
		return nil, nil, nil, errs
	}
	if first := tooManyMissing(group, reducedPositions); first != nil {
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
		Dropped:         make(map[*ast.Posting]bool),
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
			balance[currency] = pydecimal.Add(balance[currency], number)
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
		if _, perr := decimal.NewFromString(posting.Price.Value); perr != nil {
			errs = append(errs, NewInvalidAmountError(txn, posting.Account, posting.Price.Value, perr))
			return nil, nil, nil, errs
		}
		perUnit, _, _ := PerUnitPrice(posting)
		currency := posting.Price.Currency
		if currency == "" {
			if len(balance) != 1 {
				return nil, unbalancedValidation(balance), nil, nil
			}
			for c := range balance {
				currency = c
			}
		}
		weight := pydecimal.Mul(units, perUnit)
		delta.InferredPrices[posting] = &ast.Amount{Value: posting.Price.Value, Currency: currency}
		balance[currency] = pydecimal.Add(balance[currency], weight)
	}

	// Beancount interpolates at most one missing number per transaction;
	// more unknowns (missing amounts, currency-only amounts or value-less
	// prices) are "too many missing numbers". A missing cost number next to
	// one of them is reported by tooManyMissing above.
	unknowns := len(pc.withoutAmounts) + len(currencyOnlyAmounts) + len(valuelessPrices)
	if unknowns > 1 {
		return nil, unbalancedValidation(balance), nil, nil
	}

	// Beancount allows at most one posting without an amount per
	// transaction. It absorbs the residual of every weight currency, so it
	// is booked once per currency with a non-zero residual, in the order the
	// currencies first appear.
	// Like beancount, tolerances come from the whole transaction.
	specTolerances := b.tolerances.spec(classifyPostings(txn.Postings).withAmounts)
	var autoPosting *ast.Posting
	var autoAmounts []*ast.Amount
	if len(pc.withoutAmounts) == 1 {
		autoPosting = pc.withoutAmounts[0]

		for _, currency := range residualCurrencies(allWeights, balance) {
			needed := specTolerances.round(currency, balance[currency].Neg())
			autoAmounts = append(autoAmounts, &ast.Amount{
				Value:    formatInferredNumber(needed),
				Currency: currency,
			})
			balance[currency] = pydecimal.Add(balance[currency], needed)
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
		if weight.IsZero() {
			delta.Dropped[posting] = true
		}
		needed := weight
		if weightCurrency != currency {
			needed = pydecimal.Quo(pydecimal.Sub(weight, total), perUnit)
		}
		needed = specTolerances.round(currency, needed)
		delta.InferredAmounts[posting] = &ast.Amount{
			Value:    formatInferredNumber(needed),
			Currency: currency,
		}
		if weightCurrency == currency {
			balance[currency] = pydecimal.Add(balance[currency], needed)
		} else {
			balance[weightCurrency] = pydecimal.Add(balance[weightCurrency], pydecimal.Add(pydecimal.Mul(needed, perUnit), total))
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
		balance[currency] = pydecimal.Add(balance[currency], weight)
	}

	// Infer costs for empty cost specs {}
	if len(pc.withEmptyCosts) > 0 {
		// Count empty costs that need inference from the residual: those of
		// augmentations, whose lots Booking has not resolved.
		inferableEmptyCosts := 0
		for _, posting := range pc.withEmptyCosts {
			if _, err := ParseAmount(posting.Amount); err != nil {
				continue
			}
			if _, reduced := reducedPositions[posting]; !reduced {
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

			// Like beancount's interpolate_group, zero units leave the cost
			// undefined, so the posting is left out of the booked transaction.
			if amount.IsZero() {
				delta.Dropped[posting] = true
				continue
			}
			// Infer cost for augmentations; a reduction's comes from its lots.
			if _, reduced := reducedPositions[posting]; reduced {
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
			// number_total; the lot's per-unit cost follows from it, and
			// the weight is the units at that (rounded) per-unit cost.
			number := residual.Neg()
			weight := pydecimal.Mul(amount, pydecimal.Quo(number, amount.Abs()))
			if !posting.Cost.IsTotal {
				number = pydecimal.Quo(number, amount)
				weight = pydecimal.Mul(amount, number)
			}
			delta.InferredCosts[posting] = &ast.Amount{
				Value:    formatInferredNumber(number),
				Currency: currency,
			}
			balance[currency] = pydecimal.Add(residual, weight)
		}
	}

	// Check if balanced after inference, within the tolerances of the
	// booked postings: interpolated amounts count, and costs count per
	// unit, with inferred cost numbers resolved.
	bookedTolerances := b.tolerances.booked(txn.Postings, delta, reducedPositions)
	residuals := make(map[string]decimal.Decimal)
	for currency, residual := range balance {
		if residual.Abs().GreaterThan(bookedTolerances.of(currency)) {
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
func tooManyMissing(group currencyGroup, reducedPositions map[*ast.Posting][]BookedPosition) *ast.Posting {
	var first *ast.Posting
	missing := 0
	for _, posting := range group.postings {
		n := 0
		if posting.Amount == nil || posting.Amount.Value == "" {
			n++
		}
		if _, reduced := reducedPositions[posting]; posting.Cost != nil && !posting.Cost.HasNumber() && !reduced {
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
