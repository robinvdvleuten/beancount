package ledger

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	inventories map[string]*inventory
	methods     map[string]bookingMethod
	fallback    bookingMethod
}

// bookedTransaction is Booking's result for one transaction.
type bookedTransaction struct {
	// residuals is non-empty when it does not balance, in the order
	// beancount's residual inventory holds them: the Currency groups'.
	residuals []residual
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
func newBooker(cfg *sharedconfig.Config, tolerances tolerances, directives []ast.Directive) *booker {
	b := &booker{
		tolerances:  tolerances,
		inventories: make(map[string]*inventory),
		methods:     make(map[string]bookingMethod),
		fallback:    bookingMethod(cfg.BookingMethod),
	}
	for _, directive := range directives {
		if open, ok := directive.(*ast.Open); ok && sharedconfig.IsBookingMethod(open.BookingMethod) {
			b.methods[string(open.Account)] = bookingMethod(open.BookingMethod)
		}
	}
	return b
}

func (b *booker) inventory(account ast.Account) *inventory {
	inv, ok := b.inventories[string(account)]
	if !ok {
		inv = newInventory()
		b.inventories[string(account)] = inv
	}
	return inv
}

func (b *booker) method(account ast.Account) bookingMethod {
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
	// Beancount reports these while parsing, so they are reported
	// whether or not the transaction books: a merge cost {*}, a component a
	// cost spec repeats (ignored, as the parser keeps the first), a compound
	// cost inside total braces, a price that is negative or a total on a
	// posting without units, the last three of which it fixes up before
	// booking, and a price in another currency than the cost.
	for _, posting := range txn.Postings {
		if posting.Cost.IsMergeCost() {
			l.errors = append(l.errors, newMergeCostError(txn, posting))
		}
		if posting.Cost != nil {
			for _, duplicate := range posting.Cost.Duplicates {
				l.errors = append(l.errors, newDuplicateCostComponentError(txn, posting, duplicate))
			}
		}
		l.errors = append(l.errors, fixTotalCost(txn, posting)...)
		l.errors = append(l.errors, fixPrice(txn, posting)...)
		if posting.Cost != nil && posting.Price != nil {
			if cost := costCurrency(posting.Cost); cost != "" && posting.Price.Currency != "" && cost != posting.Price.Currency {
				l.errors = append(l.errors, newCostPriceCurrencyError(txn, posting, cost))
			}
		}
	}
	booked, errs := l.booker.book(txn)
	l.errors = append(l.errors, errs...)
	if booked == nil {
		return false
	}
	l.publishBooking(txn, booked)
	return true
}

// publishBooking records a transaction's booking, which Apply replays, and
// publishes its postings' booked positions (BookedPositions).
func (l *Ledger) publishBooking(txn *ast.Transaction, booked *bookedTransaction) {
	l.booked[txn] = booked
	for _, bp := range booked.postings {
		l.bookedPositions[bp.posting] = bp.positions
	}
}

// fixPrice reports and fixes up a posting's price like beancount's parser: a
// negative price is made positive, and a total price (@@) on a posting
// without units is dropped. Such a posting keeps an amount without a number
// or a currency, beancount's Amount(MISSING, MISSING), so that it is not
// taken for an auto-posting: categorize then sorts it into one Currency
// group, or reports it.
func fixPrice(txn *ast.Transaction, posting *ast.Posting) []error {
	price := posting.Price
	if price == nil {
		return nil
	}
	var errs []error
	if price.Value != "" {
		if number, err := ParseAmount(price); err == nil && number.IsNegative() {
			errs = append(errs, newNegativePriceError(txn, posting))
			posting.Price = &ast.Amount{Value: formatInferredNumber(number.Abs()), Currency: price.Currency}
		}
	}
	if posting.PriceTotal && (posting.Amount == nil || posting.Amount.Value == "") {
		errs = append(errs, newTotalPriceWithoutUnitsError(txn, posting))
		posting.Price = nil
		posting.PriceTotal = false
		if posting.Amount == nil {
			posting.Amount = &ast.Amount{}
		}
	}
	return errs
}

// fixTotalCost reports a compound cost inside total braces and, like
// beancount's parser, ignores its per-unit number: {{5 # 3 USD}} and
// {{# 3 USD}} book as {0 # 3 USD}. Total braces without an amount are no
// total at all, as in beancount: {{}} and {{2020-01-01}} book as {} and
// {2020-01-01}.
func fixTotalCost(txn *ast.Transaction, posting *ast.Posting) []error {
	cost := posting.Cost
	if cost == nil || !cost.IsTotal {
		return nil
	}
	if cost.Amount == nil {
		fixed := *cost
		fixed.IsTotal = false
		posting.Cost = &fixed
		return nil
	}
	if cost.Total == nil {
		return nil
	}
	err := newTotalCompoundCostError(txn, posting)
	fixed := *cost
	fixed.IsTotal = false
	fixed.Amount = &ast.Amount{Value: "0", Currency: cost.Total.Currency}
	posting.Cost = &fixed
	return []error{err}
}

// book books txn one Currency group at a time. A nil result with errors is a
// Dropped transaction: a date out of range, a malformed number, cost or
// price, or postings that cannot be sorted into groups. Otherwise the errors
// are the Dropped groups (missing numbers that cannot be interpolated, a
// reduction that matches no lot or several), whose postings leave txn while
// the other groups are booked, and what interpolation reports on a group it
// still books.
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

	// Like beancount, which dates an augmentation before interpolation and
	// so leaves undated a lot whose units it interpolates.
	undated := make(map[*ast.Posting]bool)
	for _, posting := range txn.Postings {
		if posting.Cost != nil && (posting.Amount == nil || isIncompleteAmount(posting.Amount)) {
			undated[posting] = true
		}
	}

	groups, errs := b.categorize(txn)
	if len(errs) > 0 {
		return nil, errs
	}
	// Like beancount, tolerances come from the whole transaction as
	// written, before a cost takes its group's currency.
	specTolerances := b.tolerances.spec(txn.Postings)
	written := resolveCurrencies(txn, groups)

	// The booked postings, and what interpolation completed on them, which
	// commitBooking writes onto txn once every group is booked.
	postings := make([]*ast.Posting, 0, len(txn.Postings))
	var completed []interpolatedPosting
	var residuals []residual
	autoBooked := false
	// Like beancount, a transaction changes the inventories only through
	// the groups it books: each group reduces lots in scratch copies, which
	// are staged once the group is booked.
	staged := make(map[string]*inventory)
	reductions := make(map[*ast.Posting][]BookedPosition)
	for _, group := range groups {
		scratch := &scratchInventories{booker: b, staged: staged, own: make(map[string]*inventory)}
		groupReductions, groupErrs := b.bookReductions(txn, group, scratch)
		var interpolated *interpolatedGroup
		if len(groupErrs) == 0 {
			interpolated, groupErrs = interpolate(txn, group, groupReductions, specTolerances)
		}
		if len(groupErrs) > 0 {
			errs = append(errs, groupErrs...)
			continue
		}
		errs = append(errs, interpolated.errs...)
		maps.Copy(staged, scratch.own)
		maps.Copy(reductions, groupReductions)

		// The amount-less posting belongs to every group: it is booked once
		// per amount, as itself the first time and as a copy after that.
		for _, done := range interpolated.postings {
			posting := done.posting
			if done.leftOut {
				continue
			}
			if posting.Amount != nil {
				postings = append(postings, posting)
				if done.amount != nil || done.cost != nil || done.price != nil {
					completed = append(completed, done)
				}
				continue
			}
			for _, amount := range done.amounts {
				if !autoBooked {
					autoBooked = true
					postings = append(postings, posting)
					completed = append(completed, interpolatedPosting{posting: posting, amount: amount})
					continue
				}
				copied := *posting
				copied.Amount = amount
				copied.Inferred = true
				copied.Automatic = true
				postings = append(postings, &copied)
			}
		}
		for _, r := range interpolated.residuals {
			i := slices.IndexFunc(residuals, func(held residual) bool { return held.currency == r.currency })
			if i < 0 {
				residuals = append(residuals, r)
				continue
			}
			residuals[i].number = pydecimal.Add(residuals[i].number, r.number)
		}
	}
	if len(errs) > 0 {
		// Like beancount's, the errors carry the transaction as written:
		// commitBooking takes the Dropped groups' postings out of txn.
		if written == nil {
			written = unbooked(txn)
		}
		for _, err := range errs {
			if diagnostic, ok := err.(*Diagnostic); ok && diagnostic.directive == txn {
				diagnostic.directive = written
			}
		}
	}
	commitBooking(txn, postings, completed)
	maps.Copy(b.inventories, staged)

	// The booked postings other than the reductions, their numbers now
	// complete, join their accounts' inventories, like beancount's
	// add_position once a transaction is booked.
	booked := &bookedTransaction{residuals: residuals}
	for _, posting := range txn.Postings {
		positions, reduced := reductions[posting]
		if !reduced {
			date := txn.Date()
			if undated[posting] {
				date = nil
			}
			positions = b.inventory(posting.Account).augment(posting, date)
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
			errs = append(errs, newInvalidAmountError(txn, posting.Account, posting.Amount.Value, err))
		}
	}
	return errs
}

