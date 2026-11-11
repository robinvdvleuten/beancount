# AGENTS.md

Conventions for the Go implementation of Beancount. The yardstick is parity with the official beancount v3 tools (3.2.3) and, for BQL, beanquery 0.2.0, v3's `bean-query`. #562's BQL sub-issues are done; the rest of beanquery's behaviours that differ are in KNOWN_GAPS.md. The deliberate deviations are listed in `queryGaps`: signed zero (#408), `OPEN ON` with a dateless `CLOSE` (#582), RE2 for Python's regular expressions and 64-bit integers that fail on overflow (#589), `GROUP BY` a frozenset failing as it compiles (#636), and opens and closes ordering and grouping by their printed form (#641).

Update this file, `CODING_STANDARDS.md` or the package's doc in `docs/packages/` in the same change whenever you add a package, change the phase pipeline, introduce a convention, or add a compliance suite. A stale convention misleads more than a missing one.

## YAGNI: question first, plan second

Before exploring or planning a feature, establish its value in this project's context (a local dev tool, not a production service): what problem it solves, its measurable impact, and whether that justifies the complexity. When the value is unclear, ask the user; when it is low, say so and propose an alternative or drop it. "Might be useful later" is no justification.

- Compression for the localhost server saves ~3ms per page load: not worth it.
- Line/column positions in parser errors save hours of debugging: worth it.

## Done when

- `gofmt -l .` prints nothing (fix with `gofmt -w <changed-files>`)
- `golangci-lint run` passes
- `deadcode -tags=dev,difffuzz -test ./... ./importer/testdata/fixture ./_examples/csv_importer` prints nothing: golangci-lint's `unused` skips exported names, so this catches an exported function nothing calls. `mise install` pins it and golangci-lint at CI's versions (`mise.toml`)
- `go test ./...` and `go test -tags=dev ./...` pass. The former embeds the frontend, so build it first in a fresh clone (`npm ci --prefix assets && npm run --prefix assets build`), or `web`'s tests panic on the missing `web/dist/index.html`. CI runs only the latter, whose `web` serves no frontend; a test of the embedded frontend goes in a `//go:build !dev` file
- A Beancount semantics or query change carries a compliance fixture (see [Beancount compliance](#beancount-compliance))
- A frontend change passes `npm run --prefix assets lint` and `npm run --prefix assets test`

Fuzz with `go test -fuzz=FuzzName -fuzztime=30s ./package`; `make fuzz-promote` copies the `FuzzParser` corpus into `parser/testdata`. Assertions use `github.com/alecthomas/assert/v2`; fuzz targets `defer recover()`.

Before and after performance work, run `go test ./cli -run '^$' -bench CheckScaling`: it checks ledgers of 1k, 4k and 16k transactions, and a scenario whose MB/s falls as the ledger grows has gone quadratic. Micro-benchmarks on a few directives cannot show that.

Library docs: Context7 for third-party dependencies (shopspring/decimal, alecthomas/kong, mattn/go-runewidth); read the source for stdlib and project code.

## Parse → Book → Plugins → Validate → Apply

Each phase owns one job and trusts the one before it. Booking comes before validation because beancount books before Plugins and checks run ([ADR 0003](docs/adr/0003-booking-before-plugins.md)).

| Phase | Does | Leaves to another phase |
|-------|------|-------------------------|
| **Parser** | Parse tokens into AST, report syntax errors and the errors beancount's parser reports on a line under the options read so far (an account root none of the account names in effect, a deprecated pipe, a tag after a posting, an invalid booking method, a posting's cost spec and price errors), keeping their directive | Cross-directive checks, business logic |
| **Booking** | Interpolate missing numbers, match reductions to lots, write booked postings onto the AST, drop transactions that cannot be booked (`ledger/booking.go`, own inventory per account) | Account open/close checks, balance errors, mutating ledger state |
| **Plugins** | Built-in Plugins rewrite the booked directives, in `plugin` directive order, dispatched through `pluginRegistry` (`ledger/plugins.go`) | Validation, mutating ledger state, booking (a transaction a Plugin adds is reported as an `UnbookedTransactionError` and not applied) |
| **Validation** | All semantic checks on booked directives; compute mutation deltas | Booking, mutating ledger state |
| **Apply** | Apply the delta Validate returned, compute derived state | Checking correctness, re-planning bookings |

Between Plugins and Validation, `planPads` decides every padding from the booked ledger, as beancount's pad plugin runs before its balance check. Validate returns its errors and, independently, a delta or nil; Apply runs whenever there is a delta, errors or not. Like beancount, a directive is reported and still applied unless it cannot be booked, so later directives see it instead of reporting follow-on errors. Booking works one Currency group at a time: a group that cannot be booked is a Dropped group, whose postings leave the transaction while its other groups stay, and a transaction whose postings cannot be sorted into groups is a Dropped transaction, removed from the processed tree. Like beancount's, Booking's errors carry the transaction as written, so their context still lists a Dropped group's postings. See `docs/adr/0002-apply-directives-that-fail-validation.md` and `docs/adr/0004-drop-failed-currency-groups.md`.

## Beancount compliance

Validate every change to semantics, parsing, lexing, formatting, or queries against the matching official tool: `bean-check`, `bean-format`, `bean-doctor`, `bean-query`. Internal refactors and infrastructure changes skip this. CI's test jobs with `compliance-check` set in the matrix (the Linux ones) install beancount 3.2.3 with `pipx` and inject beanquery 0.2.0 into it, so the suites below run there too. Each suite fails on a tool of the wrong version: `bean-check`, `bean-format` and `bean-doctor` must be beancount 3.x, `bean-query` beanquery 0.2. The tools run on Python 3.12, as what beanquery prints follows its Python (#709): 3.12 rejects group references in a `subst()` replacement that 3.9 reads, and 3.14 rewords errors the `err_` fixtures pin; `query/template.go` follows 3.12. Locally, do as CI does: `pipx install --python python3.12 beancount==3.2.3 && pipx inject beancount beanquery==0.2.0 --include-apps`.

**Parity is of intent**: match what the official tool decides, not every byte of how it says or reaches it. A divergence, from a fixture, a probe or the fuzzer, is one of three things:

- **A decision that differs** is a bug to fix: whether a ledger has an error and on which line, whether a directive is kept, applied or dropped, which numbers, costs and lots are booked, and what `format` and `query` print. These are held exactly (lines and exit status for `check`, bytes for `format` and `query`).
- **Wording** follows beancount's sentence and stops at Python's artifacts. A `check` message may say what beancount says (`Could not resolve units currency`), since a user searching for the text finds beancount's documentation. It never carries what only Python's runtime puts there: an exception's name or text (`ValueError: …`), a traceback, the repr of an object (`KeyValue(key=…)`, `Decimal('1')`, `<class '…MISSING'>`), or a Decimal's exponent (`-10.0000000000000000000000000 USD` for `-10 USD`). Quote an amount as it is written, and a computed number without trailing zeros. A message that already differs is not a bug: no suite compares wording, and no ticket is filed for it. The two exceptions are an option's message (`config`) and a `query` error, which is beanquery's own text.
- **An accident of beancount's implementation** is recorded, not reproduced: a crash with a Python exception, a result that depends on Python's hash order, or a lexer quirk that rejects a ledger no user would call wrong (#704). Answer sensibly, and list it under "Deliberate deviations" in `KNOWN_GAPS.md` with the issue that decided it.

A ticket quotes the official tool's output as evidence of the decision, never as the text to produce. When it is unclear which of the three a divergence is, ask.

**Ledger semantics**: `cli/compliance_test.go` runs every `testdata/compliance/<name>.pass.beancount` / `.fail.beancount` through both implementations whenever `bean-check` is on PATH (`go test ./cli -run 'Compliance|Official'`) and, for `.fail` fixtures, compares the lines errors are reported on (`path:line:`, ignoring our column) with `bean-check --json`'s, in `check`'s text and `--json` output alike; a `.pass` fixture's `--json` output must match byte for byte. Most messages are worded apart, so `--json` is held to filenames and lines, not bytes. A fixture whose lines differ on purpose or through an open issue goes in `lineGaps` with the reason. A `gap_` fixture is one we do not satisfy yet: the in-process suite skips it, and the differential still checks its expectation against `bean-check`. Record known divergences in `testdata/compliance/KNOWN_GAPS.md`. An `applied_` fixture holds one directive that is reported but still applied; `TestNoFollowOnErrors` checks that both implementations report exactly that one error, which exit codes cannot show.

**Queries**: `cli/query_compliance_test.go` runs every `testdata/compliance/query/*.bql` through both implementations in text and csv (`go test ./cli -run 'QueryFixtures|OfficialQueryParity'`). It compares stdout byte for byte and the exit status. When beanquery fails a statement, it also compares stderr. One-shot `bean-query` prints a Python traceback there, so the suite takes the text of beanquery's interactive shell from a Python script run on bean-query's interpreter (`beanqueryShellError`). The `query` row below gives the error format. Name prefixes: `err_` expects such an error from us, `numberify_` adds `-m`. A fixture that differs goes in `queryGaps` with the issue that closes it or, for a deliberate deviation, the issue that decided it and why. `query/run_test.go` pins every `err_` fixture's `Error.Report` and runs it through `Run`; a new `err_` fixture adds its report there, probed with beanquery's shell.

**Format**: `TestOfficialFormatParity` in `cli/compliance_test.go` formats every `testdata/compliance/format/*.beancount` and compares the output **byte-for-byte** with `bean-format`'s (`go test ./cli -run FormatParity`). A fixture that differs through an open issue goes in `formatGaps` with the reason and the issue.

**Doctor**: `cli/doctor_test.go` runs every `testdata/compliance/missing_open/*.beancount` through `beancount doctor missing_open` and, whenever `bean-doctor` is on PATH, compares stdout **byte-for-byte** with `bean-doctor missing_open` (`go test ./cli -run MissingOpen`). An include target goes in a subdirectory, so it is not a fixture itself. A fixture that differs through an open issue goes in `missingOpenGaps` with the reason and the issue.

**Differential fuzzer**: `TestDiffFuzz` in `cli/difffuzz_test.go`, behind the `difffuzz` build tag and so in no other suite or CI job, finds divergences no fixture covers (`go test -tags=difffuzz ./cli -run DiffFuzz -difffuzz.time=5m -timeout=0`; `.golangci.yml` sets the tag, so lint is what compiles it in CI). It runs every compliance fixture and fuzz corpus entry, then mutations of them, through our `check`, `format` and `query` and through the official tools, held to what the suites above compare: the lines errors are on and whether check fails, format's bytes on a ledger ours parses, and what a few fixed statements print and whether they fail (`fuzzQueries`). One Python process stands for the official tools (`cli/testdata/difffuzz_oracle.py`, on bean-query's interpreter); a ledger they raise on is skipped. It minimizes a diverging ledger by lines, confirms it with the tool itself, and writes it with both outputs to `.difffuzz/` (git-ignored), once per signature: the differing output lines without names and numbers. A mutation is reported only for a command its seed agrees on. `-difffuzz.seeds=<regexp>` starts it only from the seeds whose path matches, to fuzz one area (`booking|lot|cost|reduc` for Booking; `query/fuzz_booking_*` are seeds written for it). With `-difffuzz.seed` and `-difffuzz.ledgers=N` in place of `-difffuzz.time`, a run makes the same ledgers every time, so the frozen oracle replays it without the official tools: a ledger it has no answer for is skipped, and a divergence is written unminimized, on the oracle's answer. A divergence that is deliberate or has an issue goes in `knownDivergences` with the reason, and is then counted, not written (two deliberate ones no input pattern tells are read out of the official output: a zero's sign, #408, and a syntax error at a line's end, which bean-check blames on the next line); each other one is triaged as above (a decision that differs becomes an issue and, once fixed, a fixture; wording never does).

**Frozen oracle**: every official answer the suites above and the fuzzer compare with goes through one seam, `frozenOracle` in `cli/oracle_test.go`, which records it the first time and reads it back from then on, so a warm parity run takes seconds, not minutes, and runs without the official tools on PATH. The cache is in the user's cache directory (`beancount/oracle`; `BEANCOUNT_ORACLE_CACHE` moves it), shared by every checkout and never committed, and CI starts it cold. An entry is keyed by the toolchain (the tools' versions and bean-query's Python, #709), the call, and the content of every file its ledgers read: what they include (read by pattern, never with the parser under test; a glob counts every file under the directory it starts in) and the names under their documents directories. A path in an answer is stored relative to the repository, or the fuzzer's temporary directory, so an answer holds in every checkout; bean-query's, whose columns are padded to the paths it prints, holds in every checkout whose path is as long. A miss runs the tool when it is on PATH; without it, that comparison is skipped, as with no tools at all, and never passes. A new call to an official tool goes through `oracleCall` and `execOfficial`, with every file it reads among its inputs.

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

## Coding standards

`CODING_STANDARDS.md` holds the patterns code here follows (registry dispatch, owner computes, state as receiver, context and telemetry, errors, performance); read it before writing code. Review applies it.

## Package conventions

Before changing a package, read its doc, `docs/packages/<package path>.md`: what it owns, the beancount behaviour it mirrors, and its rules. Update the doc in the same change.

- [`ast`](docs/packages/ast.md): AST node types and the `Directive` interface.
- [`parser`](docs/packages/parser.md): Tokens to `*ast.AST`, with beancount's lexer rules, grammar errors and recovery.
- [`formatter`](docs/packages/formatter.md): `format`, held byte for byte to bean-format's line pattern.
- [`printer`](docs/packages/printer.md): Renders directives as beancount's `printer.py` does.
- [`ledger`](docs/packages/ledger.md): Booking, Plugins, Validation and Apply; valuation, balance trees and pads.
- [`loader`](docs/packages/loader.md): Includes, globs, and the options of included files.
- [`ledgerload`](docs/packages/ledgerload.md): The one way to load and process a ledger.
- [`cli`](docs/packages/cli.md): The commands, and how each prints what `ledgerload` returns.
- [`config`](docs/packages/config.md): Option parsing into typed configuration.
- [`query/bql`](docs/packages/query/bql.md): BQL lexer and parser, syntax only.
- [`query`](docs/packages/query.md): `Run`: compiles, executes and renders BQL as beanquery does.
- [`importer`](docs/packages/importer.md): The Importer protocol over go-plugin.
- [`internal/pydecimal`](docs/packages/internal/pydecimal.md): Python `decimal` arithmetic beancount depends on.
- [`internal/pyrepr`](docs/packages/internal/pyrepr.md): Python `repr()` for values bean-query quotes.
- [`diagnostic`](docs/packages/diagnostic.md): `Positioned`, the shape of an error on a line, and severities.
- [`web`](docs/packages/web.md): The local dev server and its snapshot.

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

**Ledger errors**: `assets/src/lib/errors.ts` holds one store of the ledger's errors (from `GET /api/source`) for every page: the sidebar's "Errors (N)" and the `/errors` page read it, and it refetches after a save and on each live reload (`hooks/useReloadEvents.ts`, the server's reload events, which `useFileChange` reads too). A link to a line is the editor's `?file=` (the root or one of its includes) and `?line=`.

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
