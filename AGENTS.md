# AGENTS.md

This document outlines the coding standards, conventions, and best practices for the beancount project. Follow these guidelines to maintain consistency across the codebase.

## Essential Commands

### Code Quality Checks
```bash
# Format all Go files (MUST be run before committing)
gofmt -w .

# Run linter (MUST pass before committing)
golangci-lint run

# Run all tests (MUST be green before committing)
go test ./...

# Run tests with coverage
go test -cover ./...

# Run benchmarks
go test -bench=. ./...
```

### Pre-Commit Checklist
- [ ] Run `gofmt -w .` on all modified files
- [ ] Run `golangci-lint run` and fix all issues
- [ ] Run `go test ./...` and ensure all tests pass
- [ ] Update documentation if APIs changed
- [ ] Add tests for new functionality

## Code Formatting

### Standard Formatting
- **Always use `gofmt`** for code formatting
- No exceptions - all code must be gofmt-compliant
- Line length: Aim for 100-120 characters, but readability takes precedence

### Import Organization
Organize imports in **three groups** separated by blank lines:

```go
package example

import (
    // 1. Standard library imports
    "fmt"
    "strings"
    "time"

    // 2. External dependencies (alphabetically)
    "github.com/alecthomas/participle/v2"
    "github.com/shopspring/decimal"
)
```

**Rules:**
- Standard library first
- Blank line separator
- External packages second (alphabetically sorted)
- No third group unless absolutely necessary (e.g., side-effect imports)

## Documentation Standards

### Package-Level Documentation
Every package should have a package comment:

```go
// Package formatter handles formatting of Beancount files with proper alignment.
// It provides tools for auto-aligning currencies, numbers, and accounts while
// preserving comments and blank lines from the original source.
package formatter
```

### Type Documentation
Follow the pattern established in `parser/parser.go`:

```go
// Transaction records a financial transaction with a date, flag, optional payee,
// narration, and a list of postings. The flag indicates transaction status: '*' for
// cleared/complete transactions, '!' for pending/uncleared transactions, or 'P' for
// automatically generated padding transactions. Each transaction must have at least
// two postings, and the sum of all posting amounts must balance to zero (double-entry
// bookkeeping). Tags and links can be used to categorize and connect related transactions.
//
// Example:
//
//	2014-05-05 * "Cafe Mogador" "Lamb tagine with wine"
//	  Liabilities:CreditCard:CapitalOne         -37.45 USD
//	  Expenses:Food:Restaurant
type Transaction struct {
    // fields...
}
```

**Rules:**
- Start with type name
- Explain purpose and behavior
- Include usage examples for complex types
- Use complete sentences with proper punctuation
- Add blank line before examples

### Function/Method Documentation
```go
// FormatTransaction formats a single transaction and writes the output to the writer.
// This method is useful for rendering individual transactions, such as in error messages.
// The currency column is calculated from the transaction itself if not explicitly set.
func (f *Formatter) FormatTransaction(txn *parser.Transaction, w io.Writer) error {
```

**Rules:**
- Start with function name
- Explain what it does (not how)
- Document important parameters and behavior
- Note any side effects or state changes
- Mention error conditions

### Unexported Functions
Document complex private functions too:

```go
// calculateWidthMetrics performs a single pass through the AST to calculate all width metrics.
func (f *Formatter) calculateWidthMetrics(ast *parser.AST) widthMetrics {
```

## String Building

### Use strings.Builder for Performance
**ALWAYS use `strings.Builder`** for building strings, never use string concatenation with `+=`:

```go
// ✅ CORRECT
var buf strings.Builder
buf.Grow(estimatedSize) // Optional but recommended
buf.WriteString("Hello")
buf.WriteString(" ")
buf.WriteString("World")
return buf.String()

// ❌ INCORRECT
result := ""
result += "Hello"
result += " "
result += "World"
return result
```

**Why:** String concatenation creates a new string allocation for each `+=` operation. `strings.Builder` is optimized for this use case and significantly more efficient.

### When to Use bytes.Buffer
Use `bytes.Buffer` only when:
- Working with binary data
- Need the `io.Writer` interface AND doing byte operations
- Otherwise, prefer `strings.Builder`

