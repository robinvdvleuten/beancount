# CSV Importer Example

An Importer for `beancount import`, written with the `importer` SDK. It reads a bank's CSV Statement and returns one transaction per row.

## Overview

The Importer implements the SDK's `importer.Importer` interface and hands it to `importer.Serve` from `main`:

- `Identify` accepts a `.csv` file whose first row is the expected header.
- `Extract` builds a transaction per row with the `ast` builders, picking an expense or income account from keywords in the payee.
- The row's ID goes into `import-id` metadata, which the SDK sends as the transaction's Import ID.

## CSV format

```csv
Date,ID,Payee,Amount
2024-01-01,TX-1001,Acme Corp Payroll,3500.00
2024-01-02,TX-1002,Whole Foods Market,-125.43
```

- **Date**: YYYY-MM-DD
- **ID**: the bank's own identifier for the transaction
- **Payee**: the transaction's description
- **Amount**: positive for income, negative for expenses

## Usage

Build the Importer, then run it through `beancount import`:

```bash
go build -o csv-importer .
beancount import --with ./csv-importer ledger.beancount transactions.csv
```

`beancount` checks the extracted transactions together with `ledger.beancount` and prints them only if the check passes. [`expected.beancount`](expected.beancount) holds the output:

```beancount
2024-01-01 * "Acme Corp Payroll"
    import-id: "TX-1001"
    Assets:Checking   3500.00 USD
    Income:Salary

2024-01-02 * "Whole Foods Market"
    import-id: "TX-1002"
    Assets:Checking   -125.43 USD
    Expenses:Groceries
```

Append it to the ledger:

```bash
beancount import --with ./csv-importer ledger.beancount transactions.csv >> ledger.beancount
```

## Customization

Add categorization rules in `categorizeExpense()` and `categorizeIncome()`:

```go
case strings.Contains(payeeLower, "amazon"),
     strings.Contains(payeeLower, "ebay"):
    return "Expenses:Shopping"
```

Change the bank account in `main()`:

```go
account, _ := ast.NewAccount("Assets:BankOfAmerica:Checking")
```

`Extract` may also return balance assertions (`ast.NewBalance`), for Statements that report a closing balance.
