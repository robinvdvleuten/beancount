package parser

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"golang.org/x/exp/slices"
)

type Directives []Directive

func (d Directives) Len() int           { return len(d) }
func (d Directives) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d Directives) Less(i, j int) bool { return compareDirectives(d[i], d[j]) < 0 }

// compareDirectives compares two directives by their date.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareDirectives(a, b Directive) int {
	if a.date().Before(b.date().Time) {
		return -1
	} else if a.date().After(b.date().Time) {
		return 1
	}
	return 0
}

type AST struct {
	Directives Directives  `parser:"(@@"`
	Options    []*Option   `parser:"| @@"`
	Includes   []*Include  `parser:"| @@"`
	Plugins    []*Plugin   `parser:"| @@"`
	Pushtags   []*Pushtag  `parser:"| @@"`
	Poptags    []*Poptag   `parser:"| @@"`
	Pushmetas  []*Pushmeta `parser:"| @@"`
	Popmetas   []*Popmeta  `parser:"| @@ | ~ignore)*"`
}

type WithMetadata interface {
	AddMetadata(...*Metadata)
}

type withMetadata struct {
	Metadata []*Metadata `parser:"@@*"`
}

func (w *withMetadata) AddMetadata(m ...*Metadata) {
	w.Metadata = append(w.Metadata, m...)
}

type Directive interface {
	WithMetadata

	date() *Date
	Directive() string
}

// Commodity declares a commodity or currency that can be used in the ledger.
// This directive is optional but helps document which currencies and commodities
// are expected in your accounts. It establishes the existence of a tradable
// instrument and can be used with metadata to specify display precision and formatting.
//
// Example:
//
//	2014-01-01 commodity USD
//	  name: "US Dollar"
//	  asset-class: "cash"
type Commodity struct {
	Pos      lexer.Position
	Date     *Date  `parser:"@Date 'commodity'"`
	Currency string `parser:"@Ident"`

	withMetadata
}

var _ Directive = &Commodity{}

func (c *Commodity) date() *Date       { return c.Date }
func (c *Commodity) Directive() string { return "commodity" }

// Open declares the opening of an account at a specific date, marking the beginning
// of its lifetime in the ledger. You can optionally constrain which currencies the
// account may hold and specify a booking method (STRICT, NONE, AVERAGE, FIFO, LIFO)
// for lot tracking. All accounts must be opened before they can be used in transactions.
//
// Example:
//
//	2014-05-01 open Assets:US:BofA:Checking USD
//	2014-05-01 open Assets:Investments:Brokerage USD,EUR "FIFO"
type Open struct {
	Pos                  lexer.Position
	Date                 *Date    `parser:"@Date 'open'"`
	Account              Account  `parser:"@Account"`
	ConstraintCurrencies []string `parser:"(@Ident (',' @Ident)*)?"`
	BookingMethod        string   `parser:"@String?"`

	withMetadata `parser:""`
}

var _ Directive = &Open{}

func (o *Open) date() *Date       { return o.Date }
func (o *Open) Directive() string { return "open" }

// Close declares the closing of an account at a specific date, marking the end of
// its lifetime in the ledger. After this date, the account should have a zero balance
// and no new transactions should be posted to it. This helps catch errors if you
// accidentally post transactions to closed accounts.
//
// Example:
//
//	2015-09-23 close Assets:US:BofA:Checking
type Close struct {
	Pos     lexer.Position
	Date    *Date   `parser:"@Date 'close'"`
	Account Account `parser:"@Account"`

	withMetadata
}

var _ Directive = &Close{}

func (c *Close) date() *Date       { return c.Date }
func (c *Close) Directive() string { return "close" }

// Balance asserts that an account should have a specific balance at the beginning
// of a given date. This directive is used to verify the integrity of your ledger
// against external statements like bank statements or brokerage reports. If the
// calculated balance doesn't match the assertion, an error will be raised.
//
// Example:
//
//	2014-08-09 balance Assets:US:BofA:Checking 562.00 USD
//	2014-08-09 balance Assets:Investments:Brokerage 10.00 HOOL {518.73 USD}
type Balance struct {
	Pos     lexer.Position
	Date    *Date   `parser:"@Date 'balance'"`
	Account Account `parser:"@Account"`
	Amount  *Amount `parser:"@@"`

	withMetadata
}

