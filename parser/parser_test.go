package parser

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/alecthomas/repr"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		beancount string
		fail      string
		expected  *ast.AST
	}{
		{
			name: "Open",
			beancount: `
				2014-05-01 open Equity:Opening-Balances
			`,
			expected: beancount(
				ast.NewOpen(date("2014-05-01"), account("Equity:Opening-Balances"), nil, ""),
			),
		},
		{
			name: "OpenWithConstraintCurrencies",
			beancount: `
				2014-05-01 open Liabilities:CreditCard:CapitalOne     USD
			`,
			expected: beancount(
				ast.NewOpen(date("2014-05-01"), account("Liabilities:CreditCard:CapitalOne"), []string{"USD"}, ""),
			),
		},
		{
			name: "OpenWithMultipleConstraintCurrencies",
			beancount: `
				2014-05-01 open Equity:Opening-Balances USD, EUR "NONE"
			`,
			expected: beancount(
				ast.NewOpen(date("2014-05-01"), account("Equity:Opening-Balances"), []string{"USD", "EUR"}, "NONE"),
			),
		},
		{
			name: "OpenWithBookingMethodFIFO",
			beancount: `
				2023-01-01 open Assets:Investments:BTC BTC "FIFO"
			`,
			expected: beancount(
				ast.NewOpen(date("2023-01-01"), account("Assets:Investments:BTC"), []string{"BTC"}, "FIFO"),
			),
		},
		{
			name: "Close",
			beancount: `
				2016-11-28 close Liabilities:CreditCard:CapitalOne
			`,
			expected: beancount(
				ast.NewClose(date("2016-11-28"), account("Liabilities:CreditCard:CapitalOne")),
			),
		},
		{
			name: "Commodity",
			beancount: `
				1867-07-01 commodity CAD
			`,
			expected: beancount(
				ast.NewCommodity(date("1867-07-01"), "CAD"),
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
					ast.NewCommodity(date("1867-07-01"), "CAD"),
					ast.NewMetadata("name", "Hooli Corporation Class C Shares"),
					ast.NewMetadata("asset-class", "stock"),
				),
			),
		},
		{
			name: "Balance",
			beancount: `
				2021-01-02 balance Assets:US:BofA:Checking        3793.56 USD
			`,
			expected: beancount(
				ast.NewBalance(date("2021-01-02"), account("Assets:US:BofA:Checking"), ast.NewAmount("3793.56", "USD")),
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
				ast.NewBalance(date("2014-08-09"), account("Assets:Cash"), ast.NewAmount("562.00", "USD")),
				ast.NewBalance(date("2014-08-09"), account("Assets:Cash"), ast.NewAmount("210.00", "CAD")),
				ast.NewBalance(date("2014-08-09"), account("Assets:Cash"), ast.NewAmount("60.00", "EUR")),
			),
		},
		{
			name: "Pad",
			beancount: `
				2002-01-17 pad Assets:US:BofA:Checking Equity:Opening-Balances
			`,
			expected: beancount(
				ast.NewPad(date("2002-01-17"), account("Assets:US:BofA:Checking"), account("Equity:Opening-Balances")),
			),
		},
		{
			name: "Note",
			beancount: `
				2013-11-03 note Liabilities:CreditCard "Called about fraudulent card."
			`,
			expected: beancount(
				ast.NewNote(date("2013-11-03"), account("Liabilities:CreditCard"), "Called about fraudulent card."),
			),
		},
		{
			name: "Document",
			beancount: `
				2013-11-03 document Liabilities:CreditCard "/home/joe/stmts/apr-2014.pdf"
			`,
			expected: beancount(
				ast.NewDocument(date("2013-11-03"), account("Liabilities:CreditCard"), "/home/joe/stmts/apr-2014.pdf"),
			),
		},
		{
			name: "Price",
			beancount: `
				2014-07-09 price HOOL  579.18 USD
			`,
			expected: beancount(
				ast.NewPrice(date("2014-07-09"), "HOOL", ast.NewAmount("579.18", "USD")),
			),
		},
		{
			name: "Event",
			beancount: `
				2014-07-09 event "location" "Paris, France"
			`,
			expected: beancount(
				ast.NewEvent(date("2014-07-09"), "location", "Paris, France"),
			),
		},
		{
			name: "CustomWithStrings",
			beancount: `
				2014-07-09 custom "budget" "..."
			`,
			expected: beancount(
				custom(date("2014-07-09"), "budget", customString("...")),
			),
		},
		{
			name: "CustomWithMixedTypes",
			beancount: `
				2014-07-09 custom "budget" "..." TRUE 45.30 USD
			`,
			expected: beancount(
				custom(date("2014-07-09"), "budget",
					customString("..."),
					customBoolean("TRUE"),
					customAmount(ast.NewAmount("45.30", "USD")),
				),
			),
		},
		{
			name: "CustomWithAllTypes",
			beancount: `
				2015-01-01 custom "forecast" 100.00 USD FALSE "monthly" 42
			`,
			expected: beancount(
				custom(date("2015-01-01"), "forecast",
					customAmount(ast.NewAmount("100.00", "USD")),
					customBoolean("FALSE"),
					customString("monthly"),
					customNumber("42"),
				),
			),
		},
		{
			name: "CustomWithMetadata",
			beancount: `
				2014-07-09 custom "budget" "quarterly"
					note: "Annual budget planning"
					category: "financial"
			`,
			expected: beancount(
				withMeta(
					custom(date("2014-07-09"), "budget", customString("quarterly")),
					ast.NewMetadata("note", "Annual budget planning"),
					ast.NewMetadata("category", "financial"),
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
				ast.NewTransaction(date("2014-05-05"), "Lamb tagine with wine",
					ast.WithPayee("Cafe Mogador"),
					ast.WithPostings(
						ast.NewPosting(account("Liabilities:CreditCard:CapitalOne"),
							ast.WithAmount("-37.45", "USD")),
						ast.NewPosting(account("Expenses:Restaurant")),
					),
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
				ast.NewTransaction(date("2014-05-05"), "Lamb tagine with wine",
					ast.WithFlag("!"),
					ast.WithPostings(
						ast.NewPosting(account("Liabilities:CreditCard:CapitalOne"),
							ast.WithAmount("-37.45", "USD")),
					),
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
				ast.NewTransaction(date("2014-05-05"), "Lamb tagine with wine",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Liabilities:CreditCard:CapitalOne"),
							ast.WithAmount("-37.45", "USD"),
							ast.WithPostingFlag("!")),
					),
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
				ast.NewTransaction(date("2014-03-19"), "Bi-monthly salary payment",
					ast.WithFlag("*"),
					ast.WithPayee("Acme Corp"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:MyBank:Checking"),
							ast.WithAmount("3062.68", "USD")),
						ast.NewPosting(account("Income:AcmeCorp:Salary"),
							ast.WithAmount("-4615.38", "USD")),
					),
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
				ast.NewTransaction(date("2014-02-05"), "Invoice for January",
					ast.WithFlag("*"),
					ast.WithLinks("invoice-pepe-studios-jan14"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:AccountsReceivable"),
							ast.WithAmount("-8450.00", "USD")),
						ast.NewPosting(account("Income:Clients:PepeStudios")),
					),
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
				ast.NewTransaction(date("2014-02-20"), "Check deposit - paying invoice",
					ast.WithFlag("*"),
					ast.WithLinks("invoice-pepe-studios-jan14", "payment-check"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:BofA:Checking"),
							ast.WithAmount("8450.00", "USD")),
						ast.NewPosting(account("Assets:AccountsReceivable")),
					),
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
				ast.NewTransaction(date("2021-06-15"), "",
					ast.WithFlag("*"),
					ast.WithPayee("Waterbar"),
					ast.WithTags("trip-san-francisco-2021"),
					ast.WithPostings(
						ast.NewPosting(account("Liabilities:US:Chase:Slate"),
							ast.WithAmount("-46.68", "USD")),
						ast.NewPosting(account("Expenses:Food:Restaurant"),
							ast.WithAmount("46.68", "USD")),
					),
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
				ast.NewTransaction(date("2014-04-23"), "Flight to Berlin",
					ast.WithFlag("*"),
					ast.WithTags("trip-berlin", "vacation"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:MyBank:Checking"),
							ast.WithAmount("-1230.27", "USD")),
						ast.NewPosting(account("Expenses:Flights")),
					),
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
				ast.NewTransaction(date("2014-04-23"), "Flight to Berlin",
					ast.WithFlag("*"),
					ast.WithLinks("invoice-123"),
					ast.WithTags("trip-berlin", "vacation"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:MyBank:Checking"),
							ast.WithAmount("-1230.27", "USD")),
						ast.NewPosting(account("Expenses:Flights")),
					),
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
				ast.NewTransaction(date("2013-08-26"), "Buying some shares of Hooli",
					ast.WithFlag("*"),
					ast.WithTransactionMetadata(
						ast.NewMetadata("statement", "confirmation-826453.pdf"),
					),
					ast.WithPostings(
						ast.NewPosting(account("Assets:BTrade:HOOLI"),
							ast.WithAmount("10", "HOOL"),
							ast.WithPrice(ast.NewAmount("498.45", "USD")),
							ast.WithPostingMetadata(
								ast.NewMetadata("decision", "scheduled"),
							)),
						ast.NewPosting(account("Assets:BTrade:Cash")),
					),
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
				ast.NewTransaction(date("2012-11-03"), "Transfer to account in Canada",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:MyBank:Checking"),
							ast.WithAmount("-400.00", "USD"),
							ast.WithTotalPrice(ast.NewAmount("436.01", "CAD"))),
						ast.NewPosting(account("Assets:FR:SocGen:Checking"),
							ast.WithAmount("436.01", "CAD")),
					),
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
				ast.NewTransaction(date("2014-02-11"), "Bought shares of S&P 500",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:ETrade:IVV"),
							ast.WithAmount("10", "IVV"),
							ast.WithCost(ast.NewCost(ast.NewAmount("183.07", "USD")))),
						ast.NewPosting(account("Assets:ETrade:Cash"),
							ast.WithAmount("-1830.70", "USD")),
					),
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
				ast.NewTransaction(date("2021-09-22"), "Buy shares of ITOT",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:US:ETrade:ITOT"),
							ast.WithAmount("16", "ITOT"),
							ast.WithCost(ast.NewCostWithDate(ast.NewAmount("85.66", "USD"), date("2021-09-22")))),
						ast.NewPosting(account("Assets:US:ETrade:Cash"),
							ast.WithAmount("-1379.51", "USD")),
					),
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
				ast.NewTransaction(date("2021-12-15"), "Sell specific lot",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:US:ETrade:ITOT"),
							ast.WithAmount("-16", "ITOT"),
							ast.WithCost(ast.NewCostWithDate(ast.NewAmount("85.66", "USD"), date("2021-09-22"))),
							ast.WithPrice(ast.NewAmount("90.10", "USD"))),
						ast.NewPosting(account("Assets:US:ETrade:Cash")),
					),
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
				ast.NewTransaction(date("2021-09-22"), "Buy shares with reference",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Stocks"),
							ast.WithAmount("10", "AAPL"),
							ast.WithCost(ast.NewCostWithLabel(ast.NewAmount("150.00", "USD"), nil, "ref-001"))),
						ast.NewPosting(account("Assets:Cash")),
					),
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
				ast.NewTransaction(date("2021-09-22"), "Buy shares with date and label",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Stocks"),
							ast.WithAmount("10", "AAPL"),
							ast.WithCost(ast.NewCostWithLabel(ast.NewAmount("150.00", "USD"), date("2021-09-22"), "Batch A"))),
						ast.NewPosting(account("Assets:Cash")),
					),
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
				ast.NewTransaction(date("2021-11-15"), "Sell stock - any lot",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Stocks"),
							ast.WithAmount("-10", "AAPL"),
							ast.WithCost(ast.NewEmptyCost())),
						ast.NewPosting(account("Assets:Cash")),
					),
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
				ast.NewTransaction(date("2021-11-15"), "Sell stock - any lot with current price",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Stocks"),
							ast.WithAmount("-10", "AAPL"),
							ast.WithCost(ast.NewEmptyCost()),
							ast.WithPrice(ast.NewAmount("175.00", "USD"))),
						ast.NewPosting(account("Assets:Cash")),
					),
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
				ast.NewTransaction(date("2021-09-22"), "Average cost basis",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Stocks"),
							ast.WithAmount("10", "AAPL"),
							ast.WithCost(ast.NewMergeCost())),
						ast.NewPosting(account("Assets:Cash")),
					),
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
				ast.NewTransaction(date("2021-09-22"), "Average cost basis with label",
					ast.WithFlag("*"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Stocks"),
							ast.WithAmount("10", "AAPL"),
							ast.WithCost(mergeCostWithLabel("Consolidated"))),
						ast.NewPosting(account("Assets:Cash")),
					),
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
				ast.NewTransaction(date("2002-01-17"), "(Padding inserted for balance of 987.34 USD)",
					ast.WithFlag("P"),
					ast.WithPostings(
						ast.NewPosting(account("Assets:US:BofA:Checking"),
							ast.WithAmount("987.34", "USD")),
						ast.NewPosting(account("Equity:Opening-Balances"),
							ast.WithAmount("-987.34", "USD")),
					),
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
				ast.NewTransaction(date("2021-09-22"), "Diversified stock purchase",
					ast.WithFlag("*"),
					ast.WithLinks("invoice-2021-q3", "portfolio-rebalance"),
					ast.WithTags("investment", "stocks", "retirement"),
					ast.WithTransactionMetadata(
						ast.NewMetadata("trade-confirmation", "CONF-987654321"),
						ast.NewMetadata("broker", "E*TRADE"),
					),
					ast.WithPostings(
						ast.NewPosting(account("Assets:Brokerage:Cash"),
							ast.WithAmount("-5000.00", "USD")),
						ast.NewPosting(account("Assets:Brokerage:AAPL"),
							ast.WithAmount("10", "AAPL"),
							ast.WithCost(ast.NewCostWithLabel(ast.NewAmount("150.00", "USD"), date("2021-09-22"), "Batch-Q3")),
							ast.WithPrice(ast.NewAmount("152.00", "USD")),
							ast.WithPostingMetadata(
								ast.NewMetadata("note", "Tech allocation"),
							)),
						ast.NewPosting(account("Assets:Brokerage:MSFT"),
							ast.WithAmount("5", "MSFT"),
							ast.WithCost(ast.NewCostWithLabel(ast.NewAmount("300.00", "USD"), date("2021-09-22"), "Batch-Q3")),
							ast.WithPostingMetadata(
								ast.NewMetadata("note", "Tech allocation"),
							)),
						ast.NewPosting(account("Assets:Brokerage:VTI"),
							ast.WithAmount("20", "VTI"),
							ast.WithCost(ast.NewCostWithDate(ast.NewAmount("100.00", "USD"), date("2021-09-22"))),
							ast.WithPostingFlag("*")),
						ast.NewPosting(account("Expenses:Commissions"),
							ast.WithAmount("9.95", "USD")),
					),
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
			name: "Pushtag applies to transaction",
			beancount: `
				pushtag #trip-berlin

				2014-04-23 * "Flight to Berlin"
					Expenses:Flights -1230.27 USD
					Liabilities:CreditCard
			`,
			expected: withPushtags(
				beancount(
					ast.NewTransaction(date("2014-04-23"), "Flight to Berlin",
						ast.WithFlag("*"),
						ast.WithTags("trip-berlin"),
						ast.WithPostings(
							ast.NewPosting(account("Expenses:Flights"),
								ast.WithAmount("-1230.27", "USD")),
							ast.NewPosting(account("Liabilities:CreditCard")),
						),
					),
				),
				pushtag("trip-berlin"),
			),
		},
		{
			name: "Poptag removes tag",
			beancount: `
				pushtag #trip-berlin

				2014-04-23 * "Flight to Berlin"
					Expenses:Flights -1230.27 USD
					Liabilities:CreditCard

				poptag #trip-berlin

				2014-04-24 * "Dinner"
					Expenses:Restaurant -45.00 EUR
					Assets:Cash
			`,
			expected: withPoptags(
				withPushtags(
					beancount(
						ast.NewTransaction(date("2014-04-23"), "Flight to Berlin",
							ast.WithFlag("*"),
							ast.WithTags("trip-berlin"),
							ast.WithPostings(
								ast.NewPosting(account("Expenses:Flights"),
									ast.WithAmount("-1230.27", "USD")),
								ast.NewPosting(account("Liabilities:CreditCard")),
							),
						),
						ast.NewTransaction(date("2014-04-24"), "Dinner",
							ast.WithFlag("*"),
							ast.WithPostings(
								ast.NewPosting(account("Expenses:Restaurant"),
									ast.WithAmount("-45.00", "EUR")),
								ast.NewPosting(account("Assets:Cash")),
							),
						),
					),
					pushtag("trip-berlin"),
				),
				poptag("trip-berlin"),
			),
		},
		{
			name: "Multiple pushtags stack",
			beancount: `
				pushtag #trip-berlin
				pushtag #vacation

				2014-04-23 * "Flight"
					Expenses:Flights -1230.27 USD
					Liabilities:CreditCard
			`,
			expected: withPushtags(
				beancount(
					ast.NewTransaction(date("2014-04-23"), "Flight",
						ast.WithFlag("*"),
						ast.WithTags("trip-berlin", "vacation"),
						ast.WithPostings(
							ast.NewPosting(account("Expenses:Flights"),
								ast.WithAmount("-1230.27", "USD")),
							ast.NewPosting(account("Liabilities:CreditCard")),
						),
					),
				),
				pushtag("trip-berlin"),
				pushtag("vacation"),
			),
		},
		{
			name: "Pushmeta applies metadata to transaction",
			beancount: `
				pushmeta location: "Berlin, Germany"

				2014-04-23 * "Dinner"
					Expenses:Restaurant -45.00 EUR
					Assets:Cash
			`,
			expected: withPushmetas(
				beancount(
					ast.NewTransaction(date("2014-04-23"), "Dinner",
						ast.WithFlag("*"),
						ast.WithTransactionMetadata(
							ast.NewMetadata("location", "Berlin, Germany"),
						),
						ast.WithPostings(
							ast.NewPosting(account("Expenses:Restaurant"),
								ast.WithAmount("-45.00", "EUR")),
							ast.NewPosting(account("Assets:Cash")),
						),
					),
				),
				pushmeta("location", "Berlin, Germany"),
			),
		},
		{
			name: "Popmeta removes metadata",
			beancount: `
				pushmeta location: "Berlin, Germany"

				2014-04-23 * "Dinner in Berlin"
					Expenses:Restaurant -45.00 EUR
					Assets:Cash

				popmeta location:

				2014-04-24 * "Dinner elsewhere"
					Expenses:Restaurant -30.00 EUR
					Assets:Cash
			`,
			expected: withPopmetas(
				withPushmetas(
					beancount(
						ast.NewTransaction(date("2014-04-23"), "Dinner in Berlin",
							ast.WithFlag("*"),
							ast.WithTransactionMetadata(
								ast.NewMetadata("location", "Berlin, Germany"),
							),
							ast.WithPostings(
								ast.NewPosting(account("Expenses:Restaurant"),
									ast.WithAmount("-45.00", "EUR")),
								ast.NewPosting(account("Assets:Cash")),
							),
						),
						ast.NewTransaction(date("2014-04-24"), "Dinner elsewhere",
							ast.WithFlag("*"),
							ast.WithPostings(
								ast.NewPosting(account("Expenses:Restaurant"),
									ast.WithAmount("-30.00", "EUR")),
								ast.NewPosting(account("Assets:Cash")),
							),
						),
					),
					pushmeta("location", "Berlin, Germany"),
				),
				popmeta("location"),
			),
		},
		{
			name: "Pushmeta applies to non-transaction directives",
			beancount: `
				pushmeta source: "import-script"

				2014-04-01 open Assets:Checking USD
			`,
			expected: withPushmetas(
				beancount(
					withMeta(
						ast.NewOpen(date("2014-04-01"), account("Assets:Checking"), []string{"USD"}, ""),
						ast.NewMetadata("source", "import-script"),
					),
				),
				pushmeta("source", "import-script"),
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
				ast.NewCommodity(date("1792-01-01"), "USD"),
				ast.NewOpen(date("1980-05-12"), account("Equity:Opening-Balances"), nil, ""),
				ast.NewOpen(date("2018-01-01"), account("Assets:US:BofA:Checking"), []string{"USD"}, ""),
				ast.NewTransaction(date("2019-01-01"), "",
					ast.WithPostings(
						ast.NewPosting(account("Assets:US:BofA:Checking"),
							ast.WithAmount("4135.73", "USD")),
						ast.NewPosting(account("Equity:Opening-Balances")),
					),
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
						ast.NewCommodity(date("1792-01-01"), "USD"),
						ast.NewMetadata("export", "CASH"),
						ast.NewMetadata("name", "US Dollar"),
					),
					ast.NewOpen(date("1980-05-12"), account("Equity:Opening-Balances"), nil, ""),
					ast.NewOpen(date("2019-01-01"), account("Assets:US:BofA:Checking"), []string{"USD"}, ""),
					ast.NewTransaction(date("2019-01-01"), "",
						ast.WithPostings(
							ast.NewPosting(account("Assets:US:BofA:Checking"),
								ast.WithAmount("4135.73", "USD")),
							ast.NewPosting(account("Equity:Opening-Balances"),
								ast.WithAmount("-4135.73", "USD")),
						),
					),
					ast.NewTransaction(date("2019-01-01"), "",
						ast.WithFlag("*"),
						ast.WithPostings(
							ast.NewPosting(account("Liabilities:CreditCard:CapitalOne"),
								ast.WithAmount("-37.45", "USD")),
						),
					),
					ast.NewTransaction(date("2019-01-01"), "",
						ast.WithFlag("!"),
						ast.WithPostings(
							ast.NewPosting(account("Assets:ETrade:IVV"),
								ast.WithAmount("+10", "IVV")),
						),
					),
					ast.NewTransaction(date("2019-01-01"), "Lamb tagine with wine",
						ast.WithPostings(
							ast.NewPosting(account("Assets:US:BofA:Checking"),
								ast.WithAmount("4135.73", "USD"),
								ast.WithPrice(ast.NewAmount("412", "EUR"))),
						),
					),
					ast.NewTransaction(date("2019-01-01"), "Lamb tagine with wine",
						ast.WithFlag("*"),
						ast.WithPostings(
							ast.NewPosting(account("Assets:US:BofA:Checking"),
								ast.WithAmount("4135.73", "USD"),
								ast.WithTotalPrice(ast.NewAmount("488.22", "EUR"))),
						),
					),
					ast.NewTransaction(date("2019-01-01"), "Lamb tagine with wine",
						ast.WithFlag("*"),
						ast.WithPayee("Cafe Mogador"),
					),
					ast.NewTransaction(date("2019-01-01"), "",
						ast.WithFlag("*"),
						ast.WithPayee("Cafe Mogador"),
					),
				),
				option("title", "Example Beancount file"),
				option("operating_currency", "USD"),
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// fmt.Println(parser.String())

			ast, err := ParseString(context.Background(), test.beancount)
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

	_, err = ParseBytes(context.Background(), data)
	assert.NoError(t, err)
}

func TestParseKitchenSink(t *testing.T) {
	data, err := os.ReadFile("../testdata/kitchensink.beancount")
	assert.NoError(t, err)

	tree, err := ParseBytes(context.Background(), data)
	assert.NoError(t, err)

	// Verify we parsed all the directives
	assert.True(t, len(tree.Directives) > 0, "Should have parsed directives")
	assert.True(t, len(tree.Options) > 0, "Should have parsed options")

	// Find and verify the kitchen sink transaction
	var kitchenSinkTxn *ast.Transaction
	for _, dir := range tree.Directives {
		if txn, ok := dir.(*ast.Transaction); ok {
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
	assert.Equal(t, ast.Link("rebalance-q4"), kitchenSinkTxn.Links[0])
	assert.Equal(t, ast.Link("invoice-2021-1215"), kitchenSinkTxn.Links[1])

	// Verify tags are stripped of #
	assert.Equal(t, ast.Tag("investment"), kitchenSinkTxn.Tags[0])
	assert.Equal(t, ast.Tag("stocks"), kitchenSinkTxn.Tags[1])
	assert.Equal(t, ast.Tag("retirement"), kitchenSinkTxn.Tags[2])
	assert.Equal(t, ast.Tag("tax-loss-harvesting"), kitchenSinkTxn.Tags[3])
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
			d := &ast.Date{}
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
			a := ast.Account("")
			err := a.Capture([]string{tt.input})

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.EqualError(t, err, tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, ast.Account(tt.input), a)
			}
		})
	}
}

