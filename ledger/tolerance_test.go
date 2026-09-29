package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/shopspring/decimal"
)

// toleranceOptions returns the default tolerance options changed by set.
func toleranceOptions(set func(*sharedconfig.Tolerance)) *sharedconfig.Tolerance {
	options := sharedconfig.NewTolerance()
	if set != nil {
		set(options)
	}
	return options
}

func withDefault(currency, tolerance string) func(*sharedconfig.Tolerance) {
	return func(o *sharedconfig.Tolerance) { o.Defaults[currency] = decimal.RequireFromString(tolerance) }
}

func withMultiplier(multiplier string) func(*sharedconfig.Tolerance) {
	return func(o *sharedconfig.Tolerance) { o.Multiplier = decimal.RequireFromString(multiplier) }
}

func fromCost(o *sharedconfig.Tolerance) { o.InferFromCost = true }

func both(sets ...func(*sharedconfig.Tolerance)) func(*sharedconfig.Tolerance) {
	return func(o *sharedconfig.Tolerance) {
		for _, set := range sets {
			set(o)
		}
	}
}

func units(value, currency string, opts ...ast.PostingOption) *ast.Posting {
	return ast.NewPosting("Assets:Cash", append([]ast.PostingOption{ast.WithAmount(value, currency)}, opts...)...)
}

func atCost(value, currency string) ast.PostingOption {
	return ast.WithCost(ast.NewCost(ast.NewAmount(value, currency)))
}

func atPrice(value, currency string) ast.PostingOption {
	return ast.WithPrice(ast.NewAmount(value, currency))
}

