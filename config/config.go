// Package config owns Beancount option parsing and typed processing configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
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

	// PreciseInterpolation is use_precise_interpolation: interpolated
	// numbers are rounded to the finest tolerance a transaction's numbers
	// imply, not the coarsest.
	PreciseInterpolation bool
}

// NewTolerance returns the official default tolerance configuration.
func NewTolerance() *Tolerance {
	return &Tolerance{
		Defaults:   make(map[string]decimal.Decimal),
		Multiplier: decimal.NewFromFloat(0.5),
	}
}

// Config holds the options consumed while processing a ledger.
type Config struct {
	Tolerance     *Tolerance
	BookingMethod string
	AccountNames  *AccountNames

	// Title names the ledger, "Beancount" unless the title option says otherwise.
	Title string

	// OperatingCurrencies accumulates every operating_currency option in
	// declaration order, matching beancount's list semantics (the option
	// may be declared multiple times; duplicates are preserved).
	OperatingCurrencies []string

	// DisplayPrecision maps a currency to the display_precision number
	// whose exponent fixes its fractional digits in the ledger's display
	// context, which BQL renders amounts with.
	DisplayPrecision map[string]decimal.Decimal

	// AccountUnrealizedGains is the leaf account unrealized gains post
	// to, under the income root. Nothing reads it yet.
	AccountUnrealizedGains string
}

