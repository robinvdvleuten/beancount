# AGENTS.md

Conventions for the Go implementation of Beancount. The yardstick is parity with the official Beancount v2 tools.

Update this file in the same change whenever you add a package, change the phase pipeline, introduce a convention, or add a compliance suite. A stale convention misleads more than a missing one.

## YAGNI: question first, plan second

Before exploring or planning a feature, establish its value in this project's context (a local dev tool, not a production service): what problem it solves, its measurable impact, and whether that justifies the complexity. When the value is unclear, ask the user; when it is low, say so and propose an alternative or drop it. "Might be useful later" is no justification.

- Compression for the localhost server saves ~3ms per page load: not worth it.
- Line/column positions in parser errors save hours of debugging: worth it.

## Done when

- `gofmt -l .` prints nothing (fix with `gofmt -w <changed-files>`)
- `golangci-lint run` passes
- `go test ./...` passes
- A Beancount semantics or query change carries a compliance fixture (see [Beancount compliance](#beancount-compliance))
- A frontend change passes `npm run --prefix assets lint` and `npm run --prefix assets test`

Fuzz with `go test -fuzz=FuzzName -fuzztime=30s ./package`; `make fuzz-promote` copies the `FuzzParser` corpus into `parser/testdata`. Assertions use `github.com/alecthomas/assert/v2`; fuzz targets `defer recover()`.

Library docs: Context7 for third-party dependencies (shopspring/decimal, alecthomas/kong, mattn/go-runewidth); read the source for stdlib and project code.

## Parse → Validate → Apply

Each phase owns one job and trusts the one before it.

| Phase | Does | Leaves to another phase |
|-------|------|-------------------------|
| **Parser** | Parse tokens into AST, report syntax errors | Semantic validation, cross-directive checks, business logic |
| **Validation** | All semantic checks; compute mutation deltas | Mutating ledger state |
| **Apply** | Apply validated deltas, compute derived state | Checking correctness |

## Beancount compliance

Validate every change to semantics, parsing, lexing, formatting, or queries against the matching official tool: `bean-check`, `bean-format`, `bean-doctor`, `bean-query`. Internal refactors and infrastructure changes skip this.

**Ledger semantics**: `cli/compliance_test.go` runs every `testdata/compliance/<name>.pass.beancount` / `.fail.beancount` through both implementations whenever `bean-check` is on PATH (`go test ./cli -run 'Compliance|Official'`). Record known divergences in `testdata/compliance/KNOWN_GAPS.md`.

**Queries**: `cli/query_compliance_test.go` runs every `testdata/compliance/query/*.bql` through both implementations and compares stdout **byte-for-byte** in text and csv (`go test ./cli -run 'QueryFixtures|OfficialQueryParity'`). Name prefixes: `err_` expects an `ERROR:` line, `numberify_` adds `-m`, `gap_` skips the parity leg (record it in KNOWN_GAPS.md).

**Pin first, implement second**: bean-query is full of undocumented quirks (column names like `sum_position`/`c42`, header truncation, padded CSV cells, implicit GROUP BY, per-currency precision). Probe the official tool with a small ledger and match the observed bytes:

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

**Registry dispatch**: dispatch by kind through registry maps, never switch statements. Ledger directives go through `handlerRegistry` in `ledger/handlers.go` (`DirectiveKind` → `Handler` with `Validate`/`Apply`); handlers call the functions in `validation.go` directly, so there is exactly one registry. Build validators with `newValidator(l.accounts, l.config)`, the stable read-only map, rather than a copy from `Accounts()`. Query columns, functions and aggregates follow the same rule (`query/env.go`, `functions.go`, `aggregates.go`).

**Owner computes**: the type that owns the data computes on it; coordinators call owner methods and aggregate. `Ledger.GetBalanceTree` calls `Account.GetBalanceInPeriod` instead of walking postings itself.

**Lexer newline ownership**: every content-bearing token (including COMMENT) consumes its trailing newline; NEWLINE tokens stand only for blank lines, emitted solely by `scanNextToken()`. `parseComment` strips the newline from the comment's content. Consistent ownership keeps the formatter idempotent around consecutive blank lines and comments.

**State as receiver**: validators and processors keep config and lookups on the struct and read them through the receiver.

**Constructors take the directive**: `NewAccountNotClosedError(close *ast.Close)` extracts date, account and position itself.

**Context and telemetry**: public functions doing I/O or processing take `context.Context` first, check `ctx.Done()` in long loops, and time work with `telemetry.FromContext(ctx).Start("package.operation <context>")` (e.g. `parser.lexing`, `loader.parse main.beancount`).

**Errors**: wrap I/O errors with context (`fmt.Errorf("failed to read %s: %w", filename, err)`); return parser errors as-is (they carry positions); collect validation errors into slices as structured types with `Pos` and `Directive`. Formatting for CLI text and API JSON lives in `cli/errors.go`.

**Performance**: `strings.Builder` for concatenation, `sync.Pool` for frequently allocated maps, capacity hints when the size is known.

## Package conventions

| Package | Key rules |
|---------|-----------|
| **ast** | All AST node types, `Directive` interface. Builders use functional options. |
| **parser** | Parsing only, returns `*ast.AST`. Types live in `ast`. |
| **formatter** | `runewidth.StringWidth()` for display width. Preserves comments and blank lines. |
| **ledger** | `decimal.Decimal` for amounts. Booking methods: `STRICT` (default), `NONE`, `FIFO`, `LIFO`, `AVERAGE`. |
| **loader** | Recursive includes, deduplicated by absolute path. Include globs follow Python's `glob.glob(recursive=True)`: `**` spans directories, wildcards skip dotfiles, an unmatched glob is an error. Non-fatal issues go to `LoadResult.Diagnostics`. |
| **config** | Beancount option parsing into typed configuration. Rejects unknown option names (bean-check parity). |
| **query/bql** | BQL lexer + recursive-descent parser, syntax only, held to the parser's rules: zero-copy tokens, positioned errors, fuzz test. |
| **query** | Parse → compile → execute → render. Compiler resolves names and types with bean-query-parity error messages; executor reads the ledger-processed `*ast.AST` (interpolated amounts) read-only; renderers reproduce official output byte-for-byte. |
| **diagnostic** | `SeverityError`/`SeverityWarning` for load and validation errors. Only errors affect exit codes. |
| **web** | Local dev tool: binds to localhost, no auth, guards against path traversal. |

## Frontend (assets/)

Vite + Solid + TypeScript, styled with Tailwind CSS 4 + DaisyUI, built into `web/dist/` and embedded in the Go binary. `web.go` injects metadata (version, commitSHA, readOnly) into `index.html`; the dev server injects dummy values via a Vite plugin. `npm run --prefix assets dev` proxies `/api` to `:8080`.

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
