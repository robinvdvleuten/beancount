# Known Compliance Gaps (beancount v3)

Documented divergences from official beancount v3, probed against 3.2.3,
and for BQL against beanquery 0.2.0, v3's `bean-query`. The query fixtures
that still differ are listed in `queryGaps` (`cli/query_compliance_test.go`)
with the issue that closes them.

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

The printer (BQL `PRINT`, `import`, `doctor missing_open`, error context)
follows beancount 3.2.3's `printer.py`, with these known differences from
`bean-query`'s `PRINT`:

- `PRINT` ignores `option "render_commas" "TRUE"`: bean-query prints
  `-1,000.50 USD`, we print `-1000.50 USD`.
- A `custom` directive's account value prints quoted
  (`custom "c" "Assets:Cash"`), because `ast.CustomValue` has no account
  kind.
- A number in metadata prints in fixed notation (`0.0000001`), where
  Python's `str` gives `1E-7`.
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

Probed against beanquery 0.2.0, BQL gaps with no fixture yet:

- #590: beanquery's list constants are syntax errors in ours:
  `SELECT (1, 2)` prints `[1, 2]` there, and `'a' IN ('a', 'b')` is TRUE.
- #593: beanquery's functions `round`, `substr`, `splitcomp`,
  `yearmonth`, `int`, `decimal`, `bool`, `date_trunc`, `date_part`,
  `date_bin`, `interval`, `parse_date`, `repr` and `empty` are missing in
  ours (`SELECT round(number)` prints `200` there and is `no function
  matches "round(decimal)"` here), and ours has a `ymonth` that
  beanquery has not.
- #592: `SELECT account ORDER BY sum(number)`, an aggregate in ORDER BY
  without GROUP BY, prints the rows in ledger order in beanquery; ours
  reports the GROUP-BY coverage error.
- #591: beanquery's attribute (`.x`) and subscript (`['x']`) access on a
  column are syntax errors in ours: `SELECT account.x` is `column type is
  not structured` there, with carets under `account.x`, and a syntax
  error at offset 14 in ours. A `.` after a number moves the caret too:
  beanquery's is one column right of ours for `SELECT 2.5.5` and
  `SELECT 1 .5`.
- #595: `other_accounts` is a sorted list in beanquery, though its column is
  typed as a set, and a set in ours. Both render it alike, but ORDER BY
  compares beanquery's lists element by element and ours sets by
  inclusion (`ORDER BY other_accounts DESC` orders the rows differently),
  and `str(other_accounts)` is `['Assets:Cash', 'Expenses:Food']` there
  and `frozenset({'Assets:Cash', 'Expenses:Food'})` here.
- #587: beanquery's `%`, `!~`, `?~`, `NOT IN` and `BETWEEN` operators are
  syntax errors in ours: `SELECT 7 % 3` prints `1` there.
- #588: a transaction without a payee has a NULL `payee` in beanquery and
  an empty string in ours: `SELECT payee IS NULL` is TRUE there and FALSE
  in ours. Both render the column empty.
