package ledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Config holds parsed Beancount options and configuration.
// It's designed to be easily extended as more options are supported.
type Config struct {
	Tolerance     *ToleranceConfig
	BookingMethod string
}

// NewConfig creates a Config with defaults from Beancount.
func NewConfig() *Config {
	return &Config{
		Tolerance:     NewToleranceConfig(),
		BookingMethod: "SIMPLE",
	}
}

// configFromAST extracts options from an AST and parses them into a Config.
// Handles the conversion from AST options to the format expected by configFromOptions.
func configFromAST(tree *ast.AST) (*Config, error) {
	options := make(map[string][]string)
	for _, opt := range tree.Options {
		options[opt.Name.Value] = append(options[opt.Name.Value], opt.Value.Value)
	}
	return configFromOptions(options)
}

// configFromOptions parses options map into a Config.
// Supports:
//   - option "inferred_tolerance_default" "CURRENCY:TOLERANCE"
//   - option "inferred_tolerance_multiplier" "0.6"
//   - option "infer_tolerance_from_cost" "TRUE"
//   - option "booking_method" "SIMPLE|FULL"
func configFromOptions(options map[string][]string) (*Config, error) {
	cfg := NewConfig()

	// Parse tolerance config
	var err error
	cfg.Tolerance, err = parseToleranceConfigFromOptions(options)
	if err != nil {
		return nil, err
	}

	// Parse booking method (use first value if multiple)
	if vals := options["booking_method"]; len(vals) > 0 {
		method := strings.ToUpper(vals[0])
		if method != "SIMPLE" && method != "FULL" {
			return nil, fmt.Errorf("invalid booking_method %q, expected SIMPLE or FULL", vals[0])
		}
		cfg.BookingMethod = method
	}

	return cfg, nil
}

// parseToleranceConfigFromOptions parses tolerance options into ToleranceConfig.
// Supports:
//   - option "inferred_tolerance_default" "*:0.005"
//   - option "inferred_tolerance_default" "USD:0.003"
//   - option "inferred_tolerance_multiplier" "0.6"
//   - option "infer_tolerance_from_cost" "TRUE"
func parseToleranceConfigFromOptions(options map[string][]string) (*ToleranceConfig, error) {
	config := NewToleranceConfig()

	// Parse inferred_tolerance_multiplier (takes precedence over tolerance_multiplier)
	if vals := options["inferred_tolerance_multiplier"]; len(vals) > 0 {
		multiplier, err := decimal.NewFromString(vals[0])
		if err != nil {
			return nil, fmt.Errorf("invalid inferred_tolerance_multiplier %q: %w", vals[0], err)
		}
		config.multiplier = multiplier
	} else if vals := options["tolerance_multiplier"]; len(vals) > 0 {
		// Fallback for legacy option name
		multiplier, err := decimal.NewFromString(vals[0])
		if err != nil {
			return nil, fmt.Errorf("invalid tolerance_multiplier %q: %w", vals[0], err)
		}
		config.multiplier = multiplier
	}

	// Parse inferred_tolerance_default (can appear multiple times for per-currency tolerances)
	// Format: "CURRENCY:TOLERANCE" or "*:TOLERANCE"
	if vals := options["inferred_tolerance_default"]; len(vals) > 0 {
		for _, val := range vals {
			parts := strings.SplitN(val, ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid inferred_tolerance_default format %q, expected CURRENCY:TOLERANCE", val)
			}

			currency := strings.TrimSpace(parts[0])
			toleranceStr := strings.TrimSpace(parts[1])

			tolerance, err := decimal.NewFromString(toleranceStr)
			if err != nil {
				return nil, fmt.Errorf("invalid tolerance value in %q: %w", val, err)
			}

			config.defaults[currency] = tolerance
		}
	}

	// Parse infer_tolerance_from_cost (use first value if multiple)
	if vals := options["infer_tolerance_from_cost"]; len(vals) > 0 {
		config.inferFromCost = strings.ToUpper(vals[0]) == "TRUE"
	}

	return config, nil
}

// contextKey is a private type to avoid key collisions in context.
type contextKey struct{}

// WithContext returns a new context with the Config attached.
func (c *Config) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// ConfigFromContext retrieves the Config from context.
// Returns a default Config if not found.
func ConfigFromContext(ctx context.Context) *Config {
	if cfg, ok := ctx.Value(contextKey{}).(*Config); ok {
		return cfg
	}
	return NewConfig()
}
