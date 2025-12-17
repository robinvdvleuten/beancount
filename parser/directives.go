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
		Pos:     pos,
		Date:    date,
		Account: account,
		Amount:  amount,
	}
	bal.Metadata = p.parseMetadata()

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
		Pos:     pos,
		Date:    date,
		Account: account,
	}

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
		method, _, err := p.parseString()
		if err != nil {
			return nil, err
		}
		open.BookingMethod = method
	}

	open.Metadata = p.parseMetadata()

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
		Pos:     pos,
		Date:    date,
		Account: account,
	}
	close.Metadata = p.parseMetadata()

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
		Pos:      pos,
		Date:     date,
		Currency: currency,
	}
	commodity.Metadata = p.parseMetadata()

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
		Pos:        pos,
		Date:       date,
		Account:    account,
		AccountPad: accountPad,
	}
	pad.Metadata = p.parseMetadata()

	return pad, nil
}

// parseNote parses: DATE note ACCOUNT STRING
func (p *Parser) parseNote(pos ast.Position, date *ast.Date) (*ast.Note, error) {
	p.consume(NOTE, "expected 'note'")

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	description, descMeta, err := p.parseString()
	if err != nil {
		return nil, err
	}

	note := &ast.Note{
		Pos:                pos,
		Date:               date,
		Account:            account,
		Description:        description,
		DescriptionEscapes: descMeta,
	}
	note.Metadata = p.parseMetadata()

	return note, nil
}

// parseDocument parses: DATE document ACCOUNT STRING
func (p *Parser) parseDocument(pos ast.Position, date *ast.Date) (*ast.Document, error) {
	p.consume(DOCUMENT, "expected 'document'")

	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}

	path, pathMeta, err := p.parseString()
	if err != nil {
		return nil, err
	}

	doc := &ast.Document{
		Pos:            pos,
		Date:           date,
		Account:        account,
		PathToDocument: path,
		PathEscapes:    pathMeta,
	}
	doc.Metadata = p.parseMetadata()

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
		Pos:       pos,
		Date:      date,
		Commodity: commodity,
		Amount:    amount,
	}
	price.Metadata = p.parseMetadata()

	return price, nil
}

// parseEvent parses: DATE event STRING STRING
func (p *Parser) parseEvent(pos ast.Position, date *ast.Date) (*ast.Event, error) {
	p.consume(EVENT, "expected 'event'")

	name, nameMeta, err := p.parseString()
	if err != nil {
		return nil, err
	}

	value, valueMeta, err := p.parseString()
	if err != nil {
		return nil, err
	}

	event := &ast.Event{
		Pos:          pos,
		Date:         date,
		Name:         name,
		NameEscapes:  nameMeta,
		Value:        value,
		ValueEscapes: valueMeta,
	}
	event.Metadata = p.parseMetadata()

	return event, nil
}

// parseCustom parses: DATE custom STRING VALUE*
// where VALUE can be STRING | BOOL | AMOUNT | NUMBER
func (p *Parser) parseCustom(pos ast.Position, date *ast.Date) (*ast.Custom, error) {
	p.consume(CUSTOM, "expected 'custom'")

	customType, typeMeta, err := p.parseString()
	if err != nil {
		return nil, err
	}

	custom := &ast.Custom{
		Pos:         pos,
		Date:        date,
		Type:        customType,
		TypeEscapes: typeMeta,
		Values:      make([]*ast.CustomValue, 0, 4),
	}

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
			unquoted, _, err := p.unquoteStringWithMetadata(rawValue)
			if err != nil {
				return nil, p.errorAtToken(tok, "invalid string literal: %v", err)
			}
			s := p.interner.Intern(unquoted)
			val = &ast.CustomValue{String: &s}

		case IDENT:
			// Could be TRUE, FALSE, or a currency in an amount
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
				// This might be part of an amount, but we already consumed it
				// For simplicity, treat it as a number string
				val = &ast.CustomValue{Number: &ident}
			}

		case NUMBER:
			// Could be standalone number or part of amount
			p.advance()
			numStr := tok.String(p.source)

			// Check if followed by currency
			if p.check(IDENT) {
				currTok := p.advance()
				currency := p.interner.InternBytes(currTok.Bytes(p.source))
				amt := &ast.Amount{
					Value:    numStr,
					Currency: currency,
				}
				val = &ast.CustomValue{Amount: amt}
			} else {
				val = &ast.CustomValue{Number: &numStr}
			}

		default:
			// Unknown token, skip
			p.advance()
			continue
		}

		// val is always non-nil here since all cases set it (default continues)
		custom.Values = append(custom.Values, val)
	}

	custom.Metadata = p.parseMetadata()

	return custom, nil
}