// validateCosts reports the cost specs Booking cannot read, each of which
// drops its transaction: a number that does not parse, total braces on
// units that are zero or do not parse, a compound whose currencies differ,
// a zero date or a blank label. An empty cost {} is valid.
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

		// Validate total cost {{}} requirements; total braces without an
		// amount ({{}}, {{*}}) are no total, and fixTotalCost makes them a
		// per-unit cost. Missing units are interpolation's to report.
		missingUnits := posting.Amount != nil && posting.Amount.Value == ""
		if posting.Cost.IsTotal && posting.Cost.Amount != nil && !missingUnits {
			if posting.Amount == nil {
				errs = append(errs, newTotalCostError(txn, posting, "total cost requires a quantity"))
				continue
			}

			quantity, err := decimal.NewFromString(posting.Amount.Value)
			if err != nil {
				errs = append(errs, newTotalCostError(txn, posting, fmt.Sprintf("invalid quantity %q: %v", posting.Amount.Value, err)))
				continue
			}

			if posting.Cost.Amount.Value != "" {
				if _, err := decimal.NewFromString(posting.Cost.Amount.Value); err != nil {
					errs = append(errs, newTotalCostError(txn, posting, fmt.Sprintf("invalid total cost %q: %v", posting.Cost.Amount.Value, err)))
					continue
				}
			}

			if quantity.IsZero() {
				errs = append(errs, newTotalCostError(txn, posting, "cannot use total cost with zero quantity"))
				continue
			}
		}

		// Validate the per-unit (or total cost) number if present
		if amount := posting.Cost.Amount; amount != nil && amount.Value != "" {
			if _, err := ParseAmount(amount); err != nil {
				costSpec := fmt.Sprintf("{%s %s}", amount.Value, amount.Currency)
				errs = append(errs, newInvalidCostError(txn, posting.Account, i, costSpec, err))
			}
		}
		// A compound's total may be left out; fixTotalCost has rewritten
		// one inside total braces.
		if total := posting.Cost.Total; total != nil {
			if posting.Cost.Amount == nil || total.Currency != posting.Cost.Amount.Currency {
				errs = append(errs, newInvalidCostError(txn, posting.Account, i, "{... # ...}", fmt.Errorf("compound cost currencies must match")))
			} else if total.Value != "" {
				if _, err := ParseAmount(total); err != nil {
					errs = append(errs, newInvalidCostError(txn, posting.Account, i, "{... # ...}", err))
				}
			}
		}

		// Validate parseLotSpec can parse the cost
		if _, err := parseLotSpec(posting.Cost); err != nil {
			costSpec := "{...}"
			if posting.Cost.Amount != nil {
				costSpec = fmt.Sprintf("{%s %s}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
			}
			errs = append(errs, newInvalidCostError(txn, posting.Account, i, costSpec, err))
		}

		// Validate cost date if present
		if posting.Cost.Date != nil {
			if posting.Cost.Date.IsZero() {
				costSpec := "{...}"
				if posting.Cost.Amount != nil {
					costSpec = fmt.Sprintf("{%s %s, ...}", posting.Cost.Amount.Value, posting.Cost.Amount.Currency)
				}
				errs = append(errs, newInvalidCostError(txn, posting.Account, i, costSpec,
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
				errs = append(errs, newInvalidCostError(txn, posting.Account, i, costSpec,
					fmt.Errorf("cost label cannot be empty")))
			}
		}
	}
	return errs
}

// validatePrices reports each complete price, per unit (@) or total (@@),
// whose number does not parse, which drops its transaction; interpolation
// completes the others.
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
			errs = append(errs, newInvalidPriceError(txn, posting.Account, i, priceSpec, err))
			continue
		}
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
	staged map[string]*inventory
	own    map[string]*inventory
}

