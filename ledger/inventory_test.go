package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

// testPosting parses a posting to Assets:A, written without its account.
func testPosting(t *testing.T, posting string) *ast.Posting {
	t.Helper()
	tree, err := parser.ParseString(context.Background(), "2024-01-01 *\n  Assets:A  "+posting+"\n")
	assert.NoError(t, err)
	return tree.Directives[0].(*ast.Transaction).Postings[0]
}

// holding returns an inventory that augmented the postings in a transaction
// dated date.
func holding(t *testing.T, date string, postings ...string) *Inventory {
	t.Helper()
	inv := NewInventory()
	for _, posting := range postings {
		inv.augment(testPosting(t, posting), newTestDate(date))
	}
	return inv
}

// TestBookDecidesReductionOrAugmentation pins the one place a posting is
// found to reduce or to augment: like beancount's book_reductions, a
// posting at cost with known units reduces when its account books with a
// method other than NONE and the inventory holds its commodity with the
// opposite sign. book books a reduction at once and leaves an augmentation,
// and the inventory, untouched.
func TestBookDecidesReductionOrAugmentation(t *testing.T) {
	tests := []struct {
		name    string
		held    []string
		posting string
		method  BookingMethod
		reduces bool
	}{
		{name: "an opposite-signed lot is reduced", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL {5 USD}", method: BookingSTRICT, reduces: true},
		{name: "a short lot is reduced by a positive posting", held: []string{"-10 HOOL {5 USD}"}, posting: "4 HOOL {}", method: BookingFIFO, reduces: true},
		{name: "the default method books reductions", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL {}", reduces: true},
		{name: "a same-signed posting augments", held: []string{"10 HOOL {5 USD}"}, posting: "4 HOOL {6 USD}", method: BookingSTRICT},
		{name: "selling what is not held opens a short lot", posting: "-3 HOOL {5 USD}", method: BookingSTRICT},
		{name: "another commodity is not reduced", held: []string{"10 HOOL {5 USD}"}, posting: "-3 ACME {5 USD}", method: BookingFIFO},
		{name: "NONE never reduces", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL {5 USD}", method: BookingNONE},
		{name: "units still to be interpolated augment", held: []string{"10 HOOL {5 USD}"}, posting: "HOOL {5 USD}", method: BookingSTRICT},
		{name: "a posting without cost augments", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL", method: BookingSTRICT},
		{name: "zero units augment", held: []string{"10 HOOL {5 USD}"}, posting: "0 HOOL {5 USD}", method: BookingSTRICT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := holding(t, "2024-01-01", tt.held...)
			before := inv.String()

			positions, reduced, err := inv.book(testPosting(t, tt.posting), tt.method)
			assert.NoError(t, err)
			assert.Equal(t, tt.reduces, reduced)
			if !tt.reduces {
				assert.Zero(t, positions)
				assert.Equal(t, before, inv.String(), "an augmentation waits for its transaction to be booked")
				return
			}
			assert.NotZero(t, positions)
			for _, position := range positions {
				assert.True(t, position.Reduced)
			}
			assert.NotEqual(t, before, inv.String())
		})
	}
}

func TestBookReducesTheLotsItsMethodPicks(t *testing.T) {
	at := func(units, number, date, label string) BookedPosition {
		return BookedPosition{
			Units:   mustParseDec(units),
			Cost:    &BookedCost{Number: mustParseDec(number), Currency: "USD", Date: newTestDate(date), Label: label},
			Reduced: true,
		}
	}
	threeLots := []string{"30 STOCK {1 USD, 2024-01-15}", "40 STOCK {1 USD, 2024-02-15}", "50 STOCK {1 USD, 2024-03-15}"}
	sameDate := []string{"50 STOCK {1 USD, 2024-01-15}", "60 STOCK {2 USD, 2024-01-15}"}

	tests := []struct {
		name    string
		held    []string
		posting string
		method  BookingMethod
		want    []BookedPosition
		wantErr string
	}{
		{
			name: "FIFO reduces the oldest lot first", method: BookingFIFO,
			held: threeLots, posting: "-20 STOCK {}",
			want: []BookedPosition{at("-20", "1", "2024-01-15", "")},
		},
		{
			name: "FIFO spans lots", method: BookingFIFO,
			held: threeLots, posting: "-60 STOCK {}",
			want: []BookedPosition{at("-30", "1", "2024-01-15", ""), at("-30", "1", "2024-02-15", "")},
		},
		{
			name: "FIFO keeps the order of lots of one date", method: BookingFIFO,
			held: sameDate, posting: "-80 STOCK {}",
			want: []BookedPosition{at("-50", "1", "2024-01-15", ""), at("-30", "2", "2024-01-15", "")},
		},
		{
			name: "LIFO reduces the newest lot first", method: BookingLIFO,
			held: threeLots, posting: "-40 STOCK {}",
			want: []BookedPosition{at("-40", "1", "2024-03-15", "")},
		},
		{
			name: "LIFO spans lots", method: BookingLIFO,
			held: threeLots, posting: "-60 STOCK {}",
			want: []BookedPosition{at("-50", "1", "2024-03-15", ""), at("-10", "1", "2024-02-15", "")},
		},
		{
			name: "LIFO keeps the order of lots of one date", method: BookingLIFO,
			held: sameDate, posting: "-80 STOCK {}",
			want: []BookedPosition{at("-50", "1", "2024-01-15", ""), at("-30", "2", "2024-01-15", "")},
		},
		{
			name: "HIFO reduces the costliest lot first", method: BookingHIFO,
			held:    []string{"10 STOCK {100 USD, 2024-01-15}", "10 STOCK {200 USD, 2024-02-15}", "10 STOCK {150 USD, 2024-03-15}"},
			posting: "-15 STOCK {}",
			want:    []BookedPosition{at("-10", "200", "2024-02-15", ""), at("-5", "150", "2024-03-15", "")},
		},
		{
			name: "a spec narrows the lots FIFO books", method: BookingFIFO,
			held:    []string{"50 STOCK {100 USD, 2024-01-15}", "30 STOCK {100 USD, 2024-02-15}"},
			posting: "-20 STOCK {100 USD, 2024-02-15}",
			want:    []BookedPosition{at("-20", "100", "2024-02-15", "")},
		},
		{
			name: "FIFO cannot book more than the lots hold", method: BookingFIFO,
			held: threeLots, posting: "-200 STOCK {}",
			wantErr: "not enough lots to reduce",
		},
		{
			name: "FIFO finds no lot the spec names", method: BookingFIFO,
			held: []string{"50 STOCK {100 USD, 2024-01-15}"}, posting: "-30 STOCK {200 USD}",
			wantErr: "lot not found",
		},
		{
			name: "STRICT reduces the one lot the spec names", method: BookingSTRICT,
			held:    []string{"50 STOCK {100 USD, 2024-01-15}", "30 STOCK {200 USD, 2024-01-15}"},
			posting: "-20 STOCK {100 USD}",
			want:    []BookedPosition{at("-20", "100", "2024-01-15", "")},
		},
		{
			name: "STRICT matches a label", method: BookingSTRICT,
			held:    []string{`10 STOCK {100 USD, 2024-01-15, "a"}`, `10 STOCK {100 USD, 2024-01-15, "b"}`},
			posting: `-5 STOCK {"b"}`,
			want:    []BookedPosition{at("-5", "100", "2024-01-15", "b")},
		},
		{
			name: "STRICT reduces every matching lot in full", method: BookingSTRICT,
			held:    []string{"50 STOCK {10 USD, 2024-01-15}", "60 STOCK {10 USD, 2024-02-15}"},
			posting: "-110 STOCK {}",
			want:    []BookedPosition{at("-50", "10", "2024-01-15", ""), at("-60", "10", "2024-02-15", "")},
		},
		{
			name: "STRICT cannot choose among lots it would reduce in part", method: BookingSTRICT,
			held:    []string{"50 STOCK {10 USD, 2024-01-15}", "60 STOCK {10 USD, 2024-02-15}"},
			posting: "-40 STOCK {}",
			wantErr: "ambiguous matches",
		},
		{
			name: "STRICT cannot book more than the lot holds", method: BookingSTRICT,
			held: []string{"10 STOCK {100 USD, 2024-01-15}"}, posting: "-20 STOCK {100 USD}",
			wantErr: "not enough lots to reduce",
		},
		{
			name: "STRICT finds no lot the spec names", method: BookingSTRICT,
			held: []string{"10 STOCK {100 USD, 2024-01-15}"}, posting: "-5 STOCK {200 USD}",
			wantErr: "lot not found",
		},
		{
			name: "a total cost names the lot at its per-unit cost", method: BookingSTRICT,
			held: []string{"10 STOCK {5 USD, 2024-01-15}"}, posting: "-4 STOCK {{20 USD}}",
			want: []BookedPosition{at("-4", "5", "2024-01-15", "")},
		},
		{
			name: "a compound cost names the lot at its per-unit cost", method: BookingSTRICT,
			held: []string{"10 STOCK {5 USD, 2024-01-15}"}, posting: "-2 STOCK {3 # 4 USD}",
			want: []BookedPosition{at("-2", "5", "2024-01-15", "")},
		},
		{
			name: "a short lot is covered", method: BookingFIFO,
			held: []string{"-10 STOCK {5 USD, 2024-01-15}"}, posting: "4 STOCK {}",
			want: []BookedPosition{at("4", "5", "2024-01-15", "")},
		},
		{
			name: "AVERAGE fails every reduction, like beancount v2", method: BookingAVERAGE,
			held: []string{"5 STOCK {100 USD, 2024-01-15}"}, posting: "-5 STOCK {}",
			wantErr: "AVERAGE method is not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := holding(t, "2024-01-01", tt.held...)
			positions, _, err := inv.book(testPosting(t, tt.posting), tt.method)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.HasPrefix(t, err.Error(), tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, positions)
		})
	}
}

func TestBookLeavesTheInventoryUnchangedOnFailure(t *testing.T) {
	inv := holding(t, "2024-01-01", "10 STOCK {1 USD, 2024-01-15}", "20 STOCK {1 USD, 2024-02-15}")
	before := inv.String()

	_, _, err := inv.book(testPosting(t, "-40 STOCK {}"), BookingFIFO)
	assert.Error(t, err)
	assert.Equal(t, before, inv.String())
}

func TestBookSkipsLotsWithoutCost(t *testing.T) {
	// Like beancount's book_reductions, a cost spec never books against units
	// held without cost, although they still make the posting a reduction.
	for _, method := range []BookingMethod{BookingSTRICT, BookingFIFO, BookingLIFO, BookingHIFO} {
		t.Run(string(method), func(t *testing.T) {
			inv := holding(t, "2024-01-01", "1 HOOL {10 USD, 2024-01-15}", "1 HOOL {12 USD, 2024-02-15}", "1 HOOL")

			positions, _, err := inv.book(testPosting(t, "-2 HOOL {}"), method)
			assert.NoError(t, err)
			assert.Equal(t, 2, len(positions))
			assert.Equal(t, "1", inv.Get("HOOL").String(), "the units without cost remain")
		})
	}

	inv := holding(t, "2024-01-01", "3 HOOL")
	_, _, err := inv.book(testPosting(t, "-1 HOOL {}"), BookingFIFO)
	assert.Error(t, err)
}

func TestBookShortPositionAtCost(t *testing.T) {
	for _, method := range []BookingMethod{BookingSTRICT, BookingFIFO, BookingLIFO, BookingHIFO} {
		t.Run(string(method), func(t *testing.T) {
			inv := NewInventory()

			// Selling without holdings augments: it opens a short lot, dated
			// like an acquisition.
			short := testPosting(t, "-3 HOOL {10 USD}")
			positions, reduced, err := inv.book(short, method)
			assert.NoError(t, err)
			assert.False(t, reduced)
			assert.Zero(t, positions)
			inv.augment(short, newTestDate("2024-01-15"))
			assert.Equal(t, "{-3 HOOL {10 USD, 2024-01-15}}", inv.String())

			// A positive posting now reduces the short lot instead of adding a lot.
			_, _, err = inv.book(testPosting(t, "2 HOOL {10 USD}"), method)
			assert.NoError(t, err)
			assert.Equal(t, "{-1 HOOL {10 USD, 2024-01-15}}", inv.String())

			_, _, err = inv.book(testPosting(t, "1 HOOL {}"), method)
			assert.NoError(t, err)
			assert.True(t, inv.IsEmpty())
		})
	}
}

func TestAugmentAddsTheLotItsSpecNames(t *testing.T) {
	at := func(number, date, label string) *BookedCost {
		return &BookedCost{Number: mustParseDec(number), Currency: "USD", Date: newTestDate(date), Label: label}
	}
	tests := []struct {
		name    string
		held    []string
		posting string
		want    []BookedPosition
	}{
		{
			name:    "a lot without a date is dated by its transaction",
			posting: "10 AA {5.0 USD}",
			want:    []BookedPosition{{Units: mustParseDec("10"), Cost: at("5.0", "2024-03-01", "")}},
		},
		{
			name:    "a dated lot keeps its date",
			posting: "10 AA {5 USD, 2024-01-01}",
			want:    []BookedPosition{{Units: mustParseDec("10"), Cost: at("5", "2024-01-01", "")}},
		},
		{
			name:    "a labelled lot keeps its label",
			posting: `1 DD {7 USD, "lbl"}`,
			want:    []BookedPosition{{Units: mustParseDec("1"), Cost: at("7", "2024-03-01", "lbl")}},
		},
		{
			name:    "a total cost is spread over the units",
			posting: "4 BB {{20 USD}}",
			want:    []BookedPosition{{Units: mustParseDec("4"), Cost: at("5", "2024-03-01", "")}},
		},
		{
			name:    "a compound cost adds its total spread over the units",
			posting: "2 CC {3 # 4 USD}",
			want:    []BookedPosition{{Units: mustParseDec("2"), Cost: at("5", "2024-03-01", "")}},
		},
		{
			name:    "a short lot opens like any other",
			posting: "-3 HOOL {10 USD}",
			want:    []BookedPosition{{Units: mustParseDec("-3"), Cost: at("10", "2024-03-01", "")}},
		},
		{
			name:    "units without cost are held alone",
			posting: "49.00 USD",
			want:    []BookedPosition{{Units: mustParseDec("49.00")}},
		},
		{
			name:    "the opposite sign of a held lot reduces it",
			held:    []string{"-4 YY {7 USD, 2024-02-01}"},
			posting: "3 YY {7 USD, 2024-02-01}",
			want:    []BookedPosition{{Units: mustParseDec("3"), Cost: at("7", "2024-02-01", ""), Reduced: true}},
		},
		{
			name:    "a lot of zero units is not held",
			held:    []string{"0 PP {5 USD, 2024-01-07}"},
			posting: "-5 PP {5 USD, 2024-01-07}",
			want:    []BookedPosition{{Units: mustParseDec("-5"), Cost: at("5", "2024-01-07", "")}},
		},
		{name: "a cost without a number holds nothing", posting: "5 FF {USD}"},
		{name: "units without a number hold nothing", posting: "GG {5 USD}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := holding(t, "2024-01-01", tt.held...)
			positions := inv.augment(testPosting(t, tt.posting), newTestDate("2024-03-01"))
			assert.Equal(t, tt.want, positions)
		})
	}
}

func TestInventoryStringSortsCommodities(t *testing.T) {
	inv := holding(t, "2024-01-01", "10 USD", "20 EUR")

	assert.Equal(t, "{20 EUR, 10 USD}", inv.String())
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
  Income:CapitalGains    -100 USD
`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				// Should have 5 shares left from lot 2 at 110 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].Amount.String())
				assert.Equal(t, "110", lots[0].Spec.Cost.String())
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
  Assets:Cash         1650 USD
  Income:CapitalGains    -50 USD
`,
			wantErr: false,
			check: func(t *testing.T, l *Ledger) {
				acc, ok := l.GetAccount("Assets:Brokerage")
				assert.True(t, ok)
				lots := acc.Inventory.GetLots("STOCK")
				// Should have 5 shares left from lot 1 at 100 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].Amount.String())
				assert.Equal(t, "100", lots[0].Spec.Cost.String())
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
  Income:CapitalGains    -25 USD
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
