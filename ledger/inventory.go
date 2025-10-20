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
func (inv *Inventory) ReduceLot(commodity string, amount decimal.Decimal, spec *lotSpec, bookingMethod string) error {
	// Reducing means amount should be negative
	if amount.GreaterThanOrEqual(decimal.Zero) {
		return fmt.Errorf("reduce amount must be negative, got %s", amount.String())
	}

	// Get absolute value for comparison
	reduceAmount := amount.Abs()

	// Merge cost {*} - merge all lots to average cost, then reduce
	if spec != nil && spec.Merge {
		return inv.reduceWithMerge(commodity, reduceAmount, spec)
	}

	// Empty spec {} means use booking method
	if spec != nil && spec.IsEmpty() && !spec.Merge {
		return inv.reduceWithBooking(commodity, reduceAmount, bookingMethod)
	}

	// Specific lot spec - find matching lot
	if spec != nil && spec.Cost != nil && !spec.Merge {
		return inv.reduceSpecificLot(commodity, reduceAmount, spec)
	}

	// No spec at all - treat as simple amount
	// Just add the negative amount to first available lot or create new lot
	inv.AddLot(commodity, amount, nil)
	return nil
}

// reduceSpecificLot reduces from a specific lot matching the spec
func (inv *Inventory) reduceSpecificLot(commodity string, amount decimal.Decimal, spec *lotSpec) error {
	lots := inv.lots[commodity]

	// Find matching lot
	for _, lot := range lots {
		if lotSpecsMatch(lot.Spec, spec) {
			// Check if sufficient amount
			if lot.Amount.LessThan(amount) {
				return fmt.Errorf("insufficient amount in lot %s: have %s, need %s",
					spec.String(), lot.Amount.String(), amount.String())
			}

			// Reduce from lot
			lot.Amount = lot.Amount.Sub(amount)

			// Remove lot if empty
			if lot.Amount.IsZero() {
				inv.removeLot(commodity, lot)
			}

			return nil
		}
	}

	return fmt.Errorf("lot not found: %s %s", commodity, spec.String())
}

// reduceWithMerge reduces using merge cost {*} - merges all lots to average cost, then reduces
func (inv *Inventory) reduceWithMerge(commodity string, amount decimal.Decimal, spec *lotSpec) error {
	lots := inv.lots[commodity]

	if len(lots) == 0 {
		return fmt.Errorf("no lots available for %s", commodity)
	}

	// Calculate total units and total cost basis
	totalUnits := decimal.Zero
	totalCost := decimal.Zero
	costCurrency := ""

	for _, lot := range lots {
		totalUnits = totalUnits.Add(lot.Amount)
		if lot.Spec != nil && lot.Spec.Cost != nil {
			lotCost := lot.Spec.Cost.Mul(lot.Amount)
			totalCost = totalCost.Add(lotCost)
			if costCurrency == "" {
				costCurrency = lot.Spec.CostCurrency
			} else if costCurrency != lot.Spec.CostCurrency {
				return fmt.Errorf("merge cost {*} not supported for mixed currencies")
			}
		}
	}

	if totalUnits.IsZero() {
		return fmt.Errorf("no units available for %s", commodity)
	}

	// Calculate average cost per unit
	averageCost := totalCost.Div(totalUnits)

	// Check if we have enough total units
	if totalUnits.LessThan(amount) {
		return fmt.Errorf("insufficient total amount for %s: have %s, need %s",
			commodity, totalUnits.String(), amount.String())
	}

	// Clear all existing lots and create single merged lot
	delete(inv.lots, commodity)
	remainingUnits := totalUnits.Sub(amount)
	if remainingUnits.GreaterThan(decimal.Zero) {
		// Create new lot with remaining units at average cost
		mergedSpec := &lotSpec{
			Cost:         &averageCost,
			CostCurrency: costCurrency,
		}
		inv.AddLot(commodity, remainingUnits, mergedSpec)
	}

	return nil
}

