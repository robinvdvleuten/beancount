package parser

import "github.com/robinvdvleuten/beancount/ast"

// Transaction parsing - the most complex directive type.
// Transactions have postings, which are indented on subsequent lines.

// parseTransaction parses a transaction:
// DATE [txn] FLAG [PAYEE] NARRATION [TAG]* [LINK]*
//
//	POSTING*
func (p *Parser) parseTransaction(pos ast.Position, date *ast.Date) (*ast.Transaction, error) {
	txn := &ast.Transaction{}
	txn.SetPosition(pos)
	txn.SetDate(date)

	// Handle optional 'txn' keyword and flag
	// Valid forms:
	//   DATE txn
	//   DATE * "narration"
	//   DATE ! "narration"
	//   DATE P "narration"

	if p.match(TXN) {
		// Explicit 'txn' keyword defaults to cleared (*) and does not allow
		// an additional flag token on the same line.
		if p.peek().Line == pos.Line && (p.check(ASTERISK) || p.check(EXCLAIM) || p.check(FLAG)) {
			tok := p.peek()
			return nil, p.errorAtToken(tok, "unexpected token %s %q", tok.Type, tok.String(p.source))
		}
		txn.Flag = "*"
	} else if p.match(ASTERISK) {
		txn.Flag = "*"
	} else if p.match(EXCLAIM) {
		txn.Flag = "!"
	} else if p.check(FLAG) {
		txn.Flag = p.advance().String(p.source)
	} else {
		return nil, p.error("expected transaction flag or 'txn'")
	}

	// Parse payee and/or narration
	// If one string: it's the narration
	// If two strings: first is payee, second is narration
	if p.check(STRING) {
		first, err := p.parseString()
		if err != nil {
			return nil, err
		}

		if p.check(STRING) {
			// Two strings: payee and narration
			second, err := p.parseString()
			if err != nil {
				return nil, err
			}
			txn.Payee = first
			txn.Narration = second
		} else {
			// One string: just narration
			txn.Narration = first
		}
	}

	// Parse tags and links (can be intermixed)
	for p.check(TAG) || p.check(LINK) {
		if p.check(TAG) {
			tag, err := p.parseTag()
			if err != nil {
				return nil, err
			}
			txn.Tags = append(txn.Tags, tag)
		} else {
			link, err := p.parseLink()
			if err != nil {
				return nil, err
			}
			txn.Links = append(txn.Links, link)
		}
	}

	if err := p.finishLine(txn, txn.Position().Line); err != nil {
		return nil, err
	}

	if err := p.parseTransactionBody(txn); err != nil {
		return nil, err
	}

	return txn, nil
}

func (p *Parser) parseTransactionBody(txn *ast.Transaction) error {
	if err := p.parseLeadingTransactionMetadata(txn); err != nil {
		return err
	}

	return p.parsePostingBlock(txn)
}

func (p *Parser) parseLeadingTransactionMetadata(txn *ast.Transaction) error {
	if !p.startsIndentedMetadataLine() {
		return nil
	}

	metadata, err := p.parseMetadataFromLine(txn.Position().Line)
	if err != nil {
		return err
	}
	txn.Metadata = metadata
	return nil
}

// parsePostingBlock parses all postings and trivia in the transaction's indented body.
func (p *Parser) parsePostingBlock(txn *ast.Transaction) error {
	for !p.isAtEnd() {
		tok := p.peek()

		if tok.Type == NEWLINE {
			if len(txn.BodyItems) == 0 || !p.shouldConsumeIndentedBlankLine() {
				return nil
			}
			blankLine := p.parseBlankLine()
			txn.BodyItems = append(txn.BodyItems, ast.TransactionBodyItem{BlankLine: blankLine})
			continue
		}

		if tok.Column <= 1 {
			return nil
		}

		if tok.Type == COMMENT {
			comment := p.parseComment()
			txn.BodyItems = append(txn.BodyItems, ast.TransactionBodyItem{Comment: comment})
			continue
		}

		if !p.isPostingStartToken(tok) {
			return nil
		}

		posting, err := p.parsePosting()
		if err != nil {
			return err
		}

		txn.Postings = append(txn.Postings, posting)
		txn.BodyItems = append(txn.BodyItems, ast.TransactionBodyItem{Posting: posting})
	}

	return nil
}

func (p *Parser) startsIndentedMetadataLine() bool {
	if p.isAtEnd() {
		return false
	}
	tok := p.peek()
	return tok.Type != NEWLINE && tok.Column > 1 && p.isMetadataKeyStart(tok)
}

func (p *Parser) shouldConsumeIndentedBlankLine() bool {
	for i := 1; ; i++ {
		nextTok := p.peekAhead(i)
		if nextTok.Type == NEWLINE {
			continue
		}
		return nextTok.Type != EOF && nextTok.Column > 1 && p.isPostingStartToken(nextTok)
	}
}

func (p *Parser) isPostingStartToken(tok Token) bool {
	return tok.Type == ASTERISK || tok.Type == EXCLAIM || tok.Type == ACCOUNT
}

// parsePosting parses a single posting:
// [FLAG] ACCOUNT [AMOUNT] [COST] [PRICE]
//
//	[METADATA]*
func (p *Parser) parsePosting() (*ast.Posting, error) {
	// Track the posting's starting line for inline metadata detection
	postingTok := p.peek()
	postingLine := postingTok.Line

	posting := &ast.Posting{}
	posting.SetPosition(tokenPosition(postingTok, p.filename))

	// Optional flag
	if p.match(ASTERISK) {
		posting.Flag = "*"
	} else if p.match(EXCLAIM) {
		posting.Flag = "!"
	}

	// Account (required)
	account, err := p.parseAccount()
	if err != nil {
		return nil, err
	}
	posting.Account = account

	// Optional amount; number, currency, or both may be absent and are
	// completed by interpolation (official grammar: incomplete_amount).
	amount, err := p.parseIncompleteAmount(postingLine)
	if err != nil {
		return nil, err
	}
	posting.Amount = amount

	// Optional cost specification
	if p.check(LBRACE) || p.check(LDBRACE) {
		cost, err := p.parseCost()
		if err != nil {
			return nil, err
		}
		posting.Cost = cost
	}

	// Optional price (@ or @@). The annotation may be empty or partial
	// (bare "@", number-only, currency-only); interpolation completes it.
	if isTotal := p.match(ATAT); isTotal || p.match(AT) {
		posting.PriceTotal = isTotal

		price, err := p.parseIncompleteAmount(postingLine)
		if err != nil {
			return nil, err
		}
		if price == nil {
			price = &ast.Amount{}
		}
		posting.Price = price
	}

	metadata, err := p.finishMetadataLine(posting, postingLine)
	if err != nil {
		return nil, err
	}
	posting.Metadata = metadata

	return posting, nil
}
