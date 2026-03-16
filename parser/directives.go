package parser

import "github.com/robinvdvleuten/beancount/ast"

// Directive parsers for all non-transaction directives.
// These are relatively simple parsers with deterministic structure.

// parseBalance parses: DATE balance ACCOUNT AMOUNT or
// DATE balance ACCOUNT NUMBER ~ NUMBER CURRENCY.
func (p *Parser) parseBalance(pos ast.Position, date *ast.Date) (*ast.Balance, error) {
	if err := p.consume(BALANCE, "expected 'balance'"); err != nil {
		return nil, err
	}

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	valueTok, isExpression, value, err := p.parseAmountValueToken()
	if err != nil {
		return nil, err
	}

	var tolerance *ast.Amount
	if p.match(TILDE) {
		toleranceTok, toleranceIsExpression, toleranceValue, err := p.parseAmountValueToken()
		if err != nil {
			return nil, err
		}
		if !p.check(IDENT) {
			return nil, p.errorAtEndOfPrevious("expected currency")
		}
		currTok := p.advance()
		amount := p.amountFromValueToken(valueTok, currTok, isExpression, value)
		tolerance = p.amountFromValueToken(toleranceTok, currTok, toleranceIsExpression, toleranceValue)

		bal := &ast.Balance{
			Account:   account,
			Amount:    amount,
			Tolerance: tolerance,
		}
		bal.SetPosition(pos)
		bal.SetDate(date)
		if err := p.finishDirective(bal); err != nil {
			return nil, err
		}
		return bal, nil
	}

	if !p.check(IDENT) {
		return nil, p.errorAtEndOfPrevious("expected currency")
	}
	currTok := p.advance()
	amount := p.amountFromValueToken(valueTok, currTok, isExpression, value)

	bal := &ast.Balance{
		Account: account,
		Amount:  amount,
	}
	bal.SetPosition(pos)
	bal.SetDate(date)
	if err := p.finishDirective(bal); err != nil {
		return nil, err
	}
	return bal, nil
}

// parseOpen parses: DATE open ACCOUNT [CURRENCY[,CURRENCY]*] ["BOOKING_METHOD"]
func (p *Parser) parseOpen(pos ast.Position, date *ast.Date) (*ast.Open, error) {
	if err := p.consume(OPEN, "expected 'open'"); err != nil {
		return nil, err
	}

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	open := &ast.Open{
		Account: account,
	}
	open.SetPosition(pos)
	open.SetDate(date)
	line := pos.Line

	// Optional constraint currencies
	if !p.isAtEnd() && p.peek().Line == line && p.check(IDENT) {
		open.ConstraintCurrencies = make([]string, 0, 2)
		currency, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		open.ConstraintCurrencies = append(open.ConstraintCurrencies, currency)

		// Additional currencies separated by commas
		for !p.isAtEnd() && p.peek().Line == line && p.match(COMMA) {
			currency, err := p.parseIdent()
			if err != nil {
				return nil, err
			}
			open.ConstraintCurrencies = append(open.ConstraintCurrencies, currency)
		}
	}

	// Optional booking method
	if !p.isAtEnd() && p.peek().Line == line && p.check(STRING) {
		method, err := p.parseString()
		if err != nil {
			return nil, err
		}
		open.BookingMethod = method.Value
	} else if !p.isAtEnd() && p.peek().Line == line && p.peek().Type == ILLEGAL && p.pos < len(p.source) && p.source[p.peek().Start] == '"' {
		tok := p.advance()
		return nil, p.errorAtToken(tok, "unterminated string")
	}

	if err := p.finishDirective(open); err != nil {
		return nil, err
	}
	return open, nil
}

// parseClose parses: DATE close ACCOUNT
func (p *Parser) parseClose(pos ast.Position, date *ast.Date) (*ast.Close, error) {
	if err := p.consume(CLOSE, "expected 'close'"); err != nil {
		return nil, err
	}

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	close := &ast.Close{
		Account: account,
	}
	close.SetPosition(pos)
	close.SetDate(date)
	if err := p.finishDirective(close); err != nil {
		return nil, err
	}
	return close, nil
}

// parseCommodity parses: DATE commodity CURRENCY
func (p *Parser) parseCommodity(pos ast.Position, date *ast.Date) (*ast.Commodity, error) {
	if err := p.consume(COMMODITY, "expected 'commodity'"); err != nil {
		return nil, err
	}

	currency, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	commodity := &ast.Commodity{
		Currency: currency,
	}
	commodity.SetPosition(pos)
	commodity.SetDate(date)
	if err := p.finishDirective(commodity); err != nil {
		return nil, err
	}
	return commodity, nil
}