// reduceWithBooking reduces using booking method (FIFO, LIFO, etc.)
func (inv *Inventory) reduceWithBooking(commodity string, amount decimal.Decimal, bookingMethod string) error {
	lots := inv.lots[commodity]

	if len(lots) == 0 {
		return fmt.Errorf("no lots available for %s", commodity)
	}

	// For now, implement FIFO (oldest first)
	// Sort lots by date (lots without date come first)
	sortedLots := make([]*lot, len(lots))
	copy(sortedLots, lots)
	sort.Slice(sortedLots, func(i, j int) bool {
		// Lots without date come first
		if sortedLots[i].Spec == nil || sortedLots[i].Spec.Date == nil {
			return true
		}
		if sortedLots[j].Spec == nil || sortedLots[j].Spec.Date == nil {
			return false
		}
		// Sort by date
		return sortedLots[i].Spec.Date.Before(sortedLots[j].Spec.Date.Time)
	})

	// Reduce from lots in FIFO order
	remaining := amount
	for _, lot := range sortedLots {
		if remaining.IsZero() {
			break
		}

		if lot.Amount.GreaterThanOrEqual(remaining) {
			// This lot has enough
			lot.Amount = lot.Amount.Sub(remaining)
			if lot.Amount.IsZero() {
				inv.removeLot(commodity, lot)
			}
			remaining = decimal.Zero
		} else {
			// Take all from this lot
			remaining = remaining.Sub(lot.Amount)
			lot.Amount = decimal.Zero
			inv.removeLot(commodity, lot)
		}
	}

	if !remaining.IsZero() {
		return fmt.Errorf("insufficient total amount for %s: need %s more",
			commodity, remaining.String())
	}

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

	var buf strings.Builder
	buf.WriteByte('{')

	first := true
	for commodity, lots := range inv.lots {
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
func (inv *Inventory) CanReduceLot(commodity string, amount decimal.Decimal, spec *lotSpec, bookingMethod string) error {
	// Reducing means amount should be negative
	if amount.GreaterThanOrEqual(decimal.Zero) {
		return fmt.Errorf("reduce amount must be negative, got %s", amount.String())
	}

	reduceAmount := amount.Abs()

	// Merge cost {*} - check if total units are sufficient
	if spec != nil && spec.Merge {
		lots := inv.lots[commodity]
		if len(lots) == 0 {
			return fmt.Errorf("no lots available for %s", commodity)
		}

		totalUnits := decimal.Zero
		for _, lot := range lots {
			totalUnits = totalUnits.Add(lot.Amount)
		}

		if totalUnits.LessThan(reduceAmount) {
			return fmt.Errorf("insufficient total amount for %s: have %s, need %s",
				commodity, totalUnits.String(), reduceAmount.String())
		}
		return nil
	}

	// Empty spec {} means use booking method
	if spec != nil && spec.IsEmpty() && !spec.Merge {
		return inv.canReduceWithBooking(commodity, reduceAmount, bookingMethod)
	}

	// Specific lot spec - find matching lot
	if spec != nil && spec.Cost != nil {
		return inv.canReduceSpecificLot(commodity, reduceAmount, spec)
	}

	// No spec - always succeeds (simple add of negative amount)
	return nil
}

// canReduceSpecificLot checks if a specific lot reduction is possible (read-only)
func (inv *Inventory) canReduceSpecificLot(commodity string, amount decimal.Decimal, spec *lotSpec) error {
	lots := inv.lots[commodity]

	for _, lot := range lots {
		// BEANCOUNT COMPLIANCE: lotSpecsMatch must check cost, date, and label
		// Example: {100 USD, 2024-01-01, "batch-1"} must match all three fields
		if lotSpecsMatch(lot.Spec, spec) {
			if lot.Amount.LessThan(amount) {
				return fmt.Errorf("insufficient amount in lot %s: have %s, need %s",
					spec.String(), lot.Amount.String(), amount.String())
			}
			return nil
		}
	}

	return fmt.Errorf("lot not found: %s %s", commodity, spec.String())
}

// canReduceWithBooking checks if booking method reduction is possible (read-only).
// This simulates the actual FIFO/LIFO booking to verify enough lots exist.
func (inv *Inventory) canReduceWithBooking(commodity string, amount decimal.Decimal, bookingMethod string) error {
	lots := inv.lots[commodity]

	if len(lots) == 0 {
		return fmt.Errorf("no lots available for %s", commodity)
	}

	// Sort lots according to booking method
	sortedLots := make([]*lot, len(lots))
	copy(sortedLots, lots)

	switch bookingMethod {
	case "FIFO", "":
		sort.Slice(sortedLots, func(i, j int) bool {
			// Lots without date come first
			iHasDate := sortedLots[i].Spec != nil && sortedLots[i].Spec.Date != nil
			jHasDate := sortedLots[j].Spec != nil && sortedLots[j].Spec.Date != nil

			if !iHasDate && !jHasDate {
				return i < j // Stable sort for lots without dates
			}
			if !iHasDate {
				return true
			}
			if !jHasDate {
				return false
			}

			// Both have dates - compare
			if sortedLots[i].Spec.Date.Time.Equal(sortedLots[j].Spec.Date.Time) { //nolint:staticcheck
				return i < j // Stable sort for same date
			}
			return sortedLots[i].Spec.Date.Time.Before(sortedLots[j].Spec.Date.Time) //nolint:staticcheck
		})
	case "LIFO":
		sort.Slice(sortedLots, func(i, j int) bool {
			// Reverse of FIFO - lots with dates come first, newest first
			iHasDate := sortedLots[i].Spec != nil && sortedLots[i].Spec.Date != nil
			jHasDate := sortedLots[j].Spec != nil && sortedLots[j].Spec.Date != nil

			if !iHasDate && !jHasDate {
				return i < j // Stable sort for lots without dates
			}
			if !iHasDate {
				return false
			}
			if !jHasDate {
				return true
			}

			// Both have dates - compare (reversed)
			if sortedLots[i].Spec.Date.Time.Equal(sortedLots[j].Spec.Date.Time) { //nolint:staticcheck
				return i < j // Stable sort for same date
			}
			return sortedLots[i].Spec.Date.Time.After(sortedLots[j].Spec.Date.Time) //nolint:staticcheck
		})
	}

	// Simulate reduction in booking order
	remaining := amount
	for _, lot := range sortedLots {
		if remaining.IsZero() {
			return nil
		}
		if lot.Amount.GreaterThanOrEqual(remaining) {
			return nil // This lot covers the remaining amount
		}
		remaining = remaining.Sub(lot.Amount)
	}

	// If we get here, we couldn't reduce the full amount
	return fmt.Errorf("insufficient amount for %s using %s: need %s across %d lots",
		commodity, bookingMethod, amount.String(), len(lots))
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
