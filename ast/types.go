package ast

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// RawString stores both the raw token (including quotes and escapes) and the unquoted
// logical value. This eliminates the need for the formatter to re-escape strings, enabling
// perfect round-tripping of string formatting while providing the logical value for processing.
//
// The parser populates both fields: Raw contains the original token text (e.g., "hello\\nworld")
// and Value contains the unquoted string (e.g., "hello\nworld" with an actual newline).
//
// For programmatic construction, only Value needs to be set; the formatter will quote and
// escape it automatically when Raw is empty.
type RawString struct {
	// Raw is the original token text including quotes and escape sequences.
	// Used for perfect round-trip formatting. Empty for programmatically created strings.
	Raw string
	// Value is the unquoted logical string value (escapes processed).
	// This is the semantic value used by the ledger and validation.
	Value string
}

// String returns the unquoted value for use in string contexts.
func (s RawString) String() string {
	return s.Value
}

// HasRaw returns true if the raw token is available for formatting.
func (s RawString) HasRaw() bool {
	return s.Raw != ""
}

// IsEmpty returns true if the string has no value.
func (s RawString) IsEmpty() bool {
	return s.Value == ""
}

// NewRawString creates a RawString from a logical value without raw token.
// Use this for programmatic construction; the formatter will quote it.
func NewRawString(value string) RawString {
	return RawString{Value: value}
}

// NewRawStringWithRaw creates a RawString with both raw token and unquoted value.
// Use this in the parser when both are available.
func NewRawStringWithRaw(raw, value string) RawString {
	return RawString{Raw: raw, Value: value}
}

// Amount represents a numerical value with its associated currency or commodity symbol.
// The value is stored as a string to preserve the exact decimal representation from
// the input, avoiding floating-point precision issues.
//
// Raw stores the original token text (e.g., "1,234.56") for perfect round-trip formatting.
// Value stores the canonical form with commas stripped (e.g., "1234.56") for processing.
// For programmatic construction, only Value needs to be set; the formatter will use it as-is.
type Amount struct {
	Raw      string // Original token including thousands separators (e.g., "1,234.56")
	Value    string // Canonical value with commas stripped (e.g., "1234.56")
	Currency string
}

// HasRaw returns true if the raw token is available for formatting.
func (a *Amount) HasRaw() bool {
	return a != nil && a.Raw != ""
}

// Cost represents the cost basis specification for a posting, used primarily for tracking
// the acquisition cost of investments and other commodities. An empty cost {} selects any
// lot automatically. A merge cost {*} averages all lots together. Otherwise, you can specify
// the per-unit cost amount, acquisition date, and/or a label to identify specific lots for
// capital gains calculations.
//
// Total cost syntax {{}} allows specifying the total cost for the entire lot instead of
// per-unit cost. The per-unit cost is calculated by dividing the total by the quantity.
//
// Example cost specifications:
//
//	10 HOOL {518.73 USD}              ; Per-unit cost
//	10 HOOL {{5187.30 USD}}           ; Total cost ($518.73 per unit)
//	10 HOOL {518.73 USD, 2014-05-01}  ; Cost with acquisition date
//	-5 HOOL {502.12 USD, "first-lot"} ; Cost with label for lot selection
//	10 HOOL {}                        ; Any lot (automatic selection)
//	10 HOOL {*}                       ; Merge/average all lots
type Cost struct {
	IsMerge  bool
	IsTotal  bool // True if specified with {{}} (total cost syntax)
	Inferred bool // True if Amount was inferred by the ledger (not parsed)
	Amount   *Amount
	Date     *Date
	Label    string
}

// IsEmpty returns true if this is an empty cost specification {}.
// Distinguishes between nil (no cost) and empty cost (any lot selection).
func (c *Cost) IsEmpty() bool {
	return c != nil && !c.IsMerge && !c.IsTotal && c.Amount == nil && c.Date == nil && c.Label == ""
}

// IsMergeCost returns true if this is a merge cost specification {*}.
// Used to average all lots together.
func (c *Cost) IsMergeCost() bool {
	return c != nil && c.IsMerge
}

// Account represents a Beancount account name consisting of at least two colon-separated
// segments. The first segment (account type) must be one of the five account categories:
// Assets, Liabilities, Equity, Income, or Expenses. Subsequent segments must start with
// an uppercase letter or digit and can contain letters, numbers, and hyphens.
//
// Example accounts:
//
//	Assets:US:BofA:Checking
//	Liabilities:CreditCard:CapitalOne
//	Income:US:Acme:Salary
//	Expenses:Home:Rent
type Account string

