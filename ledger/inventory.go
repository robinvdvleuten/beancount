package ledger

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// inventory tracks lots of commodities with cost basis
type inventory struct {
	// Map: commodity -> list of lots, in the order they were added, which
	// booking methods, reductions and rendering depend on
	lots map[string][]*lot
	// Map: commodity -> lot key -> the lot's position in lots, so adding to
	// a lot takes a lookup rather than a scan of every lot
	index map[string]map[lotKey]int
	// Map: commodity -> how many of its lots hold negative and
	// non-negative units, so deciding whether a posting reduces the
	// inventory takes a lookup rather than a scan of every lot
	signs map[string]signCounts
}

// signCounts counts a commodity's lots by the sign of their units:
// negative and non-negative, as beancount's same_sign tells them apart.
type signCounts [2]int

// signIndex is the signCounts slot of amount's sign: like beancount's
// same_sign, zero counts as non-negative.
func signIndex(amount decimal.Decimal) int {
	if amount.IsNegative() {
		return 0
	}
	return 1
}

type lotReduction struct {
	lot *lot
	// amount is the signed change applied to the lot: negative when a long
	// lot is sold, positive when a short lot is covered.
	amount decimal.Decimal
}

// BookedPosition is one position a booked posting holds: the signed units it
// adds to or takes from one lot of its account's inventory, and that lot's
// cost. A reduction books one per lot it is booked against; any other
// posting books one of its own. The Ledger publishes them per posting
// (Ledger.BookedPositions), and Apply replays them.
type BookedPosition struct {
	Units decimal.Decimal // Signed change to the lot
	Cost  *BookedCost     // nil without cost
	// Reduced reports that the account held the lot with the opposite
	// sign, like beancount's Booking.REDUCED.
	Reduced bool
}

// BookedCost is the cost a booked position's lot is held at.
type BookedCost struct {
	// Per unit, at the precision Booking has it: a stated per-unit cost
	// keeps its source precision (5.0 stays 5.0).
	Number   decimal.Decimal
	Currency string
	Date     *ast.Date // The cost's date, or the transaction's for an augmentation without one, unless its units were interpolated
	Label    string
}

// lotSpec returns the spec of the lot the position changes, nil without
// cost.
func (p BookedPosition) lotSpec() *lotSpec {
	if p.Cost == nil {
		return nil
	}
	number := p.Cost.Number // A copy, so the lot's spec does not alias the published cost
	return &lotSpec{cost: &number, costCurrency: p.Cost.Currency, date: p.Cost.Date, label: p.Cost.Label}
}

type reductionPlan struct {
	commodity  string
	reductions []lotReduction
}

// The errors of a reduction Booking cannot book, in beancount's words. Each
// quotes the reducing posting (reducingPosting) and renders the lots it
// names when it fails, so booking later directives into the same lots does
// not change its text.

// ambiguousBookingMatchError reports a STRICT reduction that matches several
// lots it does not reduce in full, listing them.
type ambiguousBookingMatchError struct {
	posting string
	matches []string
}

func (e *ambiguousBookingMatchError) Error() string {
	return fmt.Sprintf("Ambiguous matches for \"%s\": %s", e.posting, strings.Join(e.matches, ", "))
}

// notEnoughLotsError reports a reduction larger than the lots it matches,
// listing them.
type notEnoughLotsError struct {
	posting string
	matches []string
}

func (e *notEnoughLotsError) Error() string {
	return fmt.Sprintf("Not enough lots to reduce \"%s\": %s", e.posting, strings.Join(e.matches, ", "))
}

// noPositionMatchesError reports a reduction whose cost spec matches no lot,
// with the account's inventory. beancount quotes the posting's Python repr
// where we quote reducingPosting (KNOWN_GAPS.md).
type noPositionMatchesError struct {
	posting string
	balance string
}

func (e *noPositionMatchesError) Error() string {
	return fmt.Sprintf("No position matches \"%s\" against balance %s", e.posting, e.balance)
}

// errNotEnoughLots and errAmbiguousMatches mark a reduction the matched lots
// cannot cover, and a STRICT one they cannot settle; book reports them as a
// notEnoughLotsError and an ambiguousBookingMatchError.
var (
	errNotEnoughLots    = errors.New("not enough lots")
	errAmbiguousMatches = errors.New("ambiguous matches")
)

// errAverageUnsupported fails every reduction under the AVERAGE booking
// method, which beancount v2 accepts as an option but never implemented.
var errAverageUnsupported = errors.New("AVERAGE method is not supported")

