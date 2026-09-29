package config

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func TestFromASTOptionValidation(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name:   "supported option",
			source: `option "booking_method" "FIFO"`,
		},
		{
			name:   "known but unconsumed options are ignored",
			source: `option "render_commas" "TRUE"`,
		},
		{
			name:    "unknown option is rejected",
			source:  `option "nomatch" "x"`,
			wantErr: `invalid option: "nomatch"`,
		},
		{
			name:    "reserved option may not be set",
			source:  `option "filename" "x"`,
			wantErr: `option "filename" may not be set`,
		},
		{
			name:    "deprecated plugin option may not be set",
			source:  `option "plugin" "beancount.plugins.auto"`,
			wantErr: `option "plugin" may not be set`,
		},
		{
			name:    "all invalid options are reported",
			source:  "option \"nomatch\" \"x\"\noption \"filename\" \"y\"",
			wantErr: `invalid option: "nomatch"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parser.MustParseString(context.Background(), tt.source)
			cfg, err := FromAST(tree)

			if tt.wantErr == "" {
				assert.NoError(t, err)
				assert.True(t, cfg != nil)
				return
			}
			assert.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tt.wantErr),
				"error %q should contain %q", err.Error(), tt.wantErr)
		})
	}
}

func TestOperatingCurrenciesAccumulate(t *testing.T) {
	tree := parser.MustParseString(context.Background(),
		"option \"operating_currency\" \"USD\"\noption \"operating_currency\" \"EUR\"\noption \"operating_currency\" \"USD\"")
	cfg, err := FromAST(tree)
	assert.NoError(t, err)
	// Declaration order, duplicates preserved, matching beancount's
	// list-typed option semantics.
	assert.Equal(t, []string{"USD", "EUR", "USD"}, cfg.OperatingCurrencies)

	cfg, err = FromAST(parser.MustParseString(context.Background(), `option "title" "No currencies"`))
	assert.NoError(t, err)
	assert.Equal(t, 0, len(cfg.OperatingCurrencies))
}

func TestTitle(t *testing.T) {
	cfg, err := FromAST(parser.MustParseString(context.Background(), ""))
	assert.NoError(t, err)
	assert.Equal(t, "Beancount", cfg.Title)

	cfg, err = FromAST(parser.MustParseString(context.Background(),
		"option \"title\" \"First\"\noption \"title\" \"Joe's Ledger\""))
	assert.NoError(t, err)
	assert.Equal(t, "Joe's Ledger", cfg.Title)
}

func TestParseOptionsAppliesEachOptionOnItsOwn(t *testing.T) {
	tree := parser.MustParseString(context.Background(), `option "booking_method" "FIFO"
option "booking_method" "LIFO"
option "booking_method" "BOGUS"
option "inferred_tolerance_default" "USD:0.5"
option "inferred_tolerance_multiplier" "abc"
option "bogus_name" "x"
`)
	cfg, errs := ParseOptions(tree)

	// Like beancount: the last valid scalar value wins, and each invalid
	// option is reported at its line without affecting the others.
	assert.Equal(t, "LIFO", cfg.BookingMethod)
	assert.Equal(t, "0.5", cfg.Tolerance.Defaults["USD"].String())
	assert.Equal(t, "0.5", cfg.Tolerance.Multiplier.String())
	assert.Equal(t, 3, len(errs))
	lines := make([]int, len(errs))
	for i, err := range errs {
		lines[i] = err.(interface{ GetPosition() ast.Position }).GetPosition().Line
	}
	assert.Equal(t, []int{3, 5, 6}, lines)
}

func TestToleranceGetDefault(t *testing.T) {
	tests := []struct {
		name     string
		config   *Tolerance
		currency string
		want     string
	}{
		{
			name:     "nil config - fallback",
			config:   nil,
			currency: "USD",
			want:     "0",
		},
		{
			name: "currency-specific default",
			config: &Tolerance{
				Defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
					"EUR": decimal.NewFromFloat(0.002),
					"*":   decimal.NewFromFloat(0.005),
				},
				Multiplier: decimal.NewFromFloat(0.5),
			},
			currency: "USD",
			want:     "0.003",
		},
		{
			name: "wildcard default",
			config: &Tolerance{
				Defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
					"*":   decimal.NewFromFloat(0.005),
				},
				Multiplier: decimal.NewFromFloat(0.5),
			},
			currency: "CAD",
			want:     "0.005",
		},
		{
			name: "no wildcard - final fallback",
			config: &Tolerance{
				Defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
				},
				Multiplier: decimal.NewFromFloat(0.5),
			},
			currency: "EUR",
			want:     "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetDefault(tt.currency)
			want, err := decimal.NewFromString(tt.want)
			assert.NoError(t, err, "failed to parse expected tolerance %q", tt.want)

			assert.True(t, got.Equal(want), "GetDefault() mismatch: got %s, want %s", got, want)
		})
	}
}
