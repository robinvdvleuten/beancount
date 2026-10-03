# Beancount

A Go implementation of Beancount's tools, held to the behavior of the official Beancount v3 tools and beanquery (BQL's errors, typing and some clauses still differ, #562), for bookkeeping by people and AI agents.

## Language

### Extending the ledger

**Importer**:
Code that turns a Statement into Extracted directives to append to the ledger. It runs once per Statement.
_Avoid_: plugin, extractor, ingester

**Statement**:
The external file an Importer reads, such as a bank's CSV or OFX export.
_Avoid_: import file, source, input

**Extracted directive**:
A Transaction or Balance assertion an Importer returns for a Statement. It is checked together with the ledger and printed as Beancount text, and its Error lines point at that printed text.
_Avoid_: imported entry, new directive

**Import ID**:
The bank's own identifier for a transaction, which an Importer supplies and which is kept in the transaction's `import-id` metadata.
_Avoid_: transaction ID, external ID, bank ID

**Duplicate**:
An Extracted directive that the ledger already records, left out of the import. A transaction is a Duplicate when the Import IDs match. Without an Import ID on the ledger's side, it is a Duplicate when the ledger transaction has a posting with the same account and amount, the dates are at most 2 days apart, and the accounts one transaction posts to are all among the other's. A Balance assertion is a Duplicate when its account, date, and amount all match. Extracted directives are never Duplicates of each other. A transaction a `pad` inserts counts as a ledger transaction.
_Avoid_: double, repeat

**Unknown account**:
The account an import posts to when the Importer cannot say where the money went, `Expenses:Unknown` unless the user names another. It balances an Extracted transaction that has a single posting.
_Avoid_: suspense account, uncategorized account, placeholder

**Plugin**:
Code named by a `plugin` directive that rewrites the ledger's directives each time the ledger loads, after Booking and before checks run.
_Avoid_: extension, hook, importer

**Built-in Plugin**:
A Plugin that ships with Beancount, such as `beancount.plugins.auto_accounts`, and that this project reproduces with the same behavior.
_Avoid_: core plugin, standard plugin

### Checking the ledger

**Booking**:
Completing a transaction's postings, one Currency group at a time: matching its reductions to the lots its accounts already hold, completing its missing numbers, and splitting an amount-less posting per currency. It happens once, before Plugins run. A group whose Booking fails is a Dropped group.
_Avoid_: lot matching, interpolation (for the whole step)

**Currency group**:
The postings of a transaction that balance in one currency. A posting joins the group of its cost or price currency, or else of its units' currency; an amount-less posting joins every group.
_Avoid_: currency bucket, weight currency

**Dropped transaction**:
A transaction whose postings could not be sorted into Currency groups, reported and left out of the ledger entirely, so balances and queries do not see it.
_Avoid_: rejected, skipped, invalid transaction

**Dropped group**:
A Currency group whose Booking failed, reported and removed from its transaction. The transaction's other groups stay, so later directives see them.
_Avoid_: partial transaction, dropped postings

**Applied transaction**:
A transaction that was booked, so later directives see its effects, even when it is reported for another error such as not balancing or posting to an unopened or closed account.
_Avoid_: valid transaction, accepted transaction

**Booked position**:
The units a booked posting of an Applied transaction adds to or takes from one lot of its account, with that lot's per-unit cost, cost date and label. A reduction has one per lot it is booked against; any other posting has one of its own. It reduced its lot when the account held that lot with the opposite sign; `implicit_prices` emits no price from the cost of such a position.
_Avoid_: booked lot, lot change

**Tolerance**:
How far a transaction's weights in one currency may be from zero, or an account's balance from a Balance assertion, and still pass. It is inferred from the precision of the numbers written, from half the last digit by default, unless the options or the assertion set it.
_Avoid_: precision, epsilon, rounding error

**Error line**:
The file and line an error blames, printed as `path:line:` at the start of the message so editors can jump to it.
_Avoid_: position (a Beancount position is units held at a cost), location

### Reporting

**Valuation**:
How a report states the balances it shows: in Units, At cost, At market value, or Converted to one of the ledger's operating currencies. Every report shows the same Valuation, At cost unless the user picks another.
_Avoid_: conversion (beancount's conversions are the `Conversions:Current` account and the entries `CLOSE` adds), display mode

**Closed balances**:
The balances of a balance sheet, which adds Current earnings, Current conversions and Unrealized gains under Equity so that Assets, Liabilities and Equity sum to zero. Other reports show the accounts as they are.
_Avoid_: capped balances (fava's term), closing (BQL's `CLOSE` adds only the conversions)

**Current earnings**:
The ledger's net Income and Expenses up to the report date, at cost, shown under Equity in the account `account_current_earnings` names (`Earnings:Current` by default).
_Avoid_: net income, retained earnings, profit

**Current conversions**:
The negated cost balance that price conversions leave behind up to the report date, shown under Equity in the account `account_current_conversions` names (`Conversions:Current` by default). It is what keeps the ledger's cost balance at zero.
_Avoid_: conversion, rounding

**Unrealized gains**:
What the balance sheet's other balances leave over once each is stated At market value or Converted to X, shown under Equity in `Earnings:Unrealized` so the sheet sums to zero. Since the ledger's cost balance is zero, it is the holdings' value minus their cost, plus the units of any holding that could not be valued. It is zero At cost and not shown in Units, where a balance sheet does not balance.
_Avoid_: capital gains, market gains

**Table**:
A named set of rows a query reads, such as `postings` (the default) or `entries`. A query's columns are the columns of its Table.
_Avoid_: source, relation

**Table reference**:
Naming a Table after SELECT's FROM, as `"name"`, `#name`, or a bare name that is not a column of the current Table.
_Avoid_: from expression, table expression

**FROM filter**:
An expression after FROM that keeps some rows of the current Table, optionally with OPEN, CLOSE and CLEAR.
_Avoid_: from expression, table reference

**Empty table**:
The Table `#` names: one row and no columns.
_Avoid_: null table, dual
