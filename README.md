# beancount

![beancounting-gopher](https://github.com/user-attachments/assets/e73f4046-22d2-4824-b8f0-11a1b4702cfa)

A fast, lightweight [Beancount](https://beancount.github.io/) parser and formatter written in Go.

[![Latest Release](https://img.shields.io/github/release/robinvdvleuten/beancount.svg?style=flat-square)](https://github.com/robinvdvleuten/beancount/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/robinvdvleuten/beancount/build.yml?style=flat-square&branch=main)](https://github.com/robinvdvleuten/beancount/actions?query=workflow%3Abuild)
[![GPL-2.0 license](https://img.shields.io/github/license/robinvdvleuten/beancount.svg?style=flat-square)](https://github.com/robinvdvleuten/beancount/blob/main/COPYING)
[![PkgGoDev](https://img.shields.io/badge/godoc-reference-blue.svg?style=flat-square)](https://pkg.go.dev/github.com/robinvdvleuten/beancount)
[![Go ReportCard](https://goreportcard.com/badge/github.com/robinvdvleuten/beancount?style=flat-square)](https://goreportcard.com/report/robinvdvleuten/beancount)

## What is it?

[Beancount](https://beancount.github.io/) is a double-entry bookkeeping system that uses plain text files to track personal or business finances. It allows you to maintain your accounting ledger as readable text files, making it easy to version control, search, and programmatically manipulate your financial data. The Beancount file format uses a simple, human-readable syntax to record transactions, accounts, and other financial directives.

This project is a Go implementation of a Beancount file parser, formatter, and validator. While the official Beancount implementation is written in Python and includes a full accounting engine with balance calculations, reports, queries, and a web interface, this Go version focuses on parsing, formatting, and ledger validation. It's designed to be fast and lightweight, making it ideal for tooling, text editors, build pipelines, or any situation where you need to validate Beancount files without the overhead of the full accounting system.

## Features

This implementation currently supports:

- **Parsing**: Full parsing of Beancount file syntax
- **Formatting**: Auto-align currencies, numbers, and accounts
- **Validation**: Balance checks, account lifecycle, assertions
- **Inventory**: Lot-based tracking with cost basis (FIFO/LIFO)
- **Includes**: Recursive loading of modular Beancount files
- **CLI Interface**: Simple command-line tools for common operations

**Note**: This implementation includes ledger validation with transaction balancing, account management, and inventory tracking. It does not include reporting, queries, or a web interface like the official Python implementation.

## Installation

### Packages & Binaries

If you use Brew, you can simply install the package:

```sh
brew install robinvdvleuten/tap/beancount
```

Or download a binary from the [releases](https://github.com/robinvdvleuten/beancount/releases)
page. Linux (including ARM) binaries are available, as well as Debian, RPM AND APK
packages.

### Build From Source

Alternatively you can also build `beancount` from source. Make sure you have a
working Go environment (Go 1.24 or higher is required). See the
[install instructions](https://golang.org/doc/install.html).

To install beancount, simply run:

```sh
go install github.com/robinvdvleuten/beancount
```

## Usage

### Check a Beancount file

Validate a Beancount file with full ledger checks:

```sh
beancount check example.beancount
```

Or read from stdin (omit filename or use `-`):

```sh
echo "2024-01-01 open Assets:Checking USD" | beancount check
```

This command validates:
- Transaction balance across all currencies
- Account open/close dates are respected
- Balance assertions match actual balances
- All referenced accounts exist

Example error output:
```
example.beancount:15: Transaction does not balance: (-500.00 USD)

   2020-01-15 * "Grocery shopping"
     Assets:Checking   1000.00 USD
     Expenses:Food      500.00 USD

1 validation error(s) found
```

### Format a Beancount file

Format a Beancount file with automatic alignment:

```sh
beancount format example.beancount

# Specify currency column position
beancount format --currency-column 60 example.beancount

# Customize account name and number widths
beancount format --prefix-width 50 --num-width 12 example.beancount
```

Or read from stdin (omit filename or use `-`):

```sh
echo "2024-01-01 open Assets:Checking USD" | beancount format
```

### Telemetry

Use the global `--telemetry` flag to see detailed timing breakdowns for any command:

```sh
beancount --telemetry check example.beancount
beancount --telemetry format example.beancount
```

This displays a hierarchical breakdown of where time is spent during execution:

```
✓ Check passed

check example.beancount: 125ms
├─ loader.load example.beancount: 85ms
│  └─ loader.parse: 85ms
│     ├─ parser.lexing: 75ms
│     ├─ parser.parsing: 8ms
│     ├─ parser.push_pop: ~1ms
│     └─ parser.sorting: 245µs
├─ loader.load accounts.beancount: 35ms
│  └─ loader.parse: 35ms
│     ├─ parser.lexing: 30ms
│     ├─ parser.parsing: ~4ms
│     ├─ parser.push_pop: 823µs
│     └─ parser.sorting: 156µs
├─ ast.merging: ~2ms
└─ ledger.processing (1523 directives): ~3ms
```

The telemetry output is written to stderr, making it easy to separate from command results.

## Programmatic Usage

This library can be used programmatically in your Go applications to parse, manipulate, and generate Beancount files.

### Parsing Beancount Files

Load and parse a Beancount file:

```go
import (
    "context"
    "github.com/robinvdvleuten/beancount/loader"
)

// Load a single file
ldr := loader.New()
ast, err := ldr.Load(context.Background(), "example.beancount")
if err != nil {
    log.Fatal(err)
}

// Load with recursive include resolution
ldr = loader.New(loader.WithFollowIncludes())
ast, err = ldr.Load(context.Background(), "main.beancount")
```

### Building Transactions Programmatically

Create Beancount transactions using the builder API:

```go
import "github.com/robinvdvleuten/beancount/ast"

// Create a transaction with the functional options pattern
date, _ := ast.NewDate("2024-01-15")
checking, _ := ast.NewAccount("Assets:Checking")
groceries, _ := ast.NewAccount("Expenses:Groceries")

txn := ast.NewTransaction(date, "Grocery shopping",
    ast.WithFlag("*"),
    ast.WithPayee("Whole Foods"),
    ast.WithTags("food", "weekly"),
    ast.WithPostings(
        ast.NewPosting(groceries, ast.WithAmount("125.43", "USD")),
        ast.NewPosting(checking), // Balancing posting
    ),
)
```

### Formatting Output

Format transactions back to Beancount syntax:

```go
import (
    "context"
    "os"
    "github.com/robinvdvleuten/beancount/formatter"
)

// Create a formatter
fmtr := formatter.New()

// Format a single transaction
fmtr.FormatTransaction(txn, os.Stdout)

// Format an entire AST
fmtr.Format(context.Background(), ast, sourceContent, os.Stdout)
```

### Complete Example

See the [CSV Importer example](examples/csv_importer/) for a complete working example that demonstrates:
- Reading CSV files with Go's `encoding/csv`
- Building transactions programmatically with the builder API
- Automatic expense categorization
- Error handling and validation
- Formatting output

Run the example:

```sh
cd examples/csv_importer
go run main.go transactions.csv
```

For more details on the builder API, see the [package documentation](https://pkg.go.dev/github.com/robinvdvleuten/beancount/ast).

## Contributing

Everyone is encouraged to help improve this project. Here are a few ways you can help:

- [Report bugs](https://github.com/robinvdvleuten/beancount/issues)
- Fix bugs and [submit pull requests](https://github.com/robinvdvleuten/beancount/pulls)
- Write, clarify, or fix documentation
- Suggest or add new features

To get started with development:

```
git clone https://github.com/robinvdvleuten/beancount.git
cd beancount
go test ./...
```

Before submitting a pull request, please make sure to run
`go fmt` on any Go source files you touched so the code stays consistent.

Feel free to open an issue to get feedback on your idea before spending too much time on it.

## License

Copyright (c) 2025 Robin van der Vleuten

Licensed under the GNU General Public License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.gnu.org/licenses/old-licenses/gpl-2.0.html

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

functional programming patterns (haskell, clojure) for ledger?
