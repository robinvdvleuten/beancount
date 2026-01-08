# AGENTS.md

Project-specific conventions for the beancount Go implementation.

## Feature Evaluation: Question First, Plan Second

**CRITICAL**: Before diving into implementation planning, critically evaluate whether a feature is actually needed for this project's use case.

**Ask these questions FIRST**:
1. **What problem does this solve?** Be specific about the actual benefit.
2. **What is the context?** (e.g., localhost-only, production, development tool)
3. **What is the measurable impact?** Quantify the benefit (time saved, bytes reduced, errors prevented).
4. **Is the complexity justified?** Compare implementation cost vs. actual value delivered.

**Examples**:
- **Compression for localhost server**: Saves ~3ms per page load. Not worth the complexity.
- **Position tracking in parser errors**: Shows exact line/column for syntax errors. Worth the complexity—saves hours of debugging.
- **Validation in parser**: Catches errors earlier. Worth the complexity for better error messages.
- **Premature optimization**: "Might be useful later" is not justification. YAGNI applies.

**Process**:
1. User requests feature
2. **Before exploring or planning**, ask: "Is this actually valuable for [specific context]?"
3. If unclear, ask the user about their use case and constraints
4. If not valuable, explain why and suggest alternatives (or skip it entirely)
5. Only proceed with planning if the value is clear and justified

Don't waste time planning solutions to non-problems.

## Essential Commands

**Go**:
```bash
gofmt -w .           # Format (required)
golangci-lint run    # Lint (must pass)
go test ./...        # Test all (must pass)
go test -run TestName ./package  # Run single test
go test -fuzz=FuzzName -fuzztime=30s ./package  # Run fuzz test
make fuzz-promote    # Promote fuzz corpus to testdata
```

**Frontend**:
```bash
npm run --prefix assets dev   # Dev server with hot reload (proxies /api to :8080)
npm run --prefix assets build # Build to web/dist
npm run --prefix assets lint  # oxlint
npm run --prefix assets test  # Playwright
```

## Validation Logic Separation

**CRITICAL**: Parser → Process → Validate. No validation in parser or processing phases.

| Phase | Does | Does NOT |
|-------|------|----------|
| **Parser** | Parse tokens into AST, report syntax errors | Semantic validation, cross-directive checks, business logic |
| **Processing** | Apply directives, compute derived values, build ledger state | Validate correctness, check errors |
| **Validation** | All semantic checks (accounts open, transactions balance, assertions match) | - |

```go
// Parser: syntax only
func (p *Parser) parseTransaction() (*Transaction, error) {
    return &Transaction{Date: p.parseDate(), Flag: p.parseFlag()}, nil
}

// Processor: mutate state only
func (l *Ledger) processTransaction(txn *Transaction) {
    l.accounts[txn.Account].Inventory.Add(txn.Amount)
}

// Validator: all checks
func (v *Validator) validateTransaction(txn *Transaction) []error {
    var errs []error
    if !v.isAccountOpen(txn.Account) {
        errs = append(errs, NewError("account not open", txn))
    }
    return errs
}
```

## Context7 for Library Docs

Use Context7 MCP for **third-party libraries** (shopspring/decimal, alecthomas/kong, mattn/go-runewidth, olivere/vite). Do NOT use for Go stdlib or project code.

```
context7_resolve_library_id(libraryName: "shopspring/decimal")
context7_get_library_docs(context7CompatibleLibraryID: "/shopspring/decimal", topic: "rounding")
```

## Beancount Compliance

Always validate against official tools: `bean-check`, `bean-format`, `bean-doctor`, `bean-query`.

```bash
# Compare formatter output
bean-format input.beancount > /tmp/official.beancount
beancount format input.beancount > /tmp/our.beancount
diff /tmp/official.beancount /tmp/our.beancount

# Validate round-trip
beancount format input.beancount | bean-check /dev/stdin

# Debug lexer issues
beancount doctor lex input.beancount
bean-doctor lex input.beancount
```

