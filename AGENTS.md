# AGENTS.md

Conventions for the Go implementation of Beancount. The yardstick is parity with the official beancount v3 tools (3.2.3) and, for BQL, beanquery 0.2.0, v3's `bean-query`. #562's BQL sub-issues are done; the rest of beanquery's behaviours that differ are in KNOWN_GAPS.md. The deliberate deviations are listed in `queryGaps`: signed zero (#408), `OPEN ON` with a dateless `CLOSE` (#582), and RE2 for Python's regular expressions and 64-bit integers that fail on overflow (#589).

Update this file in the same change whenever you add a package, change the phase pipeline, introduce a convention, or add a compliance suite. A stale convention misleads more than a missing one.

## YAGNI: question first, plan second

Before exploring or planning a feature, establish its value in this project's context (a local dev tool, not a production service): what problem it solves, its measurable impact, and whether that justifies the complexity. When the value is unclear, ask the user; when it is low, say so and propose an alternative or drop it. "Might be useful later" is no justification.

- Compression for the localhost server saves ~3ms per page load: not worth it.
- Line/column positions in parser errors save hours of debugging: worth it.

## Done when

- `gofmt -l .` prints nothing (fix with `gofmt -w <changed-files>`)
- `golangci-lint run` passes
- `go test ./...` and `go test -tags=dev ./...` pass (CI runs the latter, whose `web` serves no frontend; a test of the embedded frontend goes in a `//go:build !dev` file)
- A Beancount semantics or query change carries a compliance fixture (see [Beancount compliance](#beancount-compliance))
- A frontend change passes `npm run --prefix assets lint` and `npm run --prefix assets test`

Fuzz with `go test -fuzz=FuzzName -fuzztime=30s ./package`; `make fuzz-promote` copies the `FuzzParser` corpus into `parser/testdata`. Assertions use `github.com/alecthomas/assert/v2`; fuzz targets `defer recover()`.

Before and after performance work, run `go test ./cli -run '^$' -bench CheckScaling`: it checks ledgers of 1k, 4k and 16k transactions, and a scenario whose MB/s falls as the ledger grows has gone quadratic. Micro-benchmarks on a few directives cannot show that.

Library docs: Context7 for third-party dependencies (shopspring/decimal, alecthomas/kong, mattn/go-runewidth); read the source for stdlib and project code.

## Parse → Book → Plugins → Validate → Apply

Each phase owns one job and trusts the one before it. Booking comes before validation because beancount books before Plugins and checks run ([ADR 0003](docs/adr/0003-booking-before-plugins.md)).

| Phase | Does | Leaves to another phase |
|-------|------|-------------------------|
| **Parser** | Parse tokens into AST, report syntax errors | Semantic validation, cross-directive checks, business logic |
| **Booking** | Interpolate missing numbers, match reductions to lots, write booked postings onto the AST, drop transactions that cannot be booked (`ledger/booking.go`, own inventory per account) | Account open/close checks, balance errors, mutating ledger state |
| **Plugins** | Built-in Plugins rewrite the booked directives, in `plugin` directive order, dispatched through `pluginRegistry` (`ledger/plugins.go`) | Validation, mutating ledger state, booking (a transaction a Plugin adds is reported as an `UnbookedTransactionError` and not applied) |
| **Validation** | All semantic checks on booked directives; compute mutation deltas | Booking, mutating ledger state |
| **Apply** | Apply the delta Validate returned, compute derived state | Checking correctness, re-planning bookings |

Validate returns its errors and, independently, a delta or nil; Apply runs whenever there is a delta, errors or not. Like beancount, a directive is reported and still applied unless it cannot be booked, so later directives see it instead of reporting follow-on errors. Booking works one Currency group at a time: a group that cannot be booked is a Dropped group, whose postings leave the transaction while its other groups stay, and a transaction whose postings cannot be sorted into groups is a Dropped transaction, removed from the processed AST. Like beancount's, Booking's errors carry the transaction as written, so their context still lists a Dropped group's postings. See `docs/adr/0002-apply-directives-that-fail-validation.md` and `docs/adr/0004-drop-failed-currency-groups.md`.

## Beancount compliance

Validate every change to semantics, parsing, lexing, formatting, or queries against the matching official tool: `bean-check`, `bean-format`, `bean-doctor`, `bean-query`. Internal refactors and infrastructure changes skip this. CI's test jobs with `compliance-check` set in the matrix (the Linux ones) install beancount 3.2.3 with `pipx` and inject beanquery 0.2.0 into it, so the suites below run there too. Each suite fails on a tool of the wrong version: `bean-check`, `bean-format` and `bean-doctor` must be beancount 3.x, `bean-query` beanquery 0.2. Locally, do as CI does: `pipx install beancount==3.2.3 && pipx inject beancount beanquery==0.2.0 --include-apps`.

**Ledger semantics**: `cli/compliance_test.go` runs every `testdata/compliance/<name>.pass.beancount` / `.fail.beancount` through both implementations whenever `bean-check` is on PATH (`go test ./cli -run 'Compliance|Official'`) and, for `.fail` fixtures, compares the lines errors are reported on (`path:line:`, ignoring our column) with `bean-check --json`'s, in `check`'s text and `--json` output alike; a `.pass` fixture's `--json` output must match byte for byte. Most messages are worded apart, so `--json` is held to filenames and lines, not bytes. A fixture whose lines differ on purpose or through an open issue goes in `lineGaps` with the reason. A `gap_` fixture is one we do not satisfy yet: the in-process suite skips it, and the differential still checks its expectation against `bean-check`. Record known divergences in `testdata/compliance/KNOWN_GAPS.md`. An `applied_` fixture holds one directive that is reported but still applied; `TestNoFollowOnErrors` checks that both implementations report exactly that one error, which exit codes cannot show.

**Queries**: `cli/query_compliance_test.go` runs every `testdata/compliance/query/*.bql` through both implementations in text and csv (`go test ./cli -run 'QueryFixtures|OfficialQueryParity'`). It compares stdout byte for byte and the exit status. When beanquery fails a statement, it also compares stderr. One-shot `bean-query` prints a Python traceback there, so the suite takes the text of beanquery's interactive shell from a Python script run on bean-query's interpreter (`beanqueryShellError`). The `query` row below gives the error format. Name prefixes: `err_` expects such an error from us, `numberify_` adds `-m`. A fixture that differs goes in `queryGaps` with the issue that closes it or, for a deliberate deviation, the issue that decided it and why. `query/run_test.go` pins every `err_` fixture's `Error.Report` and runs it through `Run`; a new `err_` fixture adds its report there, probed with beanquery's shell.

**Format**: `TestOfficialFormatParity` in `cli/compliance_test.go` formats every `testdata/compliance/format/*.beancount` and compares the output **byte-for-byte** with `bean-format`'s (`go test ./cli -run FormatParity`). A fixture that differs through an open issue goes in `formatGaps` with the reason and the issue.

**Doctor**: `cli/doctor_test.go` runs every `testdata/compliance/missing_open/*.beancount` through `beancount doctor missing_open` and, whenever `bean-doctor` is on PATH, compares stdout **byte-for-byte** with `bean-doctor missing_open` (`go test ./cli -run MissingOpen`). An include target goes in a subdirectory, so it is not a fixture itself. A fixture that differs through an open issue goes in `missingOpenGaps` with the reason and the issue.

**Gap maps stay live**: `lineGaps`, `formatGaps`, `queryGaps` and `missingOpenGaps` skip only the comparison with the official tool. An entry whose fixture agrees again, or that names no fixture, fails its suite, so closing a gap removes its entry.

**Pin first, implement second**: bean-query is full of undocumented quirks (column names from the source text like `SUM((position))`, centred and truncated headers, padded number cells in CSV, a sign column in every amount, per-currency precision, implicit GROUP BY). Probe the official tool with a small ledger and match the observed bytes:

```bash
bean-query testdata/compliance/query/ledger.beancount "select account, sum(position) group by account"
bean-query -f csv ledger.beancount "select 1 + 2"   # csv shows untruncated headers
bean-query ledger.beancount "help targets"           # official column/function reference
diff <(go run ./cmd/beancount query f.beancount "$Q") <(bean-query f.beancount "$Q")

bean-doctor lex f.beancount; beancount doctor lex f.beancount  # token models differ; compare by eye
diff <(bean-format f.beancount) <(beancount format f.beancount)
beancount format f.beancount | bean-check /dev/stdin  # round-trip
```

## Key patterns

**Registry dispatch**: dispatch by kind through registry maps, never switch statements. Ledger directives go through `handlerRegistry` in `ledger/handlers.go` (`DirectiveKind` → `Handler` with `Validate`/`Apply`); handlers call the functions in `validation.go` directly, so there is exactly one registry. Build validators with `newValidator(l.accounts, l.opened, l.config)`, the stable read-only map and each account's open wherever the ledger dates it, rather than a copy from `Accounts()`. Query columns, functions, aggregates, operators and the attributes of structured types follow the same rule (`query/env.go`, `functions.go`, `aggregates.go`, `operators.go`, `attributes.go`).

**Owner computes**: the type that owns the data computes on it; coordinators call owner methods and aggregate. The booker calls `Inventory.book` and `Inventory.augment` instead of matching lots itself, and the query package's `cost()`, `value()` and `convert()` call `Position.AtCost`, `Ledger.MarketValue` and `Ledger.Convert` instead of looking up prices.

**Lexer newline ownership**: every content-bearing token (including COMMENT) consumes its trailing newline; NEWLINE tokens stand only for blank lines, emitted solely by `scanNextToken()`. `parseComment` strips the newline from the comment's content. Consistent ownership keeps the formatter idempotent around consecutive blank lines and comments.

**State as receiver**: validators and processors keep config and lookups on the struct and read them through the receiver.

**Constructors take the directive**: `NewAccountNotClosedError(close *ast.Close)` extracts date, account and position itself.

**Context and telemetry**: public functions doing I/O or processing take `context.Context` first, check `ctx.Done()` in long loops, and time work with `telemetry.FromContext(ctx).Start("package.operation <context>")` (e.g. `parser.lexing`, `loader.parse main.beancount`).

**Errors**: wrap I/O errors with context (`fmt.Errorf("failed to read %s: %w", filename, err)`); return parser errors as-is (they carry positions); collect ledger errors into slices as `*ledger.Diagnostic` values (kind, message, directive, position, account), built by a `New…Error` constructor per kind; add a Go type only where a caller matches on it with `errors.As` (`BalanceMismatchError`). The CLI renders any error with `GetDirective()` with the directive under it, a transaction's postings as booked like bean-check's (`cli/errors.go`), and any other positioned error with the source lines of the file its position names, looked up in `LoadResult.Sources`, or with none when it does not have that file; the web API marshals the shared shape.

**Performance**: `strings.Builder` for concatenation, `sync.Pool` for frequently allocated maps, capacity hints when the size is known.

## Package conventions

| Package | Key rules |
|---------|-----------|
| **ast** | All AST node types, `Directive` interface. Builders use functional options. `Document.ResolvedPath` is the one place a document's path is resolved: the validator's message and the printer both read it. |
| **parser** | Parsing only, returns `*ast.AST`. Types live in `ast`. Like beancount, a syntax error drops the directive it is in and parsing resumes at the next line in column 1: `Parse` returns the partial AST with a `ParseErrors` listing each one. A dated directive's header, a posting and an undated line (`option`, `plugin`, `include`, …) end at their line (`finishHeader` checks the token lines, since tokens own their newline): one continued on the next line, or metadata on its own line, is a syntax error, since beancount has no inline metadata. A string spanning lines moves that end to the line it closes on, where a comment is the inline comment. |
| **formatter** | Renders the AST, held to bean-format's line pattern (`beancount/scripts/format.py`): it aligns only a plainly spelled number, or two joined by one operator in parentheses (`(1 + 2)`), followed by a currency, and copies every line bean-format leaves alone (headers, comments, blank and metadata lines, unaligned postings) from the source. It always formats a source, with no mode for rendering directives without one (that is the printer's job); a `Formatter` holds options only, and a Format call's state lives on its `run`. Two modules make the decisions, so a per-kind printer holds no source-line heuristics of its own: the `sourceView` (`source.go`) answers with one rule whether an item owns its source line (no other top-level item starts on it and the item starts it; the item runs up to the next node, so a string spanning lines hands it those lines too), and the pure line-pattern functions (`pattern.go`) take a line and its item and return a `lineLayout`: align, with prefix, number, currency and widths; copy; or reconstruct from the AST. `runewidth.StringWidth()` for display width. Limits are in `KNOWN_GAPS.md`. |
| **printer** | Renders directives as beancount 3.2.3's `printer.py` does, byte for byte (`print_*` query fixtures; its known differences are in KNOWN_GAPS.md): numbers as parsed, a transaction's postings aligned on their currency and indented two spaces, printer.py's fixed columns for open, balance and price, a balance's tolerance whenever one is written (`~ 0` included), a note's and a document's tags then links each after a space, a document's path resolved against its file's directory, as beancount holds it, metadata lines except keys starting with `__` (`__implicit_prices__`), no source comments. `Print` follows `print_entries`' blank-line policy; `Sprint` renders one directive (`format_entry`). `WithBookedPositions(Ledger.BookedPositions)` prints a posting as its booked postings, `WithBalanceDiffs` a failed assertion's difference. Query PRINT, `import`, `doctor missing_open` and the CLI's error context print through it. |
| **ledger** | `decimal.Decimal` for amounts. Booking methods: `STRICT` (default), `STRICT_WITH_SIZE` (as `STRICT`, and when that cannot choose among several lots, the oldest lot of the reduction's size), `NONE`, `FIFO`, `LIFO`, `HIFO`, `AVERAGE` (like beancount, every reduction under it fails, and a merge cost `{*}` is reported and booked like `{}`). Like beancount's parser, Booking reports and fixes up a compound cost inside total braces (`{{5 # 3 USD}}`, booked with its per-unit number ignored), books total braces without an amount (`{{}}`, `{{2020-01-01}}`) as the per-unit cost they amount to, a negative price and a total price without units before booking, and reports each component a cost spec repeats (`Cost.Duplicates`; the parser keeps the first of each kind); `Inventory.book` decides augment vs. reduce (beancount's `is_reduced_by`), reports which, and books a reduction at once, in its Currency group's scratch inventory, a copy made only for a reduction (`reducedBy` decides from per-sign lot counts, so a ledger that only adds lots stays linear); `Inventory.augment` adds every other posting, with its cost per unit and dated, once interpolation has completed it and the transaction is booked. The booker only groups, stages, rolls back a Dropped group, interpolates and checks the residual. Booking publishes one record per booked posting of an Applied transaction, `Ledger.BookedPositions`: a `BookedPosition` per lot a reduction was booked against, otherwise one of its own, with signed units, a per-unit `BookedCost` (number at the precision Booking holds it, currency, lot date defaulting to the transaction's unless its units were interpolated, label) and `Reduced` (beancount's `Booking.REDUCED`); a Dropped group's postings have none. Apply replays the record, and Plugins, the query package and the printer read it rather than re-deriving lots or costs from the cost spec. `tolerances` (`ledger/tolerance.go`), built once per `Process` from the tolerance options and the only reader of them, answers every tolerance question: a transaction's tolerance per currency before booking (`spec`, which rounds interpolated numbers: the coarsest its numbers imply, or the finest under `use_precise_interpolation`, with a currency's default counting only when a posting names the currency) and once booked (`booked`, for the residual check), and a balance assertion's (`balance`); the booker holds it, and the Balance handler asks it for an assertion's tolerance before padding (`padBalance`) and checking (`checkBalance`) the assertion. `priceIndex` (`ledger/prices.go`) answers `GetPrice`: a pair's latest rate on or before the date, from its own prices or the inverses of the other direction's, never through a third currency; a zero price has no inverse. The ledger owns Valuation (`ledger/valuation.go`): `Position.AtCost`, `MarketValue`, `Convert` and `ConvertAmount` value a position on a date, and the query package's `cost()`, `value()` and `convert()` call them. `GetBalanceTree` (`ledger/balance_tree.go`) builds from the accounts and the configured account-type names alone, stating each account's balance under a `Valuation` (At cost by default): it sums the `BookedPositions` of the account's postings in the period per lot and values them once, on the end date or else today. With `closed` it returns Closed balances, a balance sheet's as of a date: Equity gains Current earnings (the Income and Expenses positions through the date) and Current conversions (the negated cost balance of every posting) in the accounts `Config.CurrentAccounts()` names, and, At market value or Converted to a currency, `Equity:Earnings:Unrealized`, whatever leaves the valued sheet off zero; a computed amount merges into a real account of its name, and one that comes out empty is left out. Apply leaves booked postings in `txn.Postings`, in beancount's booked order (by Currency group, sorted into groups in `ledger/groups.go` like beancount's `categorize_by_currency`, after which a cost stating its number but not its currency takes its group's, as `replace_currencies` does; an amount-less posting becomes one posting per group with a residual); `BodyItems` keeps the source layout for the formatter. A padding transaction is dated and positioned at its pad and carries its metadata; its postings carry the balance assertion's position and metadata, like beancount's. |
| **loader** | Recursive includes, deduplicated by absolute path; a repeated include is loaded once and reported as a load error (beancount's "Duplicate filename parsed"). Like beancount, only the top-level file's options and `plugin` directives count; an included file's are dropped, after its options are checked (`config.CheckOption`): an invalid one is a load error on the included file's line, a valid one a warning. Every include is expanded like Python's `glob.glob(recursive=True)`: `**` spans directories, wildcards skip dotfiles, and an unmatched glob, including a missing plain path, is a load error. Non-fatal issues go to `LoadResult.Diagnostics`; with `WithSyntaxRecovery` (used by `check`, `query` and `web`) so do syntax errors, while `format` fails on the first one. |
| **config** | Beancount option parsing into typed configuration. Like bean-check, options apply one by one: the last valid scalar value wins, and an unknown name or invalid value is a positioned error that leaves the other options in effect (`ParseOptions`). An option written under a name beancount renamed (`renamedOptions`) is reported as renamed and still applied, under its current name; a deprecated one (`deprecatedOptions`) is reported whatever its value. An option's number is read as beancount's `D()` reads it (`parseNumber`: empty is 0, commas and spaces dropped, then `pydecimal.NewFromString`). An invalid value carries beancount's message where its converter words one; beancount's capitalised messages are a `beancountError`, which the error-string lint allows. |
| **query/bql** | BQL lexer + recursive-descent parser, syntax only, held to the parser's rules: zero-copy tokens, positioned errors, fuzz test. Every node records its `Span`, the byte offsets of its source text, which names an unaliased target (`Target.Text`) as in beanquery. |
| **query** | `Run` is the interface: BQL text, a `Context` (Ledger, Config and the processed AST, which Run requires), a `Format` and numberify in; beanquery's bytes out (nothing for an empty text result, or for a text with no statement, which its shell skips: an empty one, or one opening with `;` or `(`). For a statement that does not parse or compile, or fails while it runs (an invalid regular expression, an integer overflow: `fail` panics with an `evalError` that `Run` recovers), `Run` writes nothing and returns an `*Error` with beanquery's message. `Error.Report` renders it as beanquery's interactive shell prints it: `error: ` and the message, the statement's lines up to the error's node behind `| `, and a caret under each character of the node (its `bql` span). A parse error is `syntax error` with one caret. An error beanquery raises without a node is the `error: ` line alone (KNOWN_GAPS.md). The CLI prints the report on stderr and exits 1; the web API returns it as output. `Run`, `Context`, `Format` and `Error` are the whole exported API; the stages behind it are unexported, and the package's own tests (all `package query`) reach them directly. Inside, parse → compile → execute → render: compiler resolves names and types with beanquery-parity error messages, and type-checks operators against `operators` (`query/operators.go`, beanquery's `OPERATORS`): an operand pair must match a signature exactly, after an untyped (object) operand is cast to the other's type (`casts`, beanquery's `types.MAP`); AND, OR, IN and NOT IN take any types (`untypedOperators`); BETWEEN's three operands must all be numbers, dates or strings (`betweenGroups`); NOT and unary minus look up `unaryOperators`, where a bool also matches an int signature, as Python's bool subclasses int; a typed operator on a NULL operand gives NULL; executor reads `Context.AST` (interpolated amounts) read-only and maps `Ledger.BookedPositions` to its positions for the position, cost and weight columns rather than re-deriving booking (FROM's summarization records the positions of the postings it creates); SELECT (which BALANCES and JOURNAL desugar to) and PRINT share one FROM compile (`compileFrom`) and its OPEN, CLOSE and CLEAR transforms; like beanquery, a SELECT's FROM expression compiles over the postings table and joins WHERE as one filter on posting rows (`EvalAnd([c_from_expr, c_where])`), while PRINT's compiles over entries and keeps directives (`compiledFrom.entries`); PRINT hands what its FROM reads to the printer with `Ledger.BookedPositions`; HAVING compiles to a hidden aggregate target; like beanquery, a grouped query's targets outside the group key (HAVING's, an ORDER BY expression's) read their non-aggregate parts from the last scanned row; PIVOT BY reshapes the executed table after ORDER BY and LIMIT (`table.pivot`, beanquery's `EvalPivot`); ORDER BY, `min()`, `max()` and PIVOT BY order values as Python orders beancount's (`order.go`: `Position.sortkey`, amounts by currency, inventories by sorted positions, sets by inclusion, lists (list constants and `other_accounts`, a set-typed column holding a sorted `listValue`) element by element); the executor evaluates a row's targets where WHERE keeps it, and `balance` is lazy (`evalRow.balanceValue`), as in beanquery; numberify splits amount columns before either format renders; renderers reproduce beanquery's per-type layout byte-for-byte, quantizing amounts with `Ledger.DisplayContext` (source precisions, fixed by `display_precision`). |
| **importer** | Importer protocol over go-plugin ([ADR 0001](docs/adr/0001-importers-use-go-plugin.md)): `Serve` for Importer authors, `Open` for the host, both speaking `ast` values. The host validates every message it decodes. A new optional field stays in protocol v1; a new directive kind needs v2. `testdata/fixture` is a test Importer whose `FIXTURE_MODE` picks how it misbehaves. `internal/pb` holds `importer.proto` and its committed generated code; regenerate with `go generate ./importer/...`, which runs buf from the separate `tools.mod` so codegen dependencies stay out of `go.mod`. CI fails when the generated code drifts. |
| **internal/pydecimal** | Python `decimal` behaviour beancount depends on and shopspring lacks (arithmetic rounded to 28 significant digits, division with exact-quotient exponents, `normalize`, `str()`, reading a string as `Decimal(str)` does). Add, subtract, multiply and divide only with `Add`, `Sub`, `Mul` and `Quo`; `forbidigo` rejects shopspring's `Add`/`Sub`/`Mul`/`Div`/`DivRound` elsewhere. Use it wherever a number must keep beancount's precision. |
| **internal/pyrepr** | Python `repr()` for values bean-query quotes in its messages. |
| **diagnostic** | `SeverityError`/`SeverityWarning` for load and validation errors. Only errors affect exit codes. |
| **web** | Local dev tool: binds to localhost, no auth, guards against path traversal. `/api/balances` takes a `valuation` of `units`, `cost` (the default), `market` or a currency to convert to and has the ledger value the balances; it rounds every amount to its currency's display precision (`DisplayContext.Quantize`). The reports keep the choice in `?valuation=`, which the sidebar's report links carry. |

## Frontend (assets/)

Vite + Solid + TypeScript, styled with Tailwind CSS 4 + DaisyUI, built into `web/dist/` and embedded in the Go binary. `web/assets.go` injects metadata (version, commitSHA, readOnly, watching, and the ledger's title) into `index.html` on each request; the dev server injects dummy values via a Vite plugin. `npm run --prefix assets dev` proxies `/api` to `:8080`.

**Dependencies**: change them only through `npm install --prefix assets <pkg>` / `npm uninstall --prefix assets <pkg>`, so `package.json` and the lockfile stay in sync.

**CodeMirror**: import individual `@codemirror/{state,view,commands,language,lint,autocomplete}` packages, theme with `EditorView.theme()` and `--color-` CSS variables, and bind only `indentWithTab`. This keeps the editor ~75KB gzipped, against 400KB+ for `basicSetup` or wrapper packages that defeat tree-shaking.

**Composition**: build shared UI as small Radix-style primitives. Routes own data fetching, state branching, and page layout, and compose the primitives so intent stays visible:

```tsx
<FinancialReport.Root>
  <FinancialReport.Grid>
    <FinancialReport.Column>
      <FinancialReport.Table section={assets} currencies={currencies} />
    </FinancialReport.Column>
  </FinancialReport.Grid>
</FinancialReport.Root>

const sections = () => FinancialReport.getSections(data()?.roots, ["Assets"])
```

Playwright e2e tests live in `assets/tests/`.

## Agent skills

### Issue tracker

GitHub Issues on `robinvdvleuten/beancount`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` plus `docs/adr/`, created when first needed. See `docs/agents/domain.md`.

### Rejected enhancements

`.out-of-scope/` keeps one file per rejected enhancement, with the reason and the issues that asked for it. Read it before proposing a feature or refactor, and reopen one only when its stated reason no longer holds.
