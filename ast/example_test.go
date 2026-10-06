package ast_test

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/printer"
)

// Example_csvImporter builds the transaction for one bank statement row,
// "2024-01-15,Whole Foods,-45.60", as an importer would.
func Example_csvImporter() {
	date, _ := ast.NewDate("2024-01-15")
	groceries, _ := ast.NewAccount("Expenses:Groceries")
	checking, _ := ast.NewAccount("Assets:Checking")

	txn := ast.NewTransaction(date, "Groceries",
		ast.WithFlag("*"),
		ast.WithPayee("Whole Foods"),
		ast.WithTags("food"),
		ast.WithPostings(
			ast.NewPosting(groceries, ast.WithAmount("45.60", "USD")),
			ast.NewPosting(checking, ast.WithAmount("-45.60", "USD")),
		),
	)

	fmt.Print(printer.Sprint(txn))
	// Output:
	// 2024-01-15 * "Whole Foods" "Groceries" #food
	//   Expenses:Groceries   45.60 USD
	//   Assets:Checking     -45.60 USD
}

// Example_investmentTransaction builds a purchase of shares held at cost.
func Example_investmentTransaction() {
	date, _ := ast.NewDate("2024-01-15")
	brokerage, _ := ast.NewAccount("Assets:Investments:Brokerage")
	cash, _ := ast.NewAccount("Assets:Investments:Cash")

	txn := ast.NewTransaction(date, "Buy HOOL shares",
		ast.WithFlag("*"),
		ast.WithPayee("Vanguard"),
		ast.WithTags("investment", "stocks"),
		ast.WithPostings(
			ast.NewPosting(brokerage,
				ast.WithAmount("10", "HOOL"),
				ast.WithCost(ast.NewCost(ast.NewAmount("520.00", "USD")))),
			ast.NewPosting(cash, ast.WithAmount("-5200.00", "USD")),
		),
	)

	fmt.Print(printer.Sprint(txn))
	// Output:
	// 2024-01-15 * "Vanguard" "Buy HOOL shares" #investment #stocks
	//   Assets:Investments:Brokerage        10 HOOL {520.00 USD}
	//   Assets:Investments:Cash       -5200.00 USD
}
