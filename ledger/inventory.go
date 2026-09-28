package ledger

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// Inventory tracks lots of commodities with cost basis
type Inventory struct {
	// Map: commodity -> list of lots
	lots map[string][]*lot
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
	Date     *ast.Date // The cost's date, or the transaction's for an augmentation without one
	Label    string
}

// lotSpec returns the spec of the lot the position changes, nil without
// cost.
func (p BookedPosition) lotSpec() *lotSpec {
	if p.Cost == nil {
		return nil
	}
	number := p.Cost.Number // A copy, so the lot's spec does not alias the published cost
	return &lotSpec{Cost: &number, CostCurrency: p.Cost.Currency, Date: p.Cost.Date, Label: p.Cost.Label}
}

type reductionPlan struct {
	commodity  string
	reductions []lotReduction
}

// ambiguousBookingMatchError reports a reduction that matches several lots.
// The matches are rendered when the error is created, so booking later
// directives into the same lots does not change its text.
type ambiguousBookingMatchError struct {
	commodity string
	amount    decimal.Decimal
	spec      *lotSpec
	matches   []string
}

func (e *ambiguousBookingMatchError) Error() string {
	return fmt.Sprintf("ambiguous matches for \"-%s %s %s\": %s",
		e.amount.String(),
		e.commodity,
		e.spec.String(),
		strings.Join(e.matches, ", "),
	)
}

// lotStrings renders lots as they are now.
func lotStrings(lots []*lot) []string {
	rendered := make([]string, len(lots))
	for i, lot := range lots {
		rendered[i] = lot.String()
	}
	return rendered
}

// errNotEnoughLots marks a reduction larger than the lots it can book
// against; book reports it as a notEnoughLotsError.
var errNotEnoughLots = errors.New("not enough lots")

// errAverageUnsupported fails every reduction under the AVERAGE booking
// method, which beancount v2 accepts as an option but never implemented.
var errAverageUnsupported = errors.New("AVERAGE method is not supported")

// notEnoughLotsError reports a reduction larger than the lots it can book
// against, in beancount's words. Like ambiguousBookingMatchError, it holds
// the lots as they were when the reduction failed.
type notEnoughLotsError struct {
	commodity string
	amount    decimal.Decimal
	spec      *lotSpec
	lots      []string
}

func (e *notEnoughLotsError) Error() string {
	return fmt.Sprintf("not enough lots to reduce \"%s %s %s\": %s",
		e.amount.String(), e.commodity, e.spec.String(), strings.Join(e.lots, ", "))
}

type BookingMethod string

const (
	BookingSTRICT  BookingMethod = "STRICT"
	BookingNONE    BookingMethod = "NONE"
	BookingFIFO    BookingMethod = "FIFO"
	BookingLIFO    BookingMethod = "LIFO"
	BookingHIFO    BookingMethod = "HIFO"
	BookingAVERAGE BookingMethod = "AVERAGE"
)

func defaultBookingMethod(method BookingMethod) BookingMethod {
	if method == "" {
		return BookingSTRICT
	}
	return method
}

// NewInventory creates a new inventory
func NewInventory() *Inventory {
	return &Inventory{
		lots: make(map[string][]*lot),
	}
}

// clone returns a copy of the inventory whose lots can change without
// changing the original's.
func (inv *Inventory) clone() *Inventory {
	cloned := &Inventory{lots: make(map[string][]*lot, len(inv.lots))}
	for commodity, lots := range inv.lots {
		copied := make([]*lot, len(lots))
		for i, l := range lots {
			c := *l
			copied[i] = &c
		}
		cloned.lots[commodity] = copied
	}
	return cloned
}

// AddLot adds an amount with a specific cost basis and reports whether it
// reduced the lot: the inventory held the lot with the opposite sign, like
// beancount's Inventory.add_amount returning Booking.REDUCED. A lot of zero
// units, which a zero-units posting leaves, counts as not held: beancount
// never creates one.
func (inv *Inventory) AddLot(commodity string, amount decimal.Decimal, spec *lotSpec) bool {
	// Find existing lot with matching spec
	lots := inv.lots[commodity]
	for _, lot := range lots {
		if lotSpecsMatch(lot.Spec, spec) {
			reduced := !lot.Amount.IsZero() && lot.Amount.IsNegative() != amount.IsNegative()
			lot.Amount = pydecimal.Add(lot.Amount, amount)
			if lot.Amount.IsZero() {
				inv.removeLot(commodity, lot)
			}
			return reduced
		}
	}

	// Create new lot
	newLot := newLot(commodity, amount, spec)
	inv.lots[commodity] = append(inv.lots[commodity], newLot)
	return false
}

