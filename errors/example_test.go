package errors_test

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/robinvdvleuten/beancount/errors"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
)

// Example showing how to use TextFormatter for CLI output
func ExampleTextFormatter() {
	// Create a sample error
	date := &parser.Date{}
	err := &ledger.AccountNotOpenError{
		Account: "Assets:Checking",
		Date:    date,
		Pos: lexer.Position{
			Filename: "test.beancount",
			Line:     10,
			Column:   1,
		},
		Directive: nil,
	}

	// Format for CLI output
	formatter := errors.NewTextFormatter(nil)
	output := formatter.Format(err)
	fmt.Println(output)
}

// Example showing how to use JSONFormatter for API/web output
func ExampleJSONFormatter() {
	// Create sample errors
	date := &parser.Date{}
	errs := []error{
		&ledger.AccountNotOpenError{
			Account: "Assets:Checking",
			Date:    date,
			Pos: lexer.Position{
				Filename: "test.beancount",
				Line:     10,
			},
		},
		&ledger.BalanceMismatchError{
			Account:  "Assets:Checking",
			Date:     date,
			Expected: "100",
			Actual:   "50",
			Currency: "USD",
			Pos: lexer.Position{
				Filename: "test.beancount",
				Line:     20,
			},
		},
	}

	// Format as JSON
	formatter := errors.NewJSONFormatter()
	jsonOutput := formatter.FormatAll(errs)
	fmt.Println(jsonOutput)
	// Output will be a JSON array with structured error information
}
