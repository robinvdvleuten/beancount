// Package ast provides constructor functions for programmatically building
// Beancount Abstract Syntax Tree nodes. These builders make it easy to generate
// beancount files from code, such as CSV importers or other data sources.
//
// The builders use functional options for complex types like transactions and
// postings, following Go idioms for configurable constructors.
package ast

import (
	"strings"
	"time"
)

// NewAmount creates a new Amount with the given value and currency.
// The value should be a decimal string (e.g., "100.50", "-42.00").
// No validation is performed on the value or currency.
//
// Example:
//
//	amount := ast.NewAmount("45.60", "USD")
func NewAmount(value, currency string) *Amount {
	return &Amount{
		Value:    value,
		Currency: currency,
	}
}

// NewAmountWithRaw creates a new Amount with both raw (original) and canonical (processed) values.
// The raw value preserves formatting from the source (e.g., "1,234.56"), while the value
// is the canonical form with commas stripped (e.g., "1234.56").
// Use this in the parser when the raw token is available for perfect round-trip formatting.
//
// Example:
//
//	amount := ast.NewAmountWithRaw("1,234.56", "1234.56", "USD")
func NewAmountWithRaw(raw, value, currency string) *Amount {
	return &Amount{
		Raw:      raw,
		Value:    value,
		Currency: currency,
	}
}

// NewDate parses a date string in YYYY-MM-DD format and returns a Date.
// Returns an error if the string cannot be parsed as a valid date.
//
// Example:
//
//	date, err := ast.NewDate("2024-01-15")
//	if err != nil {
//	    log.Fatal(err)
//	}
func NewDate(s string) (*Date, error) {
	d := &Date{}
	if err := d.Capture([]string{s}); err != nil {
		return nil, err
	}
	return d, nil
}

// NewDateFromTime creates a Date from a time.Time value.
// The time is truncated to just the date portion (year, month, day).
//
// Example:
//
//	date := ast.NewDateFromTime(time.Now())
func NewDateFromTime(t time.Time) *Date {
	return &Date{Time: t}
}

// NewAccount creates an Account from the given name string and validates it.
// The account name must follow Beancount account naming rules:
//   - At least two colon-separated segments
//   - First segment must be Assets, Liabilities, Equity, Income, or Expenses
//   - Subsequent segments must start with uppercase letter or digit
//
// Returns an error if the account name is invalid.
//
// Example:
//
//	account, err := ast.NewAccount("Assets:US:BofA:Checking")
//	if err != nil {
//	    log.Fatal(err)
//	}
func NewAccount(name string) (Account, error) {
	var account Account
	if err := account.Capture([]string{name}); err != nil {
		return "", err
	}
	return account, nil
}

// NewLink creates a Link from the given name.
// If the name starts with ^, it is stripped. Otherwise the name is used as-is.
//
// Example:
//
//	link := ast.NewLink("invoice-2024-001")  // Can omit ^
//	link := ast.NewLink("^invoice-2024-001") // Or include it
func NewLink(name string) Link {
	name = strings.TrimPrefix(name, "^")
	return Link(name)
}

// NewTag creates a Tag from the given name.
// If the name starts with #, it is stripped. Otherwise the name is used as-is.
//
// Example:
//
//	tag := ast.NewTag("groceries")  // Can omit #
//	tag := ast.NewTag("#groceries") // Or include it
func NewTag(name string) Tag {
	name = strings.TrimPrefix(name, "#")
	return Tag(name)
}

// NewMetadata creates a Metadata key-value pair with a string value.
// The key should be a valid identifier, and the value can be any string.
//
// Example:
//
//	meta := ast.NewMetadata("invoice", "INV-2024-001")
func NewMetadata(key, value string) *Metadata {
	rawStr := NewRawString(value)
	return &Metadata{
		Key: key,
		Value: &MetadataValue{
			StringValue: &rawStr,
		},
	}
}

// TransactionOption is a functional option for configuring a Transaction.
type TransactionOption func(*Transaction)

// NewTransaction creates a new Transaction with the given date and narration.
// Additional fields can be set using functional options.
//
// Example:
//
//	txn := ast.NewTransaction(date, "Buy groceries",
//	    ast.WithFlag("*"),
//	    ast.WithPayee("Whole Foods"),
//	    ast.WithTags("food", "shopping"),
//	    ast.WithPostings(
//	        ast.NewPosting(expensesAccount, ast.WithAmount("45.60", "USD")),
//	        ast.NewPosting(checkingAccount),
//	    ),
//	)
func NewTransaction(date *Date, narration string, opts ...TransactionOption) *Transaction {
	txn := &Transaction{
		date:      date,
		Narration: NewRawString(narration),
		Flag:      "", // Default to no flag (will be 'txn' in output)
	}

	for _, opt := range opts {
		opt(txn)
	}

	return txn
}