// New returns configuration populated with official defaults.
func New() *Config {
	return &Config{
		Tolerance:     NewTolerance(),
		BookingMethod: "STRICT",
		Title:         "Beancount",
		// Like beancount's default for account_unrealized_gains.
		AccountUnrealizedGains: "Earnings:Unrealized",
		DisplayPrecision:       make(map[string]decimal.Decimal),
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
// reported at its directive while every other option still applies. An
// option written under a name beancount renamed is reported and still
// applied, under its current name.
func ParseOptions(tree *ast.AST) (*Config, []error) {
	cfg := New()
	var errs []error
	for _, option := range tree.Options {
		errs = append(errs, cfg.applyOption(option)...)
	}
	return cfg, errs
}

// CheckOption returns the errors beancount reports for an option directive
// without applying it anywhere, as for an option in an included file, which
// beancount checks and then ignores.
func CheckOption(option *ast.Option) []error {
	return New().applyOption(option)
}

// applyOption applies one option directive and returns its errors, in
// beancount's order: an unknown or reserved name, a deprecation (a rename
// included), then an invalid value.
func (c *Config) applyOption(option *ast.Option) []error {
	if err := validateOptionName(option); err != nil {
		return []error{err}
	}
	var errs []error
	name := option.Name.Value
	if current, renamed := renamedOptions[name]; renamed {
		errs = append(errs, &RenamedOptionError{Option: option})
		name = current
	}
	if message, deprecated := deprecatedOptions[name]; deprecated {
		errs = append(errs, &DeprecatedOptionError{Option: option, Message: message})
	}
	if err := c.apply(name, option.Value.Value); err != nil {
		errs = append(errs, &OptionValueError{Option: option, Err: err})
	}
	return errs
}

// FromAST builds the configuration from an AST's options, returning the
// per-option errors of ParseOptions joined. The configuration is usable
// even when an error is returned.
func FromAST(tree *ast.AST) (*Config, error) {
	cfg, errs := ParseOptions(tree)
	return cfg, errors.Join(errs...)
}

// knownOptions are the user-settable option names of official beancount
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
	"tolerance_multiplier":                     true,
	"inferred_tolerance_multiplier":            true,
	"infer_tolerance_from_cost":                true,
	"use_precise_interpolation":                true,
	"display_precision":                        true,
	"account_unrealized_gains":                 true,
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

// renamedOptions maps an option name beancount has renamed to its current
// name (the alias of its descriptor in beancount/parser/options.py).
var renamedOptions = map[string]string{
	"inferred_tolerance_multiplier": "tolerance_multiplier",
}

// deprecatedOptions maps an option beancount deprecates, whatever its
// value, to the message it reports it with.
var deprecatedOptions = map[string]string{
	"allow_pipe_separator":                     "Allowing pipe separator temporarily; this will go away eventually.",
	"allow_deprecated_none_for_tags_and_links": "Allowing None for tags and link will go away eventually.",
}

// currentName returns the name an option goes by: the one it was renamed
// to, or its own.
func currentName(name string) string {
	if renamed, ok := renamedOptions[name]; ok {
		return renamed
	}
	return name
}

// RenamedOptionError reports an option directive written under a name
// beancount has renamed. The option still applies.
type RenamedOptionError struct {
	Option *ast.Option
}

func (e *RenamedOptionError) Error() string {
	pos := e.Option.Position()
	return fmt.Sprintf("%s:%d: Renamed to '%s'.", pos.Filename, pos.Line, currentName(e.Option.Name.Value))
}

// GetPosition returns the source position of the renamed option directive.
func (e *RenamedOptionError) GetPosition() ast.Position { return e.Option.Position() }

// MarshalJSON renders the error for the web API.
func (e *RenamedOptionError) MarshalJSON() ([]byte, error) {
	return marshalOptionError("RenamedOptionError", e, e.Option)
}

// DeprecatedOptionError reports an option directive beancount deprecates,
// in beancount's words. The option is still accepted.
type DeprecatedOptionError struct {
	Option  *ast.Option
	Message string
}

func (e *DeprecatedOptionError) Error() string {
	pos := e.Option.Position()
	return fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, e.Message)
}

// GetPosition returns the source position of the deprecated option directive.
func (e *DeprecatedOptionError) GetPosition() ast.Position { return e.Option.Position() }

// MarshalJSON renders the error for the web API.
func (e *DeprecatedOptionError) MarshalJSON() ([]byte, error) {
	return marshalOptionError("DeprecatedOptionError", e, e.Option)
}

// InvalidOptionError reports an option directive official beancount rejects.
type InvalidOptionError struct {
	Option   *ast.Option
	Reserved bool
}

func (e *InvalidOptionError) Error() string {
	pos := e.Option.Position()
	if e.Reserved {
		return fmt.Sprintf("%s:%d: Option '%s' may not be set", pos.Filename, pos.Line, e.Option.Name.Value)
	}
	return fmt.Sprintf("%s:%d: Invalid option: '%s'", pos.Filename, pos.Line, e.Option.Name.Value)
}

// GetPosition returns the source position of the offending option directive.
func (e *InvalidOptionError) GetPosition() ast.Position { return e.Option.Position() }

// MarshalJSON renders the error for the web API.
func (e *InvalidOptionError) MarshalJSON() ([]byte, error) {
	return marshalOptionError("InvalidOptionError", e, e.Option)
}

func validateOptionName(option *ast.Option) error {
	name := option.Name.Value
	if knownOptions[name] {
		return nil
	}
	return &InvalidOptionError{Option: option, Reserved: reservedOptions[name]}
}

// FromOptions builds the configuration from option values by name, failing
// on the first invalid value. A renamed option sets its current name's value.
func FromOptions(options map[string][]string) (*Config, error) {
	cfg := New()
	for name, values := range options {
		for _, value := range values {
			if err := cfg.apply(currentName(name), value); err != nil {
				return nil, err
			}
		}
	}
	return cfg, nil
}

// OptionValueError reports an option directive whose value is invalid. Like
// beancount, it names a renamed option by its current name.
type OptionValueError struct {
	Option *ast.Option
	Err    error
}

func (e *OptionValueError) Error() string {
	pos := e.Option.Position()
	return fmt.Sprintf("%s:%d: Error for option '%s': %v", pos.Filename, pos.Line, currentName(e.Option.Name.Value), e.Err)
}

func (e *OptionValueError) Unwrap() error { return e.Err }

// GetPosition returns the source position of the offending option directive.
func (e *OptionValueError) GetPosition() ast.Position { return e.Option.Position() }

// MarshalJSON renders the error for the web API.
func (e *OptionValueError) MarshalJSON() ([]byte, error) {
	return marshalOptionError("OptionValueError", e, e.Option)
}

// marshalOptionError renders an option error in the shape the web API
// gives every positioned error: its type, message and position.
func marshalOptionError(kind string, err error, option *ast.Option) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     kind,
		"message":  err.Error(),
		"position": option.Position(),
	})
}

