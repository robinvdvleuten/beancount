package ledger

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
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

// BookedLot is the share of a reducing posting booked against one lot.
type BookedLot struct {
	Units        decimal.Decimal  // Signed change applied to the lot
	Cost         *decimal.Decimal // Per-unit cost; nil for a lot held without cost
	CostCurrency string
	Date         *ast.Date
	Label        string
}

type reductionPlan struct {
	commodity  string
	reductions []lotReduction
	addAmount  *decimal.Decimal
	addSpec    *lotSpec
}

type ambiguousBookingMatchError struct {
	commodity string
	amount    decimal.Decimal
	spec      *lotSpec
	matches   []*lot
}

func (e *ambiguousBookingMatchError) Error() string {
	matchStrings := make([]string, 0, len(e.matches))
	for _, match := range e.matches {
		matchStrings = append(matchStrings, match.String())
	}

	return fmt.Sprintf("ambiguous matches for \"-%s %s %s\": %s",
		e.amount.String(),
		e.commodity,
		e.spec.String(),
		strings.Join(matchStrings, ", "),
	)
}

// errNotEnoughLots marks a reduction larger than the lots it can book
// against; planBooking reports it as a notEnoughLotsError.
var errNotEnoughLots = errors.New("not enough lots")

// errAverageUnsupported fails every reduction under the AVERAGE booking
// method, which beancount v2 accepts as an option but never implemented.
var errAverageUnsupported = errors.New("AVERAGE method is not supported")

// notEnoughLotsError reports a reduction larger than the lots it can book
// against, in beancount's words.
type notEnoughLotsError struct {
	commodity string
	amount    decimal.Decimal
	spec      *lotSpec
	lots      []*lot
}

