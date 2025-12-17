package ast

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
	Pos   Position
	Name  string
	Value string
}

func (o *Option) Position() Position { return o.Pos }

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
	Pos      Position
	Filename string
}

func (i *Include) Position() Position { return i.Pos }

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
	Pos    Position
	Name   string
	Config string
}

func (p *Plugin) Position() Position { return p.Pos }

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
	Pos Position
	Tag Tag
}

func (p *Pushtag) Position() Position { return p.Pos }

// Poptag removes a tag from the tag stack, ending the automatic application of that tag
// to subsequent transactions. It must match a previously pushed tag. Transactions appearing
// after the poptag will no longer automatically receive the specified tag.
//
// Example:
//
//	poptag #trip-europe
type Poptag struct {
	Pos Position
	Tag Tag
}

func (p *Poptag) Position() Position { return p.Pos }

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
	Pos   Position
	Key   string
	Value string
}

func (p *Pushmeta) Position() Position { return p.Pos }

// Popmeta removes a metadata key from the metadata stack, ending the automatic application
// of that metadata to subsequent directives. It must match a previously pushed metadata key.
// Directives appearing after the popmeta will no longer automatically receive the specified
// metadata entry.
//
// Example:
//
//	popmeta location:
type Popmeta struct {
	Pos Position
	Key string
}

func (p *Popmeta) Position() Position { return p.Pos }