// view returns the inventory the group books account against, read-only:
// its own copy once made, or else the one it would copy.
func (s *scratchInventories) view(account ast.Account) *inventory {
	if inv, ok := s.own[string(account)]; ok {
		return inv
	}
	if inv, ok := s.staged[string(account)]; ok {
		return inv
	}
	return s.booker.inventory(account)
}

// get returns the group's own copy of account's inventory, to book into.
func (s *scratchInventories) get(account ast.Account) *inventory {
	if inv, ok := s.own[string(account)]; ok {
		return inv
	}
	inv := s.view(account).clone()
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
		// Only a reduction changes the inventory here, so only one clones
		// its account's: an augmentation leaves it as it is, and copying
		// every lot for each would make a ledger that only adds lots
		// quadratic.
		if posting.Cost == nil {
			continue
		}
		method := b.method(posting.Account)
		if _, reduces := scratch.view(posting.Account).reducedBy(posting, method); !reduces {
			continue
		}
		positions, reduced, err := scratch.get(posting.Account).book(posting, method)
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
		return newAmbiguousBookingError(txn, account, err)
	}
	return newInsufficientInventoryError(txn, account, err)
}

// unbooked returns a copy of txn as written, before resolveCurrencies
// and commitBooking rewrite its postings and fill in their numbers, for the
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

