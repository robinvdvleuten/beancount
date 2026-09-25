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
Code named by a `plugin` directive that rewrites the ledger's directives each time the ledger loads, after Booking and before checks run.
_Avoid_: extension, hook, importer

**Built-in Plugin**:
A Plugin that ships with Beancount v2, such as `beancount.plugins.auto_accounts`, and that this project reproduces with the same behavior.
_Avoid_: core plugin, standard plugin

### Processing the ledger

**Booking**:
Completing a transaction's postings: filling in missing amounts, matching reductions to the lots they reduce, and splitting an amount-less posting per currency. It happens once, before Plugins run. A transaction that cannot be matched to its lots is reported and left out of the ledger.
_Avoid_: interpolation, matching
