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
			name:   "STRICT_WITH_SIZE booking",
			source: `option "booking_method" "STRICT_WITH_SIZE"`,
		},
		{
			name:    "unknown booking method",
			source:  `option "booking_method" "strict"`,
			wantErr: `Error for option 'booking_method': 'strict'`,
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
option "tolerance_multiplier" "abc"
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

func TestCurrencyNumberMapOptions(t *testing.T) {
	tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(`option "display_precision" "USD:0.001"
option "display_precision" "USD:0.01abc"
option "display_precision" "A:B:0.5"
option "inferred_tolerance_default" "*:0.1"
option "inferred_tolerance_default" "USD:0.01abc"
option "display_precision" "USD"
option "display_precision" " USD : 0.01"
option "inferred_tolerance_default" "USD:-0.01"
option "inferred_tolerance_default" "USD:0.0.1"
`))
	assert.NoError(t, err)
	cfg, errs := ParseOptions(tree)

	// Like beancount's options_validate_tolerance_map, a value matches
	// from its start, splits at its last colon and ignores trailing text.
	assert.Equal(t, map[string]string{"USD": "0.01", "A:B": "0.5"}, decimalStrings(cfg.DisplayPrecision))
	assert.Equal(t, map[string]string{"*": "0.1", "USD": "0.01"}, decimalStrings(cfg.Tolerance.Defaults))
	var got []string
	for _, err := range errs {
		got = append(got, err.Error())
	}
	assert.Equal(t, []string{
		"ledger.beancount:6: Error for option 'display_precision': Invalid value 'USD'",
		"ledger.beancount:7: Error for option 'display_precision': Invalid value ' USD : 0.01'",
		"ledger.beancount:8: Error for option 'inferred_tolerance_default': Invalid value 'USD:-0.01'",
		"ledger.beancount:9: Error for option 'inferred_tolerance_default': Impossible to create Decimal instance from 0.0.1: [<class 'decimal.ConversionSyntax'>]",
	}, got)
}

func decimalStrings(m map[string]decimal.Decimal) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.String()
	}
	return out
}

func TestAccountUnrealizedGains(t *testing.T) {
	cfg, errs := ParseOptions(parser.MustParseString(context.Background(), ""))
	assert.Zero(t, errs)
	assert.Equal(t, "Earnings:Unrealized", cfg.AccountUnrealizedGains)

	tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(`option "account_unrealized_gains" "Gains:2020-Q1"
option "account_unrealized_gains" "unrealized"
option "account_unrealized_gains" "It's"
option "account_unrealized_gains" "中文"
`))
	assert.NoError(t, err)
	cfg, errs = ParseOptions(tree)

	// Like beancount's options_validate_leaf_account, which quotes the
	// value with repr().
	assert.Equal(t, "Gains:2020-Q1", cfg.AccountUnrealizedGains)
	var got []string
	for _, err := range errs {
		got = append(got, err.Error())
	}
	assert.Equal(t, []string{
		"ledger.beancount:2: Error for option 'account_unrealized_gains': Invalid leaf account name: 'unrealized'",
		`ledger.beancount:3: Error for option 'account_unrealized_gains': Invalid leaf account name: "It's"`,
		"ledger.beancount:4: Error for option 'account_unrealized_gains': Invalid leaf account name: '中文'",
	}, got)
}

func TestToleranceMultiplier(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		errs   []string
	}{
		{
			name: "default",
			want: "0.5",
		},
		{
			name:   "tolerance_multiplier",
			source: `option "tolerance_multiplier" "0.6"`,
			want:   "0.6",
		},
		{
			name:   "the old name is reported and still applies",
			source: `option "inferred_tolerance_multiplier" "10"`,
			want:   "10",
			errs:   []string{"ledger.beancount:1: Renamed to 'tolerance_multiplier'."},
		},
		{
			name: "the new name written last wins",
			source: `option "inferred_tolerance_multiplier" "10"
option "tolerance_multiplier" "0.6"`,
			want: "0.6",
			errs: []string{"ledger.beancount:1: Renamed to 'tolerance_multiplier'."},
		},
		{
			name: "the old name written last wins",
			source: `option "tolerance_multiplier" "0.6"
option "inferred_tolerance_multiplier" "10"`,
			want: "10",
			errs: []string{"ledger.beancount:2: Renamed to 'tolerance_multiplier'."},
		},
		{
			name:   "an invalid value is reported under the new name",
			source: `option "tolerance_multiplier" "abc"`,
			want:   "0.5",
			errs: []string{
				`ledger.beancount:1: Error for option 'tolerance_multiplier': invalid tolerance_multiplier "abc": can't convert abc to decimal`,
			},
		},
		{
			name: "an invalid value under the old name is reported after the rename",
			source: `option "tolerance_multiplier" "0.6"
option "inferred_tolerance_multiplier" "abc"`,
			want: "0.6",
			errs: []string{
				"ledger.beancount:2: Renamed to 'tolerance_multiplier'.",
				`ledger.beancount:2: Error for option 'tolerance_multiplier': invalid tolerance_multiplier "abc": can't convert abc to decimal`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(tt.source))
			assert.NoError(t, err)
			cfg, errs := ParseOptions(tree)

			assert.Equal(t, tt.want, cfg.Tolerance.Multiplier.String())
			var got []string
			for _, err := range errs {
				got = append(got, err.Error())
			}
			assert.Equal(t, tt.errs, got)
		})
	}
}

func TestFromOptionsToleranceMultiplier(t *testing.T) {
	for _, name := range []string{"tolerance_multiplier", "inferred_tolerance_multiplier"} {
		cfg, err := FromOptions(map[string][]string{name: {"0.6"}})
		assert.NoError(t, err)
		assert.Equal(t, "0.6", cfg.Tolerance.Multiplier.String())
	}
}

func TestInferToleranceFromCost(t *testing.T) {
	// Like beancount's boolean options without a converter: true and on in
	// any case, and 1, are true; any other value is false without an error.
	for value, want := range map[string]bool{
		"TRUE": true, "true": true, "On": true, "1": true,
		"FALSE": false, "yes": false, "0": false, "": false, "bogus": false,
	} {
		cfg, errs := ParseOptions(parser.MustParseString(context.Background(), `option "infer_tolerance_from_cost" "`+value+`"`))
		assert.Zero(t, errs, value)
		assert.Equal(t, want, cfg.Tolerance.InferFromCost, value)
	}
}

func TestUsePreciseInterpolation(t *testing.T) {
	// Like beancount's options_validate_boolean: 1, true and yes in any
	// case are true, and any other value is false without an error.
	for value, want := range map[string]bool{
		"TRUE": true, "true": true, "True": true, "1": true, "yes": true, "YES": true,
		"FALSE": false, "0": false, "on": false, "": false, "bogus": false,
	} {
		cfg, errs := ParseOptions(parser.MustParseString(context.Background(),
			`option "use_precise_interpolation" "`+value+`"`))
		assert.Equal(t, 0, len(errs), value)
		assert.Equal(t, want, cfg.Tolerance.PreciseInterpolation, value)
	}

	assert.False(t, New().Tolerance.PreciseInterpolation)

	cfg, errs := ParseOptions(parser.MustParseString(context.Background(),
		"option \"use_precise_interpolation\" \"TRUE\"\noption \"use_precise_interpolation\" \"FALSE\""))
	assert.Equal(t, 0, len(errs))
	assert.False(t, cfg.Tolerance.PreciseInterpolation, "the last value wins")
}