func (e *notEnoughLotsError) Error() string {
	lots := make([]string, len(e.lots))
	for i, lot := range e.lots {
		lots[i] = lot.String()
	}
	return fmt.Sprintf("not enough lots to reduce \"%s %s %s\": %s",
		e.amount.String(), e.commodity, e.spec.String(), strings.Join(lots, ", "))
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

// Add adds an amount without cost basis
func (inv *Inventory) Add(commodity string, amount decimal.Decimal) {
	// Add as a lot without cost spec
	inv.AddLot(commodity, amount, nil)
}

// AddLot adds an amount with a specific cost basis
func (inv *Inventory) AddLot(commodity string, amount decimal.Decimal, spec *lotSpec) {
	// Find existing lot with matching spec
	lots := inv.lots[commodity]
	for _, lot := range lots {
		if lotSpecsMatch(lot.Spec, spec) {
			// Add to existing lot
			lot.Amount = lot.Amount.Add(amount)
			if lot.Amount.IsZero() {
				inv.removeLot(commodity, lot)
			}
			return
		}
	}

	// Create new lot
	newLot := newLot(commodity, amount, spec)
	inv.lots[commodity] = append(inv.lots[commodity], newLot)
}

// Get returns the total amount of a commodity (summing all lots)
func (inv *Inventory) Get(commodity string) decimal.Decimal {
	total := decimal.Zero
	for _, lot := range inv.lots[commodity] {
		total = total.Add(lot.Amount)
	}
	return total
}

// GetLots returns all lots for a commodity
func (inv *Inventory) GetLots(commodity string) []*lot {
	return inv.lots[commodity]
}

// Book books a posting of amount units held at spec into the inventory.
//
// Like beancount, the posting reduces existing lots only when the inventory
// holds an opposite-signed position in the commodity (see isReducedBy);
// otherwise it augments, which is how short positions at cost are opened.
// An augmenting lot without an explicit date is acquired on acquisitionDate.
//
// For a reduction, Book returns the lots it was booked against in booking
// order; an augmentation returns none.
func (inv *Inventory) Book(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
	acquisitionDate *ast.Date,
) ([]BookedLot, error) {
	booked, _, err := inv.book(commodity, amount, spec, bookingMethod, acquisitionDate)
	return booked, err
}

// lotChange is one signed change to the lot held at spec. Replaying a
// booking's changes with AddLot reproduces it on another inventory.
type lotChange struct {
	spec   *lotSpec
	amount decimal.Decimal
}

// book is Book, also returning the lot changes it made.
func (inv *Inventory) book(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
	acquisitionDate *ast.Date,
) ([]BookedLot, []lotChange, error) {
	plan, err := inv.planBooking(commodity, amount, spec, bookingMethod, acquisitionDate)
	if err != nil {
		return nil, nil, err
	}

	var booked []BookedLot
	for _, reduction := range plan.reductions {
		lot := BookedLot{Units: reduction.amount}
		if s := reduction.lot.Spec; s != nil {
			lot.Cost, lot.CostCurrency, lot.Date, lot.Label = s.Cost, s.CostCurrency, s.Date, s.Label
		}
		booked = append(booked, lot)
	}

	changes := inv.planChanges(plan)
	inv.applyReduction(plan)
	return booked, changes, nil
}

// planChanges lists the lot changes applying plan makes.
func (inv *Inventory) planChanges(plan *reductionPlan) []lotChange {
	var changes []lotChange
	switch {
	case plan.addAmount != nil:
		changes = append(changes, lotChange{spec: plan.addSpec, amount: *plan.addAmount})
	default:
		for _, reduction := range plan.reductions {
			changes = append(changes, lotChange{spec: reduction.lot.Spec, amount: reduction.amount})
		}
	}
	return changes
}

// isReducedBy reports whether adding amount of commodity would reduce the
// inventory: some position in the commodity has the opposite sign. This is
// beancount's Inventory.is_reduced_by.
func (inv *Inventory) isReducedBy(commodity string, amount decimal.Decimal) bool {
	if amount.IsZero() {
		return false
	}
	for _, lot := range inv.lots[commodity] {
		if lot.Amount.Sign() != amount.Sign() {
			return true
		}
	}
	return false
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

// String returns a string representation of the inventory
func (inv *Inventory) String() string {
	if inv.IsEmpty() {
		return "{}"
	}

	commodities := make([]string, 0, len(inv.lots))
	for commodity := range inv.lots {
		commodities = append(commodities, commodity)
	}
	slices.Sort(commodities)

	var buf strings.Builder
	buf.WriteByte('{')

	first := true
	for _, commodity := range commodities {
		lots := inv.lots[commodity]
		for _, lot := range lots {
			if !first {
				buf.WriteString(", ")
			}
			if lot.Spec == nil || lot.Spec.IsEmpty() {
				buf.WriteString(lot.Amount.String())
				buf.WriteByte(' ')
				buf.WriteString(commodity)
			} else {
				buf.WriteString(lot.String())
			}
			first = false
		}
	}
	buf.WriteByte('}')
	return buf.String()
}

// CanBook checks if a booking is possible without mutating state.
// This is a read-only version of Book used for validation.
func (inv *Inventory) CanBook(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
) error {
	_, err := inv.planBooking(commodity, amount, spec, bookingMethod, nil)
	return err
}

// planBooking decides whether the posting augments or reduces the inventory
// and plans the change. A zero amount plans nothing.
func (inv *Inventory) planBooking(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
	acquisitionDate *ast.Date,
) (*reductionPlan, error) {
	if amount.IsZero() {
		return &reductionPlan{commodity: commodity}, nil
	}

	bookingMethod = defaultBookingMethod(bookingMethod)
	if bookingMethod == BookingNONE || spec == nil || !inv.isReducedBy(commodity, amount) {
		if spec != nil && spec.Date == nil && acquisitionDate != nil {
			dated := *spec
			dated.Date = acquisitionDate
			spec = &dated
		}
		return &reductionPlan{
			commodity: commodity,
			addAmount: &amount,
			addSpec:   spec,
		}, nil
	}

	// A reduction consumes only lots of the opposite sign, and a cost spec
	// only matches lots held at cost: like beancount's book_reductions, units
	// held without cost are never booked against, although they still make
	// the posting a reduction. The strategies work on magnitudes; the
	// resulting deltas take the posting's sign.
	lots := make([]*lot, 0, len(inv.lots[commodity]))
	for _, lot := range inv.lots[commodity] {
		if lot.Amount.Sign() == amount.Sign() {
			continue
		}
		if lot.Spec == nil || lot.Spec.Cost == nil {
			continue
		}
		lots = append(lots, lot)
	}

	plan, err := planReduction(commodity, lots, amount.Abs(), spec, bookingMethod)
	if errors.Is(err, errNotEnoughLots) {
		return nil, &notEnoughLotsError{commodity: commodity, amount: amount, spec: spec, lots: lots}
	}
	if err != nil {
		return nil, err
	}
	if amount.IsNegative() {
		for i := range plan.reductions {
			plan.reductions[i].amount = plan.reductions[i].amount.Neg()
		}
	}
	return plan, nil
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
		total = total.Add(lot.Amount.Abs())
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
		matches:   matches,
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

func (inv *Inventory) canReduceSpecificLot(commodity string, amount decimal.Decimal, spec *lotSpec) error {
	_, err := planSpecificReduction(commodity, inv.lots[commodity], amount, spec, BookingFIFO)
	return err
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
		remaining = remaining.Sub(reduction)
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

func (inv *Inventory) canReduceWithBooking(
	commodity string,
	amount decimal.Decimal,
	bookingMethod BookingMethod,
) error {
	_, err := planBookingReduction(commodity, inv.lots[commodity], amount, bookingMethod)
	return err
}

func (inv *Inventory) applyReduction(plan *reductionPlan) {
	if plan.addAmount != nil {
		inv.AddLot(plan.commodity, *plan.addAmount, plan.addSpec)
		return
	}

	for _, reduction := range plan.reductions {
		reduction.lot.Amount = reduction.lot.Amount.Add(reduction.amount)
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
