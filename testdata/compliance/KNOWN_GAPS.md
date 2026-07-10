# Known Compliance Gaps

Documented divergences from official beancount v2 (2.3.x). Fixtures prefixed
`gap_` in this directory exercise open gaps: the differential suite verifies
their expectations against `bean-check`, while the in-process suite skips
them. Closing a gap means making the fixture pass and renaming it to drop the
`gap_` prefix, then removing its entry here.

## Open gaps (fixture-backed)

| Gap | Fixture | Official behavior |
|-----|---------|-------------------|
| Incomplete amounts (`Assets:A -1.00` without currency, `Assets:A USD` without number) | `gap_incomplete_amounts` | Parsed and completed by interpolation |
| Bare price annotation (`100.00 EUR @`) | `gap_empty_price_annotation` | Price interpolated from the residual |

## Deviations without fixtures

- **Formatter alignment**: `bean-format` aligns `open` directive currency
  lists to the currency column and indents metadata with 2 spaces (we use 4).
  The format parity test compares whitespace-normalized output only.
- **AVERAGE booking**: official v2 *rejects* AVERAGE at reduction time
  ("AVERAGE method is not supported"); we implement average-cost merging.
  Deviation by excess, kept deliberately (matches the v3 direction).

## Declared non-goals

- **BQL / bean-query**: not implemented.
- **Plugin execution**: `plugin` directives are parsed but never run
  (official v2 ships 28 plugins; `auto_accounts` and `implicit_prices`
  change check outcomes for ledgers that rely on them).