// lotStrings renders lots as they are now.
func lotStrings(lots []*lot) []string {
	rendered := make([]string, len(lots))
	for i, lot := range lots {
		rendered[i] = lot.String()
	}
	return rendered
}

// reducingPosting renders a reducing posting as beancount's
// position.to_string quotes it: its units, then its cost spec as written
// (cost_to_str), a total cost {{T C}} as the {0 # T C} v2 parses it into
// and a compound's numbers without the one it leaves out.
func reducingPosting(posting *ast.Posting, units decimal.Decimal) string {
	cost := posting.Cost
	// The numbers the spec states; one left out is not shown. A malformed
	// number drops its transaction before Booking.
	number := func(amount *ast.Amount) string {
		n, _ := ParseAmount(amount)
		return pydecimal.String(n)
	}
	var numbers []string
	if amount := cost.Amount; amount != nil && amount.Value != "" {
		numbers = append(numbers, number(amount))
		if cost.IsTotal {
			numbers = []string{"0", "#", numbers[0]}
		}
	}
	if total := cost.Total; total != nil && total.Value != "" {
		numbers = append(numbers, "#", number(total))
	}
	var parts []string
	if len(numbers) > 0 {
		parts = append(parts, strings.Join(numbers, " ")+" "+cost.Amount.Currency)
	}
	if cost.Date != nil {
		parts = append(parts, cost.Date.String())
	}
	if cost.Label != "" {
		parts = append(parts, `"`+cost.Label+`"`)
	}
	if cost.IsMerge {
		parts = append(parts, "*")
	}
	return fmt.Sprintf("%s %s {%s}", pydecimal.String(units), posting.Amount.Currency, strings.Join(parts, ", "))
}

type bookingMethod string

const (
	bookingSTRICT         bookingMethod = "STRICT"
	bookingSTRICTWithSize bookingMethod = "STRICT_WITH_SIZE"
	bookingNONE           bookingMethod = "NONE"
	bookingFIFO           bookingMethod = "FIFO"
	bookingLIFO           bookingMethod = "LIFO"
	bookingHIFO           bookingMethod = "HIFO"
	bookingAVERAGE        bookingMethod = "AVERAGE"
)

func defaultBookingMethod(method bookingMethod) bookingMethod {
	if method == "" {
		return bookingSTRICT
	}
	return method
}

// newInventory creates a new inventory
func newInventory() *inventory {
	return &inventory{
		lots:  make(map[string][]*lot),
		index: make(map[string]map[lotKey]int),
		signs: make(map[string]signCounts),
	}
}

// countLot adds delta to the count of a commodity's lots holding units of
// amount's sign.
func (inv *inventory) countLot(commodity string, amount decimal.Decimal, delta int) {
	counts := inv.signs[commodity]
	counts[signIndex(amount)] += delta
	if counts == (signCounts{}) {
		delete(inv.signs, commodity)
		return
	}
	inv.signs[commodity] = counts
}

// holdsOtherSign reports whether the inventory holds a lot of commodity
// whose units' sign differs from units', as beancount's same_sign tells.
func (inv *inventory) holdsOtherSign(commodity string, units decimal.Decimal) bool {
	return inv.signs[commodity][1-signIndex(units)] > 0
}

// clone returns a copy of the inventory whose lots can change without
// changing the original's. The index holds positions, not lots, so the
// copy's is the original's copied.
func (inv *inventory) clone() *inventory {
	cloned := &inventory{
		lots:  make(map[string][]*lot, len(inv.lots)),
		index: make(map[string]map[lotKey]int, len(inv.index)),
		signs: maps.Clone(inv.signs),
	}
	for commodity, lots := range inv.lots {
		// One allocation per commodity rather than per lot: a scratch
		// inventory is cloned per transaction.
		backing := make([]lot, len(lots))
		copied := make([]*lot, len(lots))
		for i, l := range lots {
			backing[i] = *l
			copied[i] = &backing[i]
		}
		cloned.lots[commodity] = copied
		cloned.index[commodity] = maps.Clone(inv.index[commodity])
	}
	return cloned
}

