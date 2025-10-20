package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func TestCanReduceLot(t *testing.T) {
	date1, _ := ast.NewDate("2024-01-15")
	date2, _ := ast.NewDate("2024-02-15")

	// Helper to create decimal from string
	d := func(s string) decimal.Decimal {
		val, _ := decimal.NewFromString(s)
		return val
	}

	tests := []struct {
		name          string
		setup         func() *Inventory
		commodity     string
		amount        decimal.Decimal
		spec          *lotSpec
		bookingMethod string
		wantErr       bool
		errContains   string
	}{
		{
			name: "reducing with negative amount - valid",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.Add("USD", d("100"))
				return inv
			},
			commodity:     "USD",
			amount:        d("-50"),
			spec:          nil,
			bookingMethod: "",
			wantErr:       false,
		},
		{
			name: "reducing with positive amount - error",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.Add("USD", d("100"))
				return inv
			},
			commodity:     "USD",
			amount:        d("50"),
			spec:          nil,
			bookingMethod: "",
			wantErr:       true,
			errContains:   "reduce amount must be negative",
		},
		{
			name: "reducing with no spec - simple add",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.Add("USD", d("100"))
				return inv
			},
			commodity:     "USD",
			amount:        d("-30"),
			spec:          nil,
			bookingMethod: "",
			wantErr:       false,
		},
		{
			name: "reducing with empty spec {} - uses booking method FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("USD", d("50"), &lotSpec{Date: date1})
				inv.AddLot("USD", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "USD",
			amount:        d("-40"),
			spec:          &lotSpec{}, // Empty spec
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "reducing with specific lot spec - cost match",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				inv.AddLot("STOCK", d("10"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("-5"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("100")),
				CostCurrency: "USD",
			},
			bookingMethod: "",
			wantErr:       false,
		},
		{
			name: "reducing with specific lot spec - insufficient amount",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				inv.AddLot("STOCK", d("10"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("-20"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("100")),
				CostCurrency: "USD",
			},
			bookingMethod: "",
			wantErr:       true,
			errContains:   "insufficient amount",
		},
		{
			name: "reducing with specific lot spec - lot not found",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				inv.AddLot("STOCK", d("10"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("-5"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("200")),
				CostCurrency: "USD",
			},
			bookingMethod: "",
			wantErr:       true,
			errContains:   "lot not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.setup()
			err := inv.CanReduceLot(tt.commodity, tt.amount, tt.spec, tt.bookingMethod)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.HasPrefix(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCanReduceSpecificLot(t *testing.T) {
	date1, _ := ast.NewDate("2024-01-15")

	// Helper to create decimal from string
	d := func(s string) decimal.Decimal {
		val, _ := decimal.NewFromString(s)
		return val
	}

	tests := []struct {
		name        string
		setup       func() *Inventory
		commodity   string
		amount      decimal.Decimal
		spec        *lotSpec
		wantErr     bool
		errContains string
	}{
		{
			name: "lot found with sufficient amount - valid",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				inv.AddLot("STOCK", d("50"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("30"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("100")),
				CostCurrency: "USD",
			},
			wantErr: false,
		},
		{
			name: "lot found with insufficient amount - error",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				inv.AddLot("STOCK", d("20"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("30"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("100")),
				CostCurrency: "USD",
			},
			wantErr:     true,
			errContains: "insufficient amount",
		},
		{
			name: "lot not found - error",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				inv.AddLot("STOCK", d("50"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("30"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("200")),
				CostCurrency: "USD",
			},
			wantErr:     true,
			errContains: "lot not found",
		},
		{
			name: "multiple lots, only one matches",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				cost200 := d("200")
				inv.AddLot("STOCK", d("50"), &lotSpec{Cost: &cost100, CostCurrency: "USD"})
				inv.AddLot("STOCK", d("30"), &lotSpec{Cost: &cost200, CostCurrency: "USD"})
				return inv
			},
			commodity: "STOCK",
			amount:    d("20"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("100")),
				CostCurrency: "USD",
			},
			wantErr: false,
		},
		{
			name: "multiple lots with dates, match on date",
			setup: func() *Inventory {
				inv := NewInventory()
				cost100 := d("100")
				date2, _ := ast.NewDate("2024-02-15")
				inv.AddLot("STOCK", d("50"), &lotSpec{Cost: &cost100, CostCurrency: "USD", Date: date1})
				inv.AddLot("STOCK", d("30"), &lotSpec{Cost: &cost100, CostCurrency: "USD", Date: date2})
				return inv
			},
			commodity: "STOCK",
			amount:    d("20"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("100")),
				CostCurrency: "USD",
				Date:         date1,
			},
			wantErr: false,
		},
		{
			name: "exact amount match",
			setup: func() *Inventory {
				inv := NewInventory()
				cost150 := d("150")
				inv.AddLot("HOOL", d("100"), &lotSpec{Cost: &cost150, CostCurrency: "USD"})
				return inv
			},
			commodity: "HOOL",
			amount:    d("100"),
			spec: &lotSpec{
				Cost:         ptrDecimal(d("150")),
				CostCurrency: "USD",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.setup()
			err := inv.canReduceSpecificLot(tt.commodity, tt.amount, tt.spec)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.HasPrefix(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCanReduceWithBooking(t *testing.T) {
	date1, _ := ast.NewDate("2024-01-15")
	date2, _ := ast.NewDate("2024-02-15")
	date3, _ := ast.NewDate("2024-03-15")

	// Helper to create decimal from string
	d := func(s string) decimal.Decimal {
		val, _ := decimal.NewFromString(s)
		return val
	}

	tests := []struct {
		name          string
		setup         func() *Inventory
		commodity     string
		amount        decimal.Decimal
		bookingMethod string
		wantErr       bool
		errContains   string
	}{
		{
			name: "FIFO reduces oldest lots first",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("40"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "FIFO with exact amount from first lot",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("50"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "FIFO spanning multiple lots",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("30"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("40"), &lotSpec{Date: date2})
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date3})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("60"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "LIFO reduces newest lots first",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("40"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "LIFO with exact amount from newest lot",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("60"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "LIFO spanning multiple lots",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("30"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("40"), &lotSpec{Date: date2})
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date3})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("60"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "stable sort for same-date lots FIFO - deterministic ordering",
			setup: func() *Inventory {
				inv := NewInventory()
				// Two lots with same date, stable sort should preserve order
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("80"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "stable sort for same-date lots LIFO - deterministic ordering",
			setup: func() *Inventory {
				inv := NewInventory()
				// Two lots with same date, stable sort should preserve order
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("80"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "lots without dates come first FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), nil) // No date
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("40"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "lots without dates come last LIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), nil) // No date
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("40"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "default booking method empty string treated as FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("40"),
			bookingMethod: "", // Empty string defaults to FIFO
			wantErr:       false,
		},
		{
			name: "insufficient total across multiple lots FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("30"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("40"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("100"),
			bookingMethod: "FIFO",
			wantErr:       true,
			errContains:   "insufficient amount",
		},
		{
			name: "insufficient total across multiple lots LIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("30"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("40"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("100"),
			bookingMethod: "LIFO",
			wantErr:       true,
			errContains:   "insufficient amount",
		},
		{
			name: "empty lots array FIFO",
			setup: func() *Inventory {
				return NewInventory()
			},
			commodity:     "STOCK",
			amount:        d("50"),
			bookingMethod: "FIFO",
			wantErr:       true,
			errContains:   "no lots available",
		},
		{
			name: "empty lots array LIFO",
			setup: func() *Inventory {
				return NewInventory()
			},
			commodity:     "STOCK",
			amount:        d("50"),
			bookingMethod: "LIFO",
			wantErr:       true,
			errContains:   "no lots available",
		},
		{
			name: "single lot exact amount FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("100"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("100"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "single lot exact amount LIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("100"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("100"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "single lot more than needed FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("100"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("50"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "complex: mix of dated and undated lots FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("25"), nil) // No date (should come first in FIFO)
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("30"), nil) // Another undated
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("100"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "complex: mix of dated and undated lots LIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("25"), nil) // No date
				inv.AddLot("STOCK", d("50"), &lotSpec{Date: date1})
				inv.AddLot("STOCK", d("30"), nil) // Another undated
				inv.AddLot("STOCK", d("60"), &lotSpec{Date: date2})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("100"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
		{
			name: "zero amount needed",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("100"), &lotSpec{Date: date1})
				return inv
			},
			commodity:     "STOCK",
			amount:        d("0"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "all lots without dates FIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), nil)
				inv.AddLot("STOCK", d("60"), nil)
				return inv
			},
			commodity:     "STOCK",
			amount:        d("80"),
			bookingMethod: "FIFO",
			wantErr:       false,
		},
		{
			name: "all lots without dates LIFO",
			setup: func() *Inventory {
				inv := NewInventory()
				inv.AddLot("STOCK", d("50"), nil)
				inv.AddLot("STOCK", d("60"), nil)
				return inv
			},
			commodity:     "STOCK",
			amount:        d("80"),
			bookingMethod: "LIFO",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.setup()
			err := inv.canReduceWithBooking(tt.commodity, tt.amount, tt.bookingMethod)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.HasPrefix(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Helper function to create a pointer to a decimal
func ptrDecimal(d decimal.Decimal) *decimal.Decimal {
	return &d
}

// TestFIFOLIFOBooking tests FIFO and LIFO booking method semantics.
// FIFO reduces oldest lots first, LIFO reduces newest lots first.
// Both use stable sort for same-date lots (preserve insertion order).
func TestFIFOLIFOBooking(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *Ledger)
	}{
		{
			name: "FIFO reduces oldest lots first",
			input: `
				2020-01-01 open Assets:Brokerage "FIFO"
				2020-01-01 open Assets:Cash USD
				2020-01-01 open Income:CapitalGains

				2020-01-02 * "Buy lot 1"
				  Assets:Brokerage    10 STOCK {100 USD}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Buy lot 2"
				  Assets:Brokerage    10 STOCK {110 USD}
				  Assets:Cash        -1100 USD

				2020-01-04 * "Sell - should reduce lot 1 first"
				  Assets:Brokerage    -15 STOCK {}
				  Assets:Cash         1650 USD
				  Income:CapitalGains    -1650 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				// Should have 5 shares left from lot 2 at 110 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].Amount.String())
			},
		},
		{
			name: "LIFO reduces newest lots first",
			input: `
				2020-01-01 open Assets:Brokerage "LIFO"
				2020-01-01 open Assets:Cash USD
				2020-01-01 open Income:CapitalGains

				2020-01-02 * "Buy lot 1"
				  Assets:Brokerage    10 STOCK {100 USD}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Buy lot 2"
				  Assets:Brokerage    10 STOCK {110 USD}
				  Assets:Cash        -1100 USD

				2020-01-04 * "Sell - should reduce lot 2 first"
				  Assets:Brokerage    -15 STOCK {}
				  Assets:Cash         1600 USD
				  Income:CapitalGains    -1600 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				// Should have 5 shares left from lot 1 at 100 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].Amount.String())
			},
		},
		{
			name: "stable sort for same-date lots",
			input: `
				2020-01-01 open Assets:Brokerage "FIFO"
				2020-01-01 open Assets:Cash USD
				2020-01-01 open Income:CapitalGains

				2020-01-02 * "Buy multiple lots same day"
				  Assets:Brokerage    10 STOCK {100 USD}
				  Assets:Brokerage    10 STOCK {105 USD}
				  Assets:Brokerage    10 STOCK {110 USD}
				  Assets:Cash        -3150 USD

				2020-01-03 * "Sell - should use insertion order"
				  Assets:Brokerage    -25 STOCK {}
				  Assets:Cash         2625 USD
				  Income:CapitalGains    -2625 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				// Should have 5 shares left from last lot at 110 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].Amount.String())
			},
		},
		{
			name: "insufficient inventory across multiple lots",
			input: `
				2020-01-01 open Assets:Brokerage "FIFO"
				2020-01-01 open Assets:Cash USD
				2020-01-01 open Income:CapitalGains

				2020-01-02 * "Buy stock"
				  Assets:Brokerage    10 STOCK {100 USD}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Try to sell more than available"
				  Assets:Brokerage    -20 STOCK {}
				  Assets:Cash         2000 USD
				  Income:CapitalGains    -2000 USD
			`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.check != nil {
					tt.check(t, l)
				}
			}
		})
	}
}

// TestLotMatching tests lot matching with cost, date, and label specifications.
// Beancount supports matching lots by:
// - Cost only: {100 USD}
// - Cost + date: {100 USD, 2024-01-01}
// - Cost + label: {100 USD, "batch-1"}
// - All three: {100 USD, 2024-01-01, "batch-1"}
func TestLotMatching(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *Ledger)
	}{
		{
			name: "match by cost only: {100 USD}",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy stock"
				  Assets:Brokerage    10 STOCK {100 USD}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Sell specific lot by cost"
				  Assets:Brokerage    -5 STOCK {100 USD}
				  Assets:Cash         500 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].Amount.String())
			},
		},
		{
			name: "match by cost + date: {100 USD, 2020-01-02}",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy lot 1"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-02}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Buy lot 2 at same price but different date"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-03}
				  Assets:Cash        -1000 USD

				2020-01-04 * "Sell from specific dated lot"
				  Assets:Brokerage    -5 STOCK {100 USD, 2020-01-02}
				  Assets:Cash         500 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				assert.Equal(t, 2, len(lots))
			},
		},
		{
			name: "match by cost + label: {100 USD, 2020-01-02, \"batch-1\"}",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy batch 1"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-02, "batch-1"}
				  Assets:Cash        -1000 USD

				2020-01-02 * "Buy batch 2"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-02, "batch-2"}
				  Assets:Cash        -1000 USD

				2020-01-04 * "Sell from batch 1"
				  Assets:Brokerage    -5 STOCK {100 USD, 2020-01-02, "batch-1"}
				  Assets:Cash         500 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				assert.Equal(t, 2, len(lots))
			},
		},
		{
			name: "match by all three: {100 USD, 2020-01-02, \"batch-1\"}",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy specific lot"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-02, "batch-1"}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Buy different lot same price"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-03, "batch-2"}
				  Assets:Cash        -1000 USD

				2020-01-04 * "Sell exact lot match"
				  Assets:Brokerage    -5 STOCK {100 USD, 2020-01-02, "batch-1"}
				  Assets:Cash         500 USD
			`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				assert.Equal(t, 2, len(lots))
			},
		},
		{
			name: "lot not found - wrong cost",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy stock"
				  Assets:Brokerage    10 STOCK {100 USD}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Try to sell at wrong cost"
				  Assets:Brokerage    -5 STOCK {110 USD}
				  Assets:Cash         550 USD
			`,
			wantErr: true,
		},
		{
			name: "lot not found - wrong date",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy stock"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-02}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Try to sell with wrong date"
				  Assets:Brokerage    -5 STOCK {100 USD, 2020-01-03}
				  Assets:Cash         500 USD
			`,
			wantErr: true,
		},
		{
			name: "lot not found - wrong label",
			input: `
				2020-01-01 open Assets:Brokerage
				2020-01-01 open Assets:Cash USD

				2020-01-02 * "Buy stock"
				  Assets:Brokerage    10 STOCK {100 USD, 2020-01-02, "batch-1"}
				  Assets:Cash        -1000 USD

				2020-01-03 * "Try to sell with wrong label"
				  Assets:Brokerage    -5 STOCK {100 USD, 2020-01-02, "batch-2"}
				  Assets:Cash         500 USD
			`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parsing should succeed")

			l := New()
			err = l.Process(context.Background(), ast)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.check != nil {
					tt.check(t, l)
				}
			}
		})
	}
}
