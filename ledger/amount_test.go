package ledger

import (
	"testing"

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
				if err != nil {
					t.Fatalf("failed to parse amount %q: %v", s, err)
				}
				amounts = append(amounts, d)
			}

			got := InferTolerance(amounts, tt.currency, tt.config)
			want, err := decimal.NewFromString(tt.wantTol)
			if err != nil {
				t.Fatalf("failed to parse expected tolerance %q: %v", tt.wantTol, err)
			}

			if !got.Equal(want) {
				t.Errorf("InferTolerance() = %s, want %s", got.String(), want.String())
			}
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
				if !config.multiplier.Equal(decimal.NewFromFloat(0.5)) {
					t.Errorf("multiplier = %s, want 0.5", config.multiplier)
				}
				if !config.defaults["*"].Equal(decimal.NewFromFloat(0.005)) {
					t.Errorf("defaults[*] = %s, want 0.005", config.defaults["*"])
				}
				if config.inferFromCost {
					t.Errorf("inferFromCost = true, want false")
				}
			},
		},
		{
			name: "custom multiplier",
			options: map[string][]string{
				"tolerance_multiplier": {"0.6"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				if !config.multiplier.Equal(decimal.NewFromFloat(0.6)) {
					t.Errorf("multiplier = %s, want 0.6", config.multiplier)
				}
			},
		},
		{
			name: "wildcard default tolerance",
			options: map[string][]string{
				"inferred_tolerance_default": {"*:0.001"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				if !config.defaults["*"].Equal(decimal.NewFromFloat(0.001)) {
					t.Errorf("defaults[*] = %s, want 0.001", config.defaults["*"])
				}
			},
		},
		{
			name: "currency-specific default",
			options: map[string][]string{
				"inferred_tolerance_default": {"USD:0.003"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				if !config.defaults["USD"].Equal(decimal.NewFromFloat(0.003)) {
					t.Errorf("defaults[USD] = %s, want 0.003", config.defaults["USD"])
				}
				// Wildcard should still have default
				if !config.defaults["*"].Equal(decimal.NewFromFloat(0.005)) {
					t.Errorf("defaults[*] = %s, want 0.005", config.defaults["*"])
				}
			},
		},
		{
			name: "infer from cost",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"TRUE"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				if !config.inferFromCost {
					t.Errorf("inferFromCost = false, want true")
				}
			},
		},
		{
			name: "infer from cost false",
			options: map[string][]string{
				"infer_tolerance_from_cost": {"false"},
			},
			wantErr: false,
			checkConfig: func(t *testing.T, config *ToleranceConfig) {
				if config.inferFromCost {
					t.Errorf("inferFromCost = true, want false")
				}
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
				if !config.multiplier.Equal(decimal.NewFromFloat(0.75)) {
					t.Errorf("multiplier = %s, want 0.75", config.multiplier)
				}
				if !config.defaults["EUR"].Equal(decimal.NewFromFloat(0.002)) {
					t.Errorf("defaults[EUR] = %s, want 0.002", config.defaults["EUR"])
				}
				if !config.inferFromCost {
					t.Errorf("inferFromCost = false, want true")
				}
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
				if !config.defaults["USD"].Equal(decimal.NewFromFloat(0.01)) {
					t.Errorf("defaults[USD] = %s, want 0.01", config.defaults["USD"])
				}
				if !config.defaults["EUR"].Equal(decimal.NewFromFloat(0.01)) {
					t.Errorf("defaults[EUR] = %s, want 0.01", config.defaults["EUR"])
				}
				if !config.defaults["BTC"].Equal(decimal.NewFromFloat(0.0001)) {
					t.Errorf("defaults[BTC] = %s, want 0.0001", config.defaults["BTC"])
				}
				// Wildcard should still have default
				if !config.defaults["*"].Equal(decimal.NewFromFloat(0.005)) {
					t.Errorf("defaults[*] = %s, want 0.005", config.defaults["*"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseToleranceConfig(tt.options)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config == nil {
				t.Fatal("config is nil")
			}

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
			if err != nil {
				t.Fatalf("failed to parse expected tolerance %q: %v", tt.want, err)
			}

			if !got.Equal(want) {
				t.Errorf("GetDefaultTolerance() = %s, want %s", got.String(), want.String())
			}
		})
	}
}

func TestNewToleranceConfig(t *testing.T) {
	config := NewToleranceConfig()

	if config == nil {
		t.Fatal("NewToleranceConfig() returned nil")
	}

	if !config.multiplier.Equal(decimal.NewFromFloat(0.5)) {
		t.Errorf("multiplier = %s, want 0.5", config.multiplier)
	}

	if !config.defaults["*"].Equal(decimal.NewFromFloat(0.005)) {
		t.Errorf("defaults[*] = %s, want 0.005", config.defaults["*"])
	}

	if config.inferFromCost {
		t.Errorf("inferFromCost = true, want false")
	}
}
