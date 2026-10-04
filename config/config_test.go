package config_test

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
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
			wantErr: `Invalid option: 'nomatch'`,
		},
		{
			name:    "reserved option may not be set",
			source:  `option "filename" "x"`,
			wantErr: `Option 'filename' may not be set`,
		},
		{
			name:    "deprecated plugin option may not be set",
			source:  `option "plugin" "beancount.plugins.auto"`,
			wantErr: `Option 'plugin' may not be set`,
		},
		{
			name:    "all invalid options are reported",
			source:  "option \"nomatch\" \"x\"\noption \"filename\" \"y\"",
			wantErr: `Invalid option: 'nomatch'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parser.MustParseString(context.Background(), tt.source)
			cfg, err := config.FromAST(tree)

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
	cfg, err := config.FromAST(tree)
	assert.NoError(t, err)
	// Declaration order, duplicates preserved, matching beancount's
	// list-typed option semantics.
	assert.Equal(t, []string{"USD", "EUR", "USD"}, cfg.OperatingCurrencies)

	cfg, err = config.FromAST(parser.MustParseString(context.Background(), `option "title" "No currencies"`))
	assert.NoError(t, err)
	assert.Equal(t, 0, len(cfg.OperatingCurrencies))
}

func TestTitle(t *testing.T) {
	cfg, err := config.FromAST(parser.MustParseString(context.Background(), ""))
	assert.NoError(t, err)
	assert.Equal(t, "Beancount", cfg.Title)

	cfg, err = config.FromAST(parser.MustParseString(context.Background(),
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
	cfg, errs := config.ParseOptions(tree)

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
option "display_precision" "EUR:٠.٠١"
option "inferred_tolerance_default" "EUR:٣"
`))
	assert.NoError(t, err)
	cfg, errs := config.ParseOptions(tree)

	// Like beancount's options_validate_tolerance_map, a value matches
	// from its start, splits at its last colon and ignores trailing text.
	// Its digits are any Unicode digits, as Python's \d matches them.
	assert.Equal(t, map[string]string{"USD": "0.01", "A:B": "0.5", "EUR": "0.01"}, decimalStrings(cfg.DisplayPrecision))
	assert.Equal(t, map[string]string{"*": "0.1", "USD": "0.01", "EUR": "3"}, decimalStrings(cfg.Tolerance.Defaults))
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
	cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(), ""))
	assert.Zero(t, errs)
	assert.Equal(t, "Earnings:Unrealized", cfg.AccountUnrealizedGains)

	tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(`option "account_unrealized_gains" "Gains:2020-Q1"
option "account_unrealized_gains" "unrealized"
option "account_unrealized_gains" "It's"
option "account_unrealized_gains" "中文"
`))
	assert.NoError(t, err)
	cfg, errs = config.ParseOptions(tree)

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

func TestRootAccountNames(t *testing.T) {
	tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(`option "name_assets" "Vermögen"
option "name_assets" ""
option "name_liabilities" "liabilities"
option "name_equity" "Equity:Sub"
option "name_income" "Inc ome"
option "name_expenses" "1Expenses"
option "name_expenses" "Ünïcode-2"
`))
	assert.NoError(t, err)
	cfg, errs := config.ParseOptions(tree)

	// Like beancount's options_validate_root_account: an invalid name is
	// reported, quoted with repr(), and the previous one stays.
	assert.Equal(t, &config.AccountNames{Assets: "Vermögen", Liabilities: "Liabilities", Equity: "Equity", Income: "Income", Expenses: "Ünïcode-2"}, cfg.AccountNames)
	var got []string
	for _, err := range errs {
		got = append(got, err.Error())
	}
	assert.Equal(t, []string{
		"ledger.beancount:2: Error for option 'name_assets': Invalid root account name: ''",
		"ledger.beancount:3: Error for option 'name_liabilities': Invalid root account name: 'liabilities'",
		"ledger.beancount:4: Error for option 'name_equity': Invalid root account name: 'Equity:Sub'",
		"ledger.beancount:5: Error for option 'name_income': Invalid root account name: 'Inc ome'",
		"ledger.beancount:6: Error for option 'name_expenses': Invalid root account name: '1Expenses'",
	}, got)
}