// WithFlag sets the transaction flag.
// Common values: "*" (cleared), "!" (pending), "P" (padding).
func WithFlag(flag string) TransactionOption {
	return func(t *Transaction) {
		t.Flag = flag
	}
}

// WithPayee sets the transaction payee.
func WithPayee(payee string) TransactionOption {
	return func(t *Transaction) {
		t.Payee = NewRawString(payee)
	}
}

// WithTags adds tags to the transaction.
// Tag names should not include the # prefix (it will be added during formatting).
func WithTags(tags ...string) TransactionOption {
	return func(t *Transaction) {
		for _, tag := range tags {
			t.Tags = append(t.Tags, NewTag(tag))
		}
	}
}

// WithLinks adds links to the transaction.
// Link names should not include the ^ prefix (it will be added during formatting).
func WithLinks(links ...string) TransactionOption {
	return func(t *Transaction) {
		for _, link := range links {
			t.Links = append(t.Links, NewLink(link))
		}
	}
}

// WithTransactionMetadata adds metadata entries to the transaction.
func WithTransactionMetadata(metadata ...*Metadata) TransactionOption {
	return func(t *Transaction) {
		t.AddMetadata(metadata...)
	}
}

// WithPostings sets the postings for the transaction.
func WithPostings(postings ...*Posting) TransactionOption {
	return func(t *Transaction) {
		t.Postings = postings
	}
}

// PostingOption is a functional option for configuring a Posting.
type PostingOption func(*Posting)

// NewPosting creates a new Posting for the given account.
// Additional fields can be set using functional options.
//
// Example:
//
//	posting := ast.NewPosting(account,
//	    ast.WithAmount("100.00", "USD"),
//	    ast.WithCost(ast.NewCost(ast.NewAmount("1.35", "EUR"))),
//	)
func NewPosting(account Account, opts ...PostingOption) *Posting {
	posting := &Posting{
		Account: account,
	}

	for _, opt := range opts {
		opt(posting)
	}

	return posting
}

// WithAmount sets the amount for a posting.
// The value should be a decimal string and currency a valid currency code.
func WithAmount(value, currency string) PostingOption {
	return func(p *Posting) {
		p.Amount = NewAmount(value, currency)
	}
}

// WithCost sets the cost specification for a posting.
// Use NewCost, NewEmptyCost, or NewMergeCost to create the cost.
func WithCost(cost *Cost) PostingOption {
	return func(p *Posting) {
		p.Cost = cost
	}
}

// WithPrice sets the price for a posting (using @ syntax).
// This records a conversion rate without affecting the cost basis.
func WithPrice(price *Amount) PostingOption {
	return func(p *Posting) {
		p.Price = price
		p.PriceTotal = false
	}
}

// WithTotalPrice sets the total price for a posting (using @@ syntax).
// This specifies the total price rather than per-unit price.
func WithTotalPrice(price *Amount) PostingOption {
	return func(p *Posting) {
		p.Price = price
		p.PriceTotal = true
	}
}

// WithPostingFlag sets the flag for a posting.
// Common values: "*" (cleared), "!" (pending).
func WithPostingFlag(flag string) PostingOption {
	return func(p *Posting) {
		p.Flag = flag
	}
}

// WithPostingMetadata adds metadata entries to the posting.
func WithPostingMetadata(metadata ...*Metadata) PostingOption {
	return func(p *Posting) {
		p.AddMetadata(metadata...)
	}
}

// NewCost creates a Cost specification with just an amount (per-unit cost).
//
// Example:
//
//	cost := ast.NewCost(ast.NewAmount("518.73", "USD"))
func NewCost(amount *Amount) *Cost {
	return &Cost{
		Amount: amount,
	}
}

// NewCostWithDate creates a Cost specification with an amount and acquisition date.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-15")
//	cost := ast.NewCostWithDate(ast.NewAmount("518.73", "USD"), date)
func NewCostWithDate(amount *Amount, date *Date) *Cost {
	return &Cost{
		Amount: amount,
		Date:   date,
	}
}

// NewCostWithLabel creates a Cost specification with an amount, date, and label.
// The label helps identify specific lots for capital gains calculations.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-15")
//	cost := ast.NewCostWithLabel(ast.NewAmount("518.73", "USD"), date, "first-lot")
func NewCostWithLabel(amount *Amount, date *Date, label string) *Cost {
	return &Cost{
		Amount: amount,
		Date:   date,
		Label:  label,
	}
}

// NewEmptyCost creates an empty cost specification {}.
// This allows automatic lot selection when reducing commodity positions.
func NewEmptyCost() *Cost {
	return &Cost{}
}

// NewMergeCost creates a merge cost specification {*}.
// This averages all lots together.
func NewMergeCost() *Cost {
	return &Cost{
		IsMerge: true,
	}
}