// resolveCurrencies writes the currencies categorize resolved onto the
// postings that leave them out, like beancount's replace_currencies, so
// Booking weighs and books them in those currencies: the units of a posting
// with a cost or a price (given an amount without a number when it has
// none), a price, and a cost, with or without its number. A posting's
// amount, price or cost is replaced, not edited, and resolveCurrencies
// returns the transaction as written when it replaced one, nil otherwise.
func resolveCurrencies(txn *ast.Transaction, groups []currencyGroup) *ast.Transaction {
	var written *ast.Transaction
	replace := func() {
		if written == nil {
			written = unbooked(txn)
		}
	}
	for _, group := range groups {
		for i, posting := range group.postings {
			// The auto-posting, in every group, is expanded per group by
			// the booker.
			if isAutoPosting(posting) {
				continue
			}
			if posting.Amount == nil || posting.Amount.Currency == "" {
				replace()
				var amount ast.Amount
				if posting.Amount != nil {
					amount = *posting.Amount
				}
				amount.Currency = group.refs[i].units
				posting.Amount = &amount
			}
			if posting.Price != nil && posting.Price.Currency == "" {
				replace()
				price := *posting.Price
				price.Currency = group.refs[i].price
				posting.Price = &price
			}
			// A cost spec without a number takes its currency too, so a
			// reduction matches only the lots held in it: {} and {"label"}
			// read as {USD} and {USD, "label"}.
			if cost := posting.Cost; cost != nil && costCurrency(cost) == "" {
				replace()
				var amount ast.Amount
				if cost.Amount != nil {
					amount = *cost.Amount
				}
				amount.Currency = group.refs[i].cost
				resolved := *cost
				resolved.Amount = &amount
				posting.Cost = &resolved
			}
		}
	}
	return written
}

// commitBooking writes Booking's results onto the transaction: its booked
// postings, and the amounts, costs and prices interpolation completed on
// them. The processed AST carries booked postings, like beancount's booked
// entries; the source layout (BodyItems) is left as written.
func commitBooking(txn *ast.Transaction, postings []*ast.Posting, completed []interpolatedPosting) {
	txn.Postings = postings
	// Like beancount's __automatic__, a posting is Automatic when Booking
	// interpolated a missing number, not when it only filled in a currency.
	for _, done := range completed {
		posting := done.posting
		if amount := done.amount; amount != nil {
			if posting.Amount == nil || posting.Amount.Value == "" {
				posting.Automatic = true
			}
			posting.Amount = amount
			posting.Inferred = true
		}
		if cost := done.cost; cost != nil {
			if cost.Inferred {
				posting.Automatic = true
			}
			posting.Cost = cost
		}
		if price := done.price; price != nil {
			if posting.Price == nil || posting.Price.Value == "" {
				posting.Automatic = true
			}
			posting.Price = price
		}
	}
}