## Error Handling

### Custom Error Types
Use custom error types for well-defined error conditions:

```go
// AccountNotOpenError is returned when a directive references an account that hasn't been opened
type AccountNotOpenError struct {
    Account   parser.Account
    Date      *parser.Date
    Pos       lexer.Position
    Directive parser.Directive
}

func (e *AccountNotOpenError) Error() string {
    location := fmt.Sprintf("%s:%d", e.Pos.Filename, e.Pos.Line)
    return fmt.Sprintf("%s: Invalid reference to unknown account '%s'", location, e.Account)
}
```

**When to use:**
- Error needs to carry structured data
- Error has special formatting requirements
- Error needs type-checking with `errors.As()`

### Wrapped Errors
Use `fmt.Errorf` with `%w` for adding context:

```go
data, err := os.ReadFile(filename)
if err != nil {
    return nil, fmt.Errorf("failed to read %s: %w", filename, err)
}
```

**When to use:**
- Adding context to existing errors
- Error chain needs to be preserved
- Simple error propagation

### Error Messages
- Start with lowercase (unless proper noun or error type name)
- Be specific about what failed
- Include relevant context (filenames, accounts, etc.)
- Don't use punctuation at the end

```go
// ✅ CORRECT
fmt.Errorf("failed to parse amount %q: %w", value, err)

// ❌ INCORRECT
fmt.Errorf("Error parsing.") // Too vague, capitalized, has period
```

## Variable Declarations

### Use := for Local Variables
Prefer short declaration for local variables:

```go
// ✅ CORRECT
accounts := make(map[string]*Account)
result := calculateTotal()

// ❌ AVOID (unless zero value is specifically needed)
var accounts map[string]*Account
accounts = make(map[string]*Account)
```

### Use var When Zero Value is Desired
```go
var (
    count int          // Explicitly want 0
    buffer bytes.Buffer // Explicitly want initialized struct
)
```

### Grouped var for Package-Level
```go
var (
    // Version contains the application version number
    Version = ""

    // CommitSHA contains the SHA of the commit
    CommitSHA = ""
)
```

## Map and Slice Initialization

### Provide Capacity Hints When Possible
Always provide capacity hints when the size is known or predictable:

```go
// ✅ CORRECT
accounts := make(map[string]*Account, len(directives))
items := make([]astItem, 0, len(ast.Options)+len(ast.Directives))

// ❌ SUBOPTIMAL (but acceptable if size unknown)
accounts := make(map[string]*Account)
items := make([]astItem, 0)
```

### Use Literal Syntax for Small Fixed Collections
```go
// ✅ CORRECT
weights := WeightSet{
    {Amount: amount, Currency: currency},
}

tags := []string{"vacation", "travel"}
```

## Control Flow

### Prefer Early Returns
Avoid deep nesting by returning early for error cases:

```go
// ✅ CORRECT
func (l *Ledger) processClose(close *parser.Close) {
    accountName := string(close.Account)

    account, ok := l.accounts[accountName]
    if !ok {
        l.addError(&AccountNotClosedError{
            Account: close.Account,
            Date:    close.Date,
        })
        return
    }

    if account.IsClosed() {
        l.addError(&AccountAlreadyClosedError{
            Account:    close.Account,
            Date:       close.Date,
            ClosedDate: account.CloseDate,
        })
        return
    }

    account.CloseDate = close.Date
}

// ❌ AVOID
func (l *Ledger) processClose(close *parser.Close) {
    accountName := string(close.Account)

    if account, ok := l.accounts[accountName]; ok {
        if !account.IsClosed() {
            account.CloseDate = close.Date
        } else {
            l.addError(&AccountAlreadyClosedError{
                Account:    close.Account,
                Date:       close.Date,
                ClosedDate: account.CloseDate,
            })
        }
    } else {
        l.addError(&AccountNotClosedError{
            Account: close.Account,
            Date:    close.Date,
        })
    }
}
```

### Use Type Switches for Multiple Type Checks
When handling multiple types, use type switches instead of repeated type assertions:

```go
// ✅ CORRECT
switch d := directive.(type) {
case *parser.Transaction:
    return d.Pos.Line
case *parser.Balance:
    return d.Pos.Line
case *parser.Open:
    return d.Pos.Line
default:
    return 0
}

// ❌ AVOID
if txn, ok := directive.(*parser.Transaction); ok {
    return txn.Pos.Line
} else if bal, ok := directive.(*parser.Balance); ok {
    return bal.Pos.Line
} else if open, ok := directive.(*parser.Open); ok {
    return open.Pos.Line
}
```

## Struct Organization

### Field Ordering
1. Exported fields (alphabetically or logically grouped)
2. Unexported fields (alphabetically or logically grouped)

```go
// ✅ CORRECT
type Formatter struct {
    // Exported configuration fields
    CurrencyColumn   int
    NumWidth         int
    PreserveBlanks   bool
    PreserveComments bool
    PrefixWidth      int

    // Unexported internal state
    sourceLines []string
}

// ❌ SUBOPTIMAL (mixed ordering)
type Formatter struct {
    CurrencyColumn   int
    sourceLines      []string // unexported in middle of exported
    NumWidth         int
    PrefixWidth      int
}
```

### Struct Tags
Keep struct tags on the same line when reasonable:

```go
type Transaction struct {
    Date      *Date  `parser:"@Date ('txn' | "`
    Flag      string `parser:"@('*' | '!' | 'P') )"`
    Narration string `parser:"@String?"`
}
```

## Function Organization

### Order Within a File
1. Package constants and variables
2. Type definitions
3. Constructor functions (New, NewXxx)
4. Public methods (alphabetically or logically grouped)
5. Private helper functions (alphabetically or logically grouped)

### Group Related Functions
Keep related functions together:

```go
// Constructors
func New() *Ledger { }
func NewWithOptions(opts Options) *Ledger { }

// Public account methods
func (l *Ledger) GetAccount(name string) (*Account, bool) { }
func (l *Ledger) Accounts() map[string]*Account { }

// Public processing methods
func (l *Ledger) Process(ast *parser.AST) error { }
func (l *Ledger) processDirective(d Directive) { }

// Private helpers
func (l *Ledger) addError(err error) { }
func (l *Ledger) isAccountOpen(account Account, date Date) bool { }
```

## Testing Standards

### All Tests Must Pass
- **Never commit code with failing tests**
- Run `go test ./...` before every commit
- Fix or skip (with clear reason) any flaky tests

### Test Organization
Use table-driven tests with subtests for multiple cases:

```go
func TestFormatCmd(t *testing.T) {
    t.Run("BasicFormatting", func(t *testing.T) {
        source := `...`
        ast, err := parser.ParseBytes([]byte(source))
        assert.NoError(t, err)
        // ... test logic
    })

    t.Run("WithCustomCurrencyColumn", func(t *testing.T) {
        source := `...`
        // ... test logic
    })
}
```

### Assertion Library
Use `github.com/alecthomas/assert/v2` for assertions:

```go
assert.NoError(t, err)
assert.Equal(t, expected, actual)
assert.True(t, condition, "optional message")
```

### Test Coverage
- Aim for >80% coverage on new code
- Focus on critical paths and error cases
- Don't test trivial getters/setters

## Performance Considerations

### Memory Pooling
Use `sync.Pool` for frequently allocated objects:

```go
var balanceMapPool = sync.Pool{
    New: func() interface{} {
        return make(map[string]decimal.Decimal, 4)
    },
}

// Always clear before returning to pool
func putBalanceMap(m map[string]decimal.Decimal) {
    for k := range m {
        delete(m, k)
    }
    balanceMapPool.Put(m)
}
```

### Defer Cleanup
Always defer pool returns and resource cleanup:

```go
balance := BalanceWeights(allWeights)
defer putBalanceMap(balance) // Ensure cleanup even if function panics
```

### Pre-allocate When Possible
```go
// Estimate initial capacity
estimatedSize := (len(ast.Options) + len(ast.Directives)) * 100
buf.Grow(estimatedSize)
```

## Comment Style

### Inline Comments
- Use sparingly, prefer self-documenting code
- Explain WHY, not WHAT
- Full sentences with proper punctuation when needed