- #589: `~` takes Python's regular expressions in beanquery and RE2's in
  ours: `account ~ 'Cash(?=)'` keeps 6 rows there and none in ours, and
  `account ~ '['` fails there with `re.error` and keeps no rows in ours.
  Python's integers do not overflow: `100000000000 * 100000000000` is
  `10000000000000000000000` there and wraps around in ours, and
  `SELECT -9223372036854775808` prints the number there and is a syntax
  error in ours, whose unsigned 9223372036854775808 does not fit. Casting an
  untyped operand parses more there: the metadata strings `"1_000"` and
  `"NaN"` are decimals and `"2023-2-1"` a date in beanquery, and NULL in
  ours. `1 IN account` fails there with a `TypeError` and is FALSE in
  ours.

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
  byte-for-byte against beanquery 0.2.0 by the fixtures in `query/` (text
  and csv, including numberify, shortcut statements and FROM
  summarization). Notable pinned quirks we reproduce: columns named by
  their source text (`sum( number )`, `SUM((position))` for BALANCES),
  data-width columns with truncated headers centred like Python's
  `str.center`, a sign column in every amount, each currency's amounts
  rounded half-even to its most common precision in the source or its
  `display_precision`, inventories laid out in per-commodity
  sub-columns, CSV with Python's QUOTE_MINIMAL and CRLF that pads number,
  amount, position and inventory cells and leaves the others as they are,
  numberify in text too, nothing printed for an empty text result,
  implicit GROUP BY, operators type-checked when the query compiles
  (`operator "less(int, str)" not supported` for `year < '2024'`), an
  untyped (object) operand cast to the other operand's type, NULL
  operands giving NULL, AND and OR with beanquery's NULL handling
  (`NULL AND FALSE` is NULL), `IS [NOT] NULL`, unsigned numbers with a
  unary minus on any expression (`-number`, `- -1`, `number -1` as a
  subtraction), `count(*)`, digits in identifiers, an ASC or DESC per
  ORDER BY term with NULL first ascending and last descending, an ORDER BY
  or GROUP BY name bound to the last target with that name, an ORDER BY
  index at most the number of distinct target names, values ordered as
  Python orders beancount's (in ORDER BY, `min()`, `max()` and PIVOT BY:
  a position by `Position.sortkey`, an amount by currency then number, an
  inventory by its sorted positions, a set by inclusion, and `max()` with
  the named tuples' `>`), `HAVING` on an
  aggregate, a grouped query's targets outside the group key (HAVING's, an
  ORDER BY expression's) reading their columns from the table's last row,
  after FROM's transforms and before its filter expression and WHERE, as
  beanquery's do, `PIVOT BY` with beanquery's pivoted layout (a
  `col1/col2` first column, one column per sorted value of the second
  column, or per value and other column, named after the value as Python
  prints it, empty where a combination is missing, applied after ORDER BY
  and LIMIT), functions that give NULL for any NULL argument, `str()`
  printing `TRUE` and `FALSE`, `count()` of a value skipping NULLs,
  `coalesce()` of one type only, `has_account()` in every clause,
  `length()` counting code points, a posting's own `lineno`, `filename` and
  `location` (NULL for a posting summarization creates), a lazy `balance`
  that a row joins the first time it reads the column (so WHERE sees the
  current posting, and a row WHERE short-circuits before reading it never
  joins), and a statement that does not parse or compile reported on
  stderr with exit status 1, in the words and with the caret lines of
  beanquery's interactive shell (one-shot `bean-query` prints a Python
  traceback instead).

## Deliberate deviations

