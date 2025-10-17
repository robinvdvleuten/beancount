package parser

import (
	"os"
	"reflect"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/participle/v2/lexer"
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
				2014-05-01 open Equity:Opening-Balances USD, EUR "NONE"
			`,
			expected: beancount(
				open("2014-05-01", "Equity:Opening-Balances", []string{"USD", "EUR"}, "NONE"),
			),
		},
		{
			name: "OpenWithBookingMethodFIFO",
			beancount: `
				2023-01-01 open Assets:Investments:BTC BTC "FIFO"
			`,
			expected: beancount(
				open("2023-01-01", "Assets:Investments:BTC", []string{"BTC"}, "FIFO"),
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
			name: "Event",
			beancount: `
				2014-07-09 event "location" "Paris, France"
			`,
			expected: beancount(
				event("2014-07-09", "location", "Paris, France"),
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
			name: "TransactionWithLink",
			beancount: `
				2014-02-05 * "Invoice for January" ^invoice-pepe-studios-jan14
					Assets:AccountsReceivable         -8450.00 USD
					Income:Clients:PepeStudios
			`,
			expected: beancount(
				withLinks(
					transaction("2014-02-05", "*", "", "Invoice for January",
						posting("Assets:AccountsReceivable", "", amount("-8450.00", "USD"), nil, false, nil),
						posting("Income:Clients:PepeStudios", "", nil, nil, false, nil),
					),
					"invoice-pepe-studios-jan14",
				),
			),
		},
		{
			name: "TransactionWithMultipleLinks",
			beancount: `
				2014-02-20 * "Check deposit - paying invoice" ^invoice-pepe-studios-jan14 ^payment-check
					Assets:BofA:Checking               8450.00 USD
					Assets:AccountsReceivable
			`,
			expected: beancount(
				withLinks(
					transaction("2014-02-20", "*", "", "Check deposit - paying invoice",
						posting("Assets:BofA:Checking", "", amount("8450.00", "USD"), nil, false, nil),
						posting("Assets:AccountsReceivable", "", nil, nil, false, nil),
					),
					"invoice-pepe-studios-jan14",
					"payment-check",
				),
			),
		},
		{
			name: "TransactionWithTag",
			beancount: `
				2021-06-15 * "Waterbar" "" #trip-san-francisco-2021
					Liabilities:US:Chase:Slate                       -46.68 USD
					Expenses:Food:Restaurant                          46.68 USD
			`,
			expected: beancount(
				withTags(
					transaction("2021-06-15", "*", "Waterbar", "",
						posting("Liabilities:US:Chase:Slate", "", amount("-46.68", "USD"), nil, false, nil),
						posting("Expenses:Food:Restaurant", "", amount("46.68", "USD"), nil, false, nil),
					),
					"trip-san-francisco-2021",
				),
			),
		},
		{
			name: "TransactionWithMultipleTags",
			beancount: `
				2014-04-23 * "Flight to Berlin" #trip-berlin #vacation
					Assets:MyBank:Checking            -1230.27 USD
					Expenses:Flights
			`,
			expected: beancount(
				withTags(
					transaction("2014-04-23", "*", "", "Flight to Berlin",
						posting("Assets:MyBank:Checking", "", amount("-1230.27", "USD"), nil, false, nil),
						posting("Expenses:Flights", "", nil, nil, false, nil),
					),
					"trip-berlin",
					"vacation",
				),
			),
		},
		{
			name: "TransactionWithLinksAndTags",
			beancount: `
				2014-04-23 * "Flight to Berlin" ^invoice-123 #trip-berlin #vacation
					Assets:MyBank:Checking            -1230.27 USD
					Expenses:Flights
			`,
			expected: beancount(
				withTags(
					withLinks(
						transaction("2014-04-23", "*", "", "Flight to Berlin",
							posting("Assets:MyBank:Checking", "", amount("-1230.27", "USD"), nil, false, nil),
							posting("Expenses:Flights", "", nil, nil, false, nil),
						),
						"invoice-123",
					),
					"trip-berlin",
					"vacation",
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
					posting("Assets:ETrade:IVV", "", amount("10", "IVV"), nil, false, cost(amount("183.07", "USD"), nil)),
					posting("Assets:ETrade:Cash", "", amount("-1830.70", "USD"), nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithCostAndDate",
			beancount: `
				2021-09-22 * "Buy shares of ITOT"
					Assets:US:ETrade:ITOT                16 ITOT {85.66 USD, 2021-09-22}
					Assets:US:ETrade:Cash           -1379.51 USD
			`,
			expected: beancount(
				transaction("2021-09-22", "*", "", "Buy shares of ITOT",
					posting("Assets:US:ETrade:ITOT", "", amount("16", "ITOT"), nil, false, cost(amount("85.66", "USD"), date("2021-09-22"))),
					posting("Assets:US:ETrade:Cash", "", amount("-1379.51", "USD"), nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithCostDateAndPrice",
			beancount: `
				2021-12-15 * "Sell specific lot"
					Assets:US:ETrade:ITOT               -16 ITOT {85.66 USD, 2021-09-22} @ 90.10 USD
					Assets:US:ETrade:Cash
			`,
			expected: beancount(
				transaction("2021-12-15", "*", "", "Sell specific lot",
					posting("Assets:US:ETrade:ITOT", "", amount("-16", "ITOT"), amount("90.10", "USD"), false, cost(amount("85.66", "USD"), date("2021-09-22"))),
					posting("Assets:US:ETrade:Cash", "", nil, nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithCostAndLabel",
			beancount: `
				2021-09-22 * "Buy shares with reference"
					Assets:Stocks                          10 AAPL {150.00 USD, "ref-001"}
					Assets:Cash
			`,
			expected: beancount(
				transaction("2021-09-22", "*", "", "Buy shares with reference",
					posting("Assets:Stocks", "", amount("10", "AAPL"), nil, false, costWithLabel(amount("150.00", "USD"), nil, "ref-001")),
					posting("Assets:Cash", "", nil, nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithCostDateAndLabel",
			beancount: `
				2021-09-22 * "Buy shares with date and label"
					Assets:Stocks                          10 AAPL {150.00 USD, 2021-09-22, "Batch A"}
					Assets:Cash
			`,
			expected: beancount(
				transaction("2021-09-22", "*", "", "Buy shares with date and label",
					posting("Assets:Stocks", "", amount("10", "AAPL"), nil, false, costWithLabel(amount("150.00", "USD"), date("2021-09-22"), "Batch A")),
					posting("Assets:Cash", "", nil, nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithEmptyCost",
			beancount: `
				2021-11-15 * "Sell stock - any lot"
					Assets:Stocks                         -10 AAPL {}
					Assets:Cash
			`,
			expected: beancount(
				transaction("2021-11-15", "*", "", "Sell stock - any lot",
					posting("Assets:Stocks", "", amount("-10", "AAPL"), nil, false, emptyCost()),
					posting("Assets:Cash", "", nil, nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithEmptyCostAndPrice",
			beancount: `
				2021-11-15 * "Sell stock - any lot with current price"
					Assets:Stocks                         -10 AAPL {} @ 175.00 USD
					Assets:Cash
			`,
			expected: beancount(
				transaction("2021-11-15", "*", "", "Sell stock - any lot with current price",
					posting("Assets:Stocks", "", amount("-10", "AAPL"), amount("175.00", "USD"), false, emptyCost()),
					posting("Assets:Cash", "", nil, nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithMergeCost",
			beancount: `
				2021-09-22 * "Average cost basis"
					Assets:Stocks                          10 AAPL {*}
					Assets:Cash
			`,
			expected: beancount(
				transaction("2021-09-22", "*", "", "Average cost basis",
					posting("Assets:Stocks", "", amount("10", "AAPL"), nil, false, mergeCost()),
					posting("Assets:Cash", "", nil, nil, false, nil),
				),
			),
		},
		{
			name: "TransactionWithMergeCostAndLabel",
			beancount: `
				2021-09-22 * "Average cost basis with label"
					Assets:Stocks                          10 AAPL {*, "Consolidated"}
					Assets:Cash
			`,
			expected: beancount(
				transaction("2021-09-22", "*", "", "Average cost basis with label",
					posting("Assets:Stocks", "", amount("10", "AAPL"), nil, false, mergeCostWithLabel("Consolidated")),
					posting("Assets:Cash", "", nil, nil, false, nil),
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
			name: "TransactionKitchenSink",
			beancount: `
				2021-09-22 * "Diversified stock purchase" ^invoice-2021-q3 ^portfolio-rebalance #investment #stocks #retirement
					trade-confirmation: "CONF-987654321"
					broker: "E*TRADE"
					Assets:Brokerage:Cash                -5000.00 USD
					Assets:Brokerage:AAPL                      10 AAPL {150.00 USD, 2021-09-22, "Batch-Q3"} @ 152.00 USD
						note: "Tech allocation"
					Assets:Brokerage:MSFT                       5 MSFT {300.00 USD, 2021-09-22, "Batch-Q3"}
						note: "Tech allocation"
					* Assets:Brokerage:VTI                     20 VTI {100.00 USD, 2021-09-22}
					Expenses:Commissions                     9.95 USD
			`,
			expected: beancount(
				withTags(
					withLinks(
						withMeta(
							transaction("2021-09-22", "*", "", "Diversified stock purchase",
								posting("Assets:Brokerage:Cash", "", amount("-5000.00", "USD"), nil, false, nil),
								withMeta(
									posting("Assets:Brokerage:AAPL", "", amount("10", "AAPL"), amount("152.00", "USD"), false, costWithLabel(amount("150.00", "USD"), date("2021-09-22"), "Batch-Q3")),
									meta("note", "Tech allocation"),
								),
								withMeta(
									posting("Assets:Brokerage:MSFT", "", amount("5", "MSFT"), nil, false, costWithLabel(amount("300.00", "USD"), date("2021-09-22"), "Batch-Q3")),
									meta("note", "Tech allocation"),
								),
								posting("Assets:Brokerage:VTI", "*", amount("20", "VTI"), nil, false, cost(amount("100.00", "USD"), date("2021-09-22"))),
								posting("Expenses:Commissions", "", amount("9.95", "USD"), nil, false, nil),
							),
							meta("trade-confirmation", "CONF-987654321"),
							meta("broker", "E*TRADE"),
						),
						"invoice-2021-q3",
						"portfolio-rebalance",
					),
					"investment",
					"stocks",
					"retirement",
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
			name: "Plugin with name only",
			beancount: `
				plugin "beancount.plugins.auto_accounts"
			`,
			expected: withPlugins(
				beancount(),
				plugin("beancount.plugins.auto_accounts", ""),
			),
		},
		{
			name: "Plugin with name and config",
			beancount: `
				plugin "beancount.plugins.module_name" "configuration data"
			`,
			expected: withPlugins(
				beancount(),
				plugin("beancount.plugins.module_name", "configuration data"),
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
			name: "InvalidAccountName",
			beancount: `
				1980-05-12 open Foo:Bar
			`,
			fail: `2:20: failed to capture: unexpected account type "Foo"`,
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
				normalizeAST(ast)

				assert.NoError(t, err)
				assert.Equal(t,
					repr.String(test.expected, repr.Indent("  ")),
					repr.String(ast, repr.Indent("  ")))
			}
		})
	}
}

func TestParseExample(t *testing.T) {
	data, err := os.ReadFile("../testdata/example.beancount")
	assert.NoError(t, err)

	_, err = ParseBytes(data)
	assert.NoError(t, err)
}

func TestParseKitchenSink(t *testing.T) {
	data, err := os.ReadFile("../testdata/kitchensink.beancount")
	assert.NoError(t, err)

	ast, err := ParseBytes(data)
	assert.NoError(t, err)

	// Verify we parsed all the directives
	assert.True(t, len(ast.Directives) > 0, "Should have parsed directives")
	assert.True(t, len(ast.Options) > 0, "Should have parsed options")

	// Find and verify the kitchen sink transaction
	var kitchenSinkTxn *Transaction
	for _, dir := range ast.Directives {
		if txn, ok := dir.(*Transaction); ok {
			if txn.Narration == "Diversified Portfolio Rebalancing" {
				kitchenSinkTxn = txn
				break
			}
		}
	}

	assert.True(t, kitchenSinkTxn != nil, "Should find kitchen sink transaction")
	assert.Equal(t, 2, len(kitchenSinkTxn.Links), "Should have 2 links")
	assert.Equal(t, 4, len(kitchenSinkTxn.Tags), "Should have 4 tags")
	assert.Equal(t, 3, len(kitchenSinkTxn.Metadata), "Should have 3 metadata entries")
	assert.Equal(t, 5, len(kitchenSinkTxn.Postings), "Should have 5 postings")

	// Verify links are stripped of ^
	assert.Equal(t, Link("rebalance-q4"), kitchenSinkTxn.Links[0])
	assert.Equal(t, Link("invoice-2021-1215"), kitchenSinkTxn.Links[1])

	// Verify tags are stripped of #
	assert.Equal(t, Tag("investment"), kitchenSinkTxn.Tags[0])
	assert.Equal(t, Tag("stocks"), kitchenSinkTxn.Tags[1])
	assert.Equal(t, Tag("retirement"), kitchenSinkTxn.Tags[2])
	assert.Equal(t, Tag("tax-loss-harvesting"), kitchenSinkTxn.Tags[3])
}

func TestDateCapture(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "ValidDate",
			input:   "2024-03-15",
			wantErr: false,
		},
		{
			name:    "ValidLeapYear",
			input:   "2024-02-29",
			wantErr: false,
		},
		{
			name:    "ValidDateMinMonth",
			input:   "2024-01-15",
			wantErr: false,
		},
		{
			name:    "ValidDateMaxMonth",
			input:   "2024-12-31",
			wantErr: false,
		},
		{
			name:    "InvalidMonthZero",
			input:   "2024-00-15",
			wantErr: true,
			errMsg:  "invalid date: 2024-00-15",
		},
		{
			name:    "InvalidMonthThirteen",
			input:   "2024-13-15",
			wantErr: true,
			errMsg:  "invalid date: 2024-13-15",
		},
		{
			name:    "InvalidDayZero",
			input:   "2024-03-00",
			wantErr: true,
			errMsg:  "invalid date: 2024-03-00",
		},
		{
			name:    "InvalidDayThirtyTwo",
			input:   "2024-03-32",
			wantErr: true,
			errMsg:  "invalid date: 2024-03-32",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Date{}
			err := d.Capture([]string{tt.input})

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.EqualError(t, err, tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAccountCapture(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "ValidSimpleAccount",
			input:   "Assets:Cash",
			wantErr: false,
		},
		{
			name:    "ValidNestedAccount",
			input:   "Assets:US:BofA:Checking",
			wantErr: false,
		},
		{
			name:    "ValidAccountWithHyphen",
			input:   "Equity:Opening-Balances",
			wantErr: false,
		},
		{
			name:    "ValidAccountWithNumbers",
			input:   "Expenses:Taxes:Y2021:US:Federal",
			wantErr: false,
		},
		{
			name:    "ValidAccountStartingWithNumber",
			input:   "Assets:US:Federal:401k",
			wantErr: false,
		},
		{
			name:    "ValidAccountWithMixedCase",
			input:   "Assets:US:Federal:PreTax401k",
			wantErr: false,
		},
		{
			name:    "InvalidAccountType",
			input:   "Foo:Bar",
			wantErr: true,
			errMsg:  `unexpected account type "Foo"`,
		},
		{
			name:    "InvalidSegmentStartsWithLowercase",
			input:   "Assets:us:BofA",
			wantErr: true,
			errMsg:  "invalid account segment at position 1: us",
		},
		{
			name:    "InvalidSegmentStartsWithHyphen",
			input:   "Assets:-Invalid",
			wantErr: true,
			errMsg:  "invalid account segment at position 1: -Invalid",
		},
		{
			name:    "InvalidEmptySegment",
			input:   "Assets::Checking",
			wantErr: true,
			errMsg:  "invalid account segment at position 1: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Account("")
			err := a.Capture([]string{tt.input})

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.EqualError(t, err, tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, Account(tt.input), a)
			}
		})
	}
}

func normalizeAST(ast *AST) {
	if ast == nil {
		return
	}

	for _, dir := range ast.Directives {
		normalizeDirective(dir)
	}

	for _, opt := range ast.Options {
		if opt != nil {
			opt.Pos = lexer.Position{}
		}
	}

	for _, inc := range ast.Includes {
		if inc != nil {
			inc.Pos = lexer.Position{}
		}
	}

	for _, plugin := range ast.Plugins {
		if plugin != nil {
			plugin.Pos = lexer.Position{}
		}
	}
}

func normalizeDirective(d Directive) {
	if d == nil {
		return
	}

	rv := reflect.ValueOf(d)
	if rv.IsNil() {
		return
	}

	rv = reflect.Indirect(rv)
	posField := rv.FieldByName("Pos")
	if posField.IsValid() && posField.CanSet() {
		posField.Set(reflect.ValueOf(lexer.Position{}))
	}

	switch dir := d.(type) {
	case *Transaction:
		for _, posting := range dir.Postings {
			if posting != nil {
				posting.Pos = lexer.Position{}
			}
		}
	}
}

func beancount(directives ...Directive) *AST {
	return &AST{Directives: directives}
}

func open(d string, account Account, constraintCurrencies []string, bookingMethod string) *Open {
	return &Open{Date: date(d), Account: account, ConstraintCurrencies: constraintCurrencies, BookingMethod: bookingMethod}
}

func close(d string, account Account) *Close {
	return &Close{Date: date(d), Account: account}
}

func commodity(d string, currency string) *Commodity {
	return &Commodity{Date: date(d), Currency: currency}
}

func balance(d string, account Account, amount *Amount) *Balance {
	return &Balance{Date: date(d), Account: account, Amount: amount}
}

func pad(d string, account Account, accountPad Account) *Pad {
	return &Pad{Date: date(d), Account: account, AccountPad: accountPad}
}

func note(d string, account Account, description string) *Note {
	return &Note{Date: date(d), Account: account, Description: description}
}

func doc(d string, account Account, pathToDocument string) *Document {
	return &Document{Date: date(d), Account: account, PathToDocument: pathToDocument}
}

func price(d string, commodity string, amount *Amount) *Price {
	return &Price{Date: date(d), Commodity: commodity, Amount: amount}
}

func event(d string, name string, value string) *Event {
	return &Event{Date: date(d), Name: name, Value: value}
}

func transaction(d string, flag string, payee string, narration string, postings ...*Posting) *Transaction {
	return &Transaction{Date: date(d), Flag: flag, Payee: payee, Narration: narration, Postings: postings}
}

func posting(account Account, flag string, amount *Amount, price *Amount, priceTotal bool, cost *Cost) *Posting {
	return &Posting{Account: account, Flag: flag, Amount: amount, Price: price, PriceTotal: priceTotal, Cost: cost}
}

func amount(value string, currency string) *Amount {
	return &Amount{Value: value, Currency: currency}
}

func cost(amount *Amount, d *Date) *Cost {
	return &Cost{IsMerge: false, Amount: amount, Date: d, Label: ""}
}

func costWithLabel(amount *Amount, d *Date, label string) *Cost {
	return &Cost{IsMerge: false, Amount: amount, Date: d, Label: label}
}

func emptyCost() *Cost {
	return &Cost{IsMerge: false, Amount: nil, Date: nil, Label: ""}
}

func mergeCost() *Cost {
	return &Cost{IsMerge: true, Amount: nil, Date: nil, Label: ""}
}

func mergeCostWithLabel(label string) *Cost {
	return &Cost{IsMerge: true, Amount: nil, Date: nil, Label: label}
}

func date(value string) *Date {
	d := &Date{}

	if err := d.Capture([]string{value}); err != nil {
		panic(err)
	}

	return d
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

func plugin(name string, config string) *Plugin {
	return &Plugin{Name: name, Config: config}
}

func withPlugins(ast *AST, plugins ...*Plugin) *AST {
	ast.Plugins = plugins
	return ast
}

func meta(key string, value string) *Metadata {
	return &Metadata{Key: key, Value: value}
}

func withMeta[W WithMetadata](w W, metadata ...*Metadata) W {
	w.AddMetadata(metadata...)
	return w
}

func withLinks(t *Transaction, links ...string) *Transaction {
	t.Links = make([]Link, len(links))
	for i, link := range links {
		t.Links[i] = Link(link)
	}
	return t
}

func withTags(t *Transaction, tags ...string) *Transaction {
	t.Tags = make([]Tag, len(tags))
	for i, tag := range tags {
		t.Tags[i] = Tag(tag)
	}
	return t
}