```go
// Calculate padding using display width (not byte length)
padding := f.CurrencyColumn - currentWidth - runewidth.StringWidth(amount.Value)

// Need to negate the residual to balance
needed := residual.Neg()
```

### TODO Comments
Include context and optionally your name/date:

```go
// TODO(robinvdvleuten): Implement merge cost {*} handling
// TODO: Add support for per-currency tolerance configuration
```

### Block Comments
Use for complex algorithms or important context:

```go
// The parser's Unquote operation removes quotes during parsing, so we must
// re-quote all strings during formatting. Since the lexer doesn't support
// escaped quotes within strings, we implement our own escaping here.
```

## Naming Conventions

### Variables
- Short names for short scopes: `i`, `err`, `ok`
- Descriptive names for package-level or longer scopes
- Avoid stuttering: `user.UserName` → `user.Name`

### Functions
- Start with verb: `Get`, `Set`, `Process`, `Calculate`, `Format`
- Boolean functions: `Is`, `Has`, `Can`, `Should`

### Constants
- Use PascalCase for exported: `DefaultCurrencyColumn`
- Use camelCase for unexported: `defaultTolerance`

### Interfaces
- Single-method interfaces end in `-er`: `Reader`, `Writer`, `Formatter`
- Multi-method interfaces use descriptive names: `Directive`, `WithMetadata`

## Common Patterns

### Constructor Pattern
Use constructors to encapsulate object initialization and avoid repetition:

```go
// ❌ ANTI-PATTERN: Extracting fields from the object you're passing
func NewAccountNotOpenError(directive parser.Directive, account parser.Account, date *parser.Date, pos lexer.Position) *AccountNotOpenError {
    return &AccountNotOpenError{
        Account:   account,
        Date:      date,
        Pos:       pos,
        Directive: directive,
    }
}
// Usage: NewAccountNotOpenError(balance, balance.Account, balance.Date, balance.Pos)
//        ^^^ Redundant! We're extracting fields from balance that we just passed

// ✅ CORRECT: Let the constructor extract fields
func NewAccountNotOpenErrorFromBalance(balance *parser.Balance) *AccountNotOpenError {
    return &AccountNotOpenError{
        Account:   balance.Account,
        Date:      balance.Date,
        Pos:       balance.Pos,
        Directive: balance,
    }
}
// Usage: NewAccountNotOpenErrorFromBalance(balance)
//        ^^^ Clean! Constructor handles field extraction
```

**Rules:**
- If constructor takes a struct parameter, extract all related fields inside the constructor
- Don't make callers extract fields from objects they're passing to the constructor
- Use specific constructors per type when field extraction varies (e.g., `NewErrorFromBalance`, `NewErrorFromTransaction`)
- Only pass external data that the constructor can't extract itself

### Options Pattern
Use functional options for configurable constructors:

```go
type Option func(*Formatter)

func WithCurrencyColumn(col int) Option {
    return func(f *Formatter) {
        f.CurrencyColumn = col
    }
}

func New(opts ...Option) *Formatter {
    f := &Formatter{
        PreserveComments: true,
        PreserveBlanks:   true,
    }
    for _, opt := range opts {
        opt(f)
    }
    return f
}
```

### Interface Assertion
Verify interface implementation at compile time:

```go
var _ Directive = &Transaction{}
var _ io.Writer = &bytes.Buffer{}
```

### Context Pattern
All public functions that perform I/O, processing, or potentially long-running operations should accept `context.Context` as their first parameter:

```go
// ✅ CORRECT: Context as first parameter
func Load(ctx context.Context, filename string) (*AST, error) {
    collector := telemetry.FromContext(ctx)
    timer := collector.Start("Load " + filename)
    defer timer.End()

    // Check for cancellation in loops
    for _, inc := range includes {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        // Do work...
    }

    return ast, nil
}

// ❌ INCORRECT: No context parameter
func Load(filename string) (*AST, error) {
    // Cannot be cancelled, no telemetry support
}
```

**When to use context:**
- Functions that do I/O (file operations, network)
- Functions that process many items (loops over directives, includes)
- Functions that may take > 100ms
- Public APIs that others might want to cancel/timeout