func TestSummaryAccounts(t *testing.T) {
	cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(), ""))
	assert.Zero(t, errs)
	earnings, balances, conversions := cfg.PreviousAccounts()
	assert.Equal(t, []string{"Equity:Earnings:Previous", "Equity:Opening-Balances", "Equity:Conversions:Previous"},
		[]string{earnings, balances, conversions})
	earnings, conversions = cfg.CurrentAccounts()
	assert.Equal(t, []string{"Equity:Earnings:Current", "Equity:Conversions:Current"}, []string{earnings, conversions})

	// Like beancount, the equity root joins the options when they are
	// read, whichever comes first.
	tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(`option "account_previous_balances" "Opening"
option "account_previous_earnings" "Retained:Before"
option "account_previous_conversions" "Conv:Before"
option "account_current_earnings" "Retained:Now"
option "account_current_conversions" "conv"
option "name_equity" "Capital"
`))
	assert.NoError(t, err)
	cfg, errs = config.ParseOptions(tree)
	earnings, balances, conversions = cfg.PreviousAccounts()
	assert.Equal(t, []string{"Capital:Retained:Before", "Capital:Opening", "Capital:Conv:Before"},
		[]string{earnings, balances, conversions})
	earnings, conversions = cfg.CurrentAccounts()
	assert.Equal(t, []string{"Capital:Retained:Now", "Capital:Conversions:Current"}, []string{earnings, conversions})
	var got []string
	for _, err := range errs {
		got = append(got, err.Error())
	}
	assert.Equal(t, []string{
		"ledger.beancount:5: Error for option 'account_current_conversions': Invalid leaf account name: 'conv'",
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
				`ledger.beancount:1: Error for option 'tolerance_multiplier': Impossible to create Decimal instance from abc: [<class 'decimal.ConversionSyntax'>]`,
			},
		},
		{
			name: "an invalid value under the old name is reported after the rename",
			source: `option "tolerance_multiplier" "0.6"
option "inferred_tolerance_multiplier" "abc"`,
			want: "0.6",
			errs: []string{
				"ledger.beancount:2: Renamed to 'tolerance_multiplier'.",
				`ledger.beancount:2: Error for option 'tolerance_multiplier': Impossible to create Decimal instance from abc: [<class 'decimal.ConversionSyntax'>]`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(tt.source))
			assert.NoError(t, err)
			cfg, errs := config.ParseOptions(tree)

			assert.Equal(t, tt.want, cfg.Tolerance.Multiplier.String())
			var got []string
			for _, err := range errs {
				got = append(got, err.Error())
			}
			assert.Equal(t, tt.errs, got)
		})
	}
}

func TestOptionNumbersReadLikeBeancountsD(t *testing.T) {
	// beancount reads an option's number with D(): empty is 0, commas and
	// spaces are dropped, and Python's Decimal strips white space and takes
	// underscores and any Unicode digit.
	for value, want := range map[string]string{
		"": "0", "1,000": "1000", " 10 ": "10", "1_0": "10", "٣": "3", "\t0.6\n": "0.6",
	} {
		cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(), `option "tolerance_multiplier" "`+value+`"`))
		assert.Zero(t, errs, value)
		assert.Equal(t, want, cfg.Tolerance.Multiplier.String(), value)
	}

	// beancount takes an infinity or a NaN, then fails on its first
	// tolerance; they are rejected here.
	for _, value := range []string{"NaN", "Infinity", "-inf"} {
		cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(), `option "tolerance_multiplier" "`+value+`"`))
		assert.Equal(t, 1, len(errs), value)
		assert.Contains(t, errs[0].Error(), "Impossible to create Decimal instance from "+value+": [<class 'decimal.ConversionSyntax'>]")
		assert.Equal(t, "0.5", cfg.Tolerance.Multiplier.String(), value)
	}
}

