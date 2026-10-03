package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
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
func holding(t *testing.T, date string, postings ...string) *inventory {
	t.Helper()
	inv := newInventory()
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
		method  bookingMethod
		reduces bool
	}{
		{name: "an opposite-signed lot is reduced", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL {5 USD}", method: bookingSTRICT, reduces: true},
		{name: "a short lot is reduced by a positive posting", held: []string{"-10 HOOL {5 USD}"}, posting: "4 HOOL {}", method: bookingFIFO, reduces: true},
		{name: "the default method books reductions", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL {}", reduces: true},
		{name: "a same-signed posting augments", held: []string{"10 HOOL {5 USD}"}, posting: "4 HOOL {6 USD}", method: bookingSTRICT},
		{name: "selling what is not held opens a short lot", posting: "-3 HOOL {5 USD}", method: bookingSTRICT},
		{name: "another commodity is not reduced", held: []string{"10 HOOL {5 USD}"}, posting: "-3 ACME {5 USD}", method: bookingFIFO},
		{name: "NONE never reduces", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL {5 USD}", method: bookingNONE},
		{name: "units still to be interpolated augment", held: []string{"10 HOOL {5 USD}"}, posting: "HOOL {5 USD}", method: bookingSTRICT},
		{name: "a posting without cost augments", held: []string{"10 HOOL {5 USD}"}, posting: "-4 HOOL", method: bookingSTRICT},
		{name: "zero units augment", held: []string{"10 HOOL {5 USD}"}, posting: "0 HOOL {5 USD}", method: bookingSTRICT},
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

// TestHoldsOtherSignAgreesWithTheLots checks the per-sign lot counts that
// decide a reduction without a scan against a scan of the lots, as lots are
// added, reduced, emptied and cloned. Like beancount's same_sign, zero units
// count as non-negative.
func TestHoldsOtherSignAgreesWithTheLots(t *testing.T) {
	scan := func(inv *inventory, commodity string, units decimal.Decimal) bool {
		for _, lot := range inv.lots[commodity] {
			if lot.amount.IsNegative() != units.IsNegative() {
				return true
			}
		}
		return false
	}
	check := func(inv *inventory) {
		t.Helper()
		for _, commodity := range []string{"HOOL", "ACME"} {
			for _, units := range []string{"-1", "0", "1"} {
				assert.Equal(t, scan(inv, commodity, mustParseDec(units)), inv.holdsOtherSign(commodity, mustParseDec(units)), commodity+" "+units)
			}
		}
	}

	inv := holding(t, "2024-01-01", "10 HOOL {5 USD}", "2 HOOL {6 USD}", "0 ACME {1 USD}")
	check(inv)
	_, reduced, err := inv.book(testPosting(t, "-12 HOOL {}"), bookingFIFO)
	assert.NoError(t, err)
	assert.True(t, reduced)
	check(inv)
	inv.augment(testPosting(t, "-3 HOOL {7 USD}"), newTestDate("2024-01-02"))
	check(inv)
	cloned := inv.clone()
	cloned.augment(testPosting(t, "3 HOOL {7 USD}"), newTestDate("2024-01-02"))
	check(cloned)
	check(inv)
	assert.True(t, inv.holdsOtherSign("HOOL", mustParseDec("1")))
	assert.False(t, cloned.holdsOtherSign("HOOL", mustParseDec("1")))
}

// TestZeroUnitsHoldNoLot checks that, like beancount's add_amount, zero
// units leave no lot, so a later sale opens a short lot (#600).
func TestZeroUnitsHoldNoLot(t *testing.T) {
	inv := holding(t, "2024-01-01", "0 HOOL {5 USD}")
	assert.True(t, len(inv.lots) == 0)

	positions, reduced, err := inv.book(testPosting(t, "-1 HOOL {5 USD}"), bookingSTRICT)
	assert.NoError(t, err)
	assert.False(t, reduced)
	assert.Zero(t, positions)
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
		method  bookingMethod
		want    []BookedPosition
		wantErr string
	}{
		{
			name: "FIFO reduces the oldest lot first", method: bookingFIFO,
			held: threeLots, posting: "-20 STOCK {}",
			want: []BookedPosition{at("-20", "1", "2024-01-15", "")},
		},
		{
			name: "FIFO spans lots", method: bookingFIFO,
			held: threeLots, posting: "-60 STOCK {}",
			want: []BookedPosition{at("-30", "1", "2024-01-15", ""), at("-30", "1", "2024-02-15", "")},
		},
		{
			name: "FIFO keeps the order of lots of one date", method: bookingFIFO,
			held: sameDate, posting: "-80 STOCK {}",
			want: []BookedPosition{at("-50", "1", "2024-01-15", ""), at("-30", "2", "2024-01-15", "")},
		},
		{
			name: "LIFO reduces the newest lot first", method: bookingLIFO,
			held: threeLots, posting: "-40 STOCK {}",
			want: []BookedPosition{at("-40", "1", "2024-03-15", "")},
		},
		{
			name: "LIFO spans lots", method: bookingLIFO,
			held: threeLots, posting: "-60 STOCK {}",
			want: []BookedPosition{at("-50", "1", "2024-03-15", ""), at("-10", "1", "2024-02-15", "")},
		},
		{
			name: "LIFO keeps the order of lots of one date", method: bookingLIFO,
			held: sameDate, posting: "-80 STOCK {}",
			want: []BookedPosition{at("-50", "1", "2024-01-15", ""), at("-30", "2", "2024-01-15", "")},
		},
		{
			name: "HIFO reduces the costliest lot first", method: bookingHIFO,
			held:    []string{"10 STOCK {100 USD, 2024-01-15}", "10 STOCK {200 USD, 2024-02-15}", "10 STOCK {150 USD, 2024-03-15}"},
			posting: "-15 STOCK {}",
			want:    []BookedPosition{at("-10", "200", "2024-02-15", ""), at("-5", "150", "2024-03-15", "")},
		},
		{
			name: "a spec narrows the lots FIFO books", method: bookingFIFO,
			held:    []string{"50 STOCK {100 USD, 2024-01-15}", "30 STOCK {100 USD, 2024-02-15}"},
			posting: "-20 STOCK {100 USD, 2024-02-15}",
			want:    []BookedPosition{at("-20", "100", "2024-02-15", "")},
		},
		{
			name: "FIFO cannot book more than the lots hold", method: bookingFIFO,
			held: threeLots, posting: "-200 STOCK {}",
			wantErr: `Not enough lots to reduce "-200 STOCK {}": 30 STOCK {1 USD, 2024-01-15}, 40 STOCK {1 USD, 2024-02-15}, 50 STOCK {1 USD, 2024-03-15}`,
		},
		{
			name: "FIFO finds no lot the spec names", method: bookingFIFO,
			held: []string{"50 STOCK {100 USD, 2024-01-15}"}, posting: "-30 STOCK {200 USD}",
			wantErr: `No position matches "-30 STOCK {200 USD}" against balance (50 STOCK {100 USD, 2024-01-15})`,
		},
		{
			name: "STRICT reduces the one lot the spec names", method: bookingSTRICT,
			held:    []string{"50 STOCK {100 USD, 2024-01-15}", "30 STOCK {200 USD, 2024-01-15}"},
			posting: "-20 STOCK {100 USD}",
			want:    []BookedPosition{at("-20", "100", "2024-01-15", "")},
		},
		{
			name: "STRICT matches a label", method: bookingSTRICT,
			held:    []string{`10 STOCK {100 USD, 2024-01-15, "a"}`, `10 STOCK {100 USD, 2024-01-15, "b"}`},
			posting: `-5 STOCK {"b"}`,
			want:    []BookedPosition{at("-5", "100", "2024-01-15", "b")},
		},
		{
			name: "STRICT reduces every matching lot in full", method: bookingSTRICT,
			held:    []string{"50 STOCK {10 USD, 2024-01-15}", "60 STOCK {10 USD, 2024-02-15}"},
			posting: "-110 STOCK {}",
			want:    []BookedPosition{at("-50", "10", "2024-01-15", ""), at("-60", "10", "2024-02-15", "")},
		},
		{
			name: "STRICT cannot choose among lots it would reduce in part", method: bookingSTRICT,
			held:    []string{"50 STOCK {10 USD, 2024-01-15}", "60 STOCK {10 USD, 2024-02-15}"},
			posting: "-40 STOCK {}",
			wantErr: `Ambiguous matches for "-40 STOCK {}": 50 STOCK {10 USD, 2024-01-15}, 60 STOCK {10 USD, 2024-02-15}`,
		},
		{
			name: "STRICT cannot book more than the lot holds", method: bookingSTRICT,
			held:    []string{"10 STOCK {100 USD, 2024-01-15}", "10 STOCK {200 USD, 2024-01-15}"},
			posting: "-20 STOCK {100 USD}",
			wantErr: `Not enough lots to reduce "-20 STOCK {100 USD}": 10 STOCK {100 USD, 2024-01-15}`,
		},
		{
			name: "STRICT finds no lot the spec names", method: bookingSTRICT,
			held: []string{"10 STOCK {100 USD, 2024-01-15}"}, posting: "-5 STOCK {200 USD}",
			wantErr: `No position matches "-5 STOCK {200 USD}" against balance (10 STOCK {100 USD, 2024-01-15})`,
		},
		{
			name: "STRICT cannot choose among lots too small together", method: bookingSTRICT,
			held:    []string{"50 STOCK {10 USD, 2024-01-15}", "60 STOCK {10 USD, 2024-02-15}"},
			posting: "-200 STOCK {}",
			wantErr: `Ambiguous matches for "-200 STOCK {}": 50 STOCK {10 USD, 2024-01-15}, 60 STOCK {10 USD, 2024-02-15}`,
		},
		{
			name: "STRICT_WITH_SIZE books the lot of the reduction's size", method: bookingSTRICTWithSize,
			held:    []string{"10 STOCK {1 USD, 2024-01-15}", "5 STOCK {2 USD, 2024-02-15}"},
			posting: "-5 STOCK {}",
			want:    []BookedPosition{at("-5", "2", "2024-02-15", "")},
		},
		{
			name: "STRICT_WITH_SIZE books the oldest lot of the size", method: bookingSTRICTWithSize,
			held:    []string{"5 STOCK {1 USD, 2024-03-15}", "10 STOCK {3 USD, 2024-01-15}", "5 STOCK {2 USD, 2024-02-15}"},
			posting: "-5 STOCK {}",
			want:    []BookedPosition{at("-5", "2", "2024-02-15", "")},
		},
		{
			name: "STRICT_WITH_SIZE books the first of the oldest", method: bookingSTRICTWithSize,
			held:    []string{"5 STOCK {1 USD, 2024-01-15}", "5 STOCK {2 USD, 2024-01-15}"},
			posting: "-5 STOCK {}",
			want:    []BookedPosition{at("-5", "1", "2024-01-15", "")},
		},
		{
			name: "STRICT_WITH_SIZE covers a short position of the size", method: bookingSTRICTWithSize,
			held:    []string{"-10 STOCK {5 USD, 2024-01-15}", "-4 STOCK {6 USD, 2024-02-15}"},
			posting: "4 STOCK {}",
			want:    []BookedPosition{at("4", "6", "2024-02-15", "")},
		},
		{
			name: "STRICT_WITH_SIZE books as STRICT first", method: bookingSTRICTWithSize,
			held:    []string{"5 STOCK {10 USD, 2024-01-15}", "5 STOCK {10 USD, 2024-02-15}"},
			posting: "-10 STOCK {}",
			want:    []BookedPosition{at("-5", "10", "2024-01-15", ""), at("-5", "10", "2024-02-15", "")},
		},
		{
			name: "STRICT_WITH_SIZE reports STRICT's error without a lot of the size", method: bookingSTRICTWithSize,
			held:    []string{"50 STOCK {10 USD, 2024-01-15}", "60 STOCK {10 USD, 2024-02-15}"},
			posting: "-40 STOCK {}",
			wantErr: `Ambiguous matches for "-40 STOCK {}": 50 STOCK {10 USD, 2024-01-15}, 60 STOCK {10 USD, 2024-02-15}`,
		},
		{
			name: "STRICT_WITH_SIZE leaves one lot too small to STRICT", method: bookingSTRICTWithSize,
			held:    []string{"10 STOCK {100 USD, 2024-01-15}"},
			posting: "-20 STOCK {}",
			wantErr: `Not enough lots to reduce "-20 STOCK {}": 10 STOCK {100 USD, 2024-01-15}`,
		},
		{
			name: "a labelled lot too small", method: bookingSTRICT,
			held:    []string{`10 STOCK {100 USD, 2024-01-15, "a"}`, `10 STOCK {100 USD, 2024-01-15, "b"}`},
			posting: `-15 STOCK {"a"}`,
			wantErr: `Not enough lots to reduce "-15 STOCK {"a"}": 10 STOCK {100 USD, 2024-01-15, "a"}`,
		},
		{
			name: "a short position covered ambiguously", method: bookingSTRICT,
			held:    []string{"-10 STOCK {5 USD, 2024-01-15}", "-10 STOCK {6 USD, 2024-01-15}"},
			posting: "4 STOCK {}",
			wantErr: `Ambiguous matches for "4 STOCK {}": -10 STOCK {5 USD, 2024-01-15}, -10 STOCK {6 USD, 2024-01-15}`,
		},
		{
			name: "no lot matches a dated spec", method: bookingSTRICT,
			held:    []string{"10 STOCK {100 USD, 2024-01-15}"},
			posting: "-5.50 STOCK {100.00 USD, 2024-01-16}",
			wantErr: `No position matches "-5.50 STOCK {100.00 USD, 2024-01-16}" against balance (10 STOCK {100 USD, 2024-01-15})`,
		},
		{
			name: "no lot matches a total cost, among other currencies", method: bookingFIFO,
			held:    []string{"7 USD", "10 STOCK {100 USD, 2024-01-15}", "2 AAPL {3 USD, 2024-01-15}"},
			posting: "-5 STOCK {{65 USD}}",
			wantErr: `No position matches "-5 STOCK {0 # 65 USD}" against balance (7 USD, 2 AAPL {3 USD, 2024-01-15}, 10 STOCK {100 USD, 2024-01-15})`,
		},
		{
			name: "no lot is held at cost", method: bookingFIFO,
			held:    []string{"10 STOCK"},
			posting: "-1 STOCK {10 # 5 USD}",
			wantErr: `No position matches "-1 STOCK {10 # 5 USD}" against balance (10 STOCK)`,
		},
		{
			name: "a compound spec without its per-unit number", method: bookingSTRICT,
			held:    []string{"5 STOCK {10 USD, 2024-01-15}", "5 STOCK {11 USD, 2024-01-15}"},
			posting: "-3 STOCK {# 4 USD}",
			wantErr: `Ambiguous matches for "-3 STOCK {# 4 USD}": 5 STOCK {10 USD, 2024-01-15}, 5 STOCK {11 USD, 2024-01-15}`,
		},
		{
			name: "a compound spec without its total", method: bookingSTRICT,
			held:    []string{"5 STOCK {10 USD, 2024-01-15}", "5 STOCK {11 USD, 2024-01-15}"},
			posting: "-3 STOCK {4 # USD}",
			wantErr: `Ambiguous matches for "-3 STOCK {4 USD}": 5 STOCK {10 USD, 2024-01-15}, 5 STOCK {11 USD, 2024-01-15}`,
		},
		{
			name: "a total cost names the lot at its per-unit cost", method: bookingSTRICT,
			held: []string{"10 STOCK {5 USD, 2024-01-15}"}, posting: "-4 STOCK {{20 USD}}",
			want: []BookedPosition{at("-4", "5", "2024-01-15", "")},
		},
		{
			name: "a compound cost names the lot at its per-unit cost", method: bookingSTRICT,
			held: []string{"10 STOCK {5 USD, 2024-01-15}"}, posting: "-2 STOCK {3 # 4 USD}",
			want: []BookedPosition{at("-2", "5", "2024-01-15", "")},
		},
		{
			name: "a short lot is covered", method: bookingFIFO,
			held: []string{"-10 STOCK {5 USD, 2024-01-15}"}, posting: "4 STOCK {}",
			want: []BookedPosition{at("4", "5", "2024-01-15", "")},
		},
		{
			name: "AVERAGE fails every reduction, like beancount v2", method: bookingAVERAGE,
			held: []string{"5 STOCK {100 USD, 2024-01-15}"}, posting: "-5 STOCK {}",
			wantErr: "AVERAGE method is not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := holding(t, "2024-01-01", tt.held...)
			positions, _, err := inv.book(testPosting(t, tt.posting), tt.method)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
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

	_, _, err := inv.book(testPosting(t, "-40 STOCK {}"), bookingFIFO)
	assert.Error(t, err)
	assert.Equal(t, before, inv.String())
}

func TestBookSkipsLotsWithoutCost(t *testing.T) {
	// Like beancount's book_reductions, a cost spec never books against units
	// held without cost, although they still make the posting a reduction.
	for _, method := range []bookingMethod{bookingSTRICT, bookingFIFO, bookingLIFO, bookingHIFO} {
		t.Run(string(method), func(t *testing.T) {
			inv := holding(t, "2024-01-01", "1 HOOL {10 USD, 2024-01-15}", "1 HOOL {12 USD, 2024-02-15}", "1 HOOL")

			positions, _, err := inv.book(testPosting(t, "-2 HOOL {}"), method)
			assert.NoError(t, err)
			assert.Equal(t, 2, len(positions))
			assert.Equal(t, "1", inv.get("HOOL").String(), "the units without cost remain")
		})
	}

	inv := holding(t, "2024-01-01", "3 HOOL")
	_, _, err := inv.book(testPosting(t, "-1 HOOL {}"), bookingFIFO)
	assert.Error(t, err)
}

func TestBookShortPositionAtCost(t *testing.T) {
	for _, method := range []bookingMethod{bookingSTRICT, bookingFIFO, bookingLIFO, bookingHIFO} {
		t.Run(string(method), func(t *testing.T) {
			inv := newInventory()

			// Selling without holdings augments: it opens a short lot, dated
			// like an acquisition.
			short := testPosting(t, "-3 HOOL {10 USD}")
			positions, reduced, err := inv.book(short, method)
			assert.NoError(t, err)
			assert.False(t, reduced)
			assert.Zero(t, positions)
			inv.augment(short, newTestDate("2024-01-15"))
			assert.Equal(t, "(-3 HOOL {10 USD, 2024-01-15})", inv.String())

			// A positive posting now reduces the short lot instead of adding a lot.
			_, _, err = inv.book(testPosting(t, "2 HOOL {10 USD}"), method)
			assert.NoError(t, err)
			assert.Equal(t, "(-1 HOOL {10 USD, 2024-01-15})", inv.String())

			_, _, err = inv.book(testPosting(t, "1 HOOL {}"), method)
			assert.NoError(t, err)
			assert.True(t, len(inv.lots) == 0)
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

func TestInventoryStringIsBeancounts(t *testing.T) {
	// Like str(Inventory): positions sorted by Position.sortkey (major
	// currencies first, then by currency length, cost number, cost currency
	// and units), numbers with their own exponent.
	inv := holding(t, "2024-01-01", "20 EUR", "3.0 HOOL {12.50 USD}", "10 USD", "5 GOOG {1 EUR}", "5 HOOL {10 USD}")

	assert.Equal(t, "(10 USD, 20 EUR, 5 GOOG {1 EUR, 2024-01-01}, 5 HOOL {10 USD, 2024-01-01}, 3.0 HOOL {12.50 USD, 2024-01-01})", inv.String())
	assert.Equal(t, "()", newInventory().String())
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
				lots := acc.inventory.lots["STOCK"]
				// Should have 5 shares left from lot 2 at 110 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].amount.String())
				assert.Equal(t, "110", lots[0].spec.cost.String())
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
				lots := acc.inventory.lots["STOCK"]
				// Should have 5 shares left from lot 1 at 100 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].amount.String())
				assert.Equal(t, "100", lots[0].spec.cost.String())
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
				lots := acc.inventory.lots["STOCK"]
				// Should have 5 shares left from last lot at 110 USD
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].amount.String())
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
			_, err = processErr(context.Background(), l, ast)

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
				lots := acc.inventory.lots["STOCK"]
				assert.Equal(t, 1, len(lots))
				assert.Equal(t, "5", lots[0].amount.String())
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
				lots := acc.inventory.lots["STOCK"]
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
				lots := acc.inventory.lots["STOCK"]
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
				lots := acc.inventory.lots["STOCK"]
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
			_, err = processErr(context.Background(), l, ast)

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

// TestLotKeyAgreesWithEqual pins the index's lot identity to lotSpec.equal:
// two specs share a key exactly when they are the same lot.
func TestLotKeyAgreesWithEqual(t *testing.T) {
	dec := func(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }
	specs := map[string]*lotSpec{
		"none":        nil,
		"empty":       {},
		"100.0 USD":   {cost: dec("100.0"), costCurrency: "USD"},
		"100.00 USD":  {cost: dec("100.00"), costCurrency: "USD"},
		"100 EUR":     {cost: dec("100"), costCurrency: "EUR"},
		"101 USD":     {cost: dec("101"), costCurrency: "USD"},
		"0 USD":       {cost: dec("0"), costCurrency: "USD"},
		"0.00 USD":    {cost: dec("0.00"), costCurrency: "USD"},
		"USD":         {costCurrency: "USD"},
		"dated":       {cost: dec("100"), costCurrency: "USD", date: newTestDate("2024-01-01")},
		"dated again": {cost: dec("100.000"), costCurrency: "USD", date: newTestDate("2024-01-01")},
		"other date":  {cost: dec("100"), costCurrency: "USD", date: newTestDate("2024-01-02")},
		"labelled":    {cost: dec("100"), costCurrency: "USD", label: "a"},
		"other label": {cost: dec("100"), costCurrency: "USD", label: "b"},
		"label only":  {label: "a"},
		"date only":   {date: newTestDate("2024-01-01")},
		"all of them": {cost: dec("100"), costCurrency: "USD", date: newTestDate("2024-01-01"), label: "a"},
		"all, 100.0 ": {cost: dec("100.0"), costCurrency: "USD", date: newTestDate("2024-01-01"), label: "a"},
	}
	for an, a := range specs {
		for bn, b := range specs {
			assert.Equal(t, a.equal(b), a.key() == b.key(), "%s vs %s", an, bn)
		}
	}
}

// TestCloneHasItsOwnLotIndex checks that a scratch inventory's index points
// at its own lots: adding to or emptying a lot in the clone leaves the
// original's lots and lookups as they were.
func TestCloneHasItsOwnLotIndex(t *testing.T) {
	inv := holding(t, "2024-01-01", "10 HOOL {5 USD}", "10 HOOL {6 USD}", "10 HOOL {7 USD}")
	before := inv.String()

	scratch := inv.clone()
	scratch.augment(testPosting(t, "5 HOOL {6 USD}"), newTestDate("2024-01-01"))
	scratch.augment(testPosting(t, "-10 HOOL {5 USD, 2024-01-01}"), newTestDate("2024-01-01"))
	scratch.augment(testPosting(t, "1 HOOL {8 USD}"), newTestDate("2024-01-01"))
	assert.Equal(t, "(15 HOOL {6 USD, 2024-01-01}, 10 HOOL {7 USD, 2024-01-01}, 1 HOOL {8 USD, 2024-01-01})", scratch.String())

	assert.Equal(t, before, inv.String())
	inv.augment(testPosting(t, "1 HOOL {7 USD}"), newTestDate("2024-01-01"))
	assert.Equal(t, "(10 HOOL {5 USD, 2024-01-01}, 10 HOOL {6 USD, 2024-01-01}, 11 HOOL {7 USD, 2024-01-01})", inv.String())
}

// TestLotOrderSurvivesEmptyingALot checks that emptying a lot keeps the
// others in the order they were added, and that the index still finds each
// of them, so a lot added again goes last.
func TestLotOrderSurvivesEmptyingALot(t *testing.T) {
	inv := holding(t, "2024-01-01", "1 AA {1 USD}", "1 AA {2 USD}", "1 AA {3 USD}")
	inv.augment(testPosting(t, "-1 AA {1 USD, 2024-01-01}"), newTestDate("2024-01-01"))
	inv.augment(testPosting(t, "1 AA {3 USD}"), newTestDate("2024-01-01"))
	inv.augment(testPosting(t, "1 AA {1 USD}"), newTestDate("2024-01-01"))

	var order []string
	for _, lot := range inv.lots["AA"] {
		order = append(order, lot.String())
	}
	assert.Equal(t, []string{"1 AA {2 USD, 2024-01-01}", "2 AA {3 USD, 2024-01-01}", "1 AA {1 USD, 2024-01-01}"}, order)
}
