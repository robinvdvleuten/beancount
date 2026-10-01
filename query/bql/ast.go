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
	Span() (start, end int)
}

// position provides the Span accessor for embedding in AST nodes.
type position struct {
	start, end int
}

// Span returns the byte offsets of the node's source text, from its first
// token to the end of its last. A parenthesized expression's span leaves
// out its parentheses, as bean-query's parse tree does.
func (p position) Span() (start, end int) { return p.start, p.end }

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
	Having   Expr   // only with GROUP BY, as in beanquery's grammar
	OrderBy  []OrderTerm
	// PivotBy is empty or holds exactly two items, each a target name
	// (Ident) or a 1-based index (ColumnIndex).
	PivotBy []Expr
	Limit   *int64
}

func (*Select) stmt() {}

// Target is a single SELECT target: an expression with an optional alias.
type Target struct {
	Expr Expr
	// As is the alias, lowercased unless it was double-quoted, as
	// bean-query keeps it.
	As string
	// Text is the expression's source text, which names an unaliased
	// target that is not a bare column.
	Text string
}

// From is the FROM clause: an optional filter expression plus optional
// summarization transforms.
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
	Where   Expr // optional WHERE filter
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

// OrderTerm is one ORDER BY item with its own direction: ascending
// unless DESC follows it, as in beanquery.
type OrderTerm struct {
	Expr Expr
	Desc bool
}

// ColumnIndex is a GROUP BY or ORDER BY item written as a bare integer,
// a 1-based reference to a target. Any other integer there, such as +1 or
// (1), is a constant expression, as in beanquery's grammar.
type ColumnIndex struct {
	position
	Value int64
}

func (*ColumnIndex) expr() {}

// Asterisk is the * of a call like count(*), its only argument.
type Asterisk struct {
	position
}

func (*Asterisk) expr() {}

// Unary is a unary operation: NOT, or MINUS for a negation.
type Unary struct {
	position
	Op TokenType
	X  Expr
}

func (*Unary) expr() {}

// IsNull is X IS NULL, or X IS NOT NULL when Not is set.
type IsNull struct {
	position
	X   Expr
	Not bool
}

func (*IsNull) expr() {}

// Binary is a binary operation. Op is one of AND, OR, EQ, NE, LT, LTE, GT,
// GTE, TILDE, NOTTILDE, QTILDE, IN, NOTIN, PLUS, MINUS, ASTERISK, SLASH,
// PERCENT.
type Binary struct {
	position
	Op   TokenType
	L, R Expr
}

func (*Binary) expr() {}

// Between is X BETWEEN Lower AND Upper.
type Between struct {
	position
	X, Lower, Upper Expr
}

func (*Between) expr() {}

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

// List is a list constant, (literal, ...): beanquery's list rule, whose
// first literal a comma follows. Like beanquery's, it leaves out an empty
// slot and a NULL after the first item, so (1, NULL,, 2) holds 1 and 2
// and (NULL, 1) holds NULL and 1. Its span takes in its parentheses.
type List struct {
	position
	Items []Expr // Str, Int, Dec, DateLit, Bool and Null literals
}

func (*List) expr() {}

// Attribute is attribute access on a structured value, X.Name.
type Attribute struct {
	position
	X    Expr
	Name string
}

func (*Attribute) expr() {}

// Subscript is subscript access on a dict value, X['Key'].
type Subscript struct {
	position
	X   Expr
	Key string
}

func (*Subscript) expr() {}