// TestTransactionTolerances pins a transaction's tolerance per currency to
// beancount's interpolate.infer_tolerances, whose results for these
// postings were taken from beancount 2.3.6.
func TestTransactionTolerances(t *testing.T) {
	tests := []struct {
		name     string
		options  func(*sharedconfig.Tolerance)
		postings []*ast.Posting
		want     map[string]string
	}{
		{
			name:     "default: half the last digit",
			postings: []*ast.Posting{units("24.45", "USD"), units("-24.45", "USD")},
			want:     map[string]string{"USD": "0.005", "EUR": "0"},
		},
		{
			name:     "high precision",
			postings: []*ast.Posting{units("10.22626", "RGAGX"), units("5.12345", "RGAGX")},
			want:     map[string]string{"RGAGX": "0.000005"},
		},
		{
			name:     "the coarsest precision wins",
			postings: []*ast.Posting{units("100.00", "USD"), units("-50.123", "USD")},
			want:     map[string]string{"USD": "0.005"},
		},
		{
			name:     "zeros count their precision",
			postings: []*ast.Posting{units("0.00", "USD"), units("0.000", "USD")},
			want:     map[string]string{"USD": "0.005"},
		},
		{
			name:     "whole numbers infer nothing",
			postings: []*ast.Posting{units("100", "USD"), units("-100", "USD")},
			want:     map[string]string{"USD": "0"},
		},
		{
			name:     "a posting without amount infers nothing",
			postings: []*ast.Posting{units("10.00", "USD"), ast.NewPosting("Assets:Other")},
			want:     map[string]string{"USD": "0.005"},
		},
		{
			name:     "inferred_tolerance_default * covers currencies without fractional numbers",
			options:  withDefault("*", "0.01"),
			postings: []*ast.Posting{units("100.00", "USD"), units("-100", "EUR")},
			want:     map[string]string{"USD": "0.005", "EUR": "0.01", "GBP": "0.01"},
		},
		{
			name:     "inferred_tolerance_default for a currency joins the maximum",
			options:  withDefault("USD", "0.02"),
			postings: []*ast.Posting{units("100.00", "USD"), units("-100.00", "USD")},
			want:     map[string]string{"USD": "0.02"},
		},
		{
			name:     "inferred_tolerance_default for a currency without fractional numbers",
			options:  withDefault("USD", "0.5"),
			postings: []*ast.Posting{units("100", "USD"), units("-100", "USD")},
			want:     map[string]string{"USD": "0.5"},
		},
		{
			name:     "a currency default comes before *",
			options:  both(withDefault("USD", "0.003"), withDefault("*", "0.005")),
			postings: []*ast.Posting{units("100", "USD"), units("-100", "CAD")},
			want:     map[string]string{"USD": "0.003", "CAD": "0.005"},
		},
		{
			name:     "inferred_tolerance_multiplier",
			options:  withMultiplier("0.6"),
			postings: []*ast.Posting{units("100.00", "USD"), units("-100.00", "USD")},
			want:     map[string]string{"USD": "0.006"},
		},
		{
			name:    "cost does not widen without infer_tolerance_from_cost",
			options: nil,
			postings: []*ast.Posting{
				units("18.572", "VWELX", atCost("30.96", "USD")),
				units("18.572", "VWELX", atCost("30.96", "USD")),
				units("-1150.00", "USD"),
			},
			want: map[string]string{"VWELX": "0.0005", "USD": "0.005"},
		},
		{
			name:    "infer_tolerance_from_cost: postings at cost add up",
			options: fromCost,
			postings: []*ast.Posting{
				units("18.572", "VWELX", atCost("30.96", "USD")),
				units("18.572", "VWELX", atCost("30.96", "USD")),
				units("-1150.00", "USD"),
			},
			want: map[string]string{"VWELX": "0.0005", "USD": "0.03096"},
		},
		{
			name:    "infer_tolerance_from_cost: a price widens its currency",
			options: fromCost,
			postings: []*ast.Posting{
				units("18.572", "VWELX", atPrice("30.96", "USD")),
				units("-575.00", "USD"),
			},
			want: map[string]string{"VWELX": "0.0005", "USD": "0.01548"},
		},
		{
			name:    "infer_tolerance_from_cost: one posting adds at most 0.5",
			options: fromCost,
			postings: []*ast.Posting{
				units("1.5", "HOOL", atCost("1000.00", "EUR")),
				units("-1500.00", "EUR"),
			},
			want: map[string]string{"HOOL": "0.05", "EUR": "0.5"},
		},
		{
			name:    "infer_tolerance_from_cost: whole units add nothing",
			options: fromCost,
			postings: []*ast.Posting{
				units("10", "HOOL", atCost("99", "CHF")),
				units("-990", "CHF"),
			},
			want: map[string]string{"CHF": "0"},
		},
		{
			name:    "infer_tolerance_from_cost follows the multiplier",
			options: both(fromCost, withMultiplier("1")),
			postings: []*ast.Posting{
				units("18.572", "VWELX", atCost("30.96", "USD")),
				units("-575.00", "USD"),
			},
			want: map[string]string{"VWELX": "0.001", "USD": "0.03096"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tolerances := newTolerances(toleranceOptions(tt.options))
			// Postings that Booking leaves unchanged look the same at
			// both stages.
			spec := tolerances.spec(tt.postings)
			booked := tolerances.booked(tt.postings, &TransactionDelta{}, nil)
			for currency, want := range tt.want {
				assert.Equal(t, want, spec.of(currency).String(), "spec %s", currency)
				assert.Equal(t, want, booked.of(currency).String(), "booked %s", currency)
			}
		})
	}
}

