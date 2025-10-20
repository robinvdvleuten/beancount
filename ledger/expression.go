package ledger

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// EvaluateExpression evaluates an arithmetic expression like "(5 + 3)" or "((40 / 3) + 5)".
// Uses a Pratt parser for operator precedence (* / bind tighter than + -).
// Returns an error if the expression is invalid or contains division by zero.
func EvaluateExpression(expr string) (decimal.Decimal, error) {
	if !strings.HasPrefix(expr, "(") || !strings.HasSuffix(expr, ")") {
		return decimal.Zero, fmt.Errorf("expression must be wrapped in parentheses: %q", expr)
	}

	// Remove outer parentheses
	expr = expr[1 : len(expr)-1]

	// Create lexer
	lex := &exprLexer{input: expr, pos: 0}

	// Parse and evaluate
	result, err := lex.parseExpr(0)
	if err != nil {
		return decimal.Zero, err
	}

	// Ensure we consumed all tokens
	if !lex.isAtEnd() {
		tok := lex.peek()
		return decimal.Zero, fmt.Errorf("unexpected token at position %d: %q", lex.pos, tok)
	}

	return result, nil
}

// exprLexer is a simple lexer for arithmetic expressions
type exprLexer struct {
	input string
	pos   int
}

// skipWhitespace skips spaces and tabs
func (l *exprLexer) skipWhitespace() {
	for l.pos < len(l.input) && (l.input[l.pos] == ' ' || l.input[l.pos] == '\t') {
		l.pos++
	}
}

// isAtEnd returns true if we've consumed all input
func (l *exprLexer) isAtEnd() bool {
	l.skipWhitespace()
	return l.pos >= len(l.input)
}

// peek returns the current character without consuming it
func (l *exprLexer) peek() byte {
	l.skipWhitespace()
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

// advance consumes and returns the current character
func (l *exprLexer) advance() byte {
	l.skipWhitespace()
	if l.pos >= len(l.input) {
		return 0
	}
	ch := l.input[l.pos]
	l.pos++
	return ch
}

// parseNumber parses a number (integer or decimal, positive or negative)
func (l *exprLexer) parseNumber() (decimal.Decimal, error) {
	l.skipWhitespace()
	start := l.pos

	// Handle negative sign
	if l.pos < len(l.input) && l.input[l.pos] == '-' {
		l.pos++
	}

	// Parse digits and optional decimal point
	foundDigit := false
	foundDot := false

	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch >= '0' && ch <= '9' {
			foundDigit = true
			l.pos++
		} else if ch == '.' && !foundDot {
			foundDot = true
			l.pos++
		} else {
			break
		}
	}

	if !foundDigit {
		return decimal.Zero, fmt.Errorf("expected number at position %d", start)
	}

	numStr := l.input[start:l.pos]
	num, err := decimal.NewFromString(numStr)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid number %q: %w", numStr, err)
	}

	return num, nil
}

// parsePrimary parses a primary expression (number or parenthesized expression)
func (l *exprLexer) parsePrimary() (decimal.Decimal, error) {
	ch := l.peek()

	// Parenthesized expression
	if ch == '(' {
		l.advance() // consume '('
		result, err := l.parseExpr(0)
		if err != nil {
			return decimal.Zero, err
		}
		if l.peek() != ')' {
			return decimal.Zero, fmt.Errorf("expected ')' at position %d", l.pos)
		}
		l.advance() // consume ')'
		return result, nil
	}

	// Unary minus
	if ch == '-' {
		l.advance()
		operand, err := l.parsePrimary()
		if err != nil {
			return decimal.Zero, err
		}
		return operand.Neg(), nil
	}

	// Number
	return l.parseNumber()
}

// parseExpr is the Pratt parser core - handles operator precedence
func (l *exprLexer) parseExpr(minPrec int) (decimal.Decimal, error) {
	// Parse left operand
	left, err := l.parsePrimary()
	if err != nil {
		return decimal.Zero, err
	}

	// Parse infix operators
	for {
		op := l.peek()
		if !isOperator(op) {
			break
		}

		prec := precedence(op)
		if prec < minPrec {
			break
		}

		l.advance() // consume operator

		// Parse right operand with higher precedence
		right, err := l.parseExpr(prec + 1)
		if err != nil {
			return decimal.Zero, err
		}

		// Apply operator
		left, err = applyOp(left, op, right)
		if err != nil {
			return decimal.Zero, err
		}
	}

	return left, nil
}

// isOperator returns true if ch is an arithmetic operator
func isOperator(ch byte) bool {
	return ch == '+' || ch == '-' || ch == '*' || ch == '/'
}

// precedence returns operator precedence (higher = tighter binding)
func precedence(op byte) int {
	switch op {
	case '+', '-':
		return 1
	case '*', '/':
		return 2
	default:
		return 0
	}
}

// applyOp applies a binary operator to two operands
func applyOp(left decimal.Decimal, op byte, right decimal.Decimal) (decimal.Decimal, error) {
	switch op {
	case '+':
		return left.Add(right), nil
	case '-':
		return left.Sub(right), nil
	case '*':
		return left.Mul(right), nil
	case '/':
		if right.IsZero() {
			return decimal.Zero, fmt.Errorf("division by zero")
		}
		return left.Div(right), nil
	default:
		return decimal.Zero, fmt.Errorf("unknown operator: %c", op)
	}
}
