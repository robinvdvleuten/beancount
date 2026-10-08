package parser

import (
	"strings"

	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

// numberStart reports whether the next token starts beancount's
// number_expr: a number, a sign or an opening parenthesis.
func (p *Parser) numberStart() bool {
	switch p.peek().Type {
	case NUMBER, PLUS, MINUS, LPAREN:
		return true
	}
	return false
}

// parseNumberExpr parses beancount's number_expr from the tokens on the
// line it starts on: numbers joined by + - * /, unary signs and
// parentheses, evaluated as beancount's grammar does, in Python's decimal
// arithmetic. It returns a token spanning the expression and its value: a
// number with at most one sign written right before it as written, without
// its commas or a trailing dot, so -0.00 keeps its sign; anything else
// evaluated, keeping its exponent (canonicalExpressionValue).
func (p *Parser) parseNumberExpr() (Token, string, error) {
	first, start := p.peek(), p.pos
	if !p.numberStart() {
		return Token{}, "", numberExpr{p: p, line: p.previous().Line}.expected("number or expression")
	}
	e := numberExpr{p: p, line: first.Line}
	result, err := e.parse(0)
	if err != nil {
		return Token{}, "", err
	}
	last := p.previous()
	span := Token{Type: NUMBER, Start: first.Start, End: last.End, Line: first.Line, Column: first.Column}
	signed := first.Type == PLUS || first.Type == MINUS
	literal := p.pos-start == 1 || (p.pos-start == 2 && signed && first.End == last.Start)
	if literal {
		return span, strings.TrimSuffix(strings.ReplaceAll(span.String(p.source), ",", ""), "."), nil
	}
	return span, canonicalExpressionValue(result), nil
}

// numberExpr evaluates one number_expr, reading the parser's tokens on its
// line.
type numberExpr struct {
	p    *Parser
	line int
}

// next returns the next token when it is on the expression's line.
func (e numberExpr) next() (Token, bool) {
	tok := e.p.peek()
	return tok, !e.p.isAtEnd() && tok.Line == e.line
}

// expected is the error for a token the expression cannot go on with, or,
// at its line's end, for the missing one.
func (e numberExpr) expected(what string) error {
	if _, ok := e.next(); ok {
		return e.p.error("expected %s", what)
	}
	return e.p.errorAtEndOfPrevious("expected %s", what)
}

// parse reads operands joined by operators of at least minPrecedence, each
// operator binding to the left as beancount's %left does.
func (e numberExpr) parse(minPrecedence int) (decimal.Decimal, error) {
	left, err := e.operand()
	if err != nil {
		return decimal.Zero, err
	}
	for {
		op, ok := e.next()
		precedence := numberOperatorPrecedence(op.Type)
		if !ok || precedence < minPrecedence {
			return left, nil
		}
		e.p.advance()
		right, err := e.parse(precedence + 1)
		if err != nil {
			return decimal.Zero, err
		}
		switch op.Type {
		case PLUS:
			left = pydecimal.Add(left, right)
		case MINUS:
			left = pydecimal.Sub(left, right)
		case ASTERISK:
			left = pydecimal.Mul(left, right)
		case SLASH:
			if right.IsZero() {
				return decimal.Zero, e.p.errorAtToken(op, "division by zero")
			}
			left = pydecimal.Quo(left, right)
		}
	}
}

// operand reads a number, a signed operand, which binds tighter than any
// operator (beancount's %prec NEGATIVE), or a parenthesised expression.
func (e numberExpr) operand() (decimal.Decimal, error) {
	tok, ok := e.next()
	if !ok {
		return decimal.Zero, e.expected("number")
	}
	switch tok.Type {
	case PLUS:
		e.p.advance()
		return e.operand()
	case MINUS:
		e.p.advance()
		value, err := e.operand()
		return value.Neg(), err
	case LPAREN:
		e.p.advance()
		value, err := e.parse(0)
		if err != nil {
			return decimal.Zero, err
		}
		if closing, ok := e.next(); !ok || closing.Type != RPAREN {
			return decimal.Zero, e.expected("')'")
		}
		e.p.advance()
		return value, nil
	case NUMBER:
		e.p.advance()
		text := strings.ReplaceAll(tok.String(e.p.source), ",", "")
		value, err := decimal.NewFromString(strings.TrimSuffix(text, "."))
		if err != nil {
			return decimal.Zero, e.p.errorAtToken(tok, "invalid number %q", text)
		}
		return value, nil
	}
	return decimal.Zero, e.expected("number")
}

func numberOperatorPrecedence(operator TokenType) int {
	switch operator {
	case PLUS, MINUS:
		return 1
	case ASTERISK, SLASH:
		return 2
	default:
		return -1
	}
}

// canonicalExpressionValue renders an evaluated amount keeping its exponent,
// so 1 + 2.20 states two decimals like beancount's 3.20: tolerance
// inference and display precision depend on it.
func canonicalExpressionValue(value decimal.Decimal) string {
	return value.StringFixed(max(-value.Exponent(), 0))
}