// addLot adds an amount with a specific cost basis and reports whether it
// reduced the lot: the inventory held the lot with the opposite sign, like
// beancount's Inventory.add_amount returning Booking.REDUCED. Like
// add_amount, it never holds a lot of zero units: zero units added to a lot
// it does not hold leave it unchanged.
func (inv *inventory) addLot(commodity string, amount decimal.Decimal, spec *lotSpec) bool {
	// Find existing lot with matching spec
	key := spec.key()
	if i, ok := inv.index[commodity][key]; ok {
		lot := inv.lots[commodity][i]
		reduced := signIndex(lot.amount) != signIndex(amount)
		inv.countLot(commodity, lot.amount, -1)
		lot.amount = pydecimal.Add(lot.amount, amount)
		inv.countLot(commodity, lot.amount, 1)
		if lot.amount.IsZero() {
			inv.removeLot(commodity, lot)
		}
		return reduced
	}

	if amount.IsZero() {
		return false
	}
	newLot := &lot{commodity: commodity, amount: amount, spec: spec, key: key}
	positions, ok := inv.index[commodity]
	if !ok {
		positions = make(map[lotKey]int)
		inv.index[commodity] = positions
	}
	positions[newLot.key] = len(inv.lots[commodity])
	inv.lots[commodity] = append(inv.lots[commodity], newLot)
	inv.countLot(commodity, amount, 1)
	return false
}

// get returns the total amount of a commodity (summing all lots)
func (inv *inventory) get(commodity string) decimal.Decimal {
	total := decimal.Zero
	for _, lot := range inv.lots[commodity] {
		total = pydecimal.Add(total, lot.amount)
	}
	return total
}

// getLots returns all lots for a commodity
func (inv *inventory) getLots(commodity string) []*lot {
	return inv.lots[commodity]
}

// book decides whether a posting reduces the inventory under its account's
// booking method and, if so, books it; it books nothing for an augmentation.
// It is the one place a posting is found to reduce or to augment, like
// beancount's book_reductions. A posting reduces when it is held at cost
// with known units, its account books with a method other than NONE, and the
// inventory holds its commodity with the opposite sign; every other posting
// augments, which is how short positions at cost are opened.
//
// For a reduction, book books the lots it matches at once, so that a later
// posting of the transaction cannot book the same units again, and returns
// reduced with one position per lot, in booking order. For an augmentation
// it returns neither and leaves the inventory as it is: the augmentation's
// numbers may still be interpolated, from the reductions' weights among
// others, and like beancount the inventory takes it (augment) only once its
// transaction is booked, so the transaction's own postings never reduce it.
func (inv *inventory) book(posting *ast.Posting, method bookingMethod) (positions []BookedPosition, reduced bool, err error) {
	units, reduces := inv.reducedBy(posting, method)
	if !reduces {
		return nil, false, nil
	}
	method = defaultBookingMethod(method)

	// Like beancount's book_reductions, the posting is booked only against
	// the lots held at cost: units held without cost make it a reduction
	// but are never booked against.
	commodity := posting.Amount.Currency
	var lots []*lot
	for _, lot := range inv.lots[commodity] {
		if signIndex(lot.amount) != signIndex(units) && lot.spec != nil && lot.spec.cost != nil {
			lots = append(lots, lot)
		}
	}
	spec, err := postingLotSpec(posting)
	if err != nil {
		return nil, false, nil // A malformed cost drops its transaction before Booking
	}

	// Like beancount, the spec narrows the lots before the method picks
	// among them: date, label and cost match only where the spec has them.
	var matches []*lot
	for _, lot := range lots {
		if lotMatchesReductionSpec(lot, spec) {
			matches = append(matches, lot)
		}
	}
	if len(matches) == 0 {
		return nil, false, &noPositionMatchesError{posting: reducingPosting(posting, units), balance: inv.String()}
	}

	// The strategies work on magnitudes; the booked units take the
	// posting's sign.
	plan, err := planReduction(commodity, matches, units.Abs(), method)
	switch {
	case errors.Is(err, errNotEnoughLots):
		return nil, false, &notEnoughLotsError{posting: reducingPosting(posting, units), matches: lotStrings(matches)}
	case errors.Is(err, errAmbiguousMatches):
		return nil, false, &ambiguousBookingMatchError{posting: reducingPosting(posting, units), matches: lotStrings(matches)}
	case err != nil:
		return nil, false, err
	}

	booked := make([]BookedPosition, 0, len(plan.reductions))
	for i := range plan.reductions {
		reduction := &plan.reductions[i]
		if units.IsNegative() {
			reduction.amount = reduction.amount.Neg()
		}
		s := reduction.lot.spec
		booked = append(booked, BookedPosition{
			Units:   reduction.amount,
			Cost:    &BookedCost{Number: *s.cost, Currency: s.costCurrency, Date: s.date, Label: s.label},
			Reduced: true,
		})
	}
	inv.applyReduction(plan)
	return booked, true, nil
}