// apply sets one option value, by the option's current name. Scalar
// options take the latest value, list options accumulate; options this
// implementation does not use are ignored.
func (c *Config) apply(name, value string) error {
	switch name {
	case "booking_method":
		if !IsBookingMethod(value) {
			// Like beancount, which quotes the KeyError from its Booking enum.
			return beancountError(pyrepr.String(value))
		}
		c.BookingMethod = value
	case "title":
		c.Title = value
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
	case "tolerance_multiplier":
		multiplier, err := parseNumber(value)
		if err != nil {
			return err
		}
		c.Tolerance.Multiplier = multiplier
	case "inferred_tolerance_default":
		currency, tolerance, err := parseCurrencyNumber(value)
		if err != nil {
			return err
		}
		c.Tolerance.Defaults[currency] = tolerance
	case "display_precision":
		currency, example, err := parseCurrencyNumber(value)
		if err != nil {
			return err
		}
		c.DisplayPrecision[currency] = example
	case "account_unrealized_gains":
		if !isValidLeafAccount(value) {
			return beancountError("Invalid leaf account name: " + pyrepr.String(value))
		}
		c.AccountUnrealizedGains = value
	case "infer_tolerance_from_cost":
		// Like beancount's boolean options without a converter.
		lower := strings.ToLower(value)
		c.Tolerance.InferFromCost = lower == "true" || lower == "on" || value == "1"
	case "use_precise_interpolation":
		// Like beancount's options_validate_boolean, which takes any value.
		switch strings.ToLower(value) {
		case "1", "true", "yes":
			c.Tolerance.PreciseInterpolation = true
		default:
			c.Tolerance.PreciseInterpolation = false
		}
	}
	return nil
}

// beancountError is an option error in beancount's own words, which start
// with a capital.
type beancountError string

func (e beancountError) Error() string { return string(e) }

// currencyNumberRegex is beancount's options_validate_tolerance_map
// pattern. Like Python's re.match it anchors at the start only, and its
// greedy currency splits the value at the last colon a number follows.
// Python's \d is any Unicode decimal digit.
var currencyNumberRegex = regexp.MustCompile(`^(.*):([\p{Nd}.]+)`)

// parseCurrencyNumber reads a CURRENCY:NUMBER option value, with
// beancount's error messages.
func parseCurrencyNumber(value string) (string, decimal.Decimal, error) {
	match := currencyNumberRegex.FindStringSubmatch(value)
	if match == nil {
		return "", decimal.Decimal{}, beancountError("Invalid value '" + value + "'")
	}
	number, err := parseNumber(match[2])
	if err != nil {
		return "", decimal.Decimal{}, err
	}
	return match[1], number, nil
}

// numberSeparators are the characters beancount's D() drops from a number
// before reading it: thousands separators and spaces.
var numberSeparators = strings.NewReplacer(",", "", " ", "")

// parseNumber reads an option's number as beancount's D() does: an empty
// value is 0, and commas and spaces are dropped before Python's Decimal
// reads the rest. Unlike D(), it rejects an infinity or a NaN, which
// beancount takes and then fails on in its first tolerance.
func parseNumber(value string) (decimal.Decimal, error) {
	if value == "" {
		return decimal.Zero, nil
	}
	number, err := pydecimal.NewFromString(numberSeparators.Replace(value))
	if err != nil {
		return decimal.Decimal{}, beancountError(fmt.Sprintf("Impossible to create Decimal instance from %s: %v", value, err))
	}
	return number, nil
}

// leafComponentRegex is beancount's ACC_COMP_NAME_RE. Unlike an account
// in a directive, a component may not start with a letter without case.
var leafComponentRegex = regexp.MustCompile(`^[\p{Lu}\p{Nd}][\p{L}\p{Nd}-]*$`)

// isValidLeafAccount reports whether value is a leaf account name, as
// beancount's account.is_valid_leaf checks it: colon-separated components.
func isValidLeafAccount(value string) bool {
	for component := range strings.SplitSeq(value, ":") {
		if !leafComponentRegex.MatchString(component) {
			return false
		}
	}
	return true
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
// names, which are case-sensitive: STRICT, STRICT_WITH_SIZE, NONE, FIFO,
// LIFO, HIFO, AVERAGE.
func IsBookingMethod(name string) bool {
	switch name {
	case "STRICT", "STRICT_WITH_SIZE", "NONE", "FIFO", "LIFO", "HIFO", "AVERAGE":
		return true
	}
	return false
}
