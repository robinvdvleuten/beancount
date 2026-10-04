# beancount

![beancounting-gopher](https://github.com/user-attachments/assets/e73f4046-22d2-4824-b8f0-11a1b4702cfa)

A [Beancount](https://beancount.github.io/) toolkit written in Go, for bookkeeping with AI agents and for extending in your own code. An agent edits your plain-text ledger, and one command checks each change. It rejects wrong entries with a line number and a reason, so the agent can fix them before they reach your books. When the built-in checks are not enough, you write your own in Go, using the same parser and ledger that the command line uses, and build it into one binary. Every check, format, and query result is tested against the official Beancount tools.

- **Built for agents.** The ledger is a text file, so the agent edits it like code, and you review each change as a `git diff`. Errors give the file, the line, and a reason, plus exit code `1`.
- **Extend in plain Go.** Write importers, house rules, and reports with the `ledgerload`, `loader`, `ast`, `ledger`, `formatter`, `printer`, and `query` packages. You get typed directives, typed errors, and a source position on each one.
- **Same answers as the official tools.** Every ledger fixture runs through both this tool and `bean-check` from Beancount 3.2.3. Every BQL query fixture is compared byte for byte with beanquery 0.2.0, Beancount 3's `bean-query`, in both text and CSV. The rare differences are listed in [`KNOWN_GAPS.md`](testdata/compliance/KNOWN_GAPS.md).
- **Quick enough to run after every edit.** On a 59,000-line ledger, `beancount check` takes 0.19 s, against 1.34 s for `bean-check` 3.2.3, which has to reparse the ledger after each edit (Apple M1 Pro).
- **Clean diffs.** `beancount format` aligns amounts with the same rules as `bean-format`, so agent edits and hand edits look the same.
- **One binary, no Python.** Linux, macOS, and Windows builds, Homebrew, and a Docker image. There is also a local web editor for you to review the books.

The built-in checks plus a house rule of your own, in a few lines of Go:

```go
result, _ := ledgerload.Load(ctx, loader.Source{Path: "ledger.beancount"})
for _, err := range result.Ledger.Errors() {
    fmt.Println(err) // ledger.beancount:7: Invalid reference to unknown account 'Expenses:Coffee'
}
for _, d := range result.AST.Directives {
    if txn, ok := d.(*ast.Transaction); ok && txn.Payee.Value == "" {
        fmt.Printf("%s: transaction has no payee\n", txn.Position()) // ledger.beancount:7:12: transaction has no payee
    }
}
```

> **Plugins:** only the Built-in Plugins `beancount.plugins.auto_accounts` and `beancount.plugins.implicit_prices` run. Other `plugin` directives are parsed but ignored, so a ledger that depends on another plugin can give different results from `bean-check`.

---

- [Install](#install)
- [Getting started](#getting-started)
- [Let an agent keep the books](#let-an-agent-keep-the-books)
- [Commands](#commands)
- [Extend it in Go](#extend-it-in-go)
- [Contributing](#contributing)
- [License](#license)

---

## Install

**Homebrew:**

```sh
brew install robinvdvleuten/tap/beancount
```

**Binaries:** download one from the [releases page](https://github.com/robinvdvleuten/beancount/releases). Builds are available for Linux, macOS, and Windows.

**Docker:**

```sh
docker run --rm -v "$PWD:/data" ghcr.io/robinvdvleuten/beancount check /data/ledger.beancount
```

**From source** (Go 1.26 or newer):

```sh
go install github.com/robinvdvleuten/beancount/cmd/beancount@latest
```

## Getting started

1. Create a small ledger:

   ```sh
   cat > ledger.beancount <<'EOF'
   2024-01-01 open Assets:Checking USD
   2024-01-01 open Expenses:Food USD

   2024-01-15 * "Grocery shopping"
     Expenses:Food  42.50 USD
     Assets:Checking  -42.50 USD
   EOF
   ```

2. Check it:

   ```sh
   beancount check ledger.beancount
   ```

   ```
   ✓ Check passed
   ```

3. Align the numbers. Format writes to stdout, so save the output to a temporary file and then replace the original:

   ```sh
   beancount format ledger.beancount > ledger.tmp && mv ledger.tmp ledger.beancount
   ```

   ```
   2024-01-15 * "Grocery shopping"
     Expenses:Food     42.50 USD
     Assets:Checking  -42.50 USD
   ```

4. Ask a question:

   ```sh
   beancount query ledger.beancount "BALANCES"
   ```

   ```
       account     sum_positi
   --------------- ----------
   Assets:Checking -42.50 USD
   Expenses:Food    42.50 USD
   ```

5. Open it in the browser editor at <http://127.0.0.1:8080>:

   ```sh
   beancount web ledger.beancount
   ```

## Let an agent keep the books

Any coding agent that can run shell commands can use this tool, for example Claude Code, Codex, or Cursor. Put the rules in the instructions file your agent reads, such as `AGENTS.md` or `CLAUDE.md`, next to your ledger:

```markdown
# Bookkeeping

The ledger is `ledger.beancount`.

- After every edit, run `beancount check ledger.beancount`.
  Fix each reported line and run it again until it exits with 0.
- Then run `beancount format ledger.beancount > ledger.tmp && mv ledger.tmp ledger.beancount`.
- Answer questions about the books with `beancount query ledger.beancount "<BQL>"`.
  Do not do the arithmetic yourself.
- Never add an account without an `open` directive.
```

When the agent makes a mistake, the check tells it where:

```sh
$ beancount check ledger.beancount
/home/you/books/ledger.beancount:7: Invalid reference to unknown account 'Expenses:Coffee'

✗ 1 validation error(s) found
$ echo $?
1
```

Keep the ledger in git and review each change as a diff before you commit it.

## Commands

Every command that takes a file, except `import`, also reads from stdin. To use stdin, leave out the filename or use `-`.

### `check`: validate a ledger

```sh
beancount check ledger.beancount
```

It checks that each transaction balances in every currency, that postings use accounts only while they are open, that balance assertions hold, and that all referenced accounts exist. Each error gives the absolute file path, the line, and the reason, and the command exits with `1`:

```
/home/you/books/ledger.beancount:3: Transaction does not balance: (1500 USD)

✗ 1 validation error(s) found
```

### `format`: align numbers and currencies

```sh
beancount format ledger.beancount
beancount format --currency-column 60 ledger.beancount
beancount format --prefix-width 50 --num-width 12 ledger.beancount
```

It follows `bean-format`'s rules, so it changes the same lines and leaves the rest as written.

### `query`: run BQL

```sh
beancount query ledger.beancount "BALANCES AT cost"
beancount query ledger.beancount 'JOURNAL "Checking"'
beancount query -f csv -m ledger.beancount "SELECT account, sum(position) GROUP BY account"
```

It supports beanquery's `SELECT` syntax: `FROM` with `OPEN`/`CLOSE`/`CLEAR`, `WHERE`, `GROUP BY` with `HAVING`, `ORDER BY` with a direction per term, `PIVOT BY`, `LIMIT`, and `DISTINCT`. It also has the aggregate and simple functions and the `BALANCES`, `JOURNAL`, and `PRINT` shortcuts. Leave out the query to start an interactive shell, or pipe one in:

```sh
echo "SELECT payee, narration WHERE 'trip' IN tags" | beancount query ledger.beancount
```

### `import`: turn a bank statement into transactions

```sh
beancount import --with ./my-importer ledger.beancount statement.csv >> ledger.beancount
```

It runs an Importer, a Go program you build with the [`importer` SDK](#write-an-importer), on the statement. It checks the transactions and balance assertions the Importer returns together with the ledger, and prints them formatted only when the check passes. Otherwise stdout stays empty, and each error gives the line in the printed output, under the statement's name:

```
statement.csv (extracted):1: Transaction does not balance: (0.5 USD)

✗ 1 validation error(s) found
```

Transactions and balance assertions the ledger already records are Duplicates. They are left out of stdout, and each is reported on stderr at the full path and line of the ledger directive it matched, so importing the same statement twice adds nothing:

```
/home/me/books/ledger.beancount:42: Duplicate left out: 2024-01-15 * "Coffee Shop" "Latte"
```

A transaction is a Duplicate when a ledger transaction has the same `import-id`. It is also a Duplicate when a ledger transaction without an `import-id`, including one a `pad` inserted, has a posting on the same account with the same amount, is dated at most 2 days before or after, and posts to the same accounts, to more of them, or to fewer. So an Importer's lone `Assets:Checking` posting matches your hand-written coffee on `Assets:Checking` and `Expenses:Food`, but two coffees paid from different accounts do not match. A balance assertion is a Duplicate when a ledger one has the same account, date, and amount. The Importer's own directives are never compared with each other, so two identical coffees on one statement are both kept.

When the Importer returns a transaction with a single posting, the command balances it with a posting to the unknown account, `Expenses:Unknown`, or the one you name with `--unknown-account`:

```sh
beancount import --with ./my-importer --unknown-account Expenses:Uncategorized ledger.beancount statement.csv
```

The ledger must open that account; the command does not add the `open` for you.

The command also exits with `1` when the Importer does not recognize the statement or reports an error. Anything the Importer logs goes to stderr.

### `web`: edit in the browser

```sh
beancount web ledger.beancount --watch
```

This starts a local server on `127.0.0.1:8080`. It has a source editor, an income statement and balance sheet for a period you choose, a trial balance, and a query page that prints what `beancount query` prints. Use `--read-only` to turn off writes, `--watch` to reload when the file changes, and `--host`/`--port` to change the address.

### `doctor lex`: show tokens

```sh
beancount doctor lex ledger.beancount
```

### `doctor missing_open`: print the missing `open` directives

```sh
beancount doctor missing_open ledger.beancount
```

This prints an `open` for every account the ledger uses without an `open` or `close`, dated at its first use, like `bean-doctor missing_open`. Pasting the lines into your ledger stops `check` reporting those accounts as unknown; like `bean-doctor`, it skips an account only a `balance` uses, which `check` still reports. It prints nothing when no account is missing.

### `--telemetry`: see where the time goes

```sh
beancount --telemetry check ledger.beancount
```

This writes a timing tree to stderr:

```
check ledger.beancount: 125ms
├─ loader.load ledger.beancount: 85ms
│  └─ loader.parse: 85ms
│     ├─ parser.lexing: 75ms
│     └─ parser.parsing: 8ms
├─ ast.merging: ~2ms
└─ ledger.processing (1523 directives): ~3ms
```

## Extend it in Go

Every command is built from Go packages you can import. Use them when you need something the CLI does not do: an importer for your bank, a rule only your books follow, or a report of your own. The result is a Go program that builds into one binary.

```sh
go get github.com/robinvdvleuten/beancount
```

**Load and check a ledger.** `ledgerload.Load` loads and processes it the way `beancount check` does, and `Errors()` returns the errors it found. Each is a `*ledger.Diagnostic` to `errors.As`, with its `Kind()` (such as `"AccountNotOpenError"`), its `Message()` and its source position, `GetPosition()`. A failed balance assertion is also a `*ledger.BalanceMismatchError`, which embeds one and adds the `Difference`:

```go
ctx := context.Background()
result, err := ledgerload.Load(ctx, loader.Source{Path: "ledger.beancount"})
if err != nil {
    log.Fatal(err) // I/O errors
}

for _, err := range diagnostic.Errors(result.LoadDiagnostics) {
    fmt.Println(err) // syntax errors, includes that match nothing
}
for _, err := range result.Ledger.Errors() {
    fmt.Println(err)
}
```

**Add a house rule.** Walk the directives, which are ordinary Go structs, as the ledger has processed them:

```go
for _, d := range result.AST.Directives {
    if txn, ok := d.(*ast.Transaction); ok && txn.Payee.Value == "" {
        fmt.Printf("%s: transaction has no payee\n", txn.Position())
    }
}
```

**Build a transaction** and print it in Beancount syntax:

```go
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
// 2024-01-15 * "Whole Foods" "Grocery shopping"
//   Expenses:Groceries  125.43 USD
//   Assets:Checking
```

### Write an Importer

An Importer implements `importer.Importer` and serves it from `main`. `beancount import --with` starts the binary and talks to it over a versioned protocol:

```go
type bank struct{}

func (bank) Identify(ctx context.Context, path string) (bool, error) {
    return filepath.Ext(path) == ".csv", nil
}

func (bank) Extract(ctx context.Context, path string) ([]ast.Directive, error) {
    // Read the statement and build transactions and balance assertions with
    // the ast builders. An import-id metadata string becomes the Import ID.
}

func main() {
    importer.Serve(bank{})
}
```

The [CSV importer example](_examples/csv_importer/) is a complete Importer. It reads a bank CSV and sorts the expenses into categories:

```sh
cd _examples/csv_importer
go build -o csv-importer .
beancount import --with ./csv-importer ledger.beancount transactions.csv
```

For the full API, see the [package docs on pkg.go.dev](https://pkg.go.dev/github.com/robinvdvleuten/beancount).

## Contributing

To report a bug, open an [issue](https://github.com/robinvdvleuten/beancount/issues). To fix one, open a [pull request](https://github.com/robinvdvleuten/beancount/pulls). For bigger ideas, open an issue first so you can get feedback before you spend a lot of time on it.

```sh
git clone https://github.com/robinvdvleuten/beancount.git
cd beancount
go test ./...
```

Before you open a pull request, run `gofmt -l .`, `golangci-lint run`, and `go test ./...`. If your change affects Beancount behavior, add a fixture under `testdata/compliance/`. If `bean-check` is on your `PATH`, the test suite compares this tool against it.

## License

Copyright (c) Robin van der Vleuten

Licensed under the GNU General Public License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.gnu.org/licenses/old-licenses/gpl-2.0.html

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
