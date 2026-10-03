# One position sum shared by the ledger and the query package

The three places that sum positions per lot keep their own types:
`inventory` (`ledger/inventory.go`), `lotSums` (`ledger/valuation.go`) and
`inventoryValue` (`query/types.go`). The query package also keeps its own
`positionValue`, `costValue` and `amountValue` rather than holding
`ledger.Position`. We don't plan to merge them behind one owner in the
ledger.

## Why this is out of scope

The request was to let the ledger own one position sum and one lot
identity, used by `GetBalanceTree` and by the query's `sum()`, `balance`
and FROM's summarization. Checking the evidence showed the three differ on
purpose and that nothing is broken:

- **No bug.** Two lots written `{100.0 USD}` and `{100.00 USD}` in
  different accounts merge under `select sum(position)`, with and without
  `CLOSE ... CLEAR`, as in bean-query. `positionValue.costKey` does not
  normalise the cost number, but Booking already holds one number per lot,
  so the difference from the ledger's keys has no visible effect.
- **Each sum has its own job.** `inventoryValue` mirrors beanquery's
  Python inventory (dict order, zero positions removed, `sortkey`
  ordering) and is held to byte parity. `inventory` is shaped for Booking
  and by the `CheckScaling` benchmark (per-sign lot counts, a cheap
  clone). `lotSums` is 30 lines that keep zero positions for the balance
  tree.
- **Posting weight is not duplicated.** `ledger/weight.go` computes
  weights from the cost spec before Booking; `postingWeight` in
  `query/env.go` computes a booked position's weight. The inputs differ.
- **Closing the books is not duplicated either.** `query/summarize.go`
  builds the synthetic transactions of BQL's `CLOSE` and `CLEAR`;
  `closingBalances` in `ledger/balance_tree.go` builds a balance sheet's
  Closed balances.

The one narrow step left, having `query` hold `ledger.Position` instead of
its own structs, touches about 70 uses across 13 files for no behaviour
change. That is tidiness, which AGENTS.md's YAGNI section does not accept
as a reason.

Reopen this if a bug turns up that comes from the lot keys or the two
position structs drifting apart, or a feature needs query values inside
the ledger.

## Prior requests

- #646: "ledger, query: one owner for summing positions per lot"