var _ Directive = &Balance{}

func (b *Balance) date() *Date       { return b.Date }
func (b *Balance) Directive() string { return "balance" }

// Pad automatically inserts a transaction to bring an account to a specific balance
// determined by the next balance assertion. The padding amount is calculated from the
// difference needed and posted against AccountPad (typically an equity account).
// This is useful for initializing opening balances without manual calculation.
//
// Example:
//
//	2014-01-01 pad Assets:US:BofA:Checking Equity:Opening-Balances
//	2014-08-09 balance Assets:US:BofA:Checking 562.00 USD
type Pad struct {
	Pos        lexer.Position
	Date       *Date   `parser:"@Date 'pad'"`
	Account    Account `parser:"@Account"`
	AccountPad Account `parser:"@Account"`

	withMetadata
}

var _ Directive = &Pad{}

func (p *Pad) date() *Date       { return p.Date }
func (p *Pad) Directive() string { return "pad" }

// Note attaches a dated comment or note to an account, allowing you to record
// important information about an account at a specific point in time. These notes
// can be used to track customer service calls, account changes, or any other
// significant events related to the account.
//
// Example:
//
//	2014-07-09 note Assets:US:BofA:Checking "Called bank about pending direct deposit"
type Note struct {
	Pos         lexer.Position
	Date        *Date   `parser:"@Date 'note'"`
	Account     Account `parser:"@Account"`
	Description string  `parser:"@String"`

	withMetadata
}

var _ Directive = &Note{}

func (n *Note) date() *Date       { return n.Date }
func (n *Note) Directive() string { return "note" }

// Document associates an external file (such as a receipt, invoice, statement, or
// contract) with an account at a specific date. The path can be absolute or relative
// to the ledger file. This creates an audit trail linking your ledger entries to
// supporting documentation.
//
// Example:
//
//	2014-07-09 document Assets:US:BofA:Checking "/documents/bank-statements/2014-07.pdf"
//	2014-11-02 document Liabilities:CreditCard "receipts/amazon-invoice-2014-11-02.pdf"
type Document struct {
	Pos            lexer.Position
	Date           *Date   `parser:"@Date 'document'"`
	Account        Account `parser:"@Account"`
	PathToDocument string  `parser:"@String"`

	withMetadata
}

var _ Directive = &Document{}

func (d *Document) date() *Date       { return d.Date }
func (d *Document) Directive() string { return "document" }

// Price declares the price of a commodity in terms of another currency at a specific
// date. These entries are used to track exchange rates, stock prices, and other market
// values over time. Beancount uses price directives for reporting account values at
// market prices and for currency conversions.
//
// Example:
//
//	2014-07-09 price USD 1.08 CAD
//	2015-04-30 price HOOL 582.26 USD
type Price struct {
	Pos       lexer.Position
	Date      *Date   `parser:"@Date 'price'"`
	Commodity string  `parser:"@Ident"`
	Amount    *Amount `parser:"@@"`

	withMetadata
}

var _ Directive = &Price{}

func (p *Price) date() *Date       { return p.Date }
func (p *Price) Directive() string { return "price" }

// Event records a named event with a value at a specific date, allowing you to track
// important life events, location changes, employment history, or other time-based
// state. Events can be queried and used in reports to provide context for your
// financial history.
//
// Example:
//
//	2014-07-09 event "location" "New York, USA"
//	2014-09-01 event "employer" "Hooli Inc."
type Event struct {
	Pos   lexer.Position
	Date  *Date  `parser:"@Date 'event'"`
	Name  string `parser:"@String"`
	Value string `parser:"@String"`

	withMetadata
}

var _ Directive = &Event{}

func (e *Event) date() *Date       { return e.Date }
func (e *Event) Directive() string { return "event" }