// Get returns the total amount of a commodity (summing all lots)
func (inv *Inventory) Get(commodity string) decimal.Decimal {
	total := decimal.Zero
	for _, lot := range inv.lots[commodity] {
		total = pydecimal.Add(total, lot.Amount)
	}
	return total
}

// GetLots returns all lots for a commodity
func (inv *Inventory) GetLots(commodity string) []*lot {
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
func (inv *Inventory) book(posting *ast.Posting, method BookingMethod) (positions []BookedPosition, reduced bool, err error) {
	if posting.Cost == nil || posting.Amount == nil || posting.Amount.Value == "" {
		return nil, false, nil
	}
	units, err := ParseAmount(posting.Amount)
	if err != nil {
		return nil, false, nil // A malformed number drops its transaction before Booking
	}
	method = defaultBookingMethod(method)
	if method == BookingNONE || units.IsZero() {
		return nil, false, nil
	}

	// Like beancount's is_reduced_by, the posting reduces when the inventory
	// holds its commodity with the opposite sign. Like its book_reductions,
	// it is booked only against the lots held at cost: units held without
	// cost make it a reduction but are never booked against.
	commodity := posting.Amount.Currency
	reduces := false
	var lots []*lot
	for _, lot := range inv.lots[commodity] {
		if lot.Amount.Sign() == units.Sign() {
			continue
		}
		reduces = true
		if lot.Spec != nil && lot.Spec.Cost != nil {
			lots = append(lots, lot)
		}
	}
	if !reduces {
		return nil, false, nil
	}
	spec, err := postingLotSpec(posting)
	if err != nil {
		return nil, false, nil // A malformed cost drops its transaction before Booking
	}

	// The strategies work on magnitudes; the booked units take the
	// posting's sign.
	plan, err := planReduction(commodity, lots, units.Abs(), spec, method)
	if errors.Is(err, errNotEnoughLots) {
		return nil, false, &notEnoughLotsError{commodity: commodity, amount: units, spec: spec, lots: lotStrings(lots)}
	}
	if err != nil {
		return nil, false, err
	}

	booked := make([]BookedPosition, 0, len(plan.reductions))
	for i := range plan.reductions {
		reduction := &plan.reductions[i]
		if units.IsNegative() {
			reduction.amount = reduction.amount.Neg()
		}
		s := reduction.lot.Spec
		booked = append(booked, BookedPosition{
			Units:   reduction.amount,
			Cost:    &BookedCost{Number: *s.Cost, Currency: s.CostCurrency, Date: s.Date, Label: s.Label},
			Reduced: true,
		})
	}
	inv.applyReduction(plan)
	return booked, true, nil
}

// augment adds a posting that book did not book as a reduction, once its
// transaction is booked and its numbers are complete, like beancount's
// add_position: at cost, to the lot its spec names, per unit (a total or
// compound cost spread over the units) and, like beancount, dated by its
// transaction when the spec has none (FIFO/LIFO ordering and dated lot
// specs depend on it); without cost, its units alone. It returns the
// position it booked, which is both the change and its record, or none for
// a posting that holds nothing: one without a complete amount, or at a cost
// whose number was not interpolated.
func (inv *Inventory) augment(posting *ast.Posting, date *ast.Date) []BookedPosition {
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
		// Known gap (KNOWN_GAPS.md): ParseLotSpec reads a merge cost {*}
		// as {} and drops the number inferred for it, so an augmentation
		// at {*} books its units without cost, where beancount v2 books
		// it at the inferred cost.
		if spec.Cost != nil {
			if spec.Date != nil {
				date = spec.Date
			}
			position.Cost = &BookedCost{Number: *spec.Cost, Currency: spec.CostCurrency, Date: date, Label: spec.Label}
		}
	}
	position.Reduced = inv.AddLot(posting.Amount.Currency, units, position.lotSpec())
	return []BookedPosition{position}
}

