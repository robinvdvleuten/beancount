package ledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// AccountNamesConfig holds the customizable account root names.
type AccountNamesConfig struct {
	Assets      string
	Liabilities string
	Equity      string
	Income      string
	Expenses    string
}

// Config holds parsed Beancount options and configuration.
// It's designed to be easily extended as more options are supported.
type Config struct {
	Tolerance     *ToleranceConfig
	BookingMethod string
	AccountNames  *AccountNamesConfig
}

// NewConfig creates a Config with defaults from Beancount.
func NewConfig() *Config {
	return &Config{
		Tolerance:     NewToleranceConfig(),
		BookingMethod: "SIMPLE",
		AccountNames: &AccountNamesConfig{
			Assets:      "Assets",
			Liabilities: "Liabilities",
			Equity:      "Equity",
			Income:      "Income",
			Expenses:    "Expenses",
		},
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
//   - option "name_assets" "Assets"
//   - option "name_liabilities" "Liabilities"
//   - option "name_equity" "Equity"
//   - option "name_income" "Income"
//   - option "name_expenses" "Expenses"
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

	// Parse account names (use first value if multiple)
	if vals := options["name_assets"]; len(vals) > 0 {
		cfg.AccountNames.Assets = vals[0]
	}
	if vals := options["name_liabilities"]; len(vals) > 0 {
		cfg.AccountNames.Liabilities = vals[0]
	}
	if vals := options["name_equity"]; len(vals) > 0 {
		cfg.AccountNames.Equity = vals[0]
	}
	if vals := options["name_income"]; len(vals) > 0 {
		cfg.AccountNames.Income = vals[0]
	}
	if vals := options["name_expenses"]; len(vals) > 0 {
		cfg.AccountNames.Expenses = vals[0]
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

	// Parse inferred_tolerance_multiplier
	if vals := options["inferred_tolerance_multiplier"]; len(vals) > 0 {
		multiplier, err := decimal.NewFromString(vals[0])
		if err != nil {
			return nil, fmt.Errorf("invalid inferred_tolerance_multiplier %q: %w", vals[0], err)
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

// IsValidAccountName checks if an account name starts with a configured account type.
func (c *Config) IsValidAccountName(account ast.Account) bool {
	// Extract the first part (account type) of the account name
	idx := strings.IndexByte(string(account), ':')
	if idx == -1 {
		return false // Invalid account format
	}
	accountType := string(account)[:idx]

	return accountType == c.AccountNames.Assets ||
		accountType == c.AccountNames.Liabilities ||
		accountType == c.AccountNames.Equity ||
		accountType == c.AccountNames.Income ||
		accountType == c.AccountNames.Expenses
}

// ToAccountTypeName converts an AccountType enum to the configured root name.
func (c *Config) ToAccountTypeName(accountType ast.AccountType) string {
	switch accountType {
	case ast.AccountTypeAssets:
		return c.AccountNames.Assets
	case ast.AccountTypeLiabilities:
		return c.AccountNames.Liabilities
	case ast.AccountTypeEquity:
		return c.AccountNames.Equity
	case ast.AccountTypeIncome:
		return c.AccountNames.Income
	case ast.AccountTypeExpenses:
		return c.AccountNames.Expenses
	default:
		panic(fmt.Sprintf("invalid account type: %v", accountType))
	}
}

// GetAccountTypeFromName converts a configured account root name to its AccountType enum.
// Returns (0, false) if the name doesn't match any configured account type.
func (c *Config) GetAccountTypeFromName(name string) (ast.AccountType, bool) {
	switch name {
	case c.AccountNames.Assets:
		return ast.AccountTypeAssets, true
	case c.AccountNames.Liabilities:
		return ast.AccountTypeLiabilities, true
	case c.AccountNames.Equity:
		return ast.AccountTypeEquity, true
	case c.AccountNames.Income:
		return ast.AccountTypeIncome, true
	case c.AccountNames.Expenses:
		return ast.AccountTypeExpenses, true
	default:
		return 0, false
	}
}
