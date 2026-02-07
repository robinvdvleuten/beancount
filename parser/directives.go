package parser

import "github.com/robinvdvleuten/beancount/ast"

// Directive parsers for all non-transaction directives.
// These are relatively simple parsers with deterministic structure.

// parseBalance parses: DATE balance ACCOUNT AMOUNT
func (p *Parser) parseBalance(pos ast.Position, date *ast.Date) (*ast.Balance, error) {
	p.consume(BALANCE, "expected 'balance'")

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	amount, err := p.parseAmount()
	if err != nil {
		return nil, err
	}

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
	p.consume(OPEN, "expected 'open'")

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	open := &ast.Open{
		Account: account,
	}
	open.SetPosition(pos)
	open.SetDate(date)

	// Optional constraint currencies
	if p.check(IDENT) {
		open.ConstraintCurrencies = make([]string, 0, 2)
		currency, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		open.ConstraintCurrencies = append(open.ConstraintCurrencies, currency)

		// Additional currencies separated by commas
		for p.match(COMMA) {
			currency, err := p.parseIdent()
			if err != nil {
				return nil, err
			}
			open.ConstraintCurrencies = append(open.ConstraintCurrencies, currency)
		}
	}

	// Optional booking method
	if p.check(STRING) {
		method, err := p.parseString()
		if err != nil {
			return nil, err
		}
		open.BookingMethod = method.Value
	} else if !p.isAtEnd() && p.peek().Type == ILLEGAL && p.pos < len(p.source) && p.source[p.peek().Start] == '"' {
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
	p.consume(CLOSE, "expected 'close'")

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
	p.consume(COMMODITY, "expected 'commodity'")

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
	p.consume(PAD, "expected 'pad'")

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
	p.consume(NOTE, "expected 'note'")

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
	p.consume(DOCUMENT, "expected 'document'")

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
	p.consume(PRICE, "expected 'price'")

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
	p.consume(EVENT, "expected 'event'")

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

// parseCustom parses: DATE custom STRING VALUE*
// where VALUE can be STRING | BOOL | AMOUNT | NUMBER
func (p *Parser) parseCustom(pos ast.Position, date *ast.Date) (*ast.Custom, error) {
	p.consume(CUSTOM, "expected 'custom'")

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

		var val *ast.CustomValue

		switch tok.Type {
		case STRING:
			p.advance()
			rawValue := tok.String(p.source)
			unquoted, err := p.unquoteString(rawValue)
			if err != nil {
				return nil, p.errorAtToken(tok, "invalid string literal: %v", err)
			}
			s := p.internString(unquoted)
			val = &ast.CustomValue{String: &s}

		case IDENT:
			// Could be TRUE, FALSE, or a currency identifier
			p.advance()
			ident := tok.String(p.source)

			switch ident {
			case "TRUE":
				boolStr := "TRUE"
				val = &ast.CustomValue{BooleanValue: &boolStr}
			case "FALSE":
				boolStr := "FALSE"
				val = &ast.CustomValue{BooleanValue: &boolStr}
			default:
				// Non-boolean identifier (e.g., a currency like USD or HOOL)
				val = &ast.CustomValue{String: &ident}
			}

		case NUMBER:
			// Could be standalone number or part of amount
			p.advance()
			numStr := tok.String(p.source)

			// Check if followed by currency on the same line
			if p.check(IDENT) && p.peek().Line == startLine {
				currTok := p.advance()
				currency := p.internCurrency(currTok)
				amt := &ast.Amount{
					Value:    numStr,
					Currency: currency,
				}
				val = &ast.CustomValue{Amount: amt}
			} else {
				val = &ast.CustomValue{Number: &numStr}
			}

		case ACCOUNT:
			// Account value (e.g., Expenses:Food)
			p.advance()
			acct := tok.String(p.source)
			val = &ast.CustomValue{String: &acct}

		default:
			// Stop on unexpected tokens (COMMENT, TAG, etc.)
			// val remains nil, causing the loop to exit below
		}

		if val == nil {
			// Default case: stop parsing values on unexpected tokens
			break
		}

		custom.Values = append(custom.Values, val)
	}

	if err := p.finishDirective(custom); err != nil {
		return nil, err
	}
	return custom, nil
}