// TestBookedTolerances covers what Booking changes between the stages: an
// interpolated amount counts once booked, and a cost counts per unit, with
// an inferred cost resolved and a reduction split per lot.
func TestBookedTolerances(t *testing.T) {
	d := decimal.RequireFromString
	tolerances := newTolerances(toleranceOptions(fromCost))

	auto := ast.NewPosting("Assets:Cash")
	postings := []*ast.Posting{units("10", "USD"), auto}
	delta := &TransactionDelta{InferredAmounts: map[*ast.Posting]*ast.Amount{auto: ast.NewAmount("-10.00", "USD")}}
	assert.Equal(t, "0", tolerances.spec(postings).of("USD").String())
	assert.Equal(t, "0.005", tolerances.booked(postings, delta, nil).of("USD").String())

	total := units("18.572", "VWELX", ast.WithCost(&ast.Cost{IsTotal: true, Amount: ast.NewAmount("575.00", "USD")}))
	postings = []*ast.Posting{total, units("-575", "USD")}
	assert.Equal(t, "0.2875", tolerances.spec(postings).of("USD").String(), "a total cost counts in full")
	assert.Equal(t, "0.01548", tolerances.booked(postings, &TransactionDelta{}, nil).of("USD").Round(5).String(), "and per unit once booked")

	empty := units("1.5", "HOOL", ast.WithCost(&ast.Cost{}))
	postings = []*ast.Posting{empty, units("-150", "USD")}
	delta = &TransactionDelta{InferredCosts: map[*ast.Posting]*ast.Cost{empty: {Amount: ast.NewAmount("100", "USD"), Inferred: true}}}
	assert.Equal(t, "0", tolerances.spec(postings).of("USD").String(), "an empty cost has no currency")
	assert.Equal(t, "0.5", tolerances.booked(postings, delta, nil).of("USD").String(), "an inferred cost counts")

	currencyOnly := units("1.5", "HOOL", ast.WithCost(ast.NewCost(ast.NewAmount("", "JPY"))))
	postings = []*ast.Posting{currencyOnly, units("-150", "JPY")}
	assert.Equal(t, "0.5", tolerances.spec(postings).of("JPY").String(), "a cost without numbers adds the cap")

	sale := units("-2.5", "HOOL", ast.WithCost(&ast.Cost{}))
	postings = []*ast.Posting{sale, units("250", "USD")}
	reduced := map[*ast.Posting][]BookedPosition{sale: {
		{Units: d("-2.0"), Cost: &BookedCost{Number: d("0.10"), Currency: "USD"}},
		{Units: d("-0.5"), Cost: &BookedCost{Number: d("0.20"), Currency: "USD"}},
	}}
	assert.Equal(t, "0.015", tolerances.booked(postings, &TransactionDelta{}, reduced).of("USD").String(), "each lot adds its share")
}

func TestRoundInterpolated(t *testing.T) {
	// Expected values follow beancount's quantize_with_tolerance, computed
	// with Python decimal for a transaction stating 10.00 USD: the step is
	// twice the tolerance, and a step of five or more significant digits
	// leaves the number unrounded.
	postings := []*ast.Posting{units("10.00", "USD"), units("-10", "EUR")}
	number := decimal.RequireFromString("-6.666666666")
	for multiplier, want := range map[string]string{
		"0.5":     "-6.67",
		"1.1":     "-6.667",
		"0.3333":  "-6.666667",
		"0.33333": "-6.666666666",
	} {
		spec := newTolerances(toleranceOptions(withMultiplier(multiplier))).spec(postings)
		assert.Equal(t, want, formatInferredNumber(spec.round("USD", number)), multiplier)
	}

	// Without a stated fractional amount or default there is nothing to
	// round to; a configured default gives a step even for whole numbers.
	spec := newTolerances(toleranceOptions(nil)).spec(postings)
	assert.Equal(t, "-6.666666666", formatInferredNumber(spec.round("EUR", number)))
	spec = newTolerances(toleranceOptions(withDefault("EUR", "0.005"))).spec(postings)
	assert.Equal(t, "-6.67", formatInferredNumber(spec.round("EUR", number)))
}

// TestBalanceAssertionTolerance pins a balance assertion's tolerance to
// beancount's ops.balance.get_balance_tolerance: twice the multiplier on the
// asserted number's last digit, unless the assertion states its own.
func TestBalanceAssertionTolerance(t *testing.T) {
	tests := []struct {
		name      string
		options   func(*sharedconfig.Tolerance)
		amount    string
		tolerance string
		want      string
	}{
		{name: "two digits", amount: "100.00", want: "0.01"},
		{name: "one digit", amount: "1.5", want: "0.1"},
		{name: "whole number is exact", amount: "100", want: "0"},
		{name: "stated tolerance", amount: "100.00", tolerance: "0.02", want: "0.02"},
		{name: "multiplier", options: withMultiplier("0.6"), amount: "100.00", want: "0.012"},
		{name: "defaults do not apply", options: withDefault("*", "0.5"), amount: "100", want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balance := ast.NewBalance(nil, "Assets:Cash", ast.NewAmount(tt.amount, "USD"))
			if tt.tolerance != "" {
				balance.Tolerance = ast.NewAmount(tt.tolerance, "USD")
			}
			got, err := newTolerances(toleranceOptions(tt.options)).balance(balance)
			assert.NoError(t, err)
			assert.True(t, got.Equal(decimal.RequireFromString(tt.want)), "got %s, want %s", got, tt.want)
		})
	}
}