// Transaction records a financial transaction with a date, flag, optional payee,
// narration, and a list of postings. The flag indicates transaction status: '*' for
// cleared/complete transactions, '!' for pending/uncleared transactions, or 'P' for
// automatically generated padding transactions. Each transaction must have at least
// two postings, and the sum of all posting amounts must balance to zero (double-entry
// bookkeeping). Tags and links can be used to categorize and connect related transactions.
//
// Example:
//
//	2014-05-05 * "Cafe Mogador" "Lamb tagine with wine"
//	  Liabilities:CreditCard:CapitalOne         -37.45 USD
//	  Expenses:Food:Restaurant
//
//	2014-06-08 ! "Transfer to Savings" #savings-goal
//	  Assets:US:BofA:Checking                  -100.00 USD
//	  Assets:US:BofA:Savings                    100.00 USD
type Transaction struct {
	Pos       lexer.Position
	Date      *Date  `parser:"@Date ('txn' | "`
	Flag      string `parser:"@('*' | '!' | 'P') )"`
	Payee     string `parser:"@(String (?= String))?"`
	Narration string `parser:"@String?"`
	Links     []Link `parser:"@Link*"`
	Tags      []Tag  `parser:"@Tag*"`

	withMetadata

	Postings []*Posting `parser:"@@*"`
}

var _ Directive = &Transaction{}

func (t *Transaction) date() *Date       { return t.Date }
func (t *Transaction) Directive() string { return "transaction" }

// Posting represents a single leg of a transaction, specifying an account and optional
// amount, cost, and price. Each transaction must have at least two postings that balance
// to zero. One posting may omit its amount, which will be automatically inferred. Cost
// specifications track the acquisition cost of commodities for capital gains. Price
// specifications record the conversion rate without affecting the cost basis.
//
// Example postings within transactions:
//
//	Assets:Investments:Brokerage    10 HOOL {518.73 USD}  ; Purchase with cost
//	Assets:Investments:Cash        200 EUR @ 1.35 USD     ; Currency conversion with price
//	Expenses:Groceries              45.60 USD              ; Simple posting
//	Assets:Checking                                        ; Inferred amount
type Posting struct {
	Pos         lexer.Position
	Flag        string  `parser:"@('*' | '!')?"`
	Account     Account `parser:"@Account"`
	Amount      *Amount `parser:"(@@"`
	Cost        *Cost   `parser:"@@?"`
	PriceMarker string  `parser:"( '@'"` // Matches @ price marker (grammar only, always empty)
	PriceTotal  bool    `parser:"@'@'?"` // Captures presence of second @ for total price
	Price       *Amount `parser:"@@)?)?"`

	withMetadata
}

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

// Option sets a configuration parameter that affects how the ledger is processed or
// displayed. Options can control the ledger title, operating currency, plugin behavior,
// and other processing settings. Options apply globally to the entire ledger.
//
// Example:
//
//	option "title" "Personal Ledger of John Doe"
//	option "operating_currency" "USD"
//	option "booking_method" "STRICT"
type Option struct {
	Pos   lexer.Position
	Name  string `parser:"'option' @String"`
	Value string `parser:"@String"`
}

// Include imports and processes directives from another Beancount file, allowing you
// to split your ledger across multiple files for better organization. The path can be
// absolute or relative to the file containing the include directive. Common practice is
// to separate account definitions, price histories, and yearly transactions into different files.
//
// Example:
//
//	include "accounts.beancount"
//	include "prices/2014.beancount"
//	include "transactions/2014-expenses.beancount"
type Include struct {
	Pos      lexer.Position
	Filename string `parser:"'include' @String"`
}

// Plugin loads a processing plugin that can transform or validate the ledger data.
// Plugins are Python modules that run after parsing and can add new directives, check
// for errors, or modify existing entries. An optional configuration string can be passed
// to customize plugin behavior.
//
// Example:
//
//	plugin "beancount.plugins.auto_accounts"
//	plugin "beancount.plugins.check_commodity" "USD,EUR,GBP"
type Plugin struct {
	Pos    lexer.Position
	Name   string `parser:"'plugin' @String"`
	Config string `parser:"@String?"`
}

// Pushtag pushes a tag onto the tag stack, causing all subsequent transactions in the
// file to automatically receive this tag until a corresponding poptag is encountered.
// This is useful for tagging groups of transactions that share a common category or
// project without manually adding the tag to each transaction.
//
// Example:
//
//	pushtag #trip-europe
//	2014-07-01 * "Flight to Paris"  ; Automatically tagged #trip-europe
//	  Expenses:Travel  450.00 USD
//	  Liabilities:CreditCard
//	poptag #trip-europe
type Pushtag struct {
	Pos lexer.Position
	Tag Tag `parser:"'pushtag' @Tag"`
}

