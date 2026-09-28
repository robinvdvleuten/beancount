package printer_test

import (
	"fmt"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/printer"
)

func ExampleSprint() {
	date, _ := ast.NewDate("2024-01-15")
	checking, _ := ast.NewAccount("Assets:Checking")
	groceries, _ := ast.NewAccount("Expenses:Groceries")

	txn := ast.NewTransaction(date, "Grocery shopping",
		ast.WithFlag("*"),
		ast.WithPayee("Whole Foods"),
		ast.WithPostings(
			ast.NewPosting(groceries, ast.WithAmount("125.43", "USD")),
			ast.NewPosting(checking),
		),
	)

	fmt.Print(printer.Sprint(txn))
	// Output:
	// 2024-01-15 * "Whole Foods" "Grocery shopping"
	//   Expenses:Groceries  125.43 USD
	//   Assets:Checking
}
