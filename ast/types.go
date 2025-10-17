package ast

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Amount represents a numerical value with its associated currency or commodity symbol.
// The value is stored as a string to preserve the exact decimal representation from
// the input, avoiding floating-point precision issues.
type Amount struct {
	Value    string `parser:"@Number"`
	Currency string `parser:"@Ident"`
}

// Cost represents the cost basis specification for a posting, used primarily for tracking
// the acquisition cost of investments and other commodities. An empty cost {} selects any
// lot automatically. A merge cost {*} averages all lots together. Otherwise, you can specify
// the per-unit cost amount, acquisition date, and/or a label to identify specific lots for
// capital gains calculations.
//
// Example cost specifications:
//
//	10 HOOL {518.73 USD}              ; Per-unit cost
//	10 HOOL {518.73 USD, 2014-05-01}  ; Cost with acquisition date
//	-5 HOOL {502.12 USD, "first-lot"} ; Cost with label for lot selection
//	10 HOOL {}                        ; Any lot (automatic selection)
//	10 HOOL {*}                       ; Merge/average all lots
type Cost struct {
	IsMerge bool    `parser:"'{' (@'*'"`
	Amount  *Amount `parser:"| @@)?"`
	Date    *Date   `parser:"(',' @Date)?"`
	Label   string  `parser:"(',' @String)? '}'"`
}

// IsEmpty returns true if this is an empty cost specification {}.
// Distinguishes between nil (no cost) and empty cost (any lot selection).
func (c *Cost) IsEmpty() bool {
	return c != nil && !c.IsMerge && c.Amount == nil && c.Date == nil && c.Label == ""
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

// accountSegmentRegex validates account segments (after first).
// Must start with uppercase letter or digit, can contain alphanumerics and hyphens.
var accountSegmentRegex = regexp.MustCompile(`^[A-Z0-9][A-Za-z0-9-]*$`)

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

// Metadata represents a key-value pair that can be attached to any directive or posting.
// Metadata entries are indented on lines immediately following the directive or posting
// they annotate. They provide a flexible way to attach arbitrary structured information
// such as invoice numbers, confirmation codes, or custom categorization.
//
// Example:
//
//	2014-05-05 * "Payment"
//	  invoice: "INV-2014-05-001"
//	  Assets:Checking  -100.00 USD
//	    confirmation: "CONF123456"
//	  Expenses:Services
type Metadata struct {
	Key   string `parser:"@Ident ':'"`
	Value string `parser:"@(~'\\n'+)"`
}
