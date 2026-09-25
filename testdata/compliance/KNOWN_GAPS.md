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
only: PRINT renders through our formatter, whose layout differs from the
official printer (metadata indented by 4 spaces instead of 2, prices not
padded to the printer's fixed column). The content is equivalent beancount
text that round-trips through `bean-check`.

`query/gap_integer_division.bql` diverges on integer division: bean-query
divides two integer literals with Python's true division, so `SELECT 1 / 3`
is a float and prints its exact binary value
(`0.333333333333333314829616256247390992939472198486328125`), and the float
carries through later arithmetic (`7 / 2 * 2` is `7.0`). We give a decimal
rounded to 28 significant digits. Division with a decimal operand, which
covers every column, matches (`query/division*.bql`).

**Sums and products keep every digit** (#439): division rounds to Python's
28 significant digits like v2, but v2 also rounds `+`, `-` and `*` once an
operand has that many digits, and we don't. `1/3 USD` plus `100 USD` sums
to `100.3333333333333333333333333` in bean-query and to
`100.3333333333333333333333333333` here. Check outcomes agree, since the
difference is far below any tolerance.

`beancount format` re-renders the parsed AST, while `bean-format` only
rewrites whitespace line by line with one regular expression. The
formatter follows that expression's rules (which lines align, which
number, which lines pass through as written), so output is byte-identical
on the `format/` fixtures and on every fixture that parses, with two known
limits:

- A file that does not parse cannot be formatted; `bean-format` formats
  any text.
- On a dated line whose aligned number is not the whole amount (a balance
  tolerance, an expression's last operand), the text before that number
  is re-spelled with single spaces; `bean-format` keeps its spacing.
- Trailing whitespace on a posting line that gets aligned is dropped;
  `bean-format` keeps it, as it keeps the rest of such a line.

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
  with Python QUOTE_MINIMAL and CRLF, display-context precision (each
  currency's most common precision in the source, numbers cut to it),
  constant names sanitized by collapsing invalid runs ('USD' → `c_`),
  implicit GROUP BY, a single trailing ORDER BY direction, `ERROR:` lines
  on stdout with exit status 0, and `The PIVOT BY clause is not supported
  yet.` (a v2 limitation we mirror).

## Deliberate deviations

- **Pad runs after Booking, inside Apply**: beancount's `pad` is a built-in
  plugin that runs after Booking and before user Plugins. Ours inserts padding
  while applying balance assertions, after Plugins run. The padding
  transactions are the same, and neither supported Plugin sees a difference:
  a pad directive already names both accounts on its own date, and padding
  carries no price or cost.

- **Negative zero** (#408): an interpolated amount rounded to zero from a
  negative residual is `-0.00` in beancount (Python decimal keeps the sign);
  our decimals have no signed zero, so it books and renders as `0.00`, in
  BQL columns and in `print` alike. The value is the same; only the sign of
  zero differs, and only on the booked posting: sums and `balances` match.
  `query/gap_negative_zero.bql` pins it.

- **BQL `str()` of numbers, dates and strings**: bean-query returns
  Python's `repr` for these (`Decimal('200.00')`,
  `datetime.date(2023, 1, 1)`, `'Assets:Cash'`), a v2 implementation
  accident rather than a designed format. We print `200.00`, `2023-01-01`
  and `Assets:Cash`, with every written digit kept. `query/gap_str_scalars.bql`
  pins the difference; amounts, positions, inventories, NULL, integers,
  booleans and sets match (`query/str_*.bql`).

- **Error lines**: the differential suite compares the lines errors are
  reported on, and we keep our line where v2's is less precise
  (`lineGaps` in `cli/compliance_test.go`). A tag or link after the first
  posting is blamed on its own line and column, where v2 blames the
  transaction (`body_tags_after_posting`). An unbalanced `pushtag` or
  `pushmeta`, a missing documents root, a duplicate include and an include
  glob with no match are blamed on the directive that caused them, where v2
  prints line `0` or `<load>:0`.

## Declared non-goals

- **Other Built-in Plugins**: of the plugins official v2 ships, only
  `auto_accounts` and `implicit_prices` run (`plugin_*` fixtures). These are
  parsed but ignored: `auto`, `book_conversions`, `check_average_cost`,
  `check_closing`, `check_commodity`, `check_drained`, `close_tree`,
  `coherent_cost`, `commodity_attr`, `currency_accounts`, `divert_expenses`,
  `exclude_tag`, `fill_account`, `fix_payees`, `forecast`, `ira_contribs`,
  `leafonly`, `mark_unverified`, `merge_meta`, `noduplicates`, `nounused`,
  `onecommodity`, `pedantic`, `sellgains`, `split_expenses`, `tag_pending`,
  `unique_prices`, `unrealized`. User-written plugins do not run either.
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