// NewClearedTransaction creates a Transaction with flag="*" (cleared).
// This is a convenience helper for the most common transaction type.
//
// Example:
//
//	txn := ast.NewClearedTransaction(date, "Buy groceries",
//	    ast.NewPosting(expensesAccount, ast.WithAmount("45.60", "USD")),
//	    ast.NewPosting(checkingAccount),
//	)
func NewClearedTransaction(date *Date, narration string, postings ...*Posting) *Transaction {
	return NewTransaction(date, narration,
		WithFlag("*"),
		WithPostings(postings...),
	)
}

// NewPendingTransaction creates a Transaction with flag="!" (pending).
// This is useful for transactions that haven't cleared yet.
//
// Example:
//
//	txn := ast.NewPendingTransaction(date, "Pending transfer",
//	    ast.NewPosting(savingsAccount, ast.WithAmount("1000.00", "USD")),
//	    ast.NewPosting(checkingAccount),
//	)
func NewPendingTransaction(date *Date, narration string, postings ...*Posting) *Transaction {
	return NewTransaction(date, narration,
		WithFlag("!"),
		WithPostings(postings...),
	)
}

// NewOpen creates an Open directive for an account.
// The constraintCurrencies parameter can be nil for no currency constraints.
// The bookingMethod parameter can be empty for default booking (typically FIFO).
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-01")
//	account, _ := ast.NewAccount("Assets:US:BofA:Checking")
//	open := ast.NewOpen(date, account, []string{"USD"}, "")
func NewOpen(date *Date, account Account, constraintCurrencies []string, bookingMethod string) *Open {
	return &Open{
		date:                 date,
		Account:              account,
		ConstraintCurrencies: constraintCurrencies,
		BookingMethod:        bookingMethod,
	}
}

// NewClose creates a Close directive for an account.
//
// Example:
//
//	date, _ := ast.NewDate("2024-12-31")
//	account, _ := ast.NewAccount("Liabilities:CreditCard:OldCard")
//	close := ast.NewClose(date, account)
func NewClose(date *Date, account Account) *Close {
	return &Close{
		date:    date,
		Account: account,
	}
}

// NewBalance creates a Balance assertion directive.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-31")
//	account, _ := ast.NewAccount("Assets:Checking")
//	balance := ast.NewBalance(date, account, ast.NewAmount("1250.00", "USD"))
func NewBalance(date *Date, account Account, amount *Amount) *Balance {
	return &Balance{
		date:    date,
		Account: account,
		Amount:  amount,
	}
}

// NewPad creates a Pad directive to automatically balance an account.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-01")
//	account, _ := ast.NewAccount("Assets:Checking")
//	padAccount, _ := ast.NewAccount("Equity:Opening-Balances")
//	pad := ast.NewPad(date, account, padAccount)
func NewPad(date *Date, account, padAccount Account) *Pad {
	return &Pad{
		date:       date,
		Account:    account,
		AccountPad: padAccount,
	}
}

// NewNote creates a Note directive for an account.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-15")
//	account, _ := ast.NewAccount("Assets:Checking")
//	note := ast.NewNote(date, account, "Opened new checking account")
func NewNote(date *Date, account Account, description string) *Note {
	return &Note{
		date:        date,
		Account:     account,
		Description: NewRawString(description),
	}
}

// NewDocument creates a Document directive linking a file to an account.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-15")
//	account, _ := ast.NewAccount("Assets:Checking")
//	doc := ast.NewDocument(date, account, "/path/to/statement.pdf")
func NewDocument(date *Date, account Account, pathToDocument string) *Document {
	return &Document{
		date:           date,
		Account:        account,
		PathToDocument: NewRawString(pathToDocument),
	}
}

// NewCommodity creates a Commodity directive.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-01")
//	commodity := ast.NewCommodity(date, "USD")
func NewCommodity(date *Date, currency string) *Commodity {
	return &Commodity{
		date:     date,
		Currency: currency,
	}
}

// NewPrice creates a Price directive for a commodity.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-15")
//	price := ast.NewPrice(date, "HOOL", ast.NewAmount("520.50", "USD"))
func NewPrice(date *Date, commodity string, amount *Amount) *Price {
	return &Price{
		date:      date,
		Commodity: commodity,
		Amount:    amount,
	}
}

// NewEvent creates an Event directive.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-01")
//	event := ast.NewEvent(date, "location", "New York, USA")
func NewEvent(date *Date, name, value string) *Event {
	return &Event{
		date:  date,
		Name:  NewRawString(name),
		Value: NewRawString(value),
	}
}

// NewCustom creates a Custom directive.
//
// Example:
//
//	date, _ := ast.NewDate("2024-01-01")
//	custom := ast.NewCustom(date, "fava-option", nil)
func NewCustom(date *Date, typeName string, values []*CustomValue) *Custom {
	return &Custom{
		date:   date,
		Type:   RawString{Value: typeName},
		Values: values,
	}
}
