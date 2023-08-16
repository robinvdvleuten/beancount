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
				2014-05-01 open Equity:Opening-Balances
			`,
			expected: beancount(
				open("2014-05-01", "Equity:Opening-Balances", nil, ""),
			),
		},
		{
			name: "OpenWithConstraintCurrencies",
			beancount: `
				2014-05-01 open Liabilities:CreditCard:CapitalOne     USD
			`,
			expected: beancount(
				open("2014-05-01", "Liabilities:CreditCard:CapitalOne", []string{"USD"}, ""),
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
			name: "Balance",
			beancount: `
				2021-01-02 balance Assets:US:BofA:Checking        3793.56 USD 
			`,
			expected: beancount(
				balance("2021-01-02", "Assets:US:BofA:Checking", amount("3793.56", "USD")),
			),
		},
		{
			name: "BalanceMultipleCommodities",
			beancount: `
				; Check cash balances from wallet
				2014-08-09 balance Assets:Cash     562.00 USD
				2014-08-09 balance Assets:Cash     210.00 CAD
				2014-08-09 balance Assets:Cash      60.00 EUR
			`,
			expected: beancount(
				balance("2014-08-09", "Assets:Cash", amount("562.00", "USD")),
				balance("2014-08-09", "Assets:Cash", amount("210.00", "CAD")),
				balance("2014-08-09", "Assets:Cash", amount("60.00", "EUR")),
			),
		},
		{
			name: "Pad",
			beancount: `
				2002-01-17 pad Assets:US:BofA:Checking Equity:Opening-Balances
			`,
			expected: beancount(
				pad("2002-01-17", "Assets:US:BofA:Checking", "Equity:Opening-Balances"),
			),
		},
		{
			name: "Note",
			beancount: `
				2013-11-03 note Liabilities:CreditCard "Called about fraudulent card."
			`,
			expected: beancount(
				note("2013-11-03", "Liabilities:CreditCard", "Called about fraudulent card."),
			),
		},
		{
			name: "Document",
			beancount: `
				2013-11-03 document Liabilities:CreditCard "/home/joe/stmts/apr-2014.pdf"
			`,
			expected: beancount(
				doc("2013-11-03", "Liabilities:CreditCard", "/home/joe/stmts/apr-2014.pdf"),
			),
		},
		{
			name: "Price",
			beancount: `
				2014-07-09 price HOOL  579.18 USD
			`,
			expected: beancount(
				price("2014-07-09", "HOOL", amount("579.18", "USD")),
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
				transaction("2014-05-05", "", "Cafe Mogador", "Lamb tagine with wine",
					posting("Liabilities:CreditCard:CapitalOne", "", amount("-37.45", "USD"), nil, false, nil),
					posting("Expenses:Restaurant", "", nil, nil, false, nil),
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
					posting("Liabilities:CreditCard:CapitalOne", "", amount("-37.45", "USD"), nil, false, nil),
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
					posting("Liabilities:CreditCard:CapitalOne", "!", amount("-37.45", "USD"), nil, false, nil),
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
					posting("Assets:MyBank:Checking", "", amount("3062.68", "USD"), nil, false, nil),
					posting("Income:AcmeCorp:Salary", "", amount("-4615.38", "USD"), nil, false, nil),
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
							posting("Assets:BTrade:HOOLI", "", amount("10", "HOOL"), amount("498.45", "USD"), false, nil),
							meta("decision", "scheduled"),
						),
						posting("Assets:BTrade:Cash", "", nil, nil, false, nil),
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
					posting("Assets:MyBank:Checking", "", amount("-400.00", "USD"), amount("436.01", "CAD"), true, nil),
					posting("Assets:FR:SocGen:Checking", "", amount("436.01", "CAD"), nil, false, nil),
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
					posting("Assets:ETrade:IVV", "", amount("10", "IVV"), nil, false, amount("183.07", "USD")),
					posting("Assets:ETrade:Cash", "", amount("-1830.70", "USD"), nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithPadding",
			beancount: `
				2002-01-17 P "(Padding inserted for balance of 987.34 USD)"
					Assets:US:BofA:Checking        987.34 USD
					Equity:Opening-Balances       -987.34 USD
			`,
			expected: beancount(
				transaction("2002-01-17", "P", "", "(Padding inserted for balance of 987.34 USD)",
					posting("Assets:US:BofA:Checking", "", amount("987.34", "USD"), nil, false, nil),
					posting("Equity:Opening-Balances", "", amount("-987.34", "USD"), nil, false, nil),
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
			name: "Comment",
			beancount: `
				; 1792-01-01 commodity USD
			`,
			expected: beancount(),
		},
		{
			name: "SortDirectives",
			beancount: `
				1980-05-12 open Equity:Opening-Balances

				1792-01-01 commodity USD

				2019-01-01 txn
					Assets:US:BofA:Checking  4135.73 USD
					Equity:Opening-Balances

				2018-01-01 open Assets:US:BofA:Checking USD
			`,
			expected: beancount(
				commodity("1792-01-01", "USD"),
				open("1980-05-12", "Equity:Opening-Balances", nil, ""),
				open("2018-01-01", "Assets:US:BofA:Checking", []string{"USD"}, ""),
				transaction("2019-01-01", "", "", "",
					posting("Assets:US:BofA:Checking", "", amount("4135.73", "USD"), nil, false, nil),
					posting("Equity:Opening-Balances", "", nil, nil, false, nil),
				),
			),
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

				* Equity Accounts

				1980-05-12 open Equity:Opening-Balances

				* Accounts

				2019-01-01 open Assets:US:BofA:Checking USD

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
					open("1980-05-12", "Equity:Opening-Balances", nil, ""),
					open("2019-01-01", "Assets:US:BofA:Checking", []string{"USD"}, ""),
					transaction("2019-01-01", "", "", "",
						posting("Assets:US:BofA:Checking", "", amount("4135.73", "USD"), nil, false, nil),
						posting("Equity:Opening-Balances", "", amount("-4135.73", "USD"), nil, false, nil),
					),
					transaction("2019-01-01", "*", "", "",
						posting("Liabilities:CreditCard:CapitalOne", "", amount("-37.45", "USD"), nil, false, nil),
					),
					transaction("2019-01-01", "!", "", "",
						posting("Assets:ETrade:IVV", "", amount("+10", "IVV"), nil, false, nil),
					),
					transaction("2019-01-01", "", "", "Lamb tagine with wine",
						posting("Assets:US:BofA:Checking", "", amount("4135.73", "USD"), amount("412", "EUR"), false, nil),
					),
					transaction("2019-01-01", "*", "", "Lamb tagine with wine",
						posting("Assets:US:BofA:Checking", "", amount("4135.73", "USD"), amount("488.22", "EUR"), true, nil),
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
			// fmt.Println(parser.String())

			ast, err := ParseString(test.beancount)
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
	data, err := os.ReadFile("./testdata/example.beancount")
	assert.NoError(t, err)

	_, err = ParseBytes(data)
	assert.NoError(t, err)
}

func beancount(directives ...Directive) *AST {
	return &AST{Directives: directives}
}

func open(d string, account string, constraintCurrencies []string, bookingMethod string) *Open {
	return &Open{Date: date(d), Account: account, ConstraintCurrencies: constraintCurrencies, BookingMethod: bookingMethod}
}

func close(d string, account string) *Close {
	return &Close{Date: date(d), Account: account}
}

func commodity(d string, currency string) *Commodity {
	return &Commodity{Date: date(d), Currency: currency}
}

func balance(d string, account string, amount *Amount) *Balance {
	return &Balance{Date: date(d), Account: account, Amount: amount}
}

func pad(d string, account string, accountPad string) *Pad {
	return &Pad{Date: date(d), Account: account, AccountPad: accountPad}
}

func note(d string, account string, description string) *Note {
	return &Note{Date: date(d), Account: account, Description: description}
}

func doc(d string, account string, pathToDocument string) *Document {
	return &Document{Date: date(d), Account: account, PathToDocument: pathToDocument}
}

func price(d string, commodity string, amount *Amount) *Price {
	return &Price{Date: date(d), Commodity: commodity, Amount: amount}
}

func transaction(d string, flag string, payee string, narration string, postings ...*Posting) *Transaction {
	return &Transaction{Date: date(d), Flag: flag, Payee: payee, Narration: narration, Postings: postings}
}

func posting(account string, flag string, amount *Amount, price *Amount, priceTotal bool, cost *Amount) *Posting {
	return &Posting{Account: account, Flag: flag, Amount: amount, Price: price, PriceTotal: priceTotal, Cost: cost}
}

func amount(value string, currency string) *Amount {
	return &Amount{Value: value, Currency: currency}
}

func date(value string) *Date {
	d := &Date{}
	d.Capture([]string{value})
	return d
}

func option(name string, value string) *Option {
	return &Option{Name: name, Value: value}
}

func withOptions(ast *AST, options ...*Option) *AST {
	ast.Options = options
	return ast
}

func meta(key string, value string) *Metadata {
	return &Metadata{Key: key, Value: value}
}

func withMeta[W WithMetadata](w W, metadata ...*Metadata) W {
	w.AddMetadata(metadata...)
	return w
}
