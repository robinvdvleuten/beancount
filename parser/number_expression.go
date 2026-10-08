package parser

import (
	"fmt"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/shopspring/decimal"
)

type numberExpressionParser struct {
	source      []byte
	pos         int
	lineEnd     int
	consumedEnd int
}

func evaluateNumberExpression(source []byte, start int) (decimal.Decimal, int, error) {
	lineEnd := start
	for lineEnd < len(source) && source[lineEnd] != '\n' {
		lineEnd++
	}
	p := &numberExpressionParser{source: source, pos: start, lineEnd: lineEnd, consumedEnd: start}
	value, err := p.parseExpr(0)
	if err != nil {
		return decimal.Zero, start, err
	}
	return value, p.consumedEnd, nil
}

func (p *numberExpressionParser) skipWhitespace() {
	for p.pos < p.lineEnd && isBlank(p.source[p.pos]) {
		p.pos++
	}
}

func (p *numberExpressionParser) peek() byte {
	p.skipWhitespace()
	if p.pos >= p.lineEnd {
		return 0
	}
	return p.source[p.pos]
}

func (p *numberExpressionParser) consume() byte {
	ch := p.source[p.pos]
	p.pos++
	p.consumedEnd = p.pos
	return ch
}

func (p *numberExpressionParser) parsePrimary() (decimal.Decimal, error) {
	switch p.peek() {
	case '+':
		p.consume()
		return p.parsePrimary()
	case '-':
		p.consume()
		value, err := p.parsePrimary()
		return value.Neg(), err
	case '(':
		p.consume()
		value, err := p.parseExpr(0)
		if err != nil {
			return decimal.Zero, err
		}
		if p.peek() != ')' {
			return decimal.Zero, fmt.Errorf("expected ')' at position %d", p.pos)
		}
		p.consume()
		return value, nil
	default:
		return p.parseNumber()
	}
}

func (p *numberExpressionParser) parseNumber() (decimal.Decimal, error) {
	p.skipWhitespace()
	start := p.pos
	// Like beancount's lexer, which reads a date wherever a word starts,
	// a date where a number should start is no number: (2020-1-2) is a
	// syntax error, not 2017.
	if ast.DateLiteralLen(p.source[start:p.lineEnd]) > 0 {
		return decimal.Zero, fmt.Errorf("unexpected date at position %d", start)
	}
	foundDigit, seenDot := false, false
	for p.pos < p.lineEnd {
		ch := p.source[p.pos]
		// Like v2's number pattern, a comma is a digit group separator
		// only with a digit after it: {10, 2020-01-01} ends at "10".
		if isDigit(ch) || (ch == ',' && p.pos+1 < p.lineEnd && isDigit(p.source[p.pos+1])) {
			foundDigit = true
			p.pos++
			continue
		}
		if ch == '.' && !seenDot && foundDigit {
			// A fraction, or like beancount a bare trailing dot ("5.")
			seenDot = true
			p.pos++
			continue
		}
		break
	}
	if !foundDigit {
		return decimal.Zero, fmt.Errorf("expected number at position %d", start)
	}
	p.consumedEnd = p.pos
	raw := strings.ReplaceAll(string(p.source[start:p.pos]), ",", "")
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid number %q: %w", raw, err)
	}
	return value, nil
}

func (p *numberExpressionParser) parseExpr(minPrecedence int) (decimal.Decimal, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return decimal.Zero, err
	}
	for {
		op := p.peek()
		precedence := numberOperatorPrecedence(op)
		if precedence < minPrecedence {
			break
		}
		// A slash that starts a currency (/ESZ24) is no division.
		if op == '/' && slashCurrencyLen(p.source[p.pos:p.lineEnd]) > 0 {
			break
		}
		p.consume()
		right, err := p.parseExpr(precedence + 1)
		if err != nil {
			return decimal.Zero, err
		}
		switch op {
		case '+':
			left = pydecimal.Add(left, right)
		case '-':
			left = pydecimal.Sub(left, right)
		case '*':
			left = pydecimal.Mul(left, right)
		case '/':
			if right.IsZero() {
				return decimal.Zero, fmt.Errorf("division by zero")
			}
			left = pydecimal.Quo(left, right)
		}
	}
	return left, nil
}

func numberOperatorPrecedence(operator byte) int {
	switch operator {
	case '+', '-':
		return 1
	case '*', '/':
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
