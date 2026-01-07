package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

func TestConfigFromOptions(t *testing.T) {
	tests := []struct {
		name        string
		options     map[string][]string
		wantErr     bool
		checkConfig func(t *testing.T, config *Config)
	}{
		{
			name:    "empty options - use defaults",
			options: map[string][]string{},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.Equal(t, decimal.NewFromFloat(0.5), config.Tolerance.multiplier)
				assert.Equal(t, decimal.NewFromFloat(0.005), config.Tolerance.defaults["*"])
				assert.False(t, config.Tolerance.inferFromCost)
				assert.Equal(t, "SIMPLE", config.BookingMethod)
			},
		},
		{
			name: "custom multiplier",
			options: map[string][]string{
				"inferred_tolerance_multiplier": {"0.6"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.Equal(t, decimal.NewFromFloat(0.6), config.Tolerance.multiplier)
			},
		},
		{
			name: "wildcard default tolerance",
			options: map[string][]string{
				"inferred_tolerance_default": {"*:0.001"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.Equal(t, decimal.NewFromFloat(0.001), config.Tolerance.defaults["*"])
			},
		},
		{
			name: "currency-specific default",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:0.003"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.Equal(t, decimal.NewFromFloat(0.003), config.Tolerance.defaults["USD"])
				// Wildcard should still have default
				assert.Equal(t, decimal.NewFromFloat(0.005), config.Tolerance.defaults["*"])
			},
		},
		{
			name: "infer from cost",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"TRUE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.True(t, config.Tolerance.inferFromCost)
			},
		},
		{
			name: "infer from cost false",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"false"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.False(t, config.Tolerance.inferFromCost)
			},
		},
		{
			name: "all options combined",
			options: map[string][]string{
				"inferred_tolerance_multiplier": {"0.75"},
				"inferred_tolerance_default":    {"EUR:0.002"},
				"infer_tolerance_from_cost":     {"TRUE"},
				"booking_method":                {"FULL"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *Config) {
				assert.Equal(t, decimal.NewFromFloat(0.75), config.Tolerance.multiplier)
				assert.Equal(t, decimal.NewFromFloat(0.002), config.Tolerance.defaults["EUR"])
				assert.True(t, config.Tolerance.inferFromCost)
				assert.Equal(t, "FULL", config.BookingMethod)
			},
		},
		{
			name: "invalid multiplier",
			options: map[string][]string{
				"inferred_tolerance_multiplier": {"not-a-number"},
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
			checkConfig: func(t *testing.T, config *Config) {
				assert.Equal(t, decimal.NewFromFloat(0.01), config.Tolerance.defaults["USD"])
				assert.Equal(t, decimal.NewFromFloat(0.01), config.Tolerance.defaults["EUR"])
				assert.Equal(t, decimal.NewFromFloat(0.0001), config.Tolerance.defaults["BTC"])
				// Wildcard should still have default
				assert.Equal(t, decimal.NewFromFloat(0.005), config.Tolerance.defaults["*"])
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
			checkConfig: func(t *testing.T, config *Config) {
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
			checkConfig: func(t *testing.T, config *Config) {
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
			config, err := configFromOptions(tt.options)

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

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()
	assert.True(t, cfg != nil)
	assert.True(t, cfg.Tolerance != nil)
	assert.Equal(t, "SIMPLE", cfg.BookingMethod)
	assert.True(t, cfg.AccountNames != nil)
	assert.Equal(t, "Assets", cfg.AccountNames.Assets)
	assert.Equal(t, "Liabilities", cfg.AccountNames.Liabilities)
	assert.Equal(t, "Equity", cfg.AccountNames.Equity)
	assert.Equal(t, "Income", cfg.AccountNames.Income)
	assert.Equal(t, "Expenses", cfg.AccountNames.Expenses)
}
