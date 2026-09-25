// CSV Importer Example
//
// This example is an Importer for `beancount import`: it reads a bank's CSV
// Statement and returns one transaction per row.
//
// Usage:
//
//	go build -o csv-importer .
//	beancount import --with ./csv-importer ledger.beancount transactions.csv
package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/importer"
)

// header is the first row of every Statement this Importer reads.
var header = []string{"Date", "ID", "Payee", "Amount"}

// csvImporter reads the bank's CSV export.
type csvImporter struct {
	account ast.Account
}

func main() {
	account, _ := ast.NewAccount("Assets:Checking")
	importer.Serve(&csvImporter{account: account})
}

// Identify accepts a .csv file whose first row is the expected header.
func (i *csvImporter) Identify(_ context.Context, path string) (bool, error) {
	if filepath.Ext(path) != ".csv" {
		return false, nil
	}
	rows, err := readCSV(path)
	if err != nil {
		return false, nil
	}
	return len(rows) > 0 && slices.Equal(rows[0], header), nil
}

// Extract turns every row after the header into a transaction.
func (i *csvImporter) Extract(_ context.Context, path string) ([]ast.Directive, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}

	var directives []ast.Directive
	for n, row := range rows[1:] {
		txn, err := i.rowToTransaction(row)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", n+2, err) // +2 for the header and 1-indexing
		}
		directives = append(directives, txn)
	}
	return directives, nil
}

func readCSV(path string) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = len(header)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return rows, nil
}

// rowToTransaction books a row between the checking account and a category
// picked from the payee.
func (i *csvImporter) rowToTransaction(row []string) (*ast.Transaction, error) {
	date, err := ast.NewDate(row[0])
	if err != nil {
		return nil, fmt.Errorf("invalid date %q: %w", row[0], err)
	}
	id, payee := row[1], row[2]

	// Keep the amount as a decimal string: a float would lose precision.
	amount, err := decimal.NewFromString(row[3])
	if err != nil {
		return nil, fmt.Errorf("invalid amount %q: %w", row[3], err)
	}

	category := categorizeIncome(payee)
	if amount.IsNegative() {
		category = categorizeExpense(payee)
	}
	categoryAccount, err := ast.NewAccount(category)
	if err != nil {
		return nil, err
	}

	return ast.NewTransaction(date, payee,
		ast.WithFlag("*"),
		// The SDK sends import-id as the transaction's Import ID.
		ast.WithTransactionMetadata(ast.NewMetadata("import-id", id)),
		ast.WithPostings(
			ast.NewPosting(i.account, ast.WithAmount(amount.StringFixed(2), "USD")),
			ast.NewPosting(categoryAccount),
		),
	), nil
}

// categorizeExpense picks an expense account from keywords in the payee.
func categorizeExpense(payee string) string {
	payeeLower := strings.ToLower(payee)

	switch {
	case strings.Contains(payeeLower, "whole foods"),
		strings.Contains(payeeLower, "safeway"),
		strings.Contains(payeeLower, "trader joe"),
		strings.Contains(payeeLower, "grocery"):
		return "Expenses:Groceries"

	case strings.Contains(payeeLower, "restaurant"),
		strings.Contains(payeeLower, "cafe"),
		strings.Contains(payeeLower, "pizza"),
		strings.Contains(payeeLower, "burger"):
		return "Expenses:Dining"

	case strings.Contains(payeeLower, "uber"),
		strings.Contains(payeeLower, "lyft"),
		strings.Contains(payeeLower, "transit"),
		strings.Contains(payeeLower, "metro"):
		return "Expenses:Transport"

	case strings.Contains(payeeLower, "electric"),
		strings.Contains(payeeLower, "gas"),
		strings.Contains(payeeLower, "water"),
		strings.Contains(payeeLower, "internet"),
		strings.Contains(payeeLower, "utility"):
		return "Expenses:Utilities"

	case strings.Contains(payeeLower, "rent"),
		strings.Contains(payeeLower, "mortgage"):
		return "Expenses:Rent"

	default:
		return "Expenses:Other"
	}
}

// categorizeIncome picks an income account from keywords in the payee.
func categorizeIncome(payee string) string {
	payeeLower := strings.ToLower(payee)

	switch {
	case strings.Contains(payeeLower, "salary"),
		strings.Contains(payeeLower, "payroll"),
		strings.Contains(payeeLower, "employer"):
		return "Income:Salary"

	case strings.Contains(payeeLower, "interest"),
		strings.Contains(payeeLower, "dividend"):
		return "Income:Investment"

	default:
		return "Income:Other"
	}
}