## Error Handling

**I/O errors**: Wrap with context (`fmt.Errorf("failed to read %s: %w", filename, err)`)

**Validation errors**: Structured types with `Pos` and `Directive` fields, collected into slices

**Parser errors**: Return directly (already have position info)

## Key Patterns

### Separation of Concerns: Single Responsibility

**Rule**: Data owner computes on its own data. Coordinator calls owner methods, then aggregates/filters. Never duplicate computation logic across boundaries.

```go
// ✓ CORRECT: Owner computes, coordinator delegates
func (a *Account) GetBalanceAsOf(date *ast.Date) map[string]decimal.Decimal {
    balance := make(map[string]decimal.Decimal)
    for _, posting := range a.GetPostingsBefore(date) {
        // Account computes on its data
    }
    return balance
}

func (l *Ledger) GetBalancesAsOf(date *ast.Date) []AccountBalance {
    var result []AccountBalance
    for _, account := range l.Accounts() {
        balance := account.GetBalanceAsOf(date)  // Delegate, don't recompute
        if hasBalance(balance) {
            result = append(result, AccountBalance{...})
        }
    }
    return result
}

// ✗ WRONG: Coordinator reimplements owner's logic
func (l *Ledger) GetBalancesAsOf(date *ast.Date) []AccountBalance {
    for _, account := range l.Accounts() {
        postings := account.GetPostingsBefore(date)  // Coordinator now owns posting logic
        balance := make(map[string]decimal.Decimal)
        for _, posting := range postings {
            // ... duplicates Account's computation ...
        }
    }
    return result
}
```

### Handler Registry Pattern (No Switch Statements)

Directives dispatch via `handlerRegistry` map (DirectiveKind → Handler), not switch statements. Handlers call validation functions directly from `validation.go`:

```go
var handlerRegistry = map[ast.DirectiveKind]Handler{
    ast.KindTransaction: &TransactionHandler{}, // ... 11 total
}

type TransactionHandler struct{}
func (h *TransactionHandler) Validate(ctx context.Context, l *Ledger, d ast.Directive) ([]error, any) {
    cfg := ConfigFromContext(ctx)
    v := newValidator(l.Accounts(), cfg)
    return v.validateTransaction(ctx, d.(*ast.Transaction))
}
func (h *TransactionHandler) Apply(ctx context.Context, l *Ledger, d ast.Directive, delta any) {
    l.applyTransaction(d.(*ast.Transaction), delta.(*TransactionDelta))
}
```

**Key principle**: One registry (handlers), validation functions called directly. Don't create parallel validator registries—it's just indirection without benefit.

### Lexer Token Consumption

All content-bearing tokens (everything except NEWLINE which represents blank lines) consume their trailing newline if present. This ensures clear semantics:

- **COMMENT tokens** include their trailing newline in token bounds (and are stripped in parseComment)
- **Content tokens** (DATE, ACCOUNT, NUMBER, IDENT, etc.) consume their trailing newline  
- **NEWLINE tokens** represent only actual blank lines, never content

This prevents ambiguity: after scanToken() returns, the position is past the newline, and only scanNextToken() can emit NEWLINE tokens for blank lines.

```go
// Lexer: all content tokens consume trailing newline
func (l *Lexer) scanToken() Token {
    tok := l.scanSomeToken()
    // Consume trailing newline - content tokens own their line
    if l.pos < len(l.source) && l.source[l.pos] == '\n' {
        l.advance()
    }
    return tok
}

// Lexer: comments also consume their newline
func (l *Lexer) scanComment() Token {
    // ... scan to end of line ...
    if l.pos < len(l.source) && l.source[l.pos] == '\n' {
        l.advance()
    }
    return Token{COMMENT, start, l.pos, ...}
}

// Parser: strip newline from comment token to keep Content semantic
func (p *Parser) parseComment() *ast.Comment {
    content := tok.String(p.source)
    content = strings.TrimSuffix(content, "\n")  // Lexer includes it, we don't store it
    return &ast.Comment{Content: content, ...}
}
```