func (a *Account) Capture(values []string) error {
	parts := strings.Split(values[0], ":")

	// Validate first segment (account type)
	if len(parts) < 2 {
		return fmt.Errorf("account must have at least two segments: %s", values[0])
	}

	t := parts[0]
	switch t {
	case "Assets", "Liabilities", "Equity", "Income", "Expenses":
	default:
		return fmt.Errorf(`unexpected account type "%s"`, t)
	}

	// Validate subsequent segments
	for i := 1; i < len(parts); i++ {
		if !isValidAccountSegment(parts[i]) {
			return fmt.Errorf("invalid account segment at position %d: %s", i, parts[i])
		}
	}

	*a = Account(values[0])
	return nil
}

// AccountType represents the category of a Beancount account.
// The five account types follow double-entry bookkeeping principles.
type AccountType int

const (
	AccountTypeAssets AccountType = iota + 1 // Start at 1, no Unknown
	AccountTypeLiabilities
	AccountTypeEquity
	AccountTypeIncome
	AccountTypeExpenses
)

// String returns the string representation of the account type.
// Panics if the account type is invalid (indicates a bug).
func (t AccountType) String() string {
	switch t {
	case AccountTypeAssets:
		return "Assets"
	case AccountTypeLiabilities:
		return "Liabilities"
	case AccountTypeEquity:
		return "Equity"
	case AccountTypeIncome:
		return "Income"
	case AccountTypeExpenses:
		return "Expenses"
	default:
		panic(fmt.Sprintf("invalid account type: %d", t))
	}
}

// Type returns the account type based on the first segment.
// Panics if the account has an invalid type prefix (indicates validation was bypassed).
func (a Account) Type() AccountType {
	idx := strings.IndexByte(string(a), ':')
	if idx == -1 {
		panic(fmt.Sprintf("invalid account %q: missing type prefix", a))
	}
	switch string(a)[:idx] {
	case "Assets":
		return AccountTypeAssets
	case "Liabilities":
		return AccountTypeLiabilities
	case "Equity":
		return AccountTypeEquity
	case "Income":
		return AccountTypeIncome
	case "Expenses":
		return AccountTypeExpenses
	default:
		panic(fmt.Sprintf("invalid account type prefix %q in account %q", string(a)[:idx], a))
	}
}

// accountSegmentRegex validates account segments (after first).
// Must start with uppercase Unicode letter, digit, or any non-ASCII Unicode character.
// Can contain Unicode letters, digits, and hyphens.
// Matches official beancount behavior supporting international characters including
// scripts without case distinction (Chinese, Japanese, Korean, Arabic, Hebrew, Thai, etc.).
// Pattern: [\p{Lu}\p{Nd}\p{Lo}][\p{L}\p{Nd}-]*
// - \p{Lu} = Unicode uppercase letters (Latin, Cyrillic, Greek, etc.)
// - \p{Nd} = Unicode decimal digits
// - \p{Lo} = Other letters (Chinese, Japanese, Korean, Arabic, Hebrew, etc.)
// - \p{L}  = All Unicode letters (any case, any script)
var accountSegmentRegex = regexp.MustCompile(`^[\p{Lu}\p{Nd}\p{Lo}][\p{L}\p{Nd}-]*$`)

// isValidAccountSegment checks if an account segment (after first) is valid.
func isValidAccountSegment(segment string) bool {
	return len(segment) > 0 && accountSegmentRegex.MatchString(segment)
}

// Date represents a calendar date in ISO 8601 format (YYYY-MM-DD). All Beancount
// directives and transactions must have a date. Dates are used for sorting directives
// chronologically and for balance assertions.
type Date struct {
	time.Time
}

func (d *Date) Capture(values []string) error {
	t, err := time.Parse("2006-01-02", values[0])
	if err != nil {
		return fmt.Errorf("invalid date: %s", values[0])
	}
	// Validate year is in reasonable range (Beancount convention: 1900-9999)
	if t.Year() < 1000 || t.Year() > 9999 {
		return fmt.Errorf("invalid date: year %d out of range (must be 1000-9999)", t.Year())
	}
	d.Time = t
	return nil
}

// IsZero returns true if the Date is nil or represents the zero time.
// This method is nil-safe to prevent panics when repr or other libraries
// check if fields are zero-valued.
func (d *Date) IsZero() bool {
	if d == nil {
		return true
	}
	return d.Time.IsZero()
}

