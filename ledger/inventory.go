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

	// Empty spec {} means use booking method
	if spec != nil && spec.IsEmpty() {
		return inv.reduceWithBooking(commodity, reduceAmount, bookingMethod)
	}

	// Specific lot spec - find matching lot
	if spec != nil && spec.Cost != nil {
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

// reduceWithBooking reduces using booking method (FIFO, LIFO, etc.)
// Assumes booking method has already been validated by the validator.
//
// Booking method handling:
// - NONE: Adds negative amount without matching (allows mixed signs)
// - AVERAGE: Merges all lots and recalculates average cost
// - FIFO/LIFO: Sorts lots by date and reduces in order
// - STRICT: Should never reach here with empty spec (validator rejects it)
func (inv *Inventory) reduceWithBooking(commodity string, amount decimal.Decimal, bookingMethod string) error {
	lots := inv.lots[commodity]

	if len(lots) == 0 {
		return fmt.Errorf("no lots available for %s", commodity)
	}

	// Handle NONE booking: just add the negative amount without matching
	if bookingMethod == "NONE" {
		// Add negative amount as a new lot (always create new lot, don't merge)
		// This allows mixed signs in the inventory
		newLot := newLot(commodity, amount.Neg(), nil)
		inv.lots[commodity] = append(inv.lots[commodity], newLot)
		return nil
	}

	// Handle AVERAGE booking: merge all lots, reduce, then keep single lot with average cost
	if bookingMethod == "AVERAGE" {
		return inv.reduceWithAverage(commodity, amount)
	}

	// Sort lots by date according to booking method (validation already done)
	// If we get an unsupported method here, it's a validator bug
	sortedLots := make([]*lot, len(lots))
	copy(sortedLots, lots)

	switch bookingMethod {
	case "FIFO":
		// FIFO: oldest first (lots without date come first)
		sort.Slice(sortedLots, func(i, j int) bool {
			iHasDate := sortedLots[i].Spec != nil && sortedLots[i].Spec.Date != nil
			jHasDate := sortedLots[j].Spec != nil && sortedLots[j].Spec.Date != nil

			// Both lack dates - maintain stable order (not less than)
			if !iHasDate && !jHasDate {
				return false
			}
			// i lacks date, j has date - i comes first
			if !iHasDate {
				return true
			}
			// j lacks date, i has date - j comes first
			if !jHasDate {
				return false
			}
			// Both have dates - compare chronologically (oldest first)
			return sortedLots[i].Spec.Date.Before(sortedLots[j].Spec.Date.Time)
		})
	case "LIFO":
		// LIFO: newest first (lots with dates come first, reverse chronological)
		sort.Slice(sortedLots, func(i, j int) bool {
			iHasDate := sortedLots[i].Spec != nil && sortedLots[i].Spec.Date != nil
			jHasDate := sortedLots[j].Spec != nil && sortedLots[j].Spec.Date != nil

			// Both lack dates - maintain stable order (not less than)
			if !iHasDate && !jHasDate {
				return false
			}
			// i has date, j lacks date - i comes first (dated lots first for LIFO)
			if iHasDate && !jHasDate {
				return true
			}
			// j has date, i lacks date - j comes first
			if !iHasDate && jHasDate {
				return false
			}
			// Both have dates - compare reverse chronologically (newest first)
			return sortedLots[i].Spec.Date.After(sortedLots[j].Spec.Date.Time)
		})
	case "STRICT":
		// STRICT should never reach here with empty spec - validator should reject it
		panic("STRICT booking with empty spec {} should be rejected by validator (validator bug)")
	default:
		// Should never reach here - validator should have caught unsupported methods
		panic(fmt.Sprintf("unsupported booking method %q after validation (validator bug)", bookingMethod))
	}

	// Reduce from lots in booking method order
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

// reduceWithAverage reduces using average cost basis
// After reduction, all lots are merged into a single lot with average cost
func (inv *Inventory) reduceWithAverage(commodity string, amount decimal.Decimal) error {
	lots := inv.lots[commodity]

	// Calculate total amount and total cost basis
	totalAmount := decimal.Zero
	totalCost := decimal.Zero
	var costCurrency string
	hasCostedLots := false

	for _, lot := range lots {
		totalAmount = totalAmount.Add(lot.Amount)

		// Track cost basis if lots have cost
		if lot.Spec != nil && lot.Spec.Cost != nil {
			hasCostedLots = true
			costCurrency = lot.Spec.CostCurrency
			// Total cost = amount * cost per unit
			lotTotalCost := lot.Amount.Mul(*lot.Spec.Cost)
			totalCost = totalCost.Add(lotTotalCost)
		}
	}

	// Check if there's enough to reduce
	if totalAmount.LessThan(amount) {
		return fmt.Errorf("insufficient total amount for %s: have %s, need %s",
			commodity, totalAmount.String(), amount.String())
	}

	// Calculate remaining amount after reduction
	remainingAmount := totalAmount.Sub(amount)

	// Remove all existing lots
	delete(inv.lots, commodity)

	// If nothing remains, we're done
	if remainingAmount.IsZero() {
		return nil
	}

	// Calculate average cost per unit if we have costed lots
	var avgSpec *lotSpec
	if hasCostedLots && !totalCost.IsZero() && !totalAmount.IsZero() {
		avgCost := totalCost.Div(totalAmount)
		avgSpec = &lotSpec{
			Cost:         &avgCost,
			CostCurrency: costCurrency,
		}
	}

	// Create single lot with remaining amount at average cost
	inv.AddLot(commodity, remainingAmount, avgSpec)

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
