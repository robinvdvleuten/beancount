package ledger

import (
	"fmt"
	"sort"
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
}

type BookingMethod string

const (
	BookingFIFO BookingMethod = "FIFO"
	BookingLIFO BookingMethod = "LIFO"
)

func defaultBookingMethod(method BookingMethod) BookingMethod {
	if method == "" {
		return BookingFIFO
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
	sort.Strings(commodities)

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

	if spec != nil && spec.Merge {
		return inv.planMergeReduction(commodity, reduceAmount)
	}

	if spec != nil && spec.IsEmpty() && !spec.Merge {
		return inv.planBookingReduction(commodity, reduceAmount, bookingMethod)
	}

	if spec != nil && spec.Cost != nil && !spec.Merge {
		return inv.planSpecificReduction(commodity, reduceAmount, spec)
	}

	return &reductionPlan{
		commodity: commodity,
		addAmount: &amount,
	}, nil
}

func (inv *Inventory) planSpecificReduction(
	commodity string,
	amount decimal.Decimal,
	spec *lotSpec,
) (*reductionPlan, error) {
	lots := inv.lots[commodity]

	for _, lot := range lots {
		if lotSpecsMatch(lot.Spec, spec) {
			if lot.Amount.LessThan(amount) {
				return nil, fmt.Errorf("insufficient amount in lot %s: have %s, need %s",
					spec.String(), lot.Amount.String(), amount.String())
			}
			return &reductionPlan{
				commodity:  commodity,
				reductions: []lotReduction{{lot: lot, amount: amount}},
			}, nil
		}
	}

	return nil, fmt.Errorf("lot not found: %s %s", commodity, spec.String())
}

func (inv *Inventory) canReduceSpecificLot(commodity string, amount decimal.Decimal, spec *lotSpec) error {
	_, err := inv.planSpecificReduction(commodity, amount, spec)
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

	sortedLots := sortedLotsForBooking(lots, bookingMethod)
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
		return nil, fmt.Errorf("insufficient amount for %s using %s: need %s across %d lots",
			commodity, bookingMethod, amount.String(), len(lots))
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
		inv.AddLot(plan.commodity, *plan.addAmount, nil)
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

	sort.SliceStable(sortedLots, func(i, j int) bool {
		iHasDate := sortedLots[i].Spec != nil && sortedLots[i].Spec.Date != nil
		jHasDate := sortedLots[j].Spec != nil && sortedLots[j].Spec.Date != nil

		if iHasDate != jHasDate {
			if lifo {
				return iHasDate
			}
			return !iHasDate
		}
		if !iHasDate {
			return false
		}
		if lifo {
			return sortedLots[i].Spec.Date.After(sortedLots[j].Spec.Date.Time)
		}
		return sortedLots[i].Spec.Date.Before(sortedLots[j].Spec.Date.Time)
	})

	return sortedLots
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
