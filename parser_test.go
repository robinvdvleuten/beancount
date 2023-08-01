package beancount

import (
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/repr"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		beancount string
		fail      string
		expected  *AST
	}{
		{
			name: "Open",
			beancount: `
				2014-05-01 open Liabilities:CreditCard:CapitalOne     USD
			`,
			expected: beancount(
				open("2014-05-01", "Liabilities:CreditCard:CapitalOne", []string{"USD"}, "STRICT"),
			),
		},
		{
			name: "OpenWithMultipleConstraintCurrencies",
			beancount: `
				2014-05-01 open Equity:Opening-Balances USD, EUR NONE
			`,
			expected: beancount(
				open("2014-05-01", "Equity:Opening-Balances", []string{"USD", "EUR"}, "NONE"),
			),
		},
		{
			name: "Close",
			beancount: `
				2016-11-28 close Liabilities:CreditCard:CapitalOne
			`,
			expected: beancount(
				close("2016-11-28", "Liabilities:CreditCard:CapitalOne"),
			),
		},
		{
			name: "Commodity",
			beancount: `
				1867-07-01 commodity CAD
			`,
			expected: beancount(
				commodity("1867-07-01", "CAD"),
			),
		},
		{
			name: "CommodityWithMetadata",
			beancount: `
				1867-07-01 commodity CAD
					name: "Hooli Corporation Class C Shares"
					asset-class: "stock"
			`,
			expected: beancount(
				withMeta(
					commodity("1867-07-01", "CAD"),
					meta("name", "Hooli Corporation Class C Shares"),
					meta("asset-class", "stock"),
				),
			),
		},
		{
			name: "Transaction",
			beancount: `
				2014-05-05 txn "Cafe Mogador" "Lamb tagine with wine"
					Liabilities:CreditCard:CapitalOne         -37.45 USD
					Expenses:Restaurant
			`,
			expected: beancount(
				transaction("2014-05-05", "*", "Cafe Mogador", "Lamb tagine with wine",
					posting("Liabilities:CreditCard:CapitalOne", "*", amount("-37.45", "USD"), nil, nil),
					posting("Expenses:Restaurant", "*", nil, nil, nil),
				),
			),
		},
		{
			name: "TransactionWithoutPayee",
			beancount: `
				2014-05-05 ! "Lamb tagine with wine"
					Liabilities:CreditCard:CapitalOne         -37.45 USD
			`,
			expected: beancount(
				transaction("2014-05-05", "!", "", "Lamb tagine with wine",
					posting("Liabilities:CreditCard:CapitalOne", "!", amount("-37.45", "USD"), nil, nil),
				),
			),
		},
		{
			name: "TransactionWithFlaggedPosting",
			beancount: `
				2014-05-05 * "Lamb tagine with wine"
					! Liabilities:CreditCard:CapitalOne         -37.45 USD
			`,
			expected: beancount(
				transaction("2014-05-05", "*", "", "Lamb tagine with wine",
					posting("Liabilities:CreditCard:CapitalOne", "!", amount("-37.45", "USD"), nil, nil),
				),
			),
		},
		{
			name: "TransactionWithCommentedPostings",
			beancount: `
				2014-03-19 * "Acme Corp" "Bi-monthly salary payment"
					Assets:MyBank:Checking             3062.68 USD     ; Direct deposit
					Income:AcmeCorp:Salary            -4615.38 USD     ; Gross salary
			`,
			expected: beancount(
				transaction("2014-03-19", "*", "Acme Corp", "Bi-monthly salary payment",
					posting("Assets:MyBank:Checking", "*", amount("3062.68", "USD"), nil, nil),
					posting("Income:AcmeCorp:Salary", "*", amount("-4615.38", "USD"), nil, nil),
				),
			),
		},
		{
			name: "TransactionWithMetadata",
			beancount: `
			2013-08-26 * "Buying some shares of Hooli"
				statement: "confirmation-826453.pdf"
				Assets:BTrade:HOOLI      10 HOOL @ 498.45 USD
					decision: "scheduled"
				Assets:BTrade:Cash
			`,
			expected: beancount(
				withMeta(
					transaction("2013-08-26", "*", "", "Buying some shares of Hooli",
						withMeta(
							posting("Assets:BTrade:HOOLI", "*", amount("10", "HOOL"), price("498.45", "USD", false), nil),
							meta("decision", "scheduled"),
						),
						posting("Assets:BTrade:Cash", "*", nil, nil, nil),
					),
					meta("statement", "confirmation-826453.pdf"),
				),
			),
		},
		{
			name: "TransactionWithTotalPrice",
			beancount: `
				2012-11-03 * "Transfer to account in Canada"
					Assets:MyBank:Checking            -400.00 USD @@ 436.01 CAD
					Assets:FR:SocGen:Checking          436.01 CAD
			`,
			expected: beancount(
				transaction("2012-11-03", "*", "", "Transfer to account in Canada",
					posting("Assets:MyBank:Checking", "*", amount("-400.00", "USD"), price("436.01", "CAD", true), nil),
					posting("Assets:FR:SocGen:Checking", "*", amount("436.01", "CAD"), nil, nil),
				),
			),
		},
		{
			name: "TransactionWithCost",
			beancount: `
				2014-02-11 * "Bought shares of S&P 500"
					Assets:ETrade:IVV                10 IVV {183.07 USD}
					Assets:ETrade:Cash         -1830.70 USD
			`,
			expected: beancount(
				transaction("2014-02-11", "*", "", "Bought shares of S&P 500",
					posting("Assets:ETrade:IVV", "*", amount("10", "IVV"), nil, amount("183.07", "USD")),
					posting("Assets:ETrade:Cash", "*", amount("-1830.70", "USD"), nil, nil),
				),
			),
		},
		{
			name: "Option",
			beancount: `
				option "title" "Example Beancount file"
			`,
			expected: withOptions(
				beancount(),
				option("title", "Example Beancount file"),
			),
		},
		{
			name: "Include",
			beancount: `
				include "2024.beancount"
			`,
			expected: withIncludes(
				beancount(),
				include("2024.beancount"),
			),
		},
		{
			name: "Comment",
			beancount: `
				; 1792-01-01 commodity USD
			`,
			expected: beancount(),
		},
		{
			name: "Kitchensink",
			beancount: `
				;; -*- mode: org; mode: beancount; -*-

				* Options
				
				option "title" "Example Beancount file"
				option "operating_currency" "USD"
				
				* Commodities
				
				1792-01-01 commodity USD
					export: "CASH"
					name: "US Dollar"
				
				* Accounts
				
				2019-01-01 open Equity:Opening-Balances USD
				
				* Transactions
				
				2019-01-01 txn
					Assets:US:BofA:Checking  4135.73 USD
					Equity:Opening-Balances -4135.73 USD
				
				2019-01-01 *
					Liabilities:CreditCard:CapitalOne -37.45 USD
				
				2019-01-01 !
					Assets:ETrade:IVV                +10 IVV
				
				2019-01-01 txn "Lamb tagine with wine"
					Assets:US:BofA:Checking 4135.73 USD @ 412 EUR
				
				2019-01-01 * "Lamb tagine with wine"
					Assets:US:BofA:Checking 4135.73 USD @@ 488.22 EUR
				
				2019-01-01 * "Cafe Mogador" "Lamb tagine with wine"
				
				2019-01-01 * "Cafe Mogador" ""
			`,
			expected: withOptions(
				beancount(
					withMeta(
						commodity("1792-01-01", "USD"),
						meta("export", "CASH"),
						meta("name", "US Dollar"),
					),
					open("2019-01-01", "Equity:Opening-Balances", []string{"USD"}, "STRICT"),
					transaction("2019-01-01", "*", "", "",
						posting("Assets:US:BofA:Checking", "*", amount("4135.73", "USD"), nil, nil),
						posting("Equity:Opening-Balances", "*", amount("-4135.73", "USD"), nil, nil),
					),
					transaction("2019-01-01", "*", "", "",
						posting("Liabilities:CreditCard:CapitalOne", "*", amount("-37.45", "USD"), nil, nil),
					),
					transaction("2019-01-01", "!", "", "",
						posting("Assets:ETrade:IVV", "!", amount("+10", "IVV"), nil, nil),
					),
					transaction("2019-01-01", "*", "", "Lamb tagine with wine",
						posting("Assets:US:BofA:Checking", "*", amount("4135.73", "USD"), price("412", "EUR", false), nil),
					),
					transaction("2019-01-01", "*", "", "Lamb tagine with wine",
						posting("Assets:US:BofA:Checking", "*", amount("4135.73", "USD"), price("488.22", "EUR", true), nil),
					),
					transaction("2019-01-01", "*", "Cafe Mogador", "Lamb tagine with wine"),
					transaction("2019-01-01", "*", "Cafe Mogador", ""),
				),
				option("title", "Example Beancount file"),
				option("operating_currency", "USD"),
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ast, err := Parse(test.beancount)
			if test.fail != "" {
				assert.EqualError(t, err, test.fail)
			} else {
				assert.NoError(t, err)
				assert.Equal(t,
					repr.String(test.expected, repr.Indent("  ")),
					repr.String(ast, repr.Indent("  ")))
			}
		})
	}
}

