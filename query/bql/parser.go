package bql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// queryFilename is the filename used in positions for parsed query strings.
const queryFilename = "<query>"

// Parse parses a BQL statement from the given query string.
func Parse(query string) (Statement, error) {
	return ParseBytes([]byte(query))
}

// ParseBytes parses a BQL statement from the given query source.
func ParseBytes(source []byte) (Statement, error) {
	p := newParser(source)
	stmt, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	if p.cur.Type == SEMICOLON {
		p.next()
	}
	if p.cur.Type != EOF {
		return nil, p.errorf(p.cur, "unexpected %s", p.describe(p.cur))
	}
	return stmt, nil
}

// Parser is a recursive-descent parser for BQL statements.
type parser struct {
	source []byte
	lexer  *Lexer
	cur    Token
}

func newParser(source []byte) *parser {
	p := &parser{source: source, lexer: NewLexer(source)}
	p.next()
	return p
}

func (p *parser) next() {
	p.cur = p.lexer.Next()
}

// expect consumes the current token if it has the given type, or fails with a
// positioned error naming what was expected.
func (p *parser) expect(t TokenType, context string) (Token, error) {
	if p.cur.Type != t {
		return Token{}, p.errorf(p.cur, "expected %s in %s, found %s", t, context, p.describe(p.cur))
	}
	tok := p.cur
	p.next()
	return tok, nil
}

// accept consumes the current token if it has the given type.
func (p *parser) accept(t TokenType) bool {
	if p.cur.Type == t {
		p.next()
		return true
	}
	return false
}

func (p *parser) pos(tok Token) ast.Position {
	return ast.Position{Filename: queryFilename, Offset: tok.Start, Line: tok.Line, Column: tok.Column}
}

func (p *parser) errorf(tok Token, format string, args ...any) *ParseError {
	return &ParseError{
		Pos:     p.pos(tok),
		Message: fmt.Sprintf(format, args...),
	}
}

func (p *parser) describe(tok Token) string {
	switch tok.Type {
	case EOF:
		return "end of query"
	case IDENT, STRING, INTEGER, DECIMAL, DATE, ILLEGAL:
		return fmt.Sprintf("%s %q", strings.ToLower(tok.Type.String()), tok.String(p.source))
	default:
		return fmt.Sprintf("%q", tok.String(p.source))
	}
}

func (p *parser) parseStatement() (Statement, error) {
	switch p.cur.Type {
	case SELECT:
		return p.parseSelect()
	case BALANCES:
		return p.parseBalances()
	case JOURNAL:
		return p.parseJournal()
	case PRINT:
		return p.parsePrint()
	default:
		return nil, p.errorf(p.cur, "expected SELECT, BALANCES, JOURNAL or PRINT, found %s", p.describe(p.cur))
	}
}

func (p *parser) parseSelect() (*Select, error) {
	tok := p.cur
	p.next() // SELECT

	sel := &Select{position: position{p.pos(tok)}}
	sel.Distinct = p.accept(DISTINCT)

	if p.accept(ASTERISK) {
		sel.Wildcard = true
	} else {
		for {
			target, err := p.parseTarget()
			if err != nil {
				return nil, err
			}
			sel.Targets = append(sel.Targets, target)
			if !p.accept(COMMA) {
				break
			}
		}
	}

	if p.cur.Type == FROM {
		from, err := p.parseFrom()
		if err != nil {
			return nil, err
		}
		sel.From = from
	}

	if p.accept(WHERE) {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		sel.Where = expr
	}

	if p.cur.Type == GROUP {
		p.next()
		if _, err := p.expect(BY, "GROUP BY"); err != nil {
			return nil, err
		}
		exprs, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		sel.GroupBy = exprs
	}

	if p.cur.Type == ORDER {
		p.next()
		if _, err := p.expect(BY, "ORDER BY"); err != nil {
			return nil, err
		}
		exprs, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		sel.OrderBy = exprs
		if p.accept(DESC) {
			sel.OrderDesc = true
		} else {
			p.accept(ASC)
		}
	}

	if p.cur.Type == PIVOT {
		p.next()
		if _, err := p.expect(BY, "PIVOT BY"); err != nil {
			return nil, err
		}
		exprs, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		sel.PivotBy = exprs
	}

	if p.cur.Type == LIMIT {
		p.next()
		tok, err := p.expect(INTEGER, "LIMIT")
		if err != nil {
			return nil, err
		}
		limit, err := strconv.ParseInt(tok.String(p.source), 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "invalid LIMIT value %q", tok.String(p.source))
		}
		sel.Limit = &limit
	}

	return sel, nil
}

