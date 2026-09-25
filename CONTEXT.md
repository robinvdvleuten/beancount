# Beancount

A Go implementation of Beancount v2's tools, held to the official tools' behavior, for bookkeeping by people and AI agents.

## Language

### Extending the ledger

**Importer**:
Code that turns an external file, such as a bank statement, into new transactions to append to the ledger. It runs once per statement.
_Avoid_: plugin, extractor, ingester

**Statement**:
The external file an Importer reads, such as a bank's CSV or OFX export.
_Avoid_: import file, source, input

**Import ID**:
The bank's own identifier for a transaction, which an Importer supplies and which is kept in the transaction's `import-id` metadata.
_Avoid_: transaction ID, external ID, bank ID

**Duplicate**:
An imported directive that the ledger already records. A transaction is a Duplicate when the Import IDs match. Without an Import ID on the ledger's side, it is a Duplicate when the account and amount match and the dates are at most 2 days apart. A Balance assertion is a Duplicate when its account, date, and amount all match.
_Avoid_: double, repeat

**Unknown account**:
The account an import posts to when the Importer cannot say where the money went, `Expenses:Unknown` unless the user names another.
_Avoid_: suspense account, uncategorized account, placeholder

**Plugin**:
Code named by a `plugin` directive that rewrites the ledger's directives each time the ledger loads, before checks run.
_Avoid_: extension, hook, importer

**Built-in Plugin**:
A Plugin that ships with Beancount v2, such as `beancount.plugins.auto_accounts`, and that this project reproduces with the same behavior.
_Avoid_: core plugin, standard plugin

### Checking the ledger

**Booking**:
Matching a transaction's reductions to the lots its accounts already hold, and completing its missing numbers. A transaction whose Booking fails is a Dropped transaction.
_Avoid_: lot matching, interpolation (for the whole step)

**Dropped transaction**:
A transaction whose Booking failed, reported and left out of the ledger entirely, so balances and queries do not see it.
_Avoid_: rejected, skipped, invalid transaction

**Applied transaction**:
A transaction that was booked, so later directives see its effects, even when it is reported for another error such as not balancing or posting to an unopened or closed account.
_Avoid_: valid transaction, accepted transaction

**Error line**:
The file and line an error blames, printed as `path:line:` at the start of the message so editors can jump to it.
_Avoid_: position (a Beancount position is units held at a cost), location