**When NOT to use context:**
- Pure computation functions (no I/O)
- Internal helper functions that are always fast
- Functions that only manipulate data structures

**Cancellation checks:**
- Add `select { case <-ctx.Done(): return ctx.Err() }` in loops that process many items
- Check every 100-1000 iterations for very tight loops
- Don't check in every function call (overhead not worth it)

**Context propagation:**
- Always pass context to functions you call
- Use `context.Background()` only at program entry points
- Never store context in structs (pass as parameter)
- Context goes first: `func Foo(ctx context.Context, arg1, arg2)`

### Telemetry Pattern
Use the telemetry package to instrument operations for timing analysis:

```go
// ✅ CORRECT: Extract collector and time operation
func Process(ctx context.Context, ast *AST) error {
    collector := telemetry.FromContext(ctx)
    timer := collector.Start("Process ledger")
    defer timer.End()

    // Nested operations
    optsTimer := timer.Child("Process options")
    for _, opt := range ast.Options {
        // Process options...
    }
    optsTimer.End()

    dirsTimer := timer.Child("Process directives")
    for _, dir := range ast.Directives {
        // Process directives...
    }
    dirsTimer.End()

    return nil
}

// ❌ INCORRECT: Manual timing without telemetry
func Process(ctx context.Context, ast *AST) error {
    start := time.Now()
    defer func() {
        fmt.Printf("Process took %v\n", time.Since(start))
    }()
    // No hierarchical tracking, inconsistent output
}
```

**When to add telemetry:**
- Operations that users care about performance (loading, parsing, validation)
- Operations that may be slow (> 10ms)
- Top-level phases (Load, Parse, Process, Format)
- Sub-operations that provide useful breakdown (per-file parsing, directive types)

**When NOT to add telemetry:**
- Trivial operations (< 1ms)
- Operations called thousands of times (too noisy)
- Internal helper functions

**Telemetry best practices:**
- Always use `defer timer.End()` to ensure timers complete
- Use descriptive names: "Parse main.beancount" not just "Parse"
- Create child timers for nested operations
- Extract collector once per function (not per operation)
- Telemetry has zero overhead when disabled (NoOp collector)

**Hierarchical timers:**
```go
// Parent timer
parentTimer := collector.Start("Parent operation")
defer parentTimer.End()

// Child timers - will be nested under parent in output
child1 := parentTimer.Child("Child 1")
// ... work ...
child1.End()

child2 := parentTimer.Child("Child 2")
// ... work ...
child2.End()
```

## Project-Specific Conventions

### Parser Package
- All directives implement `Directive` interface
- Use `participle` struct tags for grammar
- Include comprehensive examples in godoc

### Formatter Package
- Always preserve original spacing when possible
- Use `runewidth.StringWidth()` for display width calculations (not `len()`)
- Comments and blank lines preserved by default

### Ledger Package
- Use `decimal.Decimal` for all monetary amounts (never float)
- Pool frequently allocated maps
- Return `ValidationErrors` wrapper for multiple errors
- All validation errors must include `Pos` and `Directive` fields for consistent formatting
- Implement getter methods: `GetPosition()`, `GetDirective()`, `GetAccount()`, `GetDate()`

### Errors Package
- Provides formatting infrastructure, not domain errors
- Domain-specific errors remain in their packages (e.g., `ledger`, `parser`)
- Use `TextFormatter` for CLI output (bean-check style: `filename:line: message` + directive)
- Use `JSONFormatter` for structured output (APIs, web UIs)
- All errors with `GetPosition()` and `GetDirective()` methods are formatted with context

### Loader Package
- Follow includes recursively when `WithFollowIncludes()` option set
- Deduplicate included files by absolute path
- Preserve directive order after merging

## Additional Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

## Changelog

When this guide is updated, add an entry here:

- 2025-01-17: Added Context Pattern and Telemetry Pattern sections for Phase 1 and Phase 2 implementation
- 2025-01-17: Added Constructor Pattern section with anti-pattern warning about redundant field extraction
- 2025-01-17: Added error formatting section describing the errors package and formatter pattern
- 2025-01-17: Initial version created based on codebase analysis
