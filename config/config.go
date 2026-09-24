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

	// OperatingCurrencies accumulates every operating_currency option in
	// declaration order, matching beancount's list semantics (the option
	// may be declared multiple times; duplicates are preserved).
	OperatingCurrencies []string
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

// ParseOptions builds the configuration from an AST's option directives.
// Like beancount, each option is applied on its own, in order: a scalar
// option's last valid value wins, and an unknown name or invalid value is
// reported at its directive while every other option still applies.
func ParseOptions(tree *ast.AST) (*Config, []error) {
	cfg := New()
	var errs []error
	for _, option := range tree.Options {
		if err := validateOptionName(option); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := cfg.apply(option.Name.Value, option.Value.Value); err != nil {
			errs = append(errs, &OptionValueError{Option: option, Err: err})
		}
	}
	return cfg, errs
}

// FromAST builds the configuration from an AST's options, returning the
// per-option errors of ParseOptions joined. The configuration is usable
// even when an error is returned.
func FromAST(tree *ast.AST) (*Config, error) {
	cfg, errs := ParseOptions(tree)
	return cfg, errors.Join(errs...)
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

// FromOptions builds the configuration from option values by name, failing
// on the first invalid value.
func FromOptions(options map[string][]string) (*Config, error) {
	cfg := New()
	for name, values := range options {
		for _, value := range values {
			if err := cfg.apply(name, value); err != nil {
				return nil, err
			}
		}
	}
	return cfg, nil
}

// OptionValueError reports an option directive whose value is invalid.
type OptionValueError struct {
	Option *ast.Option
	Err    error
}

func (e *OptionValueError) Error() string {
	pos := e.Option.Position()
	return fmt.Sprintf("%s:%d: Error for option '%s': %v", pos.Filename, pos.Line, e.Option.Name.Value, e.Err)
}

func (e *OptionValueError) Unwrap() error { return e.Err }

// GetPosition returns the source position of the offending option directive.
func (e *OptionValueError) GetPosition() ast.Position { return e.Option.Position() }

// apply sets one option value. Scalar options take the latest value, list
// options accumulate; options this implementation does not use are ignored.
func (c *Config) apply(name, value string) error {
	switch name {
	case "booking_method":
		if !IsBookingMethod(value) {
			return fmt.Errorf("invalid booking_method %q, expected STRICT, NONE, FIFO, LIFO, HIFO, or AVERAGE", value)
		}
		c.BookingMethod = value
	case "name_assets":
		c.AccountNames.Assets = value
	case "name_liabilities":
		c.AccountNames.Liabilities = value
	case "name_equity":
		c.AccountNames.Equity = value
	case "name_income":
		c.AccountNames.Income = value
	case "name_expenses":
		c.AccountNames.Expenses = value
	case "operating_currency":
		c.OperatingCurrencies = append(c.OperatingCurrencies, value)
	case "inferred_tolerance_multiplier":
		multiplier, err := decimal.NewFromString(value)
		if err != nil {
			return fmt.Errorf("invalid inferred_tolerance_multiplier %q: %w", value, err)
		}
		c.Tolerance.Multiplier = multiplier
	case "inferred_tolerance_default":
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid inferred_tolerance_default format %q, expected CURRENCY:TOLERANCE", value)
		}
		tolerance, err := decimal.NewFromString(strings.TrimSpace(parts[1]))
		if err != nil {
			return fmt.Errorf("invalid tolerance value in %q: %w", value, err)
		}
		c.Tolerance.Defaults[strings.TrimSpace(parts[0])] = tolerance
	case "infer_tolerance_from_cost":
		c.Tolerance.InferFromCost = strings.ToUpper(value) == "TRUE"
	}
	return nil
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

// IsBookingMethod reports whether name is one of beancount's booking method
// names, which are case-sensitive: STRICT, NONE, FIFO, LIFO, HIFO, AVERAGE.
func IsBookingMethod(name string) bool {
	switch name {
	case "STRICT", "NONE", "FIFO", "LIFO", "HIFO", "AVERAGE":
		return true
	}
	return false
}
