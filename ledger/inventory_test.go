package ledger

import (
	"testing"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

func TestInventory_BookingMethods(t *testing.T) {
	t.Run("FIFO reduces from oldest lots first", func(t *testing.T) {
		inv := NewInventory()

		// Add three lots with different dates
		date1, _ := ast.NewDate("2020-01-02")
		date2, _ := ast.NewDate("2020-01-03")
		date3, _ := ast.NewDate("2020-01-04")

		spec1 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(100)),
			CostCurrency: "USD",
			Date:         date1,
		}
		spec2 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(110)),
			CostCurrency: "USD",
			Date:         date2,
		}
		spec3 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(120)),
			CostCurrency: "USD",
			Date:         date3,
		}

		inv.AddLot("AAPL", decimal.NewFromInt(10), spec1)
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec2)
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec3)

		// Reduce 5 shares using FIFO
		err := inv.ReduceLot("AAPL", decimal.NewFromInt(-5), &lotSpec{}, "FIFO")
		if err != nil {
			t.Fatalf("ReduceLot failed: %v", err)
		}

		// Check that oldest lot (spec1) was reduced
		lots := inv.GetLots("AAPL")
		if len(lots) != 3 {
			t.Fatalf("Expected 3 lots, got %d", len(lots))
		}

		// Find each lot and check amounts
		var lot1Amount, lot2Amount, lot3Amount decimal.Decimal
		for _, lot := range lots {
			if lot.Spec != nil && lot.Spec.Cost != nil {
				switch lot.Spec.Cost.String() {
				case "100":
					lot1Amount = lot.Amount
				case "110":
					lot2Amount = lot.Amount
				case "120":
					lot3Amount = lot.Amount
				}
			}
		}

		if lot1Amount.String() != "5" {
			t.Errorf("FIFO should reduce oldest lot: expected 5, got %s", lot1Amount.String())
		}
		if lot2Amount.String() != "10" {
			t.Errorf("FIFO should not touch middle lot: expected 10, got %s", lot2Amount.String())
		}
		if lot3Amount.String() != "10" {
			t.Errorf("FIFO should not touch newest lot: expected 10, got %s", lot3Amount.String())
		}
	})

	t.Run("LIFO reduces from newest lots first", func(t *testing.T) {
		inv := NewInventory()

		// Add three lots with different dates
		date1, _ := ast.NewDate("2020-01-02")
		date2, _ := ast.NewDate("2020-01-03")
		date3, _ := ast.NewDate("2020-01-04")

		spec1 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(100)),
			CostCurrency: "USD",
			Date:         date1,
		}
		spec2 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(110)),
			CostCurrency: "USD",
			Date:         date2,
		}
		spec3 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(120)),
			CostCurrency: "USD",
			Date:         date3,
		}

		inv.AddLot("AAPL", decimal.NewFromInt(10), spec1)
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec2)
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec3)

		// Reduce 5 shares using LIFO
		err := inv.ReduceLot("AAPL", decimal.NewFromInt(-5), &lotSpec{}, "LIFO")
		if err != nil {
			t.Fatalf("ReduceLot failed: %v", err)
		}

		// Check that newest lot (spec3) was reduced
		lots := inv.GetLots("AAPL")
		if len(lots) != 3 {
			t.Fatalf("Expected 3 lots, got %d", len(lots))
		}

		// Find each lot and check amounts
		var lot1Amount, lot2Amount, lot3Amount decimal.Decimal
		for _, lot := range lots {
			if lot.Spec != nil && lot.Spec.Cost != nil {
				switch lot.Spec.Cost.String() {
				case "100":
					lot1Amount = lot.Amount
				case "110":
					lot2Amount = lot.Amount
				case "120":
					lot3Amount = lot.Amount
				}
			}
		}

		if lot1Amount.String() != "10" {
			t.Errorf("LIFO should not touch oldest lot: expected 10, got %s", lot1Amount.String())
		}
		if lot2Amount.String() != "10" {
			t.Errorf("LIFO should not touch middle lot: expected 10, got %s", lot2Amount.String())
		}
		if lot3Amount.String() != "5" {
			t.Errorf("LIFO should reduce newest lot: expected 5, got %s", lot3Amount.String())
		}
	})

	t.Run("AVERAGE merges all lots and uses average cost", func(t *testing.T) {
		inv := NewInventory()

		// Add three lots with different costs
		date1, _ := ast.NewDate("2020-01-02")
		date2, _ := ast.NewDate("2020-01-03")
		date3, _ := ast.NewDate("2020-01-04")

		spec1 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(100)),
			CostCurrency: "USD",
			Date:         date1,
		}
		spec2 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(110)),
			CostCurrency: "USD",
			Date:         date2,
		}
		spec3 := &lotSpec{
			Cost:         decimalPtr(decimal.NewFromInt(120)),
			CostCurrency: "USD",
			Date:         date3,
		}

		// Add 10 shares at each price
		// Total: 30 shares, cost basis: (10*100 + 10*110 + 10*120) = 3300
		// Average cost: 3300/30 = 110
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec1)
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec2)
		inv.AddLot("AAPL", decimal.NewFromInt(10), spec3)

		// Reduce 5 shares using AVERAGE
		err := inv.ReduceLot("AAPL", decimal.NewFromInt(-5), &lotSpec{}, "AVERAGE")
		if err != nil {
			t.Fatalf("ReduceLot failed: %v", err)
		}

		// After AVERAGE reduction, should have single lot with 25 shares at avg cost 110
		lots := inv.GetLots("AAPL")
		if len(lots) != 1 {
			t.Fatalf("AVERAGE should merge into single lot, got %d lots", len(lots))
		}

		lot := lots[0]
		if lot.Amount.String() != "25" {
			t.Errorf("Expected 25 shares remaining, got %s", lot.Amount.String())
		}

		if lot.Spec == nil || lot.Spec.Cost == nil {
			t.Fatal("Expected lot to have cost spec")
		}

		if lot.Spec.Cost.String() != "110" {
			t.Errorf("Expected average cost of 110, got %s", lot.Spec.Cost.String())
		}
	})

	t.Run("NONE allows mixed signs in inventory", func(t *testing.T) {
		inv := NewInventory()

		// Add 10 shares
		inv.AddLot("AAPL", decimal.NewFromInt(10), nil)

		// Reduce 15 shares using NONE (more than we have!)
		// This should work with NONE - it just adds the negative amount
		err := inv.ReduceLot("AAPL", decimal.NewFromInt(-15), &lotSpec{}, "NONE")
		if err != nil {
			t.Fatalf("ReduceLot with NONE failed: %v", err)
		}

		// Should have 2 lots: +10 and -15
		lots := inv.GetLots("AAPL")
		if len(lots) != 2 {
			t.Fatalf("Expected 2 lots with NONE booking, got %d", len(lots))
		}

		// Total should be -5 (mixed signs allowed)
		total := inv.Get("AAPL")
		if total.String() != "-5" {
			t.Errorf("Expected total of -5, got %s", total.String())
		}
	})

}

// Helper function
func decimalPtr(d decimal.Decimal) *decimal.Decimal {
	return &d
}
