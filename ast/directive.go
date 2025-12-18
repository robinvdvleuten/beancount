package ast

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
	Pos      Position
	Date     *Date
	Currency string

	withComment
	withMetadata
}

var _ Directive = &Commodity{}

func (c *Commodity) Position() Position { return c.Pos }
func (c *Commodity) date() *Date        { return c.Date }
func (c *Commodity) Directive() string  { return "commodity" }

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
	Pos                  Position
	Date                 *Date
	Account              Account
	ConstraintCurrencies []string
	BookingMethod        string

	withComment
	withMetadata
}

var _ Directive = &Open{}

func (o *Open) Position() Position { return o.Pos }
func (o *Open) date() *Date        { return o.Date }
func (o *Open) Directive() string  { return "open" }

// Close declares the closing of an account at a specific date, marking the end of
// its lifetime in the ledger. After this date, the account should have a zero balance
// and no new transactions should be posted to it. This helps catch errors if you
// accidentally post transactions to closed accounts.
//
// Example:
//
//	2015-09-23 close Assets:US:BofA:Checking
type Close struct {
	Pos     Position
	Date    *Date
	Account Account

	withComment
	withMetadata
}

var _ Directive = &Close{}

func (c *Close) Position() Position { return c.Pos }
func (c *Close) date() *Date        { return c.Date }
func (c *Close) Directive() string  { return "close" }

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
	Pos     Position
	Date    *Date
	Account Account
	Amount  *Amount

	withComment
	withMetadata
}

var _ Directive = &Balance{}

func (b *Balance) Position() Position { return b.Pos }
func (b *Balance) date() *Date        { return b.Date }
func (b *Balance) Directive() string  { return "balance" }

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
	Pos        Position
	Date       *Date
	Account    Account
	AccountPad Account

	withComment
	withMetadata
}

var _ Directive = &Pad{}

func (p *Pad) Position() Position { return p.Pos }
func (p *Pad) date() *Date        { return p.Date }
func (p *Pad) Directive() string  { return "pad" }

// Note attaches a dated comment or note to an account, allowing you to record
// important information about an account at a specific point in time. These notes
// can be used to track customer service calls, account changes, or any other
// significant events related to the account.
//
// Example:
//
//	2014-07-09 note Assets:US:BofA:Checking "Called bank about pending direct deposit"
type Note struct {
	Pos         Position
	Date        *Date
	Account     Account
	Description RawString

	withComment
	withMetadata
}

var _ Directive = &Note{}

func (n *Note) Position() Position { return n.Pos }
func (n *Note) date() *Date        { return n.Date }
func (n *Note) Directive() string  { return "note" }

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
	Pos            Position
	Date           *Date
	Account        Account
	PathToDocument RawString

	withComment
	withMetadata
}

var _ Directive = &Document{}

func (d *Document) Position() Position { return d.Pos }
func (d *Document) date() *Date        { return d.Date }
func (d *Document) Directive() string  { return "document" }

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
	Pos       Position
	Date      *Date
	Commodity string
	Amount    *Amount

	withComment
	withMetadata
}

var _ Directive = &Price{}

func (p *Price) Position() Position { return p.Pos }
func (p *Price) date() *Date        { return p.Date }
func (p *Price) Directive() string  { return "price" }

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
	Pos   Position
	Date  *Date
	Name  RawString
	Value RawString

	withComment
	withMetadata
}

var _ Directive = &Event{}

func (e *Event) Position() Position { return e.Pos }
func (e *Event) date() *Date        { return e.Date }
func (e *Event) Directive() string  { return "event" }

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
	Pos    Position
	Date   *Date
	Type   RawString
	Values []*CustomValue

	withComment
	withMetadata
}

var _ Directive = &Custom{}

func (c *Custom) Position() Position { return c.Pos }
func (c *Custom) date() *Date        { return c.Date }
func (c *Custom) Directive() string  { return "custom" }

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
