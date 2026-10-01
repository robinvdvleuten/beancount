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
	// prevEnd is the end offset of the last consumed token, where the
	// node being built ends.
	prevEnd int
}

func newParser(source []byte) *parser {
	p := &parser{source: source, lexer: NewLexer(source)}
	p.next()
	return p
}

func (p *parser) next() {
	p.prevEnd = p.cur.End
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

// node positions a node whose source text runs from start to the end of
// the last consumed token.
func (p *parser) node(start int) position {
	return position{start: start, end: p.prevEnd}
}

func (p *parser) errorf(tok Token, format string, args ...any) *ParseError {
	return &ParseError{Pos: p.pos(tok), Message: fmt.Sprintf(format, args...)}
}

// nameErrorf reports tok where a name or an expression was expected.
// beanquery's parser reads a reserved keyword there as a name before
// rejecting it, so it fails after the keyword.
func (p *parser) nameErrorf(tok Token, format string, args ...any) *ParseError {
	err := p.errorf(tok, format, args...)
	if _, ok := reservedKeywords[tok.Type]; ok {
		err.Pos.Offset = tok.End
		err.Pos.Column += tok.End - tok.Start
	}
	return err
}

// isName reports whether tok can be a name: an identifier, or a keyword
// beanquery does not reserve (AT, OPEN, CLOSE, CLEAR and ON), which the
// parser reads as a clause only where one starts.
func isName(tok Token) bool {
	_, unreserved := unreservedKeywords[tok.Type]
	return tok.Type == IDENT || unreserved
}

// clauseErrorf reports a clause, starting at the keyword clause, that
// cannot be finished. beanquery's parser backs out of such a clause and
// fails at its first keyword.
func (p *parser) clauseErrorf(clause Token, name string) *ParseError {
	return p.errorf(clause, "incomplete %s clause, found %s", name, p.describe(p.cur))
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

	sel := &Select{position: position{start: tok.Start}}
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
		clause := p.cur
		p.next()
		if !p.accept(BY) {
			return nil, p.clauseErrorf(clause, "GROUP BY")
		}
		for {
			item, err := p.parseClauseItem()
			if err != nil {
				return nil, err
			}
			sel.GroupBy = append(sel.GroupBy, item)
			if !p.accept(COMMA) {
				break
			}
		}
		if p.accept(HAVING) {
			having, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			sel.Having = having
		}
	}

	if p.cur.Type == ORDER {
		clause := p.cur
		p.next()
		if !p.accept(BY) {
			return nil, p.clauseErrorf(clause, "ORDER BY")
		}
		for {
			item, err := p.parseClauseItem()
			if err != nil {
				return nil, err
			}
			desc := p.accept(DESC)
			if !desc {
				p.accept(ASC)
			}
			sel.OrderBy = append(sel.OrderBy, OrderTerm{Expr: item, Desc: desc})
			if !p.accept(COMMA) {
				break
			}
		}
	}

	if p.cur.Type == PIVOT {
		clause := p.cur
		p.next()
		if !p.accept(BY) {
			return nil, p.clauseErrorf(clause, "PIVOT BY")
		}
		// Like beanquery's grammar, PIVOT BY takes exactly two items, each
		// an integer or a column name.
		for i := range 2 {
			if i > 0 {
				if _, err := p.expect(COMMA, "PIVOT BY"); err != nil {
					return nil, err
				}
			}
			item, err := p.parsePivotItem()
			if err != nil {
				return nil, err
			}
			sel.PivotBy = append(sel.PivotBy, item)
		}
	}

	if p.cur.Type == LIMIT {
		clause := p.cur
		p.next()
		// Numbers carry no sign, so a negative limit never reaches the
		// executor: LIMIT -1 is a syntax error, as in beanquery.
		tok := p.cur
		if tok.Type == DECIMAL && isDigit(p.source[tok.Start]) {
			// The integer rule takes the digits before the dot, and the
			// statement fails at the dot: LIMIT 1.5 is a syntax error there.
			return nil, p.dotErrorf(tok, "expected an integer LIMIT, found %s", p.describe(tok))
		}
		if tok.Type != INTEGER {
			return nil, p.clauseErrorf(clause, "LIMIT")
		}
		p.next()
		limit, err := strconv.ParseInt(tok.String(p.source), 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "invalid LIMIT value %q", tok.String(p.source))
		}
		sel.Limit = &limit
	}

	sel.end = p.prevEnd
	return sel, nil
}