func TestDeprecatedOptions(t *testing.T) {
	// beancount reports both whatever their value, and accepts them.
	tree, err := parser.ParseBytesWithFilename(context.Background(), "ledger.beancount", []byte(`option "allow_pipe_separator" "TRUE"
option "allow_deprecated_none_for_tags_and_links" ""
`))
	assert.NoError(t, err)
	_, errs := config.ParseOptions(tree)
	var got []string
	for _, err := range errs {
		got = append(got, err.Error())
	}
	assert.Equal(t, []string{
		"ledger.beancount:1: Allowing pipe separator temporarily; this will go away eventually.",
		"ledger.beancount:2: Allowing None for tags and link will go away eventually.",
	}, got)
}

func TestCheckOptionAppliesNothing(t *testing.T) {
	tree := parser.MustParseString(context.Background(), `option "booking_method" "FIFO"
option "booking_method" "XX"
`)
	assert.Zero(t, config.CheckOption(tree.Options[0]))
	errs := config.CheckOption(tree.Options[1])
	assert.Equal(t, 1, len(errs))
	assert.Contains(t, errs[0].Error(), "Error for option 'booking_method': 'XX'")
}

func TestFromOptionsToleranceMultiplier(t *testing.T) {
	for _, name := range []string{"tolerance_multiplier", "inferred_tolerance_multiplier"} {
		cfg, err := config.FromOptions(map[string][]string{name: {"0.6"}})
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
		cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(), `option "infer_tolerance_from_cost" "`+value+`"`))
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
		cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(),
			`option "use_precise_interpolation" "`+value+`"`))
		assert.Equal(t, 0, len(errs), value)
		assert.Equal(t, want, cfg.Tolerance.PreciseInterpolation, value)
	}

	assert.False(t, config.New().Tolerance.PreciseInterpolation)

	cfg, errs := config.ParseOptions(parser.MustParseString(context.Background(),
		"option \"use_precise_interpolation\" \"TRUE\"\noption \"use_precise_interpolation\" \"FALSE\""))
	assert.Equal(t, 0, len(errs))
	assert.False(t, cfg.Tolerance.PreciseInterpolation, "the last value wins")
}