- **Negative zero** (#408): an interpolated amount rounded to zero from a
  negative residual is `-0.00` in beancount (Python decimal keeps the sign);
  our decimals have no signed zero, so it books as `0.00` and renders so in
  BQL columns and in `print` alike. The value is the same; only the sign of
  that zero differs. `query/negative_zero.bql` shows it and is listed in
  `queryGaps`. Arithmetic differs the same way: beanquery keeps the sign of
  a negative number times zero (`number * 0` is `-0.00` for `-1.00`), and
  we give `0.00`. A negative number that only rounds to zero when displayed
  (`-0.001 USD` at USD's two digits) keeps its sign as in beanquery,
  `-0.00 USD`, numberified too (`query/negative_dust.bql`,
  `query/numberify_negative_dust.bql`).

- **Infinite and NaN option numbers** (#568): beancount's `D()` takes
  `"Infinity"` and `"NaN"` as a `tolerance_multiplier` or a CURRENCY:NUMBER
  value, and bean-check then crashes with `decimal.InvalidOperation` once a
  transaction infers a tolerance. Our decimals have neither, so we report
  the value as beancount reports any other it cannot read (`Impossible to
  create Decimal instance from NaN: [<class 'decimal.ConversionSyntax'>]`).

- **FIFO, LIFO or STRICT_WITH_SIZE over an undated lot** (#597): a lot
  whose units are interpolated is undated, as in beancount. When a FIFO or
  LIFO reduction, or a STRICT_WITH_SIZE one choosing among lots of its
  size, matches it and another lot, beancount sorts the matches by date and
  crashes comparing `None` with a date (`TypeError`); we sort an undated
  lot before every dated one.

- **BQL `sum()` of booleans**: bean-query accepts `sum(bool)` because
  Python's `bool` subclasses `int`, sums the values as integers and still
  types the column as boolean, so `sum(1 = 1)` over four rows renders
  `TRUE` and `sum(false)` renders `FALSE`. We reject it:
  `error: no function matches "sum(bool)" name and argument types` (#422).

- **BQL ordering of sets**: Python orders sets by inclusion, a partial
  order, so where two sets neither include the other (`{a, c}` and `{b}`)
  the order `sorted()` gives depends on its algorithm (timsort). We sort
  stably with the same comparison, which gives Python's order whenever
  incomparable sets form groups (as an empty set and single tags do,
  `query/order_by_tags.bql`), and often not otherwise: it is common, not
  an edge case, wherever tag sets mix (of 40 random ledgers of ten
  transactions with one or two of four tags, 19 differed under `ORDER BY
  tags` and 17 under `ORDER BY tags DESC`).
  `ORDER BY tags DESC` over ten transactions tagged `#b`, `#a #c`, `#a`,
  `#c`, `#a #b`, `#a`, `#a #d`, `#a #c`, `#a #b` and `#a #c` puts the
  `#b` one sixth there and first here. Exact parity would need a port of
  CPython's timsort.

- **BQL Python exceptions**: where beanquery fails with a Python exception
  rather than a query error, we answer instead. `PIVOT BY` on a query
  without aggregates (`SELECT account, year PIVOT BY account, year`) is a
  `TypeError` there and `the second PIVOT BY column must be a GROUP BY
  column` here. A NULL among other values of the second PIVOT BY column
  (`PIVOT BY year, cost_currency`) is a `TypeError` there; here NULL
  sorts first and names its column `None`. Numberifying (`-m`) a pivoted
  inventory column with a missing cell is an `AttributeError` there; here
  the cell stays empty. Values of types Python cannot order against each
  other in one untyped column (metadata holding `"x"` on one entry and `2`
  on another) are a `TypeError` there, in ORDER BY, `min()`/`max()` and
  PIVOT BY alike; here they order by their string forms. A
  PIVOT BY index of a hidden target that passes beanquery's checks
  (`SELECT account, year, count(*) GROUP BY 1, 2 HAVING count(*) > 0
  PIVOT BY 4, 2`) is an `IndexError` there and `invalid PIVOT BY column
  index 4` here. `coalesce()` without arguments is an `IndexError` there
  and `no function matches "coalesce()"` here. `maxwidth()` narrower than
  its `[...]` placeholder (`maxwidth(str(cost_number), 3)`) is a
  `ValueError` there and prints `[...]` here. `max(position)` over two
  positions with equal units where one has no cost (`10 HOOL {5.00 USD}`
  and `10 HOOL`), or equal cost dates where one has no label, is a
  `TypeError: '>' not supported between instances of 'NoneType' and
  'Cost'` there; here a missing cost or label orders first. PIVOT BY on
  positions with equal sort keys (`1.00 BRL`, `1.00 ARS`, `1.00 COP`)
  orders those pivot columns by Python's set iteration order there, which
  changes with `PYTHONHASHSEED`; here they keep the order the rows first
  show them in. `OPEN ON` with a dateless `CLOSE` (`SELECT date, account
  FROM OPEN ON 2024-01-01 CLOSE`) is a `TypeError: '>' not supported
  between instances of 'datetime.date' and 'bool'` there, as the compiler
  compares the OPEN date with the `True` a dateless CLOSE parses to; each
  clause alone works there, a dateless CLOSE closing at the end of the
  ledger, so here the two run together and print the rows (#582,
  `query/from_open_on_close.bql`, listed in `queryGaps`).

- **BQL errors without a node**: beanquery's shell underlines the node a
  compile error names, but raises some errors without one and then prints
  `error: ` and a Python traceback. We print `error: ` and the message
  alone. `SELECT account ORDER BY 5` is `error: invalid ORDER-BY column
  index 5` for us, and the last line of beanquery's traceback is
  `beanquery.compiler.CompilationError: invalid ORDER-BY column index 5`.
  The same holds for `CLOSE date must follow OPEN date`, a GROUP-BY index
  out of range, a GROUP-BY item that is or refers to an aggregate or has a
  non-hashable type (`balance`), an aggregate query whose GROUP BY misses
  a target, mixed aggregates and non-aggregates, aggregates of aggregates,
  and aggregates in WHERE or FROM. The query parity suite compares only
  the error line for these. For a node that `BALANCES` or `JOURNAL` adds,
  beanquery underlines it in the `SELECT` it rewrites the statement to:
  for `BALANCES AT bogus` it prints the line
  `SELECT account, SUM(bogus(position))` and carets under
  `bogus(position)`. We print the error line alone there too.

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

- **Leading `//` in a ledger path** (#570): beancount keeps a path as the
  OS gives it, so loading `//abs/main.beancount` keeps `//abs/…` in its
  file names and discovered document paths; we clean the path to
  `/abs/…`. A `documents` root reached through a symlink is discovered as
  in beancount, through the symlink (a loader test; no fixture, since a
  symlink in the repository is not portable).

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
- **BQL shell extras**: `EXPLAIN`, `RUN` of stored `query` directives,
  shell settings (`set format ...`) and dot-commands are not implemented;
  nor are beanquery's subqueries, `FROM #table` and `CREATE TABLE`.
- **BQL dict-typed metadata functions**: `commodity_meta`, `currency_meta`,
  `open_meta`, and `getitem` (dict-typed values) are not implemented;
  `meta`, `entry_meta`, and `any_meta` cover scalar metadata lookups.
