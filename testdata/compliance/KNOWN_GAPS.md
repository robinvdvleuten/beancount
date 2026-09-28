# Known Compliance Gaps

Documented divergences from official beancount v2 (2.3.x). Fixtures prefixed
`gap_` in this directory exercise open gaps: the differential suite verifies
their expectations against `bean-check`, while the in-process suite skips
them. Closing a gap means making the fixture pass and renaming it to drop the
`gap_` prefix, then removing its entry here.

## Open gaps (fixture-backed)

None among the `.pass`/`.fail` check fixtures. Every one agrees with
`bean-check` 2.3.x; the differential suite enforces it.

**Cost numbers without a currency, and compound costs with a missing
number**: v2 accepts a cost that states a number without its currency
(`{5}`, `{{5}}`) and takes the currency from the Currency group, and it
interpolates the missing part of a compound cost (`{# 5 USD}`, `{5 # USD}`;
`{# USD}` has two missing numbers). We reject these as syntax errors. A cost
that states only its currency (`{USD}`, `{{USD}}`) matches v2
(`cost_currency_only*` fixtures).

**Merge cost on an augmentation**: both implementations report a merge
cost `{*}` ("Cost merging is not supported yet"), and v2 then books it like
`{}`: an augmentation at `{*}` gets the cost the transaction's residual
implies, dated by the transaction (`10 HOOL {100.00 USD, 2020-02-01}`), and a
later `{}` reduction books against it. We drop that inferred cost, so the
augmentation holds its units without cost: `print` echoes `{*}`, the units
merge with any HOOL held without cost, and the later reduction finds no lot
to book against and is reported. For the same reason `implicit_prices`
emits no `from_cost` price for it, where v2 does (`1 ACME {*}` against
`-11 USD` on an AVERAGE account holding `2 ACME {10 USD}` and
`2 ACME {12 USD}` gives `price ACME 11 USD`). A reduction at `{*}` matches
v2 (`applied_merge_cost`, `merge_cost_avg`).

**Two prices on one date**: `getprice` returns the first price of the day,
bean-query the last, because v2's `build_price_map` keeps a date's latest
entry. When `implicit_prices` inserts 20 USD and then 21 USD for ACME on
the same date, we return 20 and bean-query 21. The same holds across
directions: with `price AUD 0.6 USD` and then `price USD 1.5 AUD` on one
date, we convert AUD to USD at 0.6 and bean-query at 1/1.5.

**Cost and price in different currencies**: bean-check reports `Cost and
price currencies must match: EUR != USD` for `10 BOND {95 EUR} @@ 1100 USD`;
we report nothing.

**Tolerances of the residual check**: bean-check's balance check
(`ops/validation.py`) infers tolerances without the postings Booking
interpolated (`AUTOMATIC_META`), and we count them. So a posting with an
interpolated price or cost still widens its units currency for us:
`-10.5 EUR @ USD` beside `1.04 EUR` and `-1 EUR` leaves 0.04 EUR, which
bean-check reports (tolerance 0.005) and we accept (0.05). Two more
differences are in `ledger/tolerance.go`:
- Under `infer_tolerance_from_cost`, v2 lets a currency's cost tolerance
  replace the `*` default, even a larger one: with `*:0.1`,
  `1.001 HOOL {1 USD}` and `-1 GOOG {1.011 USD}` leave -0.010 USD, beyond
  the 0.0005 USD the cost allows. bean-check reports it; we tolerate it up
  to 0.1.
- With only integers written, v2 has no tolerance at all, so it reports
  the 1E-27 USD an interpolated price such as `-3 EUR @ USD` against
  `10 USD` leaves behind. We report nothing.

For BQL, `query/gap_integer_division.bql` diverges on integer division:
bean-query divides two integer literals with Python's true division, so
`SELECT 1 / 3` is a float and prints its exact binary value
(`0.333333333333333314829616256247390992939472198486328125`), and the float
carries through later arithmetic (`7 / 2 * 2` is `7.0`). We give a decimal
rounded to 28 significant digits. Division with a decimal operand, which
covers every column, matches (`query/division*.bql`).

The printer (BQL `PRINT`, `import`, error context) follows beancount's
`printer.py`, with these known differences:

- `PRINT` ignores `option "render_commas" "TRUE"`: bean-query prints
  `-1,000.50 USD`, we print `-1000.50 USD`.
- A `custom` directive's account value prints quoted
  (`custom "c" "Assets:Cash"`), because `ast.CustomValue` has no account
  kind.
- A number in metadata prints in fixed notation (`0.0000001`), where
  Python's `str` gives `1E-7`.
- `PRINT FROM OPEN ON <date>` prints the `S` summarization transactions
  first, followed by every directive before the date. bean-query prints
  only the `open` directives and earlier prices, then the `S` transactions.
- Like `printer.py`, the printer does not escape `note`, `event`, `query`,
  `document` and `custom` strings or cost labels. So `import` output with a
  `"` in one of them fails its re-parse.

`beancount format` re-renders the parsed AST, while `bean-format` only
rewrites whitespace line by line with one regular expression. The
formatter follows that expression's rules (which lines align, which
number, which lines pass through as written), so output is byte-identical
on the `format/` fixtures and on every fixture that parses, with these known
limits:

- A file that does not parse cannot be formatted; `bean-format` formats
  any text.
- On a dated line whose aligned number is not the whole amount (a balance
  tolerance, an expression's last operand), the text before that number
  is re-spelled with single spaces; `bean-format` keeps its spacing.

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

- **BQL `sum()` of booleans**: bean-query accepts `sum(bool)` because
  Python's `bool` subclasses `int`, sums the values as integers and still
  types the column as boolean, so `sum(1 = 1)` over four rows renders
  `TRUE`, `sum(false)` renders `FALS` (cut to the header's width) and
  `sum(true) + 1` is `23`. We reject it: `ERROR: Invalid function
  'sum(bool)' in targets/column context.` (#422).

- **BQL ordering of mixed types**: `<`, `<=`, `>` and `>=` between a
  number or date and a string (`year < '2024'`) raise Python's `TypeError`
  in bean-query, which prints the traceback to stdout on the first row it
  evaluates. We compare the two values' string forms instead, so
  `query/gap_compare_mixed_types_ordering.bql` returns rows. `=` and `!=`
  between numbers, dates, booleans and strings match: values of different
  types are never equal, and a boolean equals the integer 1 or 0
  (`query/compare_*.bql`, #514). An amount, position or inventory compared
  with another type raises an `AttributeError` traceback in bean-query;
  we return false.

- **Error lines**: the differential suite compares the lines errors are
  reported on, and we keep our line where v2's is less precise
  (`lineGaps` in `cli/compliance_test.go`). A tag or link after the first
  posting is blamed on its own line and column, where v2 blames the
  transaction (`body_tags_after_posting`). An unbalanced `pushtag` or
  `pushmeta`, a missing documents root, a duplicate include, an include
  glob with no match, a missing included file and a Built-in Plugin given
  a configuration are blamed on the directive that caused them, where v2
  prints line `0` or `<load>:0`. A syntax error at the end of a line (`option "title"` without
  its value) is blamed on that line; v2's lexer counts the line break as
  part of the next line and blames that one.

- **Syntax errors drop only their own directive**: like v2, `check` and
  `query` report every syntax error, drop the directive it is in and check
  or query the rest. v2's generated parser also drops the directive right
  above a line of invalid tokens when no blank line separates them, which
  hides that directive's own errors; we keep it.

- **`doctor missing_open` under raw plugin processing**: with
  `option "plugin_processing_mode" "raw"`, v2 skips `ops.balance`, so a
  balance on an unknown account stays loaded and `bean-doctor missing_open`
  counts it as a use. `beancount doctor missing_open` always skips
  balances. Raw mode is unsupported generally; `check` diverges there too.

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
  A name under `beancount.plugins` that v2 does not ship is reported, like
  v2's `Error importing`, but any other name is not checked: v2 fails to
  import `does.not.exist` or `beancount.nope`. Names outside `beancount`
  depend on the user's Python path, and other `beancount.*` names would need
  v2's module list embedded (`.out-of-scope/beancount-module-import-check.md`).
  Naming one of v2's default plugins runs it a second time there:
  `beancount.ops.pad` reports every used pad as `Unused Pad entry`, and
  `beancount.ops.balance` reports each failing balance twice. We ignore both.
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
