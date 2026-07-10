# Known Compliance Gaps

Documented divergences from official beancount v2 (2.3.x). Fixtures prefixed
`gap_` in this directory exercise open gaps: the differential suite verifies
their expectations against `bean-check`, while the in-process suite skips
them. Closing a gap means making the fixture pass and renaming it to drop the
`gap_` prefix, then removing its entry here.

## Open gaps (fixture-backed)

None. Every `.pass`/`.fail` fixture in this directory agrees with
`bean-check` 2.3.x; the differential suite enforces it.

## Empirically pinned option behavior

- **documents** and **operating_currency** are implemented (directory
  discovery incl. the missing-root error; accumulating list semantics),
  and **bean-format parity is byte-exact** — the format leg of the
  compliance suite compares output byte-for-byte.

- **account_rounding** is accepted but inert, exactly like official v2:
  despite the option's documentation, v2's load pipeline never inserts
  rounding postings (`fill_residual_posting` only runs in ledger-export
  reports). The `account_rounding_inert` / `account_rounding_no_posting`
  fixtures prove both implementations leave the rounding account empty.

## Deliberate deviations

- **AVERAGE booking**: official v2 *rejects* AVERAGE at reduction time
  ("AVERAGE method is not supported"); we implement average-cost merging
  via the `{*}` merge spec. Deviation by excess, kept deliberately (matches
  the v3 direction). The `average_account.fail` fixture pins the common
  ground: an ambiguous reduction under AVERAGE fails in both
  implementations (with different messages), so check exit codes agree.

## Declared non-goals

- **BQL / bean-query**: not implemented.
- **Plugin execution**: `plugin` directives are parsed but never run
  (official v2 ships 28 plugins; `auto_accounts` and `implicit_prices`
  change check outcomes for ledgers that rely on them).