func TestFromOptions(t *testing.T) {
	tests := []struct {
		name        string
		options     map[string][]string
		wantErr     bool
		checkConfig func(t *testing.T, config *config.Config)
	}{
		{
			name:    "empty options - use defaults",
			options: map[string][]string{},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, decimal.NewFromFloat(0.5), config.Tolerance.Multiplier)
				assert.Equal(t, 0, len(config.Tolerance.Defaults))
				assert.False(t, config.Tolerance.InferFromCost)
				assert.Equal(t, "STRICT", config.BookingMethod)
			},
		},
		{
			name: "custom multiplier",
			options: map[string][]string{
				"tolerance_multiplier": {"0.6"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, decimal.NewFromFloat(0.6), config.Tolerance.Multiplier)
			},
		},
		{
			name: "wildcard default tolerance",
			options: map[string][]string{
				"inferred_tolerance_default": {"*:0.001"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, decimal.NewFromFloat(0.001), config.Tolerance.Defaults["*"])
			},
		},
		{
			name: "currency-specific default",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:0.003"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, decimal.NewFromFloat(0.003), config.Tolerance.Defaults["USD"])
				_, ok := config.Tolerance.Defaults["*"]
				assert.False(t, ok)
			},
		},
		{
			name: "infer from cost",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"TRUE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.True(t, config.Tolerance.InferFromCost)
			},
		},
		{
			name: "infer from cost false",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"false"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.False(t, config.Tolerance.InferFromCost)
			},
		},
		{
			name: "all options combined",
			options: map[string][]string{
				"tolerance_multiplier":       {"0.75"},
				"inferred_tolerance_default": {"EUR:0.002"},
				"infer_tolerance_from_cost":  {"TRUE"},
				"booking_method":             {"AVERAGE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, decimal.NewFromFloat(0.75), config.Tolerance.Multiplier)
				assert.Equal(t, decimal.NewFromFloat(0.002), config.Tolerance.Defaults["EUR"])
				assert.True(t, config.Tolerance.InferFromCost)
				assert.Equal(t, "AVERAGE", config.BookingMethod)
			},
		},
		{
			name: "official booking methods",
			options: map[string][]string{
				"booking_method": {"NONE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, "NONE", config.BookingMethod)
			},
		},
		{
			name: "booking method names are case-sensitive",
			options: map[string][]string{
				"booking_method": {"none"},
			},
			wantErr: true,
		},
		{
			name: "invalid multiplier",
			options: map[string][]string{
				"tolerance_multiplier": {"not-a-number"},
			},
			wantErr: true,
		},
		{
			name: "invalid tolerance format - no colon",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD0.003"},
			},
			wantErr: true,
		},
		{
			name: "invalid tolerance value",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:not-a-number"},
			},
			wantErr: true,
		},
		{
			name: "invalid booking method",
			options: map[string][]string{
				"booking_method": {"INVALID"},
			},
			wantErr: true,
		},
		{
			name: "multiple currency-specific tolerances",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:0.01", "EUR:0.01", "BTC:0.0001"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, decimal.NewFromFloat(0.01), config.Tolerance.Defaults["USD"])
				assert.Equal(t, decimal.NewFromFloat(0.01), config.Tolerance.Defaults["EUR"])
				assert.Equal(t, decimal.NewFromFloat(0.0001), config.Tolerance.Defaults["BTC"])
				_, ok := config.Tolerance.Defaults["*"]
				assert.False(t, ok)
			},
		},
		{
			name: "custom account names",
			options: map[string][]string{
				"name_assets":      {"Vermoegen"},
				"name_liabilities": {"Verbindlichkeiten"},
				"name_equity":      {"Eigenkapital"},
				"name_income":      {"Einkommen"},
				"name_expenses":    {"Ausgaben"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, "Vermoegen", config.AccountNames.Assets)
				assert.Equal(t, "Verbindlichkeiten", config.AccountNames.Liabilities)
				assert.Equal(t, "Eigenkapital", config.AccountNames.Equity)
				assert.Equal(t, "Einkommen", config.AccountNames.Income)
				assert.Equal(t, "Ausgaben", config.AccountNames.Expenses)
			},
		},
		{
			name: "partial account names (only assets)",
			options: map[string][]string{
				"name_assets": {"Actifs"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *config.Config) {
				assert.Equal(t, "Actifs", config.AccountNames.Assets)
				// Others should have defaults
				assert.Equal(t, "Liabilities", config.AccountNames.Liabilities)
				assert.Equal(t, "Equity", config.AccountNames.Equity)
				assert.Equal(t, "Income", config.AccountNames.Income)
				assert.Equal(t, "Expenses", config.AccountNames.Expenses)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := config.FromOptions(tt.options)

			if tt.wantErr {
				assert.Error(t, err, "expected error")
				return
			}

			assert.NoError(t, err, "unexpected error")
			assert.True(t, config != nil, "config should not be nil")

			if tt.checkConfig != nil {
				tt.checkConfig(t, config)
			}
		})
	}
}

func TestNew(t *testing.T) {
	cfg := config.New()
	assert.True(t, cfg != nil)
	assert.True(t, cfg.Tolerance != nil)
	assert.Equal(t, "STRICT", cfg.BookingMethod)
	assert.True(t, cfg.AccountNames != nil)
	assert.Equal(t, "Assets", cfg.AccountNames.Assets)
	assert.Equal(t, "Liabilities", cfg.AccountNames.Liabilities)
	assert.Equal(t, "Equity", cfg.AccountNames.Equity)
	assert.Equal(t, "Income", cfg.AccountNames.Income)
	assert.Equal(t, "Expenses", cfg.AccountNames.Expenses)
}