// Poptag removes a tag from the tag stack, ending the automatic application of that tag
// to subsequent transactions. It must match a previously pushed tag. Transactions appearing
// after the poptag will no longer automatically receive the specified tag.
//
// Example:
//
//	poptag #trip-europe
type Poptag struct {
	Pos lexer.Position
	Tag Tag `parser:"'poptag' @Tag"`
}

// Pushmeta pushes a metadata key-value pair onto the metadata stack, causing all
// subsequent directives in the file to automatically receive this metadata entry until
// a corresponding popmeta is encountered. This is useful for applying common metadata
// such as location or trip information to groups of transactions.
//
// Example:
//
//	pushmeta location: "New York, NY"
//	2014-07-01 * "Hotel"  ; Automatically receives location metadata
//	  Expenses:Accommodation  150.00 USD
//	  Liabilities:CreditCard
//	popmeta location:
type Pushmeta struct {
	Pos   lexer.Position
	Key   string `parser:"'pushmeta' @Ident ':'"`
	Value string `parser:"@(~'\\n'+)"`
}

// Popmeta removes a metadata key from the metadata stack, ending the automatic application
// of that metadata to subsequent directives. It must match a previously pushed metadata key.
// Directives appearing after the popmeta will no longer automatically receive the specified
// metadata entry.
//
// Example:
//
//	popmeta location:
type Popmeta struct {
	Pos lexer.Position
	Key string `parser:"'popmeta' @Ident ':'"`
}

// Node is a constraint for AST nodes that have a Pos field.
// This includes all non-directive top-level elements (options, includes, plugins, push/pop directives).
type Node interface {
	*Option | *Include | *Plugin | *Pushtag | *Poptag | *Pushmeta | *Popmeta
}

var (
	lex = lexer.MustSimple([]lexer.SimpleRule{
		{"Date", `\d{4}-\d{2}-\d{2}`},
		{"Account", `[A-Z][A-Za-z]*:[A-Za-z0-9][A-Za-z0-9:-]*`},
		{"String", `"[^"]*"`},
		{"Number", `[-+]?(\d*\.)?\d+`},
		{"Link", `\^[A-Za-z0-9_-]+`},
		{"Tag", `#[A-Za-z0-9_-]+`},
		{"Ident", `[A-Za-z][0-9A-Za-z_-]*`},
		{"Punct", `[!*:,@{}]`},
		{"Comment", `;[^\n]*\n`},
		{"Whitespace", `[[:space:]]`},
		{"ignore", `.`},
	})

	parser = participle.MustBuild[AST](
		participle.Lexer(lex),
		participle.Unquote("String"),
		participle.Elide("Comment", "Whitespace"),
		participle.Union[Directive](
			&Commodity{},
			&Open{},
			&Close{},
			&Balance{},
			&Pad{},
			&Note{},
			&Document{},
			&Price{},
			&Event{},
			&Transaction{},
		),
		participle.UseLookahead(2),
	)
)

// Parse AST from an io.Reader.
func Parse(r io.Reader) (*AST, error) {
	ast, err := parser.Parse("", r)
	if err != nil {
		return nil, err
	}

	if err := ApplyPushPopDirectives(ast); err != nil {
		return nil, err
	}

	return ast, SortDirectives(ast)
}

// ParseString parses AST from a string.
func ParseString(str string) (*AST, error) {
	ast, err := parser.ParseString("", str)
	if err != nil {
		return nil, err
	}

	if err := ApplyPushPopDirectives(ast); err != nil {
		return nil, err
	}

	return ast, SortDirectives(ast)
}

// ParseBytes parses AST from bytes.
func ParseBytes(data []byte) (*AST, error) {
	ast, err := parser.ParseBytes("", data)
	if err != nil {
		return nil, err
	}

	if err := ApplyPushPopDirectives(ast); err != nil {
		return nil, err
	}

	return ast, SortDirectives(ast)
}