func normalizeAST(tree *ast.AST) {
	if tree == nil {
		return
	}

	for _, dir := range tree.Directives {
		normalizeDirective(dir)
	}

	// Normalize positions for all non-directive AST elements
	normalizePositions(tree.Options)
	normalizePositions(tree.Includes)
	normalizePositions(tree.Plugins)
	normalizePositions(tree.Pushtags)
	normalizePositions(tree.Poptags)
	normalizePositions(tree.Pushmetas)
	normalizePositions(tree.Popmetas)
}

// normalizePositions resets Pos to zero for all items in a slice of Node types.
func normalizePositions[T ast.Node](items []T) {
	for _, item := range items {
		if item != nil {
			// Use type assertion to access Pos field
			switch v := any(item).(type) {
			case *ast.Option:
				v.Pos = lexer.Position{}
			case *ast.Include:
				v.Pos = lexer.Position{}
			case *ast.Plugin:
				v.Pos = lexer.Position{}
			case *ast.Pushtag:
				v.Pos = lexer.Position{}
			case *ast.Poptag:
				v.Pos = lexer.Position{}
			case *ast.Pushmeta:
				v.Pos = lexer.Position{}
			case *ast.Popmeta:
				v.Pos = lexer.Position{}
			}
		}
	}
}

func normalizeDirective(d ast.Directive) {
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
	case *ast.Transaction:
		for _, posting := range dir.Postings {
			if posting != nil {
				posting.Pos = lexer.Position{}
			}
		}
	}
}