// parsePad parses: DATE pad ACCOUNT ACCOUNT_PAD
func (p *Parser) parsePad(pos ast.Position, date *ast.Date) (*ast.Pad, error) {
	if err := p.consume(PAD, "expected 'pad'"); err != nil {
		return nil, err
	}

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	accountPad, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	pad := &ast.Pad{
		Account:    account,
		AccountPad: accountPad,
	}
	pad.SetPosition(pos)
	pad.SetDate(date)
	if err := p.finishDirective(pad); err != nil {
		return nil, err
	}
	return pad, nil
}

// parseNote parses: DATE note ACCOUNT STRING
func (p *Parser) parseNote(pos ast.Position, date *ast.Date) (*ast.Note, error) {
	if err := p.consume(NOTE, "expected 'note'"); err != nil {
		return nil, err
	}

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	description, err := p.parseString()
	if err != nil {
		return nil, err
	}

	note := &ast.Note{
		Account:     account,
		Description: description,
	}
	note.SetPosition(pos)
	note.SetDate(date)
	if err := p.finishDirective(note); err != nil {
		return nil, err
	}
	return note, nil
}

// parseDocument parses: DATE document ACCOUNT STRING
func (p *Parser) parseDocument(pos ast.Position, date *ast.Date) (*ast.Document, error) {
	if err := p.consume(DOCUMENT, "expected 'document'"); err != nil {
		return nil, err
	}

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	path, err := p.parseString()
	if err != nil {
		return nil, err
	}

	doc := &ast.Document{
		Account:        account,
		PathToDocument: path,
	}
	doc.SetPosition(pos)
	doc.SetDate(date)
	if err := p.finishDirective(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// parsePrice parses: DATE price CURRENCY AMOUNT
func (p *Parser) parsePrice(pos ast.Position, date *ast.Date) (*ast.Price, error) {
	if err := p.consume(PRICE, "expected 'price'"); err != nil {
		return nil, err
	}

	commodity, err := p.parseIdent()
	if err != nil {
		return nil, err
	}

	amount, err := p.parseAmount()
	if err != nil {
		return nil, err
	}

	price := &ast.Price{
		Commodity: commodity,
		Amount:    amount,
	}
	price.SetPosition(pos)
	price.SetDate(date)
	if err := p.finishDirective(price); err != nil {
		return nil, err
	}
	return price, nil
}

// parseEvent parses: DATE event STRING STRING
func (p *Parser) parseEvent(pos ast.Position, date *ast.Date) (*ast.Event, error) {
	if err := p.consume(EVENT, "expected 'event'"); err != nil {
		return nil, err
	}

	name, err := p.parseString()
	if err != nil {
		return nil, err
	}

	value, err := p.parseString()
	if err != nil {
		return nil, err
	}

	event := &ast.Event{
		Name:  name,
		Value: value,
	}
	event.SetPosition(pos)
	event.SetDate(date)
	if err := p.finishDirective(event); err != nil {
		return nil, err
	}
	return event, nil
}

// parseQuery parses: DATE query STRING STRING
func (p *Parser) parseQuery(pos ast.Position, date *ast.Date) (*ast.Query, error) {
	if err := p.consume(QUERY, "expected 'query'"); err != nil {
		return nil, err
	}

	name, err := p.parseString()
	if err != nil {
		return nil, err
	}

	queryString, err := p.parseString()
	if err != nil {
		return nil, err
	}

	query := &ast.Query{
		Name:        name,
		QueryString: queryString,
	}
	query.SetPosition(pos)
	query.SetDate(date)
	if err := p.finishDirective(query); err != nil {
		return nil, err
	}
	return query, nil
}

// parseCustom parses: DATE custom STRING VALUE*
func (p *Parser) parseCustom(pos ast.Position, date *ast.Date) (*ast.Custom, error) {
	if err := p.consume(CUSTOM, "expected 'custom'"); err != nil {
		return nil, err
	}

	customType, err := p.parseString()
	if err != nil {
		return nil, err
	}

	custom := &ast.Custom{
		Type:   customType,
		Values: make([]*ast.CustomValue, 0, 4),
	}
	custom.SetPosition(pos)
	custom.SetDate(date)

	// Parse custom values until we hit metadata or end of line
	startLine := p.peek().Line
	for !p.isAtEnd() && p.peek().Line == startLine {
		tok := p.peek()

		// Stop if we see a metadata key (IDENT followed by COLON)
		if tok.Type == IDENT && p.peekAhead(1).Type == COLON {
			break
		}

		val, err := p.parseCustomValue(startLine)
		if err != nil {
			return nil, err
		}

		if val == nil {
			break
		}

		custom.Values = append(custom.Values, val)
	}

	if err := p.finishDirective(custom); err != nil {
		return nil, err
	}
	return custom, nil
}