// positionedItem represents any AST item that has a position in the source file.
type positionedItem struct {
	pos       lexer.Position
	directive Directive
	pushtag   *Pushtag
	poptag    *Poptag
	pushmeta  *Pushmeta
	popmeta   *Popmeta
}

// getDirectivePos extracts the position from any directive type.
func getDirectivePos(d Directive) lexer.Position {
	switch v := d.(type) {
	case *Commodity:
		return v.Pos
	case *Open:
		return v.Pos
	case *Close:
		return v.Pos
	case *Balance:
		return v.Pos
	case *Pad:
		return v.Pos
	case *Note:
		return v.Pos
	case *Document:
		return v.Pos
	case *Price:
		return v.Pos
	case *Event:
		return v.Pos
	case *Transaction:
		return v.Pos
	default:
		return lexer.Position{}
	}
}

// ApplyPushPopDirectives applies pushtag/poptag and pushmeta/popmeta directives
// to transactions and other directives in file order (before date sorting).
func ApplyPushPopDirectives(ast *AST) error {
	// Collect all positioned items
	var items []positionedItem

	for i := range ast.Directives {
		items = append(items, positionedItem{
			pos:       getDirectivePos(ast.Directives[i]),
			directive: ast.Directives[i],
		})
	}

	for _, pt := range ast.Pushtags {
		items = append(items, positionedItem{pos: pt.Pos, pushtag: pt})
	}

	for _, pt := range ast.Poptags {
		items = append(items, positionedItem{pos: pt.Pos, poptag: pt})
	}

	for _, pm := range ast.Pushmetas {
		items = append(items, positionedItem{pos: pm.Pos, pushmeta: pm})
	}

	for _, pm := range ast.Popmetas {
		items = append(items, positionedItem{pos: pm.Pos, popmeta: pm})
	}

	// Sort by file position
	slices.SortFunc(items, func(a, b positionedItem) int {
		if a.pos.Line != b.pos.Line {
			if a.pos.Line < b.pos.Line {
				return -1
			}
			return 1
		}
		if a.pos.Column != b.pos.Column {
			if a.pos.Column < b.pos.Column {
				return -1
			}
			return 1
		}
		if a.pos.Offset < b.pos.Offset {
			return -1
		}
		if a.pos.Offset > b.pos.Offset {
			return 1
		}
		return 0
	})

	// Track active state - use slices to preserve order
	var activeTags []Tag
	activeMetadata := make(map[string]string)

	// Process items in file order
	for _, item := range items {
		switch {
		case item.pushtag != nil:
			activeTags = append(activeTags, item.pushtag.Tag)

		case item.poptag != nil:
			// Remove tag from slice
			for i, tag := range activeTags {
				if tag == item.poptag.Tag {
					activeTags = append(activeTags[:i], activeTags[i+1:]...)
					break
				}
			}

		case item.pushmeta != nil:
			activeMetadata[item.pushmeta.Key] = item.pushmeta.Value

		case item.popmeta != nil:
			delete(activeMetadata, item.popmeta.Key)

		case item.directive != nil:
			// Apply active tags to transactions (preserving order)
			if txn, ok := item.directive.(*Transaction); ok {
				txn.Tags = append(txn.Tags, activeTags...)
			}

			// Apply active metadata to all directives with metadata
			if withMeta, ok := item.directive.(WithMetadata); ok {
				for key, value := range activeMetadata {
					withMeta.AddMetadata(&Metadata{Key: key, Value: value})
				}
			}
		}
	}

	return nil
}

// isSorted checks if directives are already sorted by date.
func isSorted(d Directives) bool {
	for i := 1; i < len(d); i++ {
		if d.Less(i, i-1) {
			return false
		}
	}
	return true
}

// SortDirectives sort all directives by their parsed date.
//
// This is called automatically during Parse*(), but can be called on a manually constructed AST.
func SortDirectives(ast *AST) error {
	// Skip sorting if already sorted (common case for well-maintained files)
	if isSorted(ast.Directives) {
		return nil
	}

	// Use pdqsort for better performance when sorting is needed
	slices.SortFunc(ast.Directives, compareDirectives)
	return nil
}