// Helper functions for tests

// date is a test helper that panics on error (for convenience in test setup)
func date(value string) *ast.Date {
	d, err := ast.NewDate(value)
	if err != nil {
		panic(err)
	}
	return d
}

// account is a test helper that panics on error (for convenience in test setup)
func account(name string) ast.Account {
	acc, err := ast.NewAccount(name)
	if err != nil {
		panic(err)
	}
	return acc
}

// AST constructor
func beancount(directives ...ast.Directive) *ast.AST {
	return &ast.AST{Directives: directives}
}

// Custom directive helpers (no builder exists in ast package yet)
func custom(d *ast.Date, t string, values ...*ast.CustomValue) *ast.Custom {
	return &ast.Custom{Date: d, Type: t, Values: values}
}

func customString(value string) *ast.CustomValue {
	return &ast.CustomValue{String: &value}
}

func customBoolean(value string) *ast.CustomValue {
	return &ast.CustomValue{BooleanValue: &value}
}

func customAmount(a *ast.Amount) *ast.CustomValue {
	return &ast.CustomValue{Amount: a}
}

func customNumber(value string) *ast.CustomValue {
	return &ast.CustomValue{Number: &value}
}

// Merge cost with label (no builder exists in ast package yet)
func mergeCostWithLabel(label string) *ast.Cost {
	return &ast.Cost{IsMerge: true, Amount: nil, Date: nil, Label: label}
}