// String returns the date formatted as an ISO 8601 date string (YYYY-MM-DD).
// This is the canonical string representation used throughout Beancount for
// human-readable output in error messages, formatted files, and logs.
func (d *Date) String() string {
	if d == nil || d.IsZero() {
		return ""
	}
	return d.Format("2006-01-02")
}

// Link represents a reference link starting with ^, used to connect related transactions
// together. Links can be used to group transactions that are part of the same event,
// such as a purchase and its associated payment, or multiple legs of a complex transaction.
//
// Example: 2014-05-05 * "Payment" ^trip-to-europe
type Link string

func (l *Link) Capture(values []string) error {
	// Lexer guarantees format \^[A-Za-z0-9_-]+, so we can skip first character
	*l = Link(values[0][1:])
	return nil
}

// Tag represents a hashtag starting with #, used to categorize and filter transactions.
// Tags are commonly used for budgeting categories, projects, or any other classification
// scheme. Multiple tags can be attached to a single transaction.
//
// Example: 2014-05-05 * "Dinner" #dining #entertainment
type Tag string

func (t *Tag) Capture(values []string) error {
	// Lexer guarantees format #[A-Za-z0-9_-]+, so we can skip first character
	*t = Tag(values[0][1:])
	return nil
}

// MetadataValue represents a typed value that can be stored in metadata. Beancount supports
// eight different value types: strings, dates, accounts, currencies, tags, links, numbers,
// amounts, and booleans. This is a discriminated union where exactly one of the pointer
// fields should be non-nil to indicate the value type.
//
// Example metadata with different value types:
//
//	invoice: "INV-2024-001"           ; String (quoted)
//	trip-start: 2024-01-15            ; Date (ISO format)
//	linked-account: Assets:Checking   ; Account (colon-separated)
//	target-currency: USD              ; Currency (uppercase identifier)
//	category: #vacation               ; Tag (with # prefix)
//	ref: ^invoice123                  ; Link (with ^ prefix)
//	quantity: 42                      ; Number (decimal)
//	budget: 1000.00 USD               ; Amount (number + currency)
//	active: TRUE                      ; Boolean (uppercase TRUE/FALSE)
type MetadataValue struct {
	StringValue *RawString
	Date        *Date
	Account     *Account
	Currency    *string
	Tag         *Tag
	Link        *Link
	Number      *string // Stored as string to preserve precision
	Amount      *Amount
	Boolean     *bool
}

// Type returns a string representation of the metadata value's type.
func (m *MetadataValue) Type() string {
	if m == nil {
		return "nil"
	}
	switch {
	case m.StringValue != nil:
		return "string"
	case m.Date != nil:
		return "date"
	case m.Account != nil:
		return "account"
	case m.Currency != nil:
		return "currency"
	case m.Tag != nil:
		return "tag"
	case m.Link != nil:
		return "link"
	case m.Number != nil:
		return "number"
	case m.Amount != nil:
		return "amount"
	case m.Boolean != nil:
		return "boolean"
	default:
		return "unknown"
	}
}

// String returns a string representation of the metadata value.
func (m *MetadataValue) String() string {
	if m == nil {
		return ""
	}
	switch {
	case m.StringValue != nil:
		return m.StringValue.Value
	case m.Date != nil:
		return m.Date.String()
	case m.Account != nil:
		return string(*m.Account)
	case m.Currency != nil:
		return *m.Currency
	case m.Tag != nil:
		return string(*m.Tag)
	case m.Link != nil:
		return string(*m.Link)
	case m.Number != nil:
		return *m.Number
	case m.Amount != nil:
		return m.Amount.Value + " " + m.Amount.Currency
	case m.Boolean != nil:
		if *m.Boolean {
			return "TRUE"
		}
		return "FALSE"
	default:
		return ""
	}
}

// Metadata represents a key-value pair that can be attached to any directive or posting.
// Metadata entries are indented on lines immediately following the directive or posting
// they annotate. They provide a flexible way to attach arbitrary structured information
// such as invoice numbers, confirmation codes, or custom categorization.
//
// Metadata values can be of various types including strings, dates, accounts, currencies,
// tags, links, numbers, amounts, and booleans. See MetadataValue for details.
//
// Example:
//
//	2014-05-05 * "Payment"
//	  invoice: "INV-2014-05-001"
//	  trip-start: 2024-01-15
//	  Assets:Checking  -100.00 USD
//	    confirmation: "CONF123456"
//	  Expenses:Services
type Metadata struct {
	Key   string
	Value *MetadataValue

	// Inline is true if the metadata appeared on the same line as its owner
	// (directive or posting), rather than on a separate indented line.
	Inline bool
}