func (p *parser) parseTarget() (Target, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return Target{}, err
	}
	target := Target{Expr: expr}
	if p.accept(AS) {
		tok, err := p.expect(IDENT, "target alias")
		if err != nil {
			return Target{}, err
		}
		target.As = tok.String(p.source)
	}
	return target, nil
}

// parseFrom parses a FROM clause: an optional entry filter expression
// followed by optional OPEN ON, CLOSE [ON], and CLEAR transforms, in that
// order (matching the official grammar).
func (p *parser) parseFrom() (*From, error) {
	tok := p.cur
	p.next() // FROM

	from := &From{position: position{p.pos(tok)}}

	if p.startsExpr() {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		from.Expr = expr
	}

	if p.cur.Type == OPEN {
		p.next()
		if _, err := p.expect(ON, "FROM ... OPEN"); err != nil {
			return nil, err
		}
		date, err := p.parseDate()
		if err != nil {
			return nil, err
		}
		from.OpenOn = date
	}

	if p.cur.Type == CLOSE {
		p.next()
		from.Close = true
		if p.accept(ON) {
			date, err := p.parseDate()
			if err != nil {
				return nil, err
			}
			from.CloseOn = date
		}
	}

	if p.cur.Type == CLEAR {
		p.next()
		from.Clear = true
	}

	if from.Expr == nil && from.OpenOn == nil && !from.Close && !from.Clear {
		return nil, p.errorf(p.cur, "expected expression, OPEN, CLOSE or CLEAR after FROM, found %s", p.describe(p.cur))
	}

	return from, nil
}

func (p *parser) parseBalances() (*Balances, error) {
	tok := p.cur
	p.next() // BALANCES

	stmt := &Balances{position: position{p.pos(tok)}}
	summary, err := p.parseAtSummary()
	if err != nil {
		return nil, err
	}
	stmt.Summary = summary

	if p.cur.Type == FROM {
		from, err := p.parseFrom()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}
	return stmt, nil
}

func (p *parser) parseJournal() (*Journal, error) {
	tok := p.cur
	p.next() // JOURNAL

	stmt := &Journal{position: position{p.pos(tok)}}
	if p.cur.Type == STRING {
		stmt.Account = stripQuotes(p.cur.String(p.source))
		p.next()
	}

	summary, err := p.parseAtSummary()
	if err != nil {
		return nil, err
	}
	stmt.Summary = summary

	if p.cur.Type == FROM {
		from, err := p.parseFrom()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}
	return stmt, nil
}

func (p *parser) parsePrint() (*Print, error) {
	tok := p.cur
	p.next() // PRINT

	stmt := &Print{position: position{p.pos(tok)}}
	if p.cur.Type == FROM {
		from, err := p.parseFrom()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}
	return stmt, nil
}

func (p *parser) parseAtSummary() (string, error) {
	if !p.accept(AT) {
		return "", nil
	}
	tok, err := p.expect(IDENT, "AT")
	if err != nil {
		return "", err
	}
	return tok.String(p.source), nil
}

func (p *parser) parseExprList() ([]Expr, error) {
	var exprs []Expr
	for {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)
		if !p.accept(COMMA) {
			break
		}
	}
	return exprs, nil
}

// Expression precedence, low to high: OR, AND, NOT, comparison, additive,
// multiplicative, unary, primary.

