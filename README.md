# beancount

A fast, lightweight [Beancount](https://beancount.github.io/) parser and formatter written in Go.

[![Latest Release](https://img.shields.io/github/release/robinvdvleuten/beancount.svg?style=flat-square)](https://github.com/robinvdvleuten/beancount/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/robinvdvleuten/beancount/build.yml?style=flat-square&branch=main)](https://github.com/robinvdvleuten/beancount/actions?query=workflow%3Abuild)
[![GPL-2.0 license](https://img.shields.io/github/license/robinvdvleuten/beancount.svg?style=flat-square)](https://github.com/robinvdvleuten/beancount/blob/main/COPYING)
[![PkgGoDev](https://img.shields.io/badge/godoc-reference-blue.svg?style=flat-square)](https://pkg.go.dev/github.com/robinvdvleuten/beancount)
[![Go ReportCard](https://goreportcard.com/badge/github.com/robinvdvleuten/beancount?style=flat-square)](https://goreportcard.com/report/robinvdvleuten/beancount)

## What is it?

[Beancount](https://beancount.github.io/) is a double-entry bookkeeping system that uses plain text files to track personal or business finances. It allows you to maintain your accounting ledger as readable text files, making it easy to version control, search, and programmatically manipulate your financial data. The Beancount file format uses a simple, human-readable syntax to record transactions, accounts, and other financial directives.

This project is a Go implementation of a Beancount file parser and formatter. While the official Beancount implementation is written in Python and includes a full accounting engine with balance calculations, reports, and queries, this Go version focuses specifically on parsing and formatting. It's designed to be fast and lightweight, making it ideal for tooling, text editors, build pipelines, or any situation where you need to work with Beancount file syntax without the overhead of the full accounting system.

## Features

This implementation currently supports:

- **Parsing**: Full parsing of Beancount file syntax
- **Formatting**: Auto-align currencies, numbers, and accounts
- **Validation**: Syntax checking and error reporting
- **CLI Interface**: Simple command-line tools for common operations

**Note**: This is a parser and formatter implementation. It does not currently include the full accounting engine features of the official Python implementation (balance calculations, reporting, queries, etc.).

## Installation

Download the latest release for your platform from the [releases page](https://github.com/robinvdvleuten/beancount/releases).

Or, just install it with Go:

```bash
go install github.com/robinvdvleuten/beancount@latest
```

## Usage

### Check a Beancount file

Parse and validate a Beancount file:

```bash
beancount check example.beancount
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
