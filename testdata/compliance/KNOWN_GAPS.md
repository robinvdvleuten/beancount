# Known Compliance Gaps (beancount v3)

Documented divergences from official beancount v3, probed against 3.2.3.
BQL and the printer are the exception: beancount 3 ships no `bean-query`, so
until BQL moves to beanquery (#562, and #560 for the printer) their entries
are measured against `bean-query` 2.3.6.

Fixtures prefixed `gap_` in this directory exercise open gaps: the
differential suite verifies their expectations against `bean-check`, while
the in-process suite skips them. Closing a gap means making the fixture pass
and renaming it to drop the `gap_` prefix, then removing its entry here.

## Open gaps (fixture-backed)

Every `.pass`/`.fail` check fixture agrees with `bean-check` 3.2.3; the
differential suite enforces it.

**Balance on an unknown account** (`balance_unknown_account.fail`):
bean-check reports `Invalid reference to unknown account 'Assets:Nope'`
twice on the balance's line, from validation and again from its balance
check, which then checks the assertion too: one of a non-zero amount also
fails (`Balance failed for 'Assets:Nope': expected 1 USD != accumulated 0
USD (1 too little)`). We report the unknown account once and check nothing
more. The lines agree. Like beancount v3, the balance is a use of its
account for `doctor missing_open`.

**Merge cost on an augmentation**: both implementations report a merge
cost `{*}` ("Cost merging is not supported yet"), and beancount then books
it like `{}`: an augmentation at `{*}` gets the cost the transaction's
residual implies, dated by the transaction
(`10 HOOL {100.00 USD, 2020-02-01}`), and a later `{}` reduction books
against it. We drop that inferred cost, so the augmentation holds its units
without cost: `print` shows `10 HOOL`, the units merge with any HOOL held
without cost, and the later reduction finds no lot to book against and is
reported. For the same reason `implicit_prices` emits no `from_cost` price
for it, where beancount does (`1 ACME {*}` against `-11 USD` on an AVERAGE
account holding `2 ACME {10 USD}` and `2 ACME {12 USD}` gives
`price ACME 11 USD`). A reduction at `{*}` matches beancount
(`applied_merge_cost`, `merge_cost_avg`).

**Two prices on one date**: `getprice` returns the first price of the day,
bean-query the last, because beancount's `build_price_map` keeps a date's
latest entry. When `implicit_prices` inserts 20 USD and then 21 USD for ACME on
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
bean-check reports (tolerance 0.005) and we accept (0.05). One more
difference is in `ledger/tolerance.go`: with only integers written,
beancount has no tolerance at all, so it reports the 1E-27 USD an
interpolated price such as `-3 EUR @ USD` against `10 USD` leaves behind.
We report nothing.

For BQL, `query/gap_integer_division.bql` diverges on integer division:
bean-query divides two integer literals with Python's true division, so
`SELECT 1 / 3` is a float and prints its exact binary value
(`0.333333333333333314829616256247390992939472198486328125`), and the float
carries through later arithmetic (`7 / 2 * 2` is `7.0`). We give a decimal
rounded to 28 significant digits. Division with a decimal operand, which
covers every column, matches (`query/division*.bql`).

A currency beancount v3 added, one starting with `/` (`/ESZ24`) or longer
than 24 characters, is a syntax error in `bean-query` 2.3.6, which drops
its directive, and loads here. A slash, digits and a currency of two or
more characters is no error there but a division: `10 /2USD` is `5 USD`
in `bean-query` 2.3.6, and 10 units of `/2USD` here and in bean-check. So
no `query/` fixture can hold such a currency until BQL moves to beanquery
(#562).

Interpolated numbers follow beancount v3, so three of them differ from
`bean-query` 2.3.6's until BQL moves to beanquery (#562):
- `option "tolerance_multiplier"` is an invalid option there, which keeps
  the default 0.5; we apply it. With `"5"`, `10.00 USD` and
  `3.33333 EUR @ 1.11111 USD` leave `-13.70 USD` there and `-13.7 USD`
  here. Under the old name, `inferred_tolerance_multiplier`, stdout
  matches, and we report the rename on stderr as bean-check does.
- `option "use_precise_interpolation"` is an invalid option there:
  `precise_interpolation.pass` interpolates `-7.0 USD` there and
  `-7.02345 USD` here.
- A currency's `inferred_tolerance_default` counts before booking only
  when a posting names the currency, with no option involved:
  `tolerance_default_unused_currency.pass` interpolates `33.3 USD` there,
  which fails its balance assertion, and `33.333 USD` here.

The printer (BQL `PRINT`, `import`, error context) follows beancount
2.3.6's `printer.py`, with these known differences from `bean-query`'s
`PRINT`:

- `PRINT` ignores `option "render_commas" "TRUE"`: bean-query prints
  `-1,000.50 USD`, we print `-1000.50 USD`.
- A `custom` directive's account value prints quoted
  (`custom "c" "Assets:Cash"`), because `ast.CustomValue` has no account
  kind.
- A number in metadata prints in fixed notation (`0.0000001`), where
  Python's `str` gives `1E-7`.
- A document's tags and links print 2.3.6's way (`#a#b^link1`) until the
  printer follows v3 (#560). A note's print as beancount 3.2.3's
  `printer.py` writes them (`"hello" #trip ^link1`): `bean-query` 2.3.6
  rejects them, so no `print_*` fixture can hold them. A note between
  `pushtag` and `poptag` therefore prints the pushed tag, where
  `bean-query` 2.3.6, whose notes have no tags, prints none;
  `query/gap_print_note_pushed_tag.bql` pins it until BQL moves to
  beanquery (#562). Until then a note's tags and links, like a
  document's, are not in the `tags` and `links` columns, which
  `bean-query` fills for transactions only.
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

## Open gaps (no fixture yet)

Probed against bean-check 3.2.3, each without a fixture until its issue
lands:

- #562: `option "display_precision"` is parsed and checked, not applied.
  With `"USD:0.001"`, beancount v3's display context formats `1.5 USD` as
  `1.500`; we render `1.5`, as `bean-query` 2.3.6 does, which reports the
  option as invalid.
- #565: a single capital letter followed by whitespace is bean-check's
  `CAPITAL` token, new in v3, which its grammar reads as a currency
  (`1 V`, `commodity V`, `price V 1 USD`) or as a transaction flag
  (`2020-01-02 V "x"`). We read only `P`, `S`, `T`, `C`, `U`, `R` and `M`
  as flags, never as a currency, and report any other single letter as an
  invalid token.
- #563: a merge cost next to a number (`{100 USD, *}`, `{*, 100 USD}`,
  `{{*}}`) parses in bean-check, which reports `Cost merging is not
  supported yet`; we report a syntax error and drop the transaction. The
  lines agree.
- #564: an indented comment line before the metadata of an `open` passes
  bean-check; we report `unexpected indentation` and drop the directive.
- #566: a currency ending in `-` (`price BA- 1 USD`) passes bean-check
  3.2.3; we report an invalid token.
- #566: a bare currency as a `custom` value (`custom "x" USD`,
  `custom "x" /ESZ24`) is `syntax error, unexpected CURRENCY` in bean-check
  (`USD` in 2.3.6 too); we accept it as a string.
- #567: an invalid line in a transaction's body drops the whole
  transaction in bean-check; we drop the line and report the rest as
  `Transaction does not balance`. An account with an invalid component
  (`Assets:😀x`) is `Invalid account name` in bean-check, which keeps the
  posting and reports the account as unknown; we report a syntax error and
  drop the transaction.
- #568: an option's CURRENCY:NUMBER value (`inferred_tolerance_default`,
  `display_precision`) takes any Unicode digit in bean-check, as Python's
  `\d` does (`"USD:٣"` is 3); we take ASCII digits only and report the
  value as invalid.
- #568: an option's number is read by beancount's `D()`, which takes `""`
  (as 0), `"1,000"`, `" 10 "` and `"1_0"`: each passes bean-check as a
  `tolerance_multiplier`; we report an invalid value. `"Infinity"` and
  `"NaN"` pass too, until a transaction infers a tolerance: bean-check then
  crashes with `decimal.InvalidOperation`.
- #568: an included file's options are ignored by both, but bean-check
  still reports their errors (an invalid name or value, a rename) on the
  included file's line and exits 1; we print a warning that the option is
  ignored, valid or not, and pass.
- #568: `option "allow_pipe_separator"` and
  `option "allow_deprecated_none_for_tags_and_links"` are errors in
  bean-check whatever their value (`Allowing pipe separator temporarily;
  this will go away eventually.`), in 2.3.6 too; we accept them silently.
- #569: `plugin "beancount.plugins.__init__"` imports in bean-check; we
  report `Error importing`.
- #570: a `documents` root reached through a symlink discovers nothing
  here, and a ledger path starting with `//` keeps it in beancount's file
  and document names, where we collapse it.

Differences in message text only, which the suites cannot see since they
compare the lines errors are on:

- `Transaction does not balance` lists the whole residual in bean-check,
  every currency with the exponent its sum leaves
  (`(0.080 USD, 0.00001 EUR)`), in 2.3.6 too; we list only the currencies
  beyond their tolerance, trailing zeros dropped (`(0.08 USD)`).
- `check --json` prints bean-check `--json`'s shape, filenames and lines,
  but its messages are worded as our text errors are, and it lists errors
  in our order: bean-check lists a failing balance's errors before
  validation errors, and orders errors about one directive by Python's
  hash seed. The differential compares the lines only.

## Empirically pinned option behavior

- **documents** and **operating_currency** are implemented (directory
  discovery incl. the missing-root error; accumulating list semantics).

- **account_rounding** is accepted but inert, exactly like official
  beancount: despite the option's documentation, its load pipeline never
  inserts rounding postings (nothing in beancount 3 calls
  `fill_residual_posting`). The `account_rounding_inert` / `account_rounding_no_posting`
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

- **BQL numbers with a positive exponent** (#512): Python keeps a
  quotient's exponent, so `100 / 5.0` is `2E+1`. bean-query sizes the
  column for one digit and cuts the value to it, printing `2` for 20 and
  a blank cell for `1000 / 5.0` (`2E+2`), which hides the value. We print
  fixed notation (`20`, `200`); `query/gap_number_exponent_positive.bql`
  pins it. A number whose adjusted exponent is below -6 matches: it keeps
  Python's form (`1E-7`) in the width fixed notation would take
  (`query/number_exponent_*.bql`).

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
  reported on, and we keep our line where beancount's is less precise
  (`lineGaps` in `cli/compliance_test.go`). A tag or link after the first
  posting is blamed on its own line and column, where beancount blames the
  transaction (`body_tags_after_posting`). An unbalanced `pushtag` or
  `pushmeta`, a missing documents root, a duplicate include, an include
  glob with no match, a missing included file and a Built-in Plugin given
  a configuration are blamed on the directive that caused them, where
  beancount prints line `0` or `<load>:0`. A syntax error at the end of a
  line (`option "title"` without its value) is blamed on that line;
  beancount's lexer counts the line break as part of the next line and
  blames that one.

- **Symlinked working directory** (#570): given a relative ledger path in a
  directory reached through a symlink, we resolve it against `$PWD` and
  beancount against the physical directory, so the file names in error
  prefixes and the document paths in messages and `print` differ
  (`link/l.beancount` against `real/l.beancount`; on macOS also `/tmp`
  against `/private/tmp`). No fixture: the suites run from a physical
  directory.

- **Failed balance assertion wording** (#524): a balance assertion that
  fails reads `Balance mismatch for <account>:` with `Expected:` and
  `Actual:` lines, where beancount prints `Balance failed for '<account>':
  expected <amount> != accumulated <amount> (<difference> too much)` (or
  `too little`). The line is the same; we keep our wording.

- **Posting in `No position matches`**: a reduction whose cost spec
  matches no lot is reported in beancount's words, `No position matches "<posting>"
  against balance <inventory>`, but `<posting>` is the posting's units and
  cost spec as `Ambiguous matches` and `Not enough lots` quote them
  (`-5 HOOL {13 USD}`), not Python's `Posting(account=..., meta={...})`
  repr, which spells out the file name and line (#518).

- **Syntax errors drop only their own directive**: like beancount, `check`
  and `query` report every syntax error, drop the directive it is in and check
  or query the rest. beancount's generated parser also drops the directive right
  above a line of invalid tokens when no blank line separates them, which
  hides that directive's own errors; we keep it.

- **Raw plugin processing**: `option "plugin_processing_mode" "raw"` is
  accepted but has no effect. beancount skips `ops.pad` and `ops.balance`
  under it, so a failing balance assertion passes `bean-check`; we report
  it.

## Declared non-goals

- **Other Built-in Plugins**: beancount 3.2.3 ships 18 plugin modules in
  `beancount/plugins` (its `_test.py` files not counted). Two of them run,
  `auto_accounts` and `implicit_prices` (`plugin_*` fixtures). The other 16
  are parsed but ignored: `auto`, `check_average_cost`, `check_closing`,
  `check_commodity`, `check_drained`, `close_tree`, `coherent_cost`,
  `commodity_attr`, `currency_accounts`, `leafonly`, `noduplicates`,
  `nounused`, `onecommodity`, `pedantic`, `sellgains`, `unique_prices`.
  User-written plugins do not run either. Any other name under
  `beancount.plugins` is reported, like beancount's `Error importing`
  (the 12 modules v3 removed from 2.3.6's 30 included,
  `plugin_removed_in_v3`), but a name elsewhere is not checked: beancount fails to
  import `does.not.exist` or `beancount.nope`. Names outside `beancount`
  depend on the user's Python path, and other `beancount.*` names would need
  beancount's module list embedded
  (`.out-of-scope/beancount-module-import-check.md`).
  Naming one of beancount's default plugins runs it a second time there:
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
