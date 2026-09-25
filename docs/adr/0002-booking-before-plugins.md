# Booking is its own phase, before Plugins

Beancount v2 books transactions before it runs Plugins, and Built-in Plugins such as `implicit_prices` depend on that: they read interpolated amounts, per-unit prices and whether a posting reduced a lot. We used to book inside Validate and Apply, which planned the booking twice and never exposed it. We made Booking its own phase, so the pipeline is Parse → Book → Plugins → Validate → Apply, and every Plugin transforms a booked ledger. Like beancount, Booking keeps its own inventory per account, independent of `open` directives, and leaves out a transaction whose reductions it cannot match to lots.

## Considered options

- **A post-booking hook in Apply** that emitted implicit prices as each transaction was applied. It was far cheaper, but it would have given Plugins two different contracts, depending on whether they need booked postings.
- **Plugins before validation on the unbooked AST.** This is wrong for `@@` prices, interpolated amounts and reductions, so it would miss bean-check parity.

## Consequences

- Pad still runs inside Apply, which is after Plugins here and before them in beancount. For the supported Plugins this makes no visible difference. The gap is recorded in `KNOWN_GAPS.md`.
- A transaction that fails Booking disappears from query results, as it does in bean-query.
