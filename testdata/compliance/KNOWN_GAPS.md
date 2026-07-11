# Known Compliance Gaps

Documented divergences from official beancount v2 (2.3.x). Fixtures prefixed
`gap_` in this directory exercise open gaps: the differential suite verifies
their expectations against `bean-check`, while the in-process suite skips
them. Closing a gap means making the fixture pass and renaming it to drop the
`gap_` prefix, then removing its entry here.

## Open gaps (fixture-backed)

None among the `.pass`/`.fail` check fixtures. Every one agrees with
`bean-check` 2.3.x; the differential suite enforces it.

For BQL, `query/gap_print_*.bql` diverge from `bean-query` in whitespace
only: PRINT renders through our formatter, whose canonical layout differs
from the official printer (per-entry number alignment, 2-space metadata
indent). The content is equivalent beancount text that round-trips through
`bean-check`.

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

- **BQL / bean-query** is implemented (`beancount query`) and pinned
  byte-for-byte against `bean-query` 2.3.6 by the fixtures in `query/`
  (text and csv, including numberify, shortcut statements, FROM
  summarization, and error output). Notable pinned quirks we reproduce:
  data-width columns with truncated centered headers, padded CSV cells
  with Python QUOTE_MINIMAL and CRLF, per-currency decimal precision,
  constant names sanitized by collapsing invalid runs ('USD' → `c_`),
  implicit GROUP BY, a single trailing ORDER BY direction, `ERROR:` lines
  on stdout with exit status 0, and `The PIVOT BY clause is not supported
  yet.` (a v2 limitation we mirror).

## Deliberate deviations

- **AVERAGE booking**: official v2 *rejects* AVERAGE at reduction time
  ("AVERAGE method is not supported"); we implement average-cost merging
  via the `{*}` merge spec. Deviation by excess, kept deliberately (matches
  the v3 direction). The `average_account.fail` fixture pins the common
  ground: an ambiguous reduction under AVERAGE fails in both
  implementations (with different messages), so check exit codes agree.

## Declared non-goals

- **Plugin execution**: `plugin` directives are parsed but never run
  (official v2 ships 28 plugins; `auto_accounts` and `implicit_prices`
  change check outcomes for ledgers that rely on them).
- **BQL `id` column digests**: ids are unique and stable but hash the
  source location, not the directive contents like `compare.hash_entry`,
  so the hex digests differ from official output.
- **BQL shell extras**: `EXPLAIN`, `RUN` of stored `query` directives, and
  shell settings (`set format ...`) are not implemented; nor are the
  beanquery v3 extensions (`HAVING`, subqueries, `CREATE TABLE`, per-term
  ORDER BY directions).
- **BQL dict-typed metadata functions**: `commodity_meta`, `currency_meta`,
  `open_meta`, and `getitem` (dict-typed values) are not implemented;
  `meta`, `entry_meta`, and `any_meta` cover scalar metadata lookups.
