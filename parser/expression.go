package parser

import (
	"github.com/shopspring/decimal"
)

// Expression parsing for arithmetic expressions in amounts.
//
// Supports:
//   - Binary operators: +, -, *, /
//   - Parentheses for grouping
//   - Decimal numbers with proper precision
//
// Operator precedence (low to high):
//   1. + -     (addition, subtraction)
//   2. * /     (multiplication, division)
//   3. ( )     (parentheses, highest)
//
// Grammar:
//   expression  → term (('+' | '-') term)*
//   term        → factor (('*' | '/') factor)*
//   factor      → NUMBER | '(' expression ')'
//
// Examples:
//   2 + 3           → 5
//   2 + 3 * 4       → 14 (multiplication has higher precedence)
//   (2 + 3) * 4     → 20 (parentheses override precedence)
//   40.00 / 3       → 13.333...
//   ((40.00/3) + 5) → 18.333...

// parseExpression parses and evaluates an arithmetic expression.
// This is the entry point for expression parsing.
func (p *Parser) parseExpression() (decimal.Decimal, error) {
	return p.parseAddSubtract()
}

// parseAddSubtract handles addition and subtraction (lowest precedence).
func (p *Parser) parseAddSubtract() (decimal.Decimal, error) {
	left, err := p.parseMultiplyDivide()
	if err != nil {
		return decimal.Zero, err
	}

	for {
		op := p.peek().Type
		if op != PLUS && op != MINUS {
			break
		}

		p.advance() // consume operator

		right, err := p.parseMultiplyDivide()
		if err != nil {
			return decimal.Zero, err
		}

		switch op {
		case PLUS:
			left = left.Add(right)
		case MINUS:
			left = left.Sub(right)
		}
	}

	return left, nil
}

// parseMultiplyDivide handles multiplication and division (higher precedence).
func (p *Parser) parseMultiplyDivide() (decimal.Decimal, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return decimal.Zero, err
	}

	for {
		op := p.peek().Type
		if op != ASTERISK && op != SLASH {
			break
		}

		opToken := p.advance() // consume operator

		right, err := p.parsePrimary()
		if err != nil {
			return decimal.Zero, err
		}

		switch op {
		case ASTERISK:
			left = left.Mul(right)
		case SLASH:
			if right.IsZero() {
				return decimal.Zero, p.errorAtToken(opToken, "division by zero")
			}
			left = left.Div(right)
		}
	}

	return left, nil
}

// parsePrimary handles numbers and parenthesized expressions (highest precedence).
func (p *Parser) parsePrimary() (decimal.Decimal, error) {
	tok := p.peek()

	// Parenthesized expression: (expr)
	if tok.Type == LPAREN {
		p.advance() // consume '('

		result, err := p.parseExpression()
		if err != nil {
			return decimal.Zero, err
		}

		if !p.check(RPAREN) {
			return decimal.Zero, p.error("expected ')' after expression")
		}
		p.advance() // consume ')'

		return result, nil
	}

	// Number (possibly negative)
	if tok.Type == NUMBER {
		numTok := p.advance()
		value := numTok.String(p.source)

		d, err := decimal.NewFromString(value)
		if err != nil {
			return decimal.Zero, p.errorAtToken(numTok, "invalid number in expression: %v", err)
		}

		return d, nil
	}

	// Handle unary minus: -expr
	if tok.Type == MINUS {
		p.advance() // consume '-'

		value, err := p.parsePrimary()
		if err != nil {
			return decimal.Zero, err
		}

		return value.Neg(), nil
	}

	return decimal.Zero, p.errorAtToken(tok, "expected number or '(' in expression, got %s", tok.Type)
}

// isExpressionStart checks if the current position looks like the start of an expression.
// This is used by parseAmount to detect expressions vs simple numbers.
func (p *Parser) isExpressionStart() bool {
	// Check if we have: NUMBER followed by an operator (+, -, *, /)
	if p.check(NUMBER) {
		nextTok := p.peekAhead(1)
		return nextTok.Type == PLUS || nextTok.Type == MINUS ||
			nextTok.Type == ASTERISK || nextTok.Type == SLASH
	}

	// Check if we start with a parenthesis or minus (definitely an expression)
	return p.check(LPAREN) || p.check(MINUS)
}