func (p *parser) parseExpr() (Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == OR {
		tok := p.cur
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{position: position{p.pos(tok)}, Op: OR, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == AND {
		tok := p.cur
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &Binary{position: position{p.pos(tok)}, Op: AND, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseNot() (Expr, error) {
	if p.cur.Type == NOT {
		tok := p.cur
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Unary{position: position{p.pos(tok)}, Op: NOT, X: x}, nil
	}
	return p.parseComparison()
}

// parseComparison parses a non-associative comparison: at most one comparison
// operator between two additive expressions.
func (p *parser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	switch p.cur.Type {
	case EQ, NE, LT, LTE, GT, GTE, TILDE, IN:
		tok := p.cur
		p.next()
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		return &Binary{position: position{p.pos(tok)}, Op: tok.Type, L: left, R: right}, nil
	}
	return left, nil
}

func (p *parser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == PLUS || p.cur.Type == MINUS {
		tok := p.cur
		p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &Binary{position: position{p.pos(tok)}, Op: tok.Type, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == ASTERISK || p.cur.Type == SLASH {
		tok := p.cur
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Binary{position: position{p.pos(tok)}, Op: tok.Type, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expr, error) {
	if p.cur.Type == MINUS || p.cur.Type == PLUS {
		tok := p.cur
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{position: position{p.pos(tok)}, Op: tok.Type, X: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	tok := p.cur
	switch tok.Type {
	case LPAREN:
		p.next()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(RPAREN, "parenthesized expression"); err != nil {
			return nil, err
		}
		return expr, nil

	case STRING:
		p.next()
		return &Str{position: position{p.pos(tok)}, Value: stripQuotes(tok.String(p.source))}, nil

	case INTEGER:
		p.next()
		value, err := strconv.ParseInt(tok.String(p.source), 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "invalid integer %q", tok.String(p.source))
		}
		return &Int{position: position{p.pos(tok)}, Value: value}, nil

	case DECIMAL:
		p.next()
		value, err := decimal.NewFromString(tok.String(p.source))
		if err != nil {
			return nil, p.errorf(tok, "invalid decimal %q", tok.String(p.source))
		}
		return &Dec{position: position{p.pos(tok)}, Value: value}, nil

	case DATE:
		p.next()
		date := &ast.Date{}
		if err := date.Capture([]string{tok.String(p.source)}); err != nil {
			return nil, p.errorf(tok, "invalid date %q", tok.String(p.source))
		}
		return &DateLit{position: position{p.pos(tok)}, Value: date}, nil

	case TRUE, FALSE:
		p.next()
		return &Bool{position: position{p.pos(tok)}, Value: tok.Type == TRUE}, nil

	case NULL:
		p.next()
		return &Null{position: position{p.pos(tok)}}, nil

	case IDENT:
		p.next()
		name := tok.String(p.source)
		if p.cur.Type != LPAREN {
			return &Ident{position: position{p.pos(tok)}, Name: name}, nil
		}
		p.next() // (
		call := &Call{position: position{p.pos(tok)}, Func: name}
		if p.cur.Type != RPAREN {
			args, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			call.Args = args
		}
		if _, err := p.expect(RPAREN, "function call"); err != nil {
			return nil, err
		}
		return call, nil
	}

	return nil, p.errorf(tok, "expected expression, found %s", p.describe(tok))
}

// parseDate parses a DATE token into an ast.Date, validating its value.
func (p *parser) parseDate() (*ast.Date, error) {
	tok, err := p.expect(DATE, "date")
	if err != nil {
		return nil, err
	}
	date := &ast.Date{}
	if err := date.Capture([]string{tok.String(p.source)}); err != nil {
		return nil, p.errorf(tok, "invalid date %q", tok.String(p.source))
	}
	return date, nil
}

// startsExpr reports whether the current token can begin an expression. Used
// to decide whether a FROM clause has a filter expression before its
// OPEN/CLOSE/CLEAR transforms.
func (p *parser) startsExpr() bool {
	switch p.cur.Type {
	case LPAREN, STRING, INTEGER, DECIMAL, DATE, TRUE, FALSE, NULL, IDENT, NOT, MINUS, PLUS:
		return true
	}
	return false
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return s
}