// reducedBy reports whether book would book a posting as a reduction, and
// its units: like beancount's is_reduced_by, a posting at cost with units
// reduces when the inventory holds its commodity with the opposite sign,
// unless its account books with NONE. It changes nothing, so it can be
// asked of an inventory shared with others.
func (inv *inventory) reducedBy(posting *ast.Posting, method bookingMethod) (decimal.Decimal, bool) {
	if posting.Cost == nil || posting.Amount == nil || posting.Amount.Value == "" {
		return decimal.Decimal{}, false
	}
	units, err := ParseAmount(posting.Amount)
	if err != nil {
		return decimal.Decimal{}, false // A malformed number drops its transaction before Booking
	}
	if defaultBookingMethod(method) == bookingNONE || units.IsZero() {
		return decimal.Decimal{}, false
	}
	return units, inv.holdsOtherSign(posting.Amount.Currency, units)
}

// augment adds a posting that book did not book as a reduction, once its
// transaction is booked and its numbers are complete, like beancount's
// add_position: at cost, to the lot its spec names, per unit (a total or
// compound cost spread over the units) and, like beancount, dated by date
// when the spec has none (FIFO/LIFO ordering and dated lot specs depend on
// it): the transaction's, or nil for units it interpolated; without cost,
// its units alone. It returns the position it booked, which is both the
// change and its record, or none for a posting that holds nothing: one
// without a complete amount, or at a cost whose number was not
// interpolated.
func (inv *inventory) augment(posting *ast.Posting, date *ast.Date) []BookedPosition {
	if posting.Amount == nil || isIncompleteAmount(posting.Amount) {
		return nil
	}
	units, err := ParseAmount(posting.Amount)
	if err != nil {
		return nil
	}
	position := BookedPosition{Units: units}
	if posting.Cost != nil {
		if !posting.Cost.HasNumber() {
			return nil
		}
		spec, err := postingLotSpec(posting)
		if err != nil {
			return nil
		}
		// Known gap (KNOWN_GAPS.md): parseLotSpec reads a merge cost {*}
		// as {} and drops the number inferred for it, so an augmentation
		// at {*} books its units without cost, where beancount v2 books
		// it at the inferred cost.
		if spec.cost != nil {
			if spec.date != nil {
				date = spec.date
			}
			position.Cost = &BookedCost{Number: *spec.cost, Currency: spec.costCurrency, Date: date, Label: spec.label}
		}
	}
	position.Reduced = inv.addLot(posting.Amount.Currency, units, position.lotSpec())
	return []BookedPosition{position}
}

// removeLot removes a lot from the inventory, keeping the others in order.
// It builds a new slice, since getLots hands the old one out.
func (inv *inventory) removeLot(commodity string, lotToRemove *lot) {
	inv.countLot(commodity, lotToRemove.amount, -1)
	lots := inv.lots[commodity]
	if len(lots) == 1 {
		delete(inv.lots, commodity)
		delete(inv.index, commodity)
		return
	}
	positions := inv.index[commodity]
	i := positions[lotToRemove.key]
	newLots := make([]*lot, 0, len(lots)-1)
	newLots = append(newLots, lots[:i]...)
	newLots = append(newLots, lots[i+1:]...)
	inv.lots[commodity] = newLots

	delete(positions, lotToRemove.key)
	for j := i; j < len(newLots); j++ {
		positions[newLots[j].key] = j
	}
}

// isEmpty returns true if the inventory has no lots
func (inv *inventory) isEmpty() bool {
	return len(inv.lots) == 0
}

// currencies returns all commodities in the inventory
func (inv *inventory) currencies() []string {
	currencies := make([]string, 0, len(inv.lots))
	for currency := range inv.lots {
		currencies = append(currencies, currency)
	}
	return currencies
}

// costCurrencies returns the distinct cost currencies of the lots held at
// cost, sorted.
func (inv *inventory) costCurrencies() []string {
	var currencies []string
	for _, lots := range inv.lots {
		for _, lot := range lots {
			if lot.spec != nil && lot.spec.costCurrency != "" && !slices.Contains(currencies, lot.spec.costCurrency) {
				currencies = append(currencies, lot.spec.costCurrency)
			}
		}
	}
	slices.Sort(currencies)
	return currencies
}

// currencyOrder ranks the major currencies first, like beancount's
// CURRENCY_ORDER.
var currencyOrder = map[string]int{
	"USD": 0, "EUR": 1, "JPY": 2, "CAD": 3, "GBP": 4, "AUD": 5, "NZD": 6, "CHF": 7,
}

