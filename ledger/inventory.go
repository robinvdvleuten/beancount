package ledger

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shopspring/decimal"
)

// Inventory tracks lots of commodities with cost basis
type Inventory struct {
	// Map: commodity -> list of lots
	lots map[string][]*lot
}

type lotReduction struct {
	lot    *lot
	amount decimal.Decimal
}

type reductionPlan struct {
	commodity       string
	reductions      []lotReduction
	replaceLots     bool
	replacementLots []*lot
	addAmount       *decimal.Decimal
	addSpec         *lotSpec
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

type BookingMethod string

const (
	BookingSTRICT  BookingMethod = "STRICT"
	BookingNONE    BookingMethod = "NONE"
	BookingFIFO    BookingMethod = "FIFO"
	BookingLIFO    BookingMethod = "LIFO"
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

// ReduceLot reduces from a specific lot or uses booking method
func (inv *Inventory) ReduceLot(commodity string, amount decimal.Decimal, spec *lotSpec, bookingMethod BookingMethod) error {
	plan, err := inv.planReduction(commodity, amount, spec, bookingMethod)
	if err != nil {
		return err
	}
	inv.applyReduction(plan)
	return nil
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

// CanReduceLot checks if a reduction is possible without mutating state.
// This is a read-only version of ReduceLot used for validation.
func (inv *Inventory) CanReduceLot(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
) error {
	_, err := inv.planReduction(commodity, amount, spec, bookingMethod)
	return err
}

func (inv *Inventory) planReduction(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
) (*reductionPlan, error) {
	// Reducing means amount should be negative
	if amount.GreaterThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("reduce amount must be negative, got %s", amount.String())
	}

	reduceAmount := amount.Abs()
	bookingMethod = defaultBookingMethod(bookingMethod)

	if bookingMethod == BookingNONE {
		return &reductionPlan{
			commodity: commodity,
			addAmount: &amount,
			addSpec:   spec,
		}, nil
	}

	if spec == nil {
		return &reductionPlan{
			commodity: commodity,
			addAmount: &amount,
		}, nil
	}

	if spec.Merge {
		return inv.planMergeReduction(commodity, reduceAmount)
	}

	if bookingMethod == BookingSTRICT {
		return inv.planStrictReduction(commodity, reduceAmount, spec)
	}

	if spec.IsEmpty() {
		return inv.planBookingReduction(commodity, reduceAmount, bookingMethod)
	}

	// Non-empty spec: any combination of cost, date, and label narrows
	// the candidate lots via lotMatchesReductionSpec.
	return inv.planSpecificReduction(commodity, reduceAmount, spec, bookingMethod)
}

func (inv *Inventory) planStrictReduction(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
) (*reductionPlan, error) {
	lots := inv.lots[commodity]
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
		if lot.Amount.LessThan(amount) {
			return nil, fmt.Errorf("insufficient amount in lot %s: have %s, need %s",
				spec.String(), lot.Amount.String(), amount.String())
		}
		return &reductionPlan{
			commodity:  commodity,
			reductions: []lotReduction{{lot: lot, amount: amount}},
		}, nil
	}

	total := decimal.Zero
	for _, lot := range matches {
		total = total.Add(lot.Amount)
	}

	if total.LessThan(amount) {
		return nil, fmt.Errorf("insufficient total amount for %s: have %s, need %s",
			commodity, total.String(), amount.String())
	}

	if total.Equal(amount) {
		reductions := make([]lotReduction, 0, len(matches))
		for _, lot := range matches {
			reductions = append(reductions, lotReduction{lot: lot, amount: lot.Amount})
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

func (inv *Inventory) planSpecificReduction(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
	bookingMethod BookingMethod,
) (*reductionPlan, error) {
	// The spec acts as a filter: lots match on the components it provides
	// (cost, date, label), so a spec without a date still matches dated lots.
	matches := make([]*lot, 0, len(inv.lots[commodity]))
	for _, lot := range inv.lots[commodity] {
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
	_, err := inv.planSpecificReduction(commodity, amount, spec, BookingFIFO)
	return err
}

func (inv *Inventory) planBookingReduction(
	commodity string,
	amount decimal.Decimal,
	bookingMethod BookingMethod,
) (*reductionPlan, error) {
	lots := inv.lots[commodity]

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

		reduction := decimal.Min(lot.Amount, remaining)
		reductions = append(reductions, lotReduction{lot: lot, amount: reduction})
		remaining = remaining.Sub(reduction)
	}

	if !remaining.IsZero() {
		return nil, fmt.Errorf("insufficient amount for %s: need %s across %d lots",
			commodity, amount.String(), len(sortedLots))
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
	_, err := inv.planBookingReduction(commodity, amount, bookingMethod)
	return err
}

func (inv *Inventory) planMergeReduction(
	commodity string,
	amount decimal.Decimal,
) (*reductionPlan, error) {
	lots := inv.lots[commodity]
	if len(lots) == 0 {
		return nil, fmt.Errorf("no lots available for %s", commodity)
	}

	totalUnits := decimal.Zero
	totalCost := decimal.Zero
	costCurrency := ""
	for _, lot := range lots {
		totalUnits = totalUnits.Add(lot.Amount)
		if lot.Spec == nil || lot.Spec.Cost == nil {
			continue
		}
		totalCost = totalCost.Add(lot.Spec.Cost.Mul(lot.Amount))
		if costCurrency == "" {
			costCurrency = lot.Spec.CostCurrency
		} else if costCurrency != lot.Spec.CostCurrency {
			return nil, fmt.Errorf("merge cost {*} not supported for mixed currencies")
		}
	}

	if totalUnits.IsZero() {
		return nil, fmt.Errorf("no units available for %s", commodity)
	}
	if totalUnits.LessThan(amount) {
		return nil, fmt.Errorf("insufficient total amount for %s: have %s, need %s",
			commodity, totalUnits.String(), amount.String())
	}

	plan := &reductionPlan{
		commodity:   commodity,
		replaceLots: true,
	}
	remainingUnits := totalUnits.Sub(amount)
	if remainingUnits.GreaterThan(decimal.Zero) {
		averageCost := totalCost.Div(totalUnits)
		plan.replacementLots = []*lot{newLot(commodity, remainingUnits, &lotSpec{
			Cost:         &averageCost,
			CostCurrency: costCurrency,
		})}
	}
	return plan, nil
}

func (inv *Inventory) applyReduction(plan *reductionPlan) {
	if plan.replaceLots {
		if len(plan.replacementLots) == 0 {
			delete(inv.lots, plan.commodity)
		} else {
			inv.lots[plan.commodity] = plan.replacementLots
		}
		return
	}

	if plan.addAmount != nil {
		inv.AddLot(plan.commodity, *plan.addAmount, plan.addSpec)
		return
	}

	for _, reduction := range plan.reductions {
		reduction.lot.Amount = reduction.lot.Amount.Sub(reduction.amount)
		if reduction.lot.Amount.IsZero() {
			inv.removeLot(plan.commodity, reduction.lot)
		}
	}
}

func sortedLotsForBooking(lots []*lot, bookingMethod BookingMethod) []*lot {
	sortedLots := append([]*lot(nil), lots...)
	lifo := defaultBookingMethod(bookingMethod) == BookingLIFO

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

	if spec.Cost != nil {
		if lot.Spec.Cost == nil || !lot.Spec.Cost.Equal(*spec.Cost) {
			return false
		}
		if lot.Spec.CostCurrency != spec.CostCurrency {
			return false
		}
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