// removeLot removes a lot from the inventory
func (inv *Inventory) removeLot(commodity string, lotToRemove *lot) {
	lots := inv.lots[commodity]
	newLots := make([]*lot, 0, len(lots)-1)
	for _, lot := range lots {
		if lot != lotToRemove {
			newLots = append(newLots, lot)
		}
	}
	if len(newLots) == 0 {
		delete(inv.lots, commodity)
	} else {
		inv.lots[commodity] = newLots
	}
}

// IsEmpty returns true if the inventory has no lots
func (inv *Inventory) IsEmpty() bool {
	return len(inv.lots) == 0
}

// Currencies returns all commodities in the inventory
func (inv *Inventory) Currencies() []string {
	currencies := make([]string, 0, len(inv.lots))
	for currency := range inv.lots {
		currencies = append(currencies, currency)
	}
	return currencies
}

// costCurrencies returns the distinct cost currencies of the lots held at
// cost, sorted.
func (inv *Inventory) costCurrencies() []string {
	var currencies []string
	for _, lots := range inv.lots {
		for _, lot := range lots {
			if lot.Spec != nil && lot.Spec.CostCurrency != "" && !slices.Contains(currencies, lot.Spec.CostCurrency) {
				currencies = append(currencies, lot.Spec.CostCurrency)
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
func (inv *Inventory) String() string {
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
		if l.Spec == nil || l.Spec.Cost == nil {
			return decimal.Zero, ""
		}
		return *l.Spec.Cost, l.Spec.CostCurrency
	}
	slices.SortStableFunc(lots, func(a, b *lot) int {
		if c := CurrencyRank(a.Commodity) - CurrencyRank(b.Commodity); c != 0 {
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
		return a.Amount.Cmp(b.Amount)
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
func (inv *Inventory) countAtCost(commodity string) int {
	n := 0
	for _, lot := range inv.lots[commodity] {
		if lot.Spec != nil {
			n++
		}
	}
	return n
}

// planReduction plans reducing amount (a magnitude) from the given lots.
func planReduction(
	commodity string,
	lots []*lot,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
) (*reductionPlan, error) {
	// Beancount v2 never implemented AVERAGE: every reduction under it fails.
	if bookingMethod == BookingAVERAGE {
		return nil, errAverageUnsupported
	}

	if bookingMethod == BookingSTRICT {
		return planStrictReduction(commodity, lots, amount, spec)
	}

	if spec.IsEmpty() {
		return planBookingReduction(commodity, lots, amount, bookingMethod)
	}

	// Non-empty spec: any combination of cost, date, and label narrows
	// the candidate lots via lotMatchesReductionSpec.
	return planSpecificReduction(commodity, lots, amount, spec, bookingMethod)
}

func planStrictReduction(
	commodity string,
	lots []*lot,
	amount decimal.Decimal,
	spec *lotSpec,
) (*reductionPlan, error) {
	if len(lots) == 0 {
		return nil, fmt.Errorf("no lots available for %s", commodity)
	}

	matches := make([]*lot, 0, len(lots))
	for _, lot := range lots {
		if lotMatchesReductionSpec(lot, spec) {
			matches = append(matches, lot)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("lot not found: %s %s", commodity, spec.String())
	}

	if len(matches) == 1 {
		lot := matches[0]
		if lot.Amount.Abs().LessThan(amount) {
			return nil, fmt.Errorf("%w: insufficient amount in lot %s: have %s, need %s",
				errNotEnoughLots, spec.String(), lot.Amount.Abs().String(), amount.String())
		}
		return &reductionPlan{
			commodity:  commodity,
			reductions: []lotReduction{{lot: lot, amount: amount}},
		}, nil
	}

	total := decimal.Zero
	for _, lot := range matches {
		total = pydecimal.Add(total, lot.Amount.Abs())
	}

	if total.LessThan(amount) {
		return nil, fmt.Errorf("%w: insufficient total amount for %s: have %s, need %s",
			errNotEnoughLots, commodity, total.String(), amount.String())
	}

	if total.Equal(amount) {
		reductions := make([]lotReduction, 0, len(matches))
		for _, lot := range matches {
			reductions = append(reductions, lotReduction{lot: lot, amount: lot.Amount.Abs()})
		}
		return &reductionPlan{
			commodity:  commodity,
			reductions: reductions,
		}, nil
	}

	return nil, &ambiguousBookingMatchError{
		commodity: commodity,
		amount:    amount,
		spec:      spec,
		matches:   lotStrings(matches),
	}
}

func planSpecificReduction(
	commodity string,
	lots []*lot,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
) (*reductionPlan, error) {
	// The spec acts as a filter: lots match on the components it provides
	// (cost, date, label), so a spec without a date still matches dated lots.
	matches := make([]*lot, 0, len(lots))
	for _, lot := range lots {
		if lotMatchesReductionSpec(lot, spec) {
			matches = append(matches, lot)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("lot not found: %s %s", commodity, spec.String())
	}

	return planReductionAcrossLots(commodity, amount, sortedLotsForBooking(matches, bookingMethod))
}

func planBookingReduction(
	commodity string,
	lots []*lot,
	amount decimal.Decimal,
	bookingMethod BookingMethod,
) (*reductionPlan, error) {
	if len(lots) == 0 {
		return nil, fmt.Errorf("no lots available for %s", commodity)
	}

	return planReductionAcrossLots(commodity, amount, sortedLotsForBooking(lots, bookingMethod))
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

		reduction := decimal.Min(lot.Amount.Abs(), remaining)
		reductions = append(reductions, lotReduction{lot: lot, amount: reduction})
		remaining = pydecimal.Sub(remaining, reduction)
	}

	if !remaining.IsZero() {
		return nil, fmt.Errorf("%w: insufficient amount for %s: need %s across %d lots",
			errNotEnoughLots, commodity, amount.String(), len(sortedLots))
	}

	return &reductionPlan{
		commodity:  commodity,
		reductions: reductions,
	}, nil
}

func (inv *Inventory) applyReduction(plan *reductionPlan) {
	for _, reduction := range plan.reductions {
		reduction.lot.Amount = pydecimal.Add(reduction.lot.Amount, reduction.amount)
		if reduction.lot.Amount.IsZero() {
			inv.removeLot(plan.commodity, reduction.lot)
		}
	}
}

func sortedLotsForBooking(lots []*lot, bookingMethod BookingMethod) []*lot {
	sortedLots := append([]*lot(nil), lots...)
	method := defaultBookingMethod(bookingMethod)
	lifo := method == BookingLIFO

	if method == BookingHIFO {
		// Highest cost basis first; lots without a cost sort last.
		slices.SortStableFunc(sortedLots, func(a, b *lot) int {
			aHasCost := a.Spec != nil && a.Spec.Cost != nil
			bHasCost := b.Spec != nil && b.Spec.Cost != nil
			if aHasCost != bHasCost {
				if aHasCost {
					return -1
				}
				return 1
			}
			if !aHasCost {
				return 0
			}
			return b.Spec.Cost.Cmp(*a.Spec.Cost)
		})
		return sortedLots
	}

	slices.SortStableFunc(sortedLots, func(a, b *lot) int {
		aHasDate := a.Spec != nil && a.Spec.Date != nil
		bHasDate := b.Spec != nil && b.Spec.Date != nil

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
			if a.Spec.Date.After(b.Spec.Date.Time) {
				return -1
			}
			if a.Spec.Date.Before(b.Spec.Date.Time) {
				return 1
			}
			return 0
		}
		if a.Spec.Date.Before(b.Spec.Date.Time) {
			return -1
		}
		if a.Spec.Date.After(b.Spec.Date.Time) {
			return 1
		}
		return 0
	})

	return sortedLots
}

func lotMatchesReductionSpec(lot *lot, spec *lotSpec) bool {
	if spec == nil {
		return lot.Spec == nil || lot.Spec.IsEmpty()
	}
	if spec.IsEmpty() {
		return true
	}
	if lot.Spec == nil {
		return false
	}

	if spec.Cost != nil && (lot.Spec.Cost == nil || !lot.Spec.Cost.Equal(*spec.Cost)) {
		return false
	}
	if spec.CostCurrency != "" && lot.Spec.CostCurrency != spec.CostCurrency {
		return false
	}

	if spec.Date != nil {
		if lot.Spec.Date == nil || !lot.Spec.Date.Equal(spec.Date.Time) {
			return false
		}
	}

	if spec.Label != "" && lot.Spec.Label != spec.Label {
		return false
	}

	return true
}

// lotSpecsMatch checks if two lot specs match
func lotSpecsMatch(a, b *lotSpec) bool {
	// Both nil
	if a == nil && b == nil {
		return true
	}

	// One nil, one not
	if a == nil || b == nil {
		return false
	}

	return a.Equal(b)
}
