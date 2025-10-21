package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

func TestInferTolerance(t *testing.T) {
	tests := []struct {
		name     string
		amounts  []string
		currency string
		config   *ToleranceConfig
		wantTol  string
	}{
		{
			name:     "standard 2 decimals",
			amounts:  []string{"24.45", "100.00"},
			currency: "USD",
			config:   NewToleranceConfig(), // 0.5 multiplier
			wantTol:  "0.005",              // 10^-2 * 0.5 = 0.005
		},
		{
			name:     "high precision 5 decimals",
			amounts:  []string{"10.22626", "5.12345"},
			currency: "RGAGX",
			config:   NewToleranceConfig(),
			wantTol:  "0.000005", // 10^-5 * 0.5 = 0.000005
		},
		{
			name:     "single decimal",
			amounts:  []string{"384.6"},
			currency: "USD",
			config:   NewToleranceConfig(),
			wantTol:  "0.05", // 10^-1 * 0.5 = 0.05
		},
		{
			name:     "mixed precision uses smallest",
			amounts:  []string{"100.00", "50.123"},
			currency: "USD",
			config:   NewToleranceConfig(),
			wantTol:  "0.0005", // 10^-3 * 0.5 = 0.0005
		},
		{
			name:     "custom multiplier",
			amounts:  []string{"100.00"},
			currency: "USD",
			config: &ToleranceConfig{
				defaults: map[string]decimal.Decimal{
					"*": decimal.NewFromFloat(0.005),
				},
				multiplier: decimal.NewFromFloat(0.6),
			},
			wantTol: "0.006", // 10^-2 * 0.6 = 0.006
		},
		{
			name:     "no amounts - use default",
			amounts:  []string{},
			currency: "USD",
			config:   NewToleranceConfig(),
			wantTol:  "0.005", // Default
		},
		{
			name:     "all zero amounts - use default",
			amounts:  []string{"0.00", "0.000"},
			currency: "USD",
			config:   NewToleranceConfig(),
			wantTol:  "0.005", // Default
		},
		{
			name:     "integer amounts",
			amounts:  []string{"100", "200"},
			currency: "USD",
			config:   NewToleranceConfig(),
			wantTol:  "0.5", // 10^0 * 0.5 = 0.5
		},
		{
			name:     "currency-specific default",
			amounts:  []string{},
			currency: "USD",
			config: &ToleranceConfig{
				defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
					"*":   decimal.NewFromFloat(0.005),
				},
				multiplier: decimal.NewFromFloat(0.5),
			},
			wantTol: "0.003", // Currency-specific default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert string amounts to decimals
			amounts := make([]decimal.Decimal, 0, len(tt.amounts))
			for _, s := range tt.amounts {
				d, err := decimal.NewFromString(s)
				assert.NoError(t, err, "failed to parse amount %q", s)
				amounts = append(amounts, d)
			}

			got := InferTolerance(amounts, tt.currency, tt.config)
			want, err := decimal.NewFromString(tt.wantTol)
			assert.NoError(t, err, "failed to parse expected tolerance %q", tt.wantTol)

			assert.Equal(t, want, got, "InferTolerance() mismatch")
		})
	}
}

func TestParseToleranceConfig(t *testing.T) {
	tests := []struct {
		name        string
		options     map[string][]string
		wantErr     bool
		checkConfig func(t *testing.T, config *ToleranceConfig)
	}{
		{
			name:    "empty options - use defaults",
			options: map[string][]string{},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.Equal(t, decimal.NewFromFloat(0.5), config.multiplier)
				assert.Equal(t, decimal.NewFromFloat(0.005), config.defaults["*"])
				assert.False(t, config.inferFromCost)
			},
		},
		{
			name: "custom multiplier",
			options: map[string][]string{
				"tolerance_multiplier": {"0.6"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.Equal(t, decimal.NewFromFloat(0.6), config.multiplier)
			},
		},
		{
			name: "wildcard default tolerance",
			options: map[string][]string{
				"inferred_tolerance_default": {"*:0.001"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.Equal(t, decimal.NewFromFloat(0.001), config.defaults["*"])
			},
		},
		{
			name: "currency-specific default",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:0.003"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.Equal(t, decimal.NewFromFloat(0.003), config.defaults["USD"])
				// Wildcard should still have default
				assert.Equal(t, decimal.NewFromFloat(0.005), config.defaults["*"])
			},
		},
		{
			name: "infer from cost",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"TRUE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.True(t, config.inferFromCost)
			},
		},
		{
			name: "infer from cost false",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"false"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.False(t, config.inferFromCost)
			},
		},
		{
			name: "all options combined",
			options: map[string][]string{
				"tolerance_multiplier":       {"0.75"},
				"inferred_tolerance_default": {"EUR:0.002"},
				"infer_tolerance_from_cost":  {"TRUE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.Equal(t, decimal.NewFromFloat(0.75), config.multiplier)
				assert.Equal(t, decimal.NewFromFloat(0.002), config.defaults["EUR"])
				assert.True(t, config.inferFromCost)
			},
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
			name: "multiple currency-specific tolerances",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:0.01", "EUR:0.01", "BTC:0.0001"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				assert.Equal(t, decimal.NewFromFloat(0.01), config.defaults["USD"])
				assert.Equal(t, decimal.NewFromFloat(0.01), config.defaults["EUR"])
				assert.Equal(t, decimal.NewFromFloat(0.0001), config.defaults["BTC"])
				// Wildcard should still have default
				assert.Equal(t, decimal.NewFromFloat(0.005), config.defaults["*"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseToleranceConfig(tt.options)

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

func TestGetDefaultTolerance(t *testing.T) {
	tests := []struct {
		name     string
		config   *ToleranceConfig
		currency string
		want     string
	}{
		{
			name:     "nil config - fallback",
			config:   nil,
			currency: "USD",
			want:     "0.005",
		},
		{
			name: "currency-specific default",
			config: &ToleranceConfig{
				defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
					"EUR": decimal.NewFromFloat(0.002),
					"*":   decimal.NewFromFloat(0.005),
				},
				multiplier: decimal.NewFromFloat(0.5),
			},
			currency: "USD",
			want:     "0.003",
		},
		{
			name: "wildcard default",
			config: &ToleranceConfig{
				defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
					"*":   decimal.NewFromFloat(0.005),
				},
				multiplier: decimal.NewFromFloat(0.5),
			},
			currency: "CAD",
			want:     "0.005",
		},
		{
			name: "no wildcard - final fallback",
			config: &ToleranceConfig{
				defaults: map[string]decimal.Decimal{
					"USD": decimal.NewFromFloat(0.003),
				},
				multiplier: decimal.NewFromFloat(0.5),
			},
			currency: "EUR",
			want:     "0.005",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetDefaultTolerance(tt.currency)
			want, err := decimal.NewFromString(tt.want)
			assert.NoError(t, err, "failed to parse expected tolerance %q", tt.want)

			assert.Equal(t, want, got, "GetDefaultTolerance() mismatch")
		})
	}
}

func TestNewToleranceConfig(t *testing.T) {
	config := NewToleranceConfig()

	assert.True(t, config != nil, "NewToleranceConfig() should not return nil")
	assert.Equal(t, decimal.NewFromFloat(0.5), config.multiplier)
	assert.Equal(t, decimal.NewFromFloat(0.005), config.defaults["*"])
	assert.False(t, config.inferFromCost)
}