**Why this matters**: Without consistent ownership, NEWLINE tokens become ambiguous (blank line or line terminator?), causing idempotency issues in formatters when handling consecutive blank lines and comments.

### State as Receiver

For validators/processors, store state on struct and use methods instead of passing state through parameters:

```go
type processor struct {
    config   *Config
    accounts map[string]*Account
}

func (p *processor) processTransaction(ctx context.Context, txn *Transaction) error {
    return p.validateAndApply(ctx, txn)  // Access p.config, p.accounts via receiver
}
```

### Constructor Pattern

Let constructors extract fields from passed structs:

```go
// Prefer: func NewErrorFromBalance(b *parser.Balance) *Error
// Avoid:  func NewError(d Directive, account Account, date *Date, pos Position) *Error
```

### Context and Telemetry

Public functions doing I/O or processing take `context.Context` first. Use telemetry package for timing:

```go
func Load(ctx context.Context, filename string) (*AST, error) {
    timer := telemetry.FromContext(ctx).Start("loader.load " + filename)
    defer timer.End()
    // Check ctx.Done() in long loops
}
```

Telemetry naming: `package.operation` or `package.operation <context>` (e.g., `parser.lexing`, `loader.parse main.beancount`)

## Package Conventions

| Package | Key Rules |
|---------|-----------|
| **ast** | All AST node types, `Directive` interface. Use functional options for builders. |
| **parser** | Parsing only, returns `*ast.AST`. No type definitions. |
| **formatter** | Use `runewidth.StringWidth()` for display width. Preserve comments/blanks. |
| **ledger** | `decimal.Decimal` for amounts. Validation errors have `Pos`/`Directive` fields. |
| **loader** | Recursive includes with deduplication by absolute path. |
| **web** | **Local dev only**. Bind to localhost. No auth. Path traversal protection. |
| **errors** | Formatting infrastructure. `TextFormatter` for CLI, `JSONFormatter` for APIs. |

## Frontend (assets/)

**Structure**: Vite + Solidjs + TypeScript. Built into `web/dist/`, embedded in Go binary.

**Dependency Management**: Use `npm install --prefix assets <package>` and `npm uninstall --prefix assets <package>`. NEVER manually edit `package.json`.

**CodeMirror**: Minimal setup only. Import only what you use: `@codemirror/{state,view,commands,language,lint,autocomplete}`. Drop `@uiw/react-codemirror`, `@uiw/codemirror-themes`—use `EditorView.theme()` directly. Only `indentWithTab` for keybindings. Result: ~75KB gzipped (vs 400KB+ with basicSetup). Wrappers defeat tree-shaking.

| Directory | Purpose |
|-----------|---------|
| `src/codemirror/` | CodeMirror setup (language, theme, linting, autocomplete) |
| `src/components/` | Solidjs components (editor, application) |
| `index.html` | Entry point with metadata template (replaced by Go at runtime) |

**Styling**: Tailwind CSS 4 + DaisyUI for component presets. CSS variables for (CodeMirror) theming (prefixed `--color-`).

**Metadata**: Injected at build time by `web.go` (version, commitSHA, readOnly). Dev server injects dummy values via Vite plugin.

## Testing

**Go**: Use `github.com/alecthomas/assert/v2` for all assertions. Fuzz tests must `defer recover()`.

```bash
go test ./...                                   # Test all (must pass)
go test -run TestName ./package                 # Run single test
go test -fuzz=FuzzName -fuzztime=30s ./package  # Run fuzz test
```

**Frontend**: Playwright for e2e tests (from `assets/`).

```bash
npm run test                  # Run all tests
npx playwright show-report    # View last test report
```

Tests in `assets/tests/`. Config in `playwright.config.ts`.

## Performance

- Use `strings.Builder` (not `+=` concatenation)
- Use `sync.Pool` for frequently allocated maps
- Pre-allocate with capacity hints when size is known