// Node constructors (no builders exist in ast package)
func option(name string, value string) *ast.Option {
	return &ast.Option{Name: name, Value: value}
}

func include(filename string) *ast.Include {
	return &ast.Include{Filename: filename}
}

func plugin(name string, config string) *ast.Plugin {
	return &ast.Plugin{Name: name, Config: config}
}

func pushtag(tag string) *ast.Pushtag {
	return &ast.Pushtag{Tag: ast.Tag(tag)}
}

func poptag(tag string) *ast.Poptag {
	return &ast.Poptag{Tag: ast.Tag(tag)}
}

func pushmeta(key string, value string) *ast.Pushmeta {
	return &ast.Pushmeta{Key: key, Value: value}
}

func popmeta(key string) *ast.Popmeta {
	return &ast.Popmeta{Key: key}
}

// AST modifiers
func withOptions(a *ast.AST, options ...*ast.Option) *ast.AST {
	a.Options = options
	return a
}

func withIncludes(a *ast.AST, includes ...*ast.Include) *ast.AST {
	a.Includes = includes
	return a
}

func withPlugins(a *ast.AST, plugins ...*ast.Plugin) *ast.AST {
	a.Plugins = plugins
	return a
}

func withPushtags(a *ast.AST, pushtags ...*ast.Pushtag) *ast.AST {
	a.Pushtags = pushtags
	return a
}

func withPoptags(a *ast.AST, poptags ...*ast.Poptag) *ast.AST {
	a.Poptags = poptags
	return a
}

func withPushmetas(a *ast.AST, pushmetas ...*ast.Pushmeta) *ast.AST {
	a.Pushmetas = pushmetas
	return a
}

func withPopmetas(a *ast.AST, popmetas ...*ast.Popmeta) *ast.AST {
	a.Popmetas = popmetas
	return a
}

// Directive modifiers
func withMeta[W ast.WithMetadata](w W, metadata ...*ast.Metadata) W {
	w.AddMetadata(metadata...)
	return w
}