func (p *parser) parseTarget() (Target, error) {
	expr, err := p.parseExpr()
	if err != nil {
		return Target{}, err
	}
	start, end := expr.Span()
	target := Target{Expr: expr, Text: strings.TrimSpace(string(p.source[start:end]))}
	if p.accept(AS) {
		// Like bean-query, a double-quoted alias is kept as written and
		// any other is lowercased.
		if p.cur.Type == STRING && p.source[p.cur.Start] == '"' {
			target.As = stripQuotes(p.cur.String(p.source))
			p.next()
			return target, nil
		}
		tok := p.cur
		if !isName(tok) {
			return Target{}, p.nameErrorf(tok, "expected target alias, found %s", p.describe(tok))
		}
		p.next()
		target.As = strings.ToLower(tok.String(p.source))
	}
	return target, nil
}

// parseFrom parses a FROM clause: an optional entry filter expression
// followed by optional OPEN ON, CLOSE [ON], and CLEAR transforms, in that
// order (matching the official grammar).
func (p *parser) parseFrom() (*From, error) {
	tok := p.cur
	p.next() // FROM

	from := &From{position: position{start: tok.Start}}

	if p.startsExpr() {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		from.Expr = expr
	}

	if p.cur.Type == OPEN {
		clause := p.cur
		p.next()
		if !p.accept(ON) || p.cur.Type != DATE {
			// With no expression before it, beanquery has committed to
			// the OPEN transform (a cut) and fails at what follows OPEN.
			if from.Expr == nil {
				return nil, p.errorf(p.cur, "expected ON and a date after OPEN, found %s", p.describe(p.cur))
			}
			return nil, p.clauseErrorf(clause, "OPEN ON")
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
		if p.cur.Type == ON {
			clause := p.cur
			p.next()
			if p.cur.Type != DATE {
				return nil, p.clauseErrorf(clause, "CLOSE ON")
			}
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
		return nil, p.nameErrorf(p.cur, "expected expression, OPEN, CLOSE or CLEAR after FROM, found %s", p.describe(p.cur))
	}

	from.end = p.prevEnd
	return from, nil
}

func (p *parser) parseBalances() (*Balances, error) {
	tok := p.cur
	p.next() // BALANCES

	stmt := &Balances{position: position{start: tok.Start}}
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

	// Unlike JOURNAL, the official grammar gives BALANCES a WHERE clause.
	if p.accept(WHERE) {
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = expr
	}
	stmt.end = p.prevEnd
	return stmt, nil
}

func (p *parser) parseJournal() (*Journal, error) {
	tok := p.cur
	p.next() // JOURNAL

	stmt := &Journal{position: position{start: tok.Start}}
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
	stmt.end = p.prevEnd
	return stmt, nil
}

func (p *parser) parsePrint() (*Print, error) {
	tok := p.cur
	p.next() // PRINT

	stmt := &Print{position: position{start: tok.Start}}
	if p.cur.Type == FROM {
		from, err := p.parseFrom()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}
	stmt.end = p.prevEnd
	return stmt, nil
}

func (p *parser) parseAtSummary() (string, error) {
	if !p.accept(AT) {
		return "", nil
	}
	tok := p.cur
	if !isName(tok) {
		return "", p.errorf(tok, "expected IDENT in AT, found %s", p.describe(tok))
	}
	p.next()
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

// parsePivotItem parses a PIVOT BY item: a column index or a name.
func (p *parser) parsePivotItem() (Expr, error) {
	tok := p.cur
	switch tok.Type {
	case INTEGER:
		return p.parseClauseItem()
	case DECIMAL:
		if isDigit(p.source[tok.Start]) {
			return p.parseClauseItem() // fails at the dot
		}
	case IDENT, AT, OPEN, CLOSE, CLEAR, ON:
		p.next()
		return &Ident{position: p.node(tok.Start), Name: strings.ToLower(tok.String(p.source))}, nil
	case STRING:
		// beanquery's identifier rule takes a double-quoted name too.
		if p.source[tok.Start] == '"' {
			p.next()
			return &Ident{position: p.node(tok.Start), Name: stripQuotes(tok.String(p.source))}, nil
		}
	}
	return nil, p.nameErrorf(tok, "expected a PIVOT BY column, found %s", p.describe(tok))
}

// parseClauseItem parses a GROUP BY or ORDER BY item. Like beanquery's
// grammar, which tries its integer rule first, an item that starts with an
// integer is a column index and ends there: GROUP BY 1 + 1 is a syntax
// error at the +.
func (p *parser) parseClauseItem() (Expr, error) {
	tok := p.cur
	switch {
	case tok.Type == INTEGER:
		p.next()
		value, err := strconv.ParseInt(tok.String(p.source), 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "invalid integer %q", tok.String(p.source))
		}
		return &ColumnIndex{position: p.node(tok.Start), Value: value}, nil
	case tok.Type == DECIMAL && isDigit(p.source[tok.Start]):
		// The integer rule takes the digits before the dot, and the item
		// fails at the dot: GROUP BY 1.0 is a syntax error.
		return nil, p.dotErrorf(tok, "expected an integer column index, found %s", p.describe(tok))
	}
	return p.parseExpr()
}

// dotErrorf reports the decimal tok, which starts with digits, at its dot,
// where beanquery's integer rule leaves off.
func (p *parser) dotErrorf(tok Token, format string, args ...any) *ParseError {
	digits := strings.IndexByte(tok.String(p.source), '.')
	dot := Token{Type: DECIMAL, Start: tok.Start + digits, End: tok.End, Line: tok.Line, Column: tok.Column + digits}
	return p.errorf(dot, format, args...)
}

// Expression precedence, low to high: OR, AND, NOT, comparison, additive,
// multiplicative, unary, primary.

func (p *parser) parseExpr() (Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Expr, error) {
	start := p.cur.Start
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == OR {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{position: p.node(start), Op: OR, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	start := p.cur.Start
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.cur.Type == AND {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &Binary{position: p.node(start), Op: AND, L: left, R: right}
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
		return &Unary{position: p.node(tok.Start), Op: NOT, X: x}, nil
	}
	return p.parseComparison()
}

// parseComparison parses a non-associative comparison: at most one comparison
// operator between two additive expressions.
func (p *parser) parseComparison() (Expr, error) {
	start := p.cur.Start
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
		return &Binary{position: p.node(start), Op: tok.Type, L: left, R: right}, nil
	case IS:
		p.next()
		negated := p.accept(NOT)
		if _, err := p.expect(NULL, "IS NULL"); err != nil {
			return nil, err
		}
		return &IsNull{position: p.node(start), X: left, Not: negated}, nil
	}
	return left, nil
}

func (p *parser) parseAdditive() (Expr, error) {
	start := p.cur.Start
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
		left = &Binary{position: p.node(start), Op: tok.Type, L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (Expr, error) {
	start := p.cur.Start
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
		left = &Binary{position: p.node(start), Op: tok.Type, L: left, R: right}
	}
	return left, nil
}

// parseUnary parses beanquery's factor: a unary minus applies to a factor,
// so -(1 + 2) and - -1 are negations, while a unary plus takes only an
// atom (no parenthesized expression) and leaves no node of its own.
// Numbers carry no sign, so number -1 is a subtraction.
func (p *parser) parseUnary() (Expr, error) {
	switch p.cur.Type {
	case MINUS:
		tok := p.cur
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{position: p.node(tok.Start), Op: MINUS, X: x}, nil
	case PLUS:
		p.next()
		if p.cur.Type == LPAREN && !p.listAhead() {
			// A unary plus takes an atom, of which only a list constant
			// starts with (, so beanquery fails where the list does:
			// after a first literal it expects a comma behind.
			p.next()
			if _, ok := literalTypes[p.cur.Type]; ok {
				p.next()
			}
			return nil, p.errorf(p.cur, "expected an atom after unary plus, found %s", p.describe(p.cur))
		}
	}
	return p.parsePrimary()
}

// listAhead reports whether the ( at the current token starts a list
// constant: beanquery's list rule looks ahead for a literal and a comma
// after it, and a parenthesized expression is read otherwise.
func (p *parser) listAhead() bool {
	lexer := *p.lexer
	if _, ok := literalTypes[lexer.Next().Type]; !ok {
		return false
	}
	return lexer.Next().Type == COMMA
}

// parseList parses a list constant, its ( the current token: literals and
// empty slots separated by commas, of which beanquery keeps the literals
// but a NULL after the first.
func (p *parser) parseList() (Expr, error) {
	start := p.cur.Start
	p.next() // (
	list := &List{}
	for {
		if _, ok := literalTypes[p.cur.Type]; ok {
			item, err := p.parseLiteral()
			if err != nil {
				return nil, err
			}
			if _, null := item.(*Null); !null || len(list.Items) == 0 {
				list.Items = append(list.Items, item)
			}
		}
		if !p.accept(COMMA) {
			break
		}
	}
	if _, err := p.expect(RPAREN, "list"); err != nil {
		return nil, err
	}
	list.position = p.node(start)
	return list, nil
}

// literalTypes are the tokens of beanquery's literal rule.
var literalTypes = map[TokenType]struct{}{
	INTEGER: {}, DECIMAL: {}, DATE: {}, STRING: {}, NULL: {}, TRUE: {}, FALSE: {},
}

func (p *parser) parsePrimary() (Expr, error) {
	tok := p.cur
	if _, ok := literalTypes[tok.Type]; ok {
		return p.parseLiteral()
	}
	switch tok.Type {
	case LPAREN:
		if p.listAhead() {
			return p.parseList()
		}
		p.next()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(RPAREN, "parenthesized expression"); err != nil {
			return nil, err
		}
		return expr, nil

	case IDENT, AT, OPEN, CLOSE, CLEAR, ON:
		p.next()
		// Identifiers are case-insensitive: bean-query lower-cases them.
		name := strings.ToLower(tok.String(p.source))
		if p.cur.Type != LPAREN {
			return &Ident{position: p.node(tok.Start), Name: name}, nil
		}
		p.next() // (
		call := &Call{Func: name}
		if p.cur.Type == ASTERISK {
			// Like beanquery's grammar, any function takes * as its only
			// argument; the compiler decides which accept it.
			star := p.cur
			p.next()
			if p.cur.Type != RPAREN {
				// beanquery backs out of the * alternative to the start
				// of the *.
				return nil, p.errorf(star, "expected ) after *, found %s", p.describe(p.cur))
			}
			call.Args = []Expr{&Asterisk{position: p.node(star.Start)}}
		} else if p.cur.Type != RPAREN {
			args, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			call.Args = args
		}
		if _, err := p.expect(RPAREN, "function call"); err != nil {
			return nil, err
		}
		call.position = p.node(tok.Start)
		return call, nil
	}

	return nil, p.nameErrorf(tok, "expected expression, found %s", p.describe(tok))
}

// parseLiteral parses the literal at the current token, one of
// literalTypes.
func (p *parser) parseLiteral() (Expr, error) {
	tok := p.cur
	switch tok.Type {
	case STRING:
		p.next()
		return &Str{position: p.node(tok.Start), Value: stripQuotes(tok.String(p.source))}, nil

	case INTEGER:
		p.next()
		value, err := strconv.ParseInt(tok.String(p.source), 10, 64)
		if err != nil {
			return nil, p.errorf(tok, "invalid integer %q", tok.String(p.source))
		}
		return &Int{position: p.node(tok.Start), Value: value}, nil

	case DECIMAL:
		p.next()
		value, err := decimal.NewFromString(numberText(tok.String(p.source)))
		if err != nil {
			return nil, p.errorf(tok, "invalid decimal %q", tok.String(p.source))
		}
		return &Dec{position: p.node(tok.Start), Value: value}, nil

	case DATE:
		p.next()
		date := &ast.Date{}
		if err := date.Capture([]string{tok.String(p.source)}); err != nil {
			return nil, p.errorf(tok, "invalid date %q", tok.String(p.source))
		}
		return &DateLit{position: p.node(tok.Start), Value: date}, nil

	case TRUE, FALSE:
		p.next()
		return &Bool{position: p.node(tok.Start), Value: tok.Type == TRUE}, nil

	case NULL:
		p.next()
		return &Null{position: p.node(tok.Start)}, nil
	}
	return nil, p.errorf(tok, "expected a literal, found %s", p.describe(tok))
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
// OPEN/CLOSE/CLEAR transforms: like beanquery's grammar, which tries the
// transforms first and commits to one once its keyword matches, a FROM
// that starts with OPEN, CLOSE or CLEAR has none, while AT and ON are
// names there.
func (p *parser) startsExpr() bool {
	switch p.cur.Type {
	case LPAREN, STRING, INTEGER, DECIMAL, DATE, TRUE, FALSE, NULL, IDENT, AT, ON, NOT, MINUS, PLUS:
		return true
	}
	return false
}

// numberText rewrites a decimal token into a form decimal parses, keeping
// its digits: 2. is 2 and .5 is 0.5.
func numberText(text string) string {
	text = strings.TrimSuffix(text, ".")
	if rest, ok := strings.CutPrefix(text, "."); ok {
		return "0." + rest
	}
	return text
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return s
}
