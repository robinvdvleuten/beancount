package ast

import "github.com/alecthomas/participle/v2/lexer"

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
