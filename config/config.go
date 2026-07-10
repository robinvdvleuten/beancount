// Package config owns Beancount option parsing and typed processing configuration.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// AccountNames holds the customizable account root names.
type AccountNames struct {
	Assets      string
	Liabilities string
	Equity      string
	Income      string
	Expenses    string
}

// Tolerance holds tolerance-inference options.
type Tolerance struct {
	Defaults      map[string]decimal.Decimal
	Multiplier    decimal.Decimal
	InferFromCost bool
}

// NewTolerance returns the official default tolerance configuration.
func NewTolerance() *Tolerance {
	return &Tolerance{
		Defaults:   make(map[string]decimal.Decimal),
		Multiplier: decimal.NewFromFloat(0.5),
	}
}

// GetDefault returns the configured currency tolerance, falling back to "*".
func (c *Tolerance) GetDefault(currency string) decimal.Decimal {
	if c == nil {
		return decimal.Zero
	}
	if value, ok := c.Defaults[currency]; ok {
		return value
	}
	return c.Defaults["*"]
}

// Config holds the options consumed while processing a ledger.
type Config struct {
	Tolerance     *Tolerance
	BookingMethod string
	AccountNames  *AccountNames
}

// New returns configuration populated with official defaults.
func New() *Config {
	return &Config{
		Tolerance:     NewTolerance(),
		BookingMethod: "STRICT",
		AccountNames: &AccountNames{
			Assets:      "Assets",
			Liabilities: "Liabilities",
			Equity:      "Equity",
			Income:      "Income",
			Expenses:    "Expenses",
		},
	}
}

// FromAST extracts and parses options from an AST.
func FromAST(tree *ast.AST) (*Config, error) {
	var errs []error
	options := make(map[string][]string)
	for _, option := range tree.Options {
		if err := validateOptionName(option); err != nil {
			errs = append(errs, err)
			continue
		}
		options[option.Name.Value] = append(options[option.Name.Value], option.Value.Value)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return FromOptions(options)
}

// knownOptions are the user-settable option names of official beancount v2
// (transcribed from beancount/parser/options.py). Options in this set that we
// do not consume are accepted and ignored, exactly like official beancount.
var knownOptions = map[string]bool{
	"title":                                    true,
	"name_assets":                              true,
	"name_liabilities":                         true,
	"name_equity":                              true,
	"name_income":                              true,
	"name_expenses":                            true,
	"account_previous_balances":                true,
	"account_previous_earnings":                true,
	"account_previous_conversions":             true,
	"account_current_earnings":                 true,
	"account_current_conversions":              true,
	"account_rounding":                         true,
	"conversion_currency":                      true,
	"inferred_tolerance_default":               true,
	"inferred_tolerance_multiplier":            true,
	"infer_tolerance_from_cost":                true,
	"documents":                                true,
	"operating_currency":                       true,
	"render_commas":                            true,
	"plugin_processing_mode":                   true,
	"long_string_maxlines":                     true,
	"booking_method":                           true,
	"allow_pipe_separator":                     true,
	"allow_deprecated_none_for_tags_and_links": true,
	"insert_pythonpath":                        true,
}

// reservedOptions exist in official beancount but are derived outputs (or the
// deprecated plugin option) that may not be set from a ledger file.
var reservedOptions = map[string]bool{
	"filename":    true,
	"include":     true,
	"input_hash":  true,
	"dcontext":    true,
	"commodities": true,
	"plugin":      true,
}

// InvalidOptionError reports an option directive official beancount rejects.
type InvalidOptionError struct {
	Option   *ast.Option
	Reserved bool
}

func (e *InvalidOptionError) Error() string {
	pos := e.Option.Position()
	if e.Reserved {
		return fmt.Sprintf("%s:%d: option %q may not be set", pos.Filename, pos.Line, e.Option.Name.Value)
	}
	return fmt.Sprintf("%s:%d: invalid option: %q", pos.Filename, pos.Line, e.Option.Name.Value)
}

// GetPosition returns the source position of the offending option directive.
func (e *InvalidOptionError) GetPosition() ast.Position { return e.Option.Position() }

func validateOptionName(option *ast.Option) error {
	name := option.Name.Value
	if knownOptions[name] {
		return nil
	}
	return &InvalidOptionError{Option: option, Reserved: reservedOptions[name]}
}

// FromOptions parses supported option values.
func FromOptions(options map[string][]string) (*Config, error) {
	cfg := New()

	var err error
	cfg.Tolerance, err = parseTolerance(options)
	if err != nil {
		return nil, err
	}

	if values := options["booking_method"]; len(values) > 0 {
		method := strings.ToUpper(values[0])
		switch method {
		case "STRICT", "NONE", "FIFO", "LIFO", "HIFO", "AVERAGE":
			cfg.BookingMethod = method
		default:
			return nil, fmt.Errorf("invalid booking_method %q, expected STRICT, NONE, FIFO, LIFO, HIFO, or AVERAGE", values[0])
		}
	}

	setFirst(options, "name_assets", &cfg.AccountNames.Assets)
	setFirst(options, "name_liabilities", &cfg.AccountNames.Liabilities)
	setFirst(options, "name_equity", &cfg.AccountNames.Equity)
	setFirst(options, "name_income", &cfg.AccountNames.Income)
	setFirst(options, "name_expenses", &cfg.AccountNames.Expenses)

	return cfg, nil
}

func setFirst(options map[string][]string, name string, target *string) {
	if values := options[name]; len(values) > 0 {
		*target = values[0]
	}
}

func parseTolerance(options map[string][]string) (*Tolerance, error) {
	config := NewTolerance()
	if values := options["inferred_tolerance_multiplier"]; len(values) > 0 {
		multiplier, err := decimal.NewFromString(values[0])
		if err != nil {
			return nil, fmt.Errorf("invalid inferred_tolerance_multiplier %q: %w", values[0], err)
		}
		config.Multiplier = multiplier
	}

	for _, value := range options["inferred_tolerance_default"] {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid inferred_tolerance_default format %q, expected CURRENCY:TOLERANCE", value)
		}
		currency := strings.TrimSpace(parts[0])
		tolerance, err := decimal.NewFromString(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid tolerance value in %q: %w", value, err)
		}
		config.Defaults[currency] = tolerance
	}

	if values := options["infer_tolerance_from_cost"]; len(values) > 0 {
		config.InferFromCost = strings.ToUpper(values[0]) == "TRUE"
	}
	return config, nil
}

// IsValidAccountName reports whether an account starts with a configured root.
func (c *Config) IsValidAccountName(account ast.Account) bool {
	root := account.Root()
	return root == c.AccountNames.Assets || root == c.AccountNames.Liabilities ||
		root == c.AccountNames.Equity || root == c.AccountNames.Income || root == c.AccountNames.Expenses
}

// ToAccountTypeName maps a stable account type to its configured root.
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

// GetAccountTypeFromName maps a configured root to its stable account type.
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
