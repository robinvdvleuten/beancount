# beancount

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

Download the latest release for your platform from the [releases page](https://github.com/robinvdvleuten/beancount/releases).

Or, just install it with Go:

```bash
go install github.com/robinvdvleuten/beancount@latest
```

## Usage

### Check a Beancount file

Validate a Beancount file with full ledger checks:

```bash
beancount check example.beancount
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

**Performance profiling:**

Use the `--telemetry` flag to see detailed timing breakdowns:

```bash
beancount check --telemetry example.beancount
```

Example output:
```
✓ Check passed

loader.load example.beancount: 125ms
├─ loader.parse example.beancount: 85ms
│  ├─ parser.lexing: 75ms
│  ├─ parser.push_pop: 5ms
│  └─ parser.sorting: 5ms
├─ loader.parse accounts.beancount: 35ms
│  ├─ parser.lexing: 30ms
│  ├─ parser.push_pop: 3ms
│  └─ parser.sorting: 2ms
├─ ast.merging: 2ms
└─ ledger.processing (1523 directives): 3ms
```

### Format a Beancount file

Format a Beancount file with automatic alignment:

```bash
beancount format example.beancount

# Specify currency column position
beancount format --currency-column 60 example.beancount

# Customize account name and number widths
beancount format --prefix-width 50 --num-width 12 example.beancount
```

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
