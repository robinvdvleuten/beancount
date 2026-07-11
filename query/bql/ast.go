// Package bql provides the lexer, AST, and parser for the Beancount Query
// Language (BQL) as implemented by the official bean-query tool. The parser
// handles syntax only; name resolution and type checking live in the query
// package's compiler.
package bql

import (
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Node is implemented by all BQL AST nodes.
type Node interface {
	Pos() ast.Position
}

// position provides the Pos accessor for embedding in AST nodes.
type position struct {
	pos ast.Position
}

func (p position) Pos() ast.Position { return p.pos }

// Statement is a complete BQL statement.
type Statement interface {
	Node
	stmt()
}

// Select is a SELECT statement, the core BQL query form.
type Select struct {
	position
	Distinct bool
	Wildcard bool     // SELECT *
	Targets  []Target // empty when Wildcard
	From     *From
	Where    Expr
	GroupBy  []Expr // column names, aliases, or 1-based integer indices
	OrderBy  []Expr
	// OrderDesc applies to the whole ORDER BY list; the official grammar
	// accepts a single trailing ASC or DESC, not one per term.
	OrderDesc bool
	PivotBy   []Expr
	Limit     *int64
}

func (*Select) stmt() {}

// Target is a single SELECT target: an expression with an optional alias.
type Target struct {
	Expr Expr
	As   string
}

// From is the FROM clause: an optional entry-level filter expression plus
// optional summarization transforms.
type From struct {
	position
	Expr    Expr
	OpenOn  *ast.Date
	Close   bool // bare CLOSE, or CLOSE ON when CloseOn is set
	CloseOn *ast.Date
	Clear   bool
}

// Balances is the BALANCES shortcut statement.
type Balances struct {
	position
	Summary string // AT <function>, empty if absent
	From    *From
}

func (*Balances) stmt() {}

// Journal is the JOURNAL shortcut statement.
type Journal struct {
	position
	Account string // optional account regex, empty if absent
	Summary string // AT <function>, empty if absent
	From    *From
}

func (*Journal) stmt() {}

// Print is the PRINT statement, rendering matching directives as beancount text.
type Print struct {
	position
	From *From
}

func (*Print) stmt() {}

// Expr is a BQL expression node.
type Expr interface {
	Node
	expr()
}

// Ident is a column reference.
type Ident struct {
	position
	Name string
}

func (*Ident) expr() {}

// Call is a function call.
type Call struct {
	position
	Func string
	Args []Expr
}

func (*Call) expr() {}

// Unary is a unary operation. Op is MINUS, PLUS, or NOT.
type Unary struct {
	position
	Op TokenType
	X  Expr
}

func (*Unary) expr() {}

// Binary is a binary operation. Op is one of AND, OR, EQ, NE, LT, LTE, GT,
// GTE, TILDE, IN, PLUS, MINUS, ASTERISK, SLASH.
type Binary struct {
	position
	Op   TokenType
	L, R Expr
}

func (*Binary) expr() {}

// Str is a string literal.
type Str struct {
	position
	Value string
}

func (*Str) expr() {}

// Int is an integer literal.
type Int struct {
	position
	Value int64
}

func (*Int) expr() {}

// Dec is a decimal literal.
type Dec struct {
	position
	Value decimal.Decimal
}

func (*Dec) expr() {}

// DateLit is a date literal (YYYY-MM-DD).
type DateLit struct {
	position
	Value *ast.Date
}

func (*DateLit) expr() {}

// Bool is a TRUE or FALSE literal.
type Bool struct {
	position
	Value bool
}

func (*Bool) expr() {}

// Null is the NULL literal.
type Null struct {
	position
}

func (*Null) expr() {}