// CurrencyRank is a currency's first sort key in beancount's
// Position.sortkey: the major currencies first, any other ranked after them
// by the length of its name.
func CurrencyRank(currency string) int {
	if r, ok := currencyOrder[currency]; ok {
		return r
	}
	return len(currencyOrder) + len(currency)
}

// String renders the inventory like beancount's str(Inventory): its
// positions in parentheses, sorted by Position.sortkey (currency rank, cost
// number, cost currency, then units). Ties fall back to commodity name and
// lot order, where beancount keeps insertion order.
func (inv *inventory) String() string {
	commodities := make([]string, 0, len(inv.lots))
	for commodity := range inv.lots {
		commodities = append(commodities, commodity)
	}
	slices.Sort(commodities)

	var lots []*lot
	for _, commodity := range commodities {
		lots = append(lots, inv.lots[commodity]...)
	}
	costOf := func(l *lot) (decimal.Decimal, string) {
		if l.spec == nil || l.spec.cost == nil {
			return decimal.Zero, ""
		}
		return *l.spec.cost, l.spec.costCurrency
	}
	slices.SortStableFunc(lots, func(a, b *lot) int {
		if c := CurrencyRank(a.commodity) - CurrencyRank(b.commodity); c != 0 {
			return c
		}
		an, ac := costOf(a)
		bn, bc := costOf(b)
		if c := an.Cmp(bn); c != 0 {
			return c
		}
		if c := strings.Compare(ac, bc); c != 0 {
			return c
		}
		return a.amount.Cmp(b.amount)
	})

	var buf strings.Builder
	buf.WriteByte('(')
	for i, lot := range lots {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(lot.String())
	}
	buf.WriteByte(')')
	return buf.String()
}

// countAtCost returns how many lots of commodity the inventory holds at
// cost.
func (inv *inventory) countAtCost(commodity string) int {
	n := 0
	for _, lot := range inv.lots[commodity] {
		if lot.spec != nil {
			n++
		}
	}
	return n
}

// planReduction plans reducing amount (a magnitude) from the lots the
// reduction's spec matches, at least one.
func planReduction(commodity string, matches []*lot, amount decimal.Decimal, bookingMethod bookingMethod) (*reductionPlan, error) {
	switch bookingMethod {
	case bookingAVERAGE:
		// Beancount v2 never implemented AVERAGE: every reduction under it fails.
		return nil, errAverageUnsupported
	case bookingSTRICT:
		return planStrictReduction(commodity, matches, amount)
	case bookingSTRICTWithSize:
		return planStrictReductionWithSize(commodity, matches, amount)
	default:
		return planReductionAcrossLots(commodity, amount, sortedLotsForBooking(matches, bookingMethod))
	}
}

// planStrictReduction books the one lot matched, or every lot matched when
// the reduction takes them all, like beancount's booking_method_STRICT;
// otherwise it cannot choose.
func planStrictReduction(commodity string, matches []*lot, amount decimal.Decimal) (*reductionPlan, error) {
	if len(matches) == 1 {
		if matches[0].amount.Abs().LessThan(amount) {
			return nil, errNotEnoughLots
		}
		return &reductionPlan{
			commodity:  commodity,
			reductions: []lotReduction{{lot: matches[0], amount: amount}},
		}, nil
	}

	total := decimal.Zero
	for _, lot := range matches {
		total = pydecimal.Add(total, lot.amount.Abs())
	}
	if !total.Equal(amount) {
		return nil, errAmbiguousMatches
	}
	reductions := make([]lotReduction, 0, len(matches))
	for _, lot := range matches {
		reductions = append(reductions, lotReduction{lot: lot, amount: lot.amount.Abs()})
	}
	return &reductionPlan{
		commodity:  commodity,
		reductions: reductions,
	}, nil
}

// planStrictReductionWithSize books as planStrictReduction, and when that
// cannot choose among several lots, the oldest lot of the reduction's size,
// like beancount's booking_method_STRICT_WITH_SIZE.
func planStrictReductionWithSize(commodity string, matches []*lot, amount decimal.Decimal) (*reductionPlan, error) {
	plan, err := planStrictReduction(commodity, matches, amount)
	if err == nil || len(matches) < 2 {
		return plan, err
	}
	var sized []*lot
	for _, lot := range matches {
		if lot.amount.Abs().Equal(amount) {
			sized = append(sized, lot)
		}
	}
	if len(sized) == 0 {
		return nil, err
	}
	// The first of the oldest, as beancount's stable sort on the cost date
	// leaves it.
	oldest := slices.MinFunc(sized, func(a, b *lot) int { return compareLotDates(a, b) })
	return &reductionPlan{
		commodity:  commodity,
		reductions: []lotReduction{{lot: oldest, amount: amount}},
	}, nil
}

