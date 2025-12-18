# AGENTS.md

Project-specific conventions for the beancount Go implementation.

## Essential Commands

```bash
gofmt -w .           # Format (required)
golangci-lint run    # Lint (must pass)
go test ./...        # Test all (must pass)
go test -run TestName ./package  # Run single test
go test -fuzz=FuzzName -fuzztime=30s ./package  # Run fuzz test
make fuzz-promote    # Promote fuzz corpus to testdata
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
```

## Error Handling

**I/O errors**: Wrap with context (`fmt.Errorf("failed to read %s: %w", filename, err)`)

**Validation errors**: Structured types with `Pos` and `Directive` fields, collected into slices

**Parser errors**: Return directly (already have position info)

## Key Patterns

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

## Testing

Use `github.com/alecthomas/assert/v2` for all assertions. Fuzz tests must `defer recover()`.

```bash
go test -fuzz=FuzzParser -fuzztime=30s ./parser
```

## Performance

- Use `strings.Builder` (not `+=` concatenation)
- Use `sync.Pool` for frequently allocated maps
- Pre-allocate with capacity hints when size is known
