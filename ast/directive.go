package ast

// Stateful indicates a directive affects graph nodes and can report which ones.
// This enables semantic analysis without type switching or field inspection.
type Stateful interface {
	// AffectedNodes returns the node IDs (account names, currency codes) this directive touches.
	// Used to build the graph skeleton and understand directive dependencies.
	// Returns empty slice if directive affects no nodes.
	AffectedNodes() []string
}

// DirectiveKind identifies the type of directive for dispatch without type switching.
type DirectiveKind string

const (
	KindCommodity   DirectiveKind = "commodity"
	KindOpen        DirectiveKind = "open"
	KindClose       DirectiveKind = "close"
	KindBalance     DirectiveKind = "balance"
	KindPad         DirectiveKind = "pad"
	KindNote        DirectiveKind = "note"
	KindDocument    DirectiveKind = "document"
	KindPrice       DirectiveKind = "price"
	KindEvent       DirectiveKind = "event"
	KindCustom      DirectiveKind = "custom"
	KindTransaction DirectiveKind = "transaction"
)

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
	pos      Position
	date     *Date
	Currency string

	withComment
	withMetadata
}

var _ Directive = &Commodity{}

func (c *Commodity) Position() Position  { return c.pos }
func (c *Commodity) Date() *Date         { return c.date }
func (c *Commodity) Kind() DirectiveKind { return KindCommodity }
func (c *Commodity) AffectedNodes() []string {
	if c.Currency == "" {
		return []string{}
	}
	return []string{c.Currency}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (c *Commodity) SetPosition(pos Position) { c.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (c *Commodity) SetDate(date *Date) { c.date = date }

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
	pos                  Position
	date                 *Date
	Account              Account
	ConstraintCurrencies []string
	BookingMethod        string

	withComment
	withMetadata
}

var _ Directive = &Open{}

func (o *Open) Position() Position  { return o.pos }
func (o *Open) Date() *Date         { return o.date }
func (o *Open) Kind() DirectiveKind { return KindOpen }
func (o *Open) AffectedNodes() []string {
	nodes := []string{string(o.Account)}
	nodes = append(nodes, o.ConstraintCurrencies...)
	return nodes
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (o *Open) SetPosition(pos Position) { o.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (o *Open) SetDate(date *Date) { o.date = date }

// Close declares the closing of an account at a specific date, marking the end of
// its lifetime in the ledger. After this date, the account should have a zero balance
// and no new transactions should be posted to it. This helps catch errors if you
// accidentally post transactions to closed accounts.
//
// Example:
//
//	2015-09-23 close Assets:US:BofA:Checking
type Close struct {
	pos     Position
	date    *Date
	Account Account

	withComment
	withMetadata
}

var _ Directive = &Close{}

func (c *Close) Position() Position  { return c.pos }
func (c *Close) Date() *Date         { return c.date }
func (c *Close) Kind() DirectiveKind { return KindClose }
func (c *Close) AffectedNodes() []string {
	return []string{string(c.Account)}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (c *Close) SetPosition(pos Position) { c.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (c *Close) SetDate(date *Date) { c.date = date }

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
	pos     Position
	date    *Date
	Account Account
	Amount  *Amount

	withComment
	withMetadata
}

var _ Directive = &Balance{}

func (b *Balance) Position() Position  { return b.pos }
func (b *Balance) Date() *Date         { return b.date }
func (b *Balance) Kind() DirectiveKind { return KindBalance }
func (b *Balance) AffectedNodes() []string {
	nodes := []string{string(b.Account)}
	if b.Amount != nil {
		nodes = append(nodes, b.Amount.Currency)
	}
	return nodes
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (b *Balance) SetPosition(pos Position) { b.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (b *Balance) SetDate(date *Date) { b.date = date }

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
	pos        Position
	date       *Date
	Account    Account
	AccountPad Account

	withComment
	withMetadata
}

var _ Directive = &Pad{}

func (p *Pad) Position() Position  { return p.pos }
func (p *Pad) Date() *Date         { return p.date }
func (p *Pad) Kind() DirectiveKind { return KindPad }
func (p *Pad) AffectedNodes() []string {
	return []string{string(p.Account), string(p.AccountPad)}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (p *Pad) SetPosition(pos Position) { p.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (p *Pad) SetDate(date *Date) { p.date = date }

// Note attaches a dated comment or note to an account, allowing you to record
// important information about an account at a specific point in time. These notes
// can be used to track customer service calls, account changes, or any other
// significant events related to the account.
//
// Example:
//
//	2014-07-09 note Assets:US:BofA:Checking "Called bank about pending direct deposit"
type Note struct {
	pos         Position
	date        *Date
	Account     Account
	Description RawString

	withComment
	withMetadata
}

var _ Directive = &Note{}

func (n *Note) Position() Position  { return n.pos }
func (n *Note) Date() *Date         { return n.date }
func (n *Note) Kind() DirectiveKind { return KindNote }
func (n *Note) AffectedNodes() []string {
	return []string{string(n.Account)}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (n *Note) SetPosition(pos Position) { n.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (n *Note) SetDate(date *Date) { n.date = date }

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
	pos            Position
	date           *Date
	Account        Account
	PathToDocument RawString

	withComment
	withMetadata
}

var _ Directive = &Document{}

func (d *Document) Position() Position  { return d.pos }
func (d *Document) Date() *Date         { return d.date }
func (d *Document) Kind() DirectiveKind { return KindDocument }
func (d *Document) AffectedNodes() []string {
	return []string{string(d.Account)}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (d *Document) SetPosition(pos Position) { d.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (d *Document) SetDate(date *Date) { d.date = date }

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
	pos       Position
	date      *Date
	Commodity string
	Amount    *Amount

	withComment
	withMetadata
}

var _ Directive = &Price{}

func (p *Price) Position() Position  { return p.pos }
func (p *Price) Date() *Date         { return p.date }
func (p *Price) Kind() DirectiveKind { return KindPrice }
func (p *Price) AffectedNodes() []string {
	nodes := []string{p.Commodity}
	if p.Amount != nil {
		nodes = append(nodes, p.Amount.Currency)
	}
	return nodes
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (p *Price) SetPosition(pos Position) { p.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (p *Price) SetDate(date *Date) { p.date = date }

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
	pos   Position
	date  *Date
	Name  RawString
	Value RawString

	withComment
	withMetadata
}

var _ Directive = &Event{}

func (e *Event) Position() Position  { return e.pos }
func (e *Event) Date() *Date         { return e.date }
func (e *Event) Kind() DirectiveKind { return KindEvent }
func (e *Event) AffectedNodes() []string {
	return []string{}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (e *Event) SetPosition(pos Position) { e.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (e *Event) SetDate(date *Date) { e.date = date }

// Custom is a prototype directive for plugin development, allowing arbitrary typed values
// after the directive name. This provides a flexible extension mechanism for plugins to
// define their own directives with custom data. Values can be strings, numbers, booleans,
// or amounts in any combination.
//
// Example:
//
//	2014-07-09 custom "budget" "..." TRUE 45.30 USD
//	2015-01-01 custom "forecast" 100.00 USD FALSE "monthly"
type Custom struct {
	pos    Position
	date   *Date
	Type   RawString
	Values []*CustomValue

	withComment
	withMetadata
}

var _ Directive = &Custom{}

func (c *Custom) Position() Position  { return c.pos }
func (c *Custom) Date() *Date         { return c.date }
func (c *Custom) Kind() DirectiveKind { return KindCustom }
func (c *Custom) AffectedNodes() []string {
	return []string{}
}

// SetPosition sets the position (for use by parser/builders in ast package)
func (c *Custom) SetPosition(pos Position) { c.pos = pos }

// SetDate sets the date (for use by parser/builders in ast package)
func (c *Custom) SetDate(date *Date) { c.date = date }

// CustomValue represents a single value in a custom directive, which can be a string,
// number, boolean, or amount. Only one field will be non-nil/non-zero for each value.
type CustomValue struct {
	String       *string
	BooleanValue *string
	Amount       *Amount
	Number       *string
}

// GetValue returns the actual value stored in this CustomValue.
func (cv *CustomValue) GetValue() any {
	switch {
	case cv.String != nil:
		return *cv.String
	case cv.BooleanValue != nil:
		return *cv.BooleanValue == "TRUE"
	case cv.Amount != nil:
		return cv.Amount
	case cv.Number != nil:
		return *cv.Number
	default:
		return nil
	}
}

// IsBoolean returns true if this value is a boolean.
func (cv *CustomValue) IsBoolean() bool {
	return cv.BooleanValue != nil
}

// Boolean returns the boolean value if this is a boolean value.
func (cv *CustomValue) Boolean() bool {
	if cv.BooleanValue != nil {
		return *cv.BooleanValue == "TRUE"
	}
	return false
}
