# Faster `balance` column on large ledgers

BQL's `balance` column keeps its current implementation: each row that
reads it takes a copy of the running inventory, as beanquery's does. We
don't plan to make it share inventories between rows (copy-on-write
snapshots) or to render straight from the running inventory.

## Why this is out of scope

The request was to make `SELECT date, balance` scale linearly with the
ledger. Measuring it showed that the time follows the size of the output,
not the work behind it:

- With an inventory that grows with every transaction (the `lots`
  scenario of the `CheckScaling` benchmark ledgers), the output itself
  grows quadratically: 48 MB at 1k transactions, 768 MB at 4k. Every row
  prints the whole inventory, so no copying scheme can make the query
  linear.
- With a bounded inventory (`spending`), the query is already linear, at
  about 15–20 MB/s across 1k, 4k and 16k transactions.

What is left is peak memory or a constant factor on a query whose output
is already too large to read. For a local tool that is not worth the
added complexity in the query executor (see AGENTS.md's YAGNI section).
`balance` stays lazy (#577), so rows that never read it pay nothing.

Reopen this if a concrete query turns up whose time is not explained by
the size of its output.

## Prior requests

- #594: "query: make the balance column linear on large ledgers"