func TestParseExample(t *testing.T) {
	buffer, err := os.ReadFile("./testdata/example.beancount")
	assert.NoError(t, err)

	_, err = Parse(string(buffer))
	assert.NoError(t, err)
}

func beancount(directives ...Directive) *AST {
	return &AST{Directives: directives}
}

func open(date string, account string, constraintCurrencies []string, bookingMethod string) *Open {
	return &Open{Date: date, Account: account, ConstraintCurrencies: constraintCurrencies, BookingMethod: bookingMethod}
}

func close(date string, account string) *Close {
	return &Close{Date: date, Account: account}
}

func commodity(date string, currency string) *Commodity {
	return &Commodity{Date: date, Currency: currency}
}

func transaction(date string, flag string, payee string, narration string, postings ...*Posting) *Transaction {
	return &Transaction{Date: date, Flag: flag, Payee: payee, Narration: narration, Postings: postings}
}

func posting(account string, flag string, amount *Amount, price *Price, cost *Amount) *Posting {
	return &Posting{Account: account, Flag: flag, Amount: amount, Price: price, Cost: cost}
}

func price(value string, currency string, total bool) *Price {
	return &Price{Amount: *amount(value, currency), Total: total}
}

func amount(value string, currency string) *Amount {
	return &Amount{Value: value, Currency: currency}
}

func option(name string, value string) *Option {
	return &Option{Name: name, Value: value}
}

func withOptions(ast *AST, options ...*Option) *AST {
	ast.Options = options
	return ast
}

func include(filename string) *Include {
	return &Include{Filename: filename}
}

func withIncludes(ast *AST, includes ...*Include) *AST {
	ast.Includes = includes
	return ast
}

func meta(key string, value string) *Metadata {
	return &Metadata{Key: key, Value: value}
}

func withMeta[W WithMetadata](w W, metadata ...*Metadata) W {
	w.AddMetadata(metadata...)
	return w
}