// compareLotDates orders lots by their cost date. A lot at cost is dated
// once augmented unless its units were interpolated; one without a date
// sorts first (beancount fails to compare it).
func compareLotDates(a, b *lot) int {
	switch {
	case a.spec.date == nil && b.spec.date == nil:
		return 0
	case a.spec.date == nil:
		return -1
	case b.spec.date == nil:
		return 1
	}
	return a.spec.date.Compare(b.spec.date.Time)
}

// planReductionAcrossLots reduces the given amount across lots in order,
// consuming each lot before moving to the next.
func planReductionAcrossLots(commodity string, amount decimal.Decimal, sortedLots []*lot) (*reductionPlan, error) {
	remaining := amount
	reductions := make([]lotReduction, 0, len(sortedLots))
	for _, lot := range sortedLots {
		if remaining.IsZero() {
			break
		}

		reduction := decimal.Min(lot.amount.Abs(), remaining)
		reductions = append(reductions, lotReduction{lot: lot, amount: reduction})
		remaining = pydecimal.Sub(remaining, reduction)
	}

	if !remaining.IsZero() {
		return nil, errNotEnoughLots
	}

	return &reductionPlan{
		commodity:  commodity,
		reductions: reductions,
	}, nil
}

func (inv *inventory) applyReduction(plan *reductionPlan) {
	for _, reduction := range plan.reductions {
		inv.countLot(plan.commodity, reduction.lot.amount, -1)
		reduction.lot.amount = pydecimal.Add(reduction.lot.amount, reduction.amount)
		inv.countLot(plan.commodity, reduction.lot.amount, 1)
		if reduction.lot.amount.IsZero() {
			inv.removeLot(plan.commodity, reduction.lot)
		}
	}
}

func sortedLotsForBooking(lots []*lot, bookingMethod bookingMethod) []*lot {
	sortedLots := append([]*lot(nil), lots...)
	method := defaultBookingMethod(bookingMethod)
	lifo := method == bookingLIFO

	if method == bookingHIFO {
		// Highest cost basis first; lots without a cost sort last.
		slices.SortStableFunc(sortedLots, func(a, b *lot) int {
			aHasCost := a.spec != nil && a.spec.cost != nil
			bHasCost := b.spec != nil && b.spec.cost != nil
			if aHasCost != bHasCost {
				if aHasCost {
					return -1
				}
				return 1
			}
			if !aHasCost {
				return 0
			}
			return b.spec.cost.Cmp(*a.spec.cost)
		})
		return sortedLots
	}

	slices.SortStableFunc(sortedLots, func(a, b *lot) int {
		aHasDate := a.spec != nil && a.spec.date != nil
		bHasDate := b.spec != nil && b.spec.date != nil

		if aHasDate != bHasDate {
			if lifo {
				if aHasDate {
					return -1
				}
				return 1
			}
			if !aHasDate {
				return -1
			}
			return 1
		}
		if !aHasDate {
			return 0
		}
		if lifo {
			if a.spec.date.After(b.spec.date.Time) {
				return -1
			}
			if a.spec.date.Before(b.spec.date.Time) {
				return 1
			}
			return 0
		}
		if a.spec.date.Before(b.spec.date.Time) {
			return -1
		}
		if a.spec.date.After(b.spec.date.Time) {
			return 1
		}
		return 0
	})

	return sortedLots
}

func lotMatchesReductionSpec(lot *lot, spec *lotSpec) bool {
	if spec == nil {
		return lot.spec == nil || lot.spec.isEmpty()
	}
	if spec.isEmpty() {
		return true
	}
	if lot.spec == nil {
		return false
	}

	if spec.cost != nil && (lot.spec.cost == nil || !lot.spec.cost.Equal(*spec.cost)) {
		return false
	}
	if spec.costCurrency != "" && lot.spec.costCurrency != spec.costCurrency {
		return false
	}

	if spec.date != nil {
		if lot.spec.date == nil || !lot.spec.date.Equal(spec.date.Time) {
			return false
		}
	}

	if spec.label != "" && lot.spec.label != spec.label {
		return false
	}

	return true
}
