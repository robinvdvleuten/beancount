# A failed Currency group is dropped, not its whole transaction

Booking sorts a transaction's postings into Currency groups and completes each group on its own, like beancount's `booking_full`. When a group cannot be booked (too many missing numbers, a reduction that matches no lot or several), only that group's postings leave the transaction; the other groups are booked and applied. A transaction whose only group fails stays in the ledger with no postings, as `bean-query`'s `print` shows. Only a transaction whose postings cannot be sorted into groups, or whose numbers, costs or prices are malformed, is a Dropped transaction. This refines ADR 0002, which dropped every transaction that could not be booked.

## Considered options

- **Drop the whole transaction** when any group fails (the old behavior). Simpler to explain, but a later balance assertion on an account in a group that booked fails for us and passes in `bean-check`.

## Consequences

- A booked transaction may hold only some of its source postings, so code that walks `txn.Postings` sees the booked groups, and the formatter's source layout (`BodyItems`) can list postings that were not booked.
