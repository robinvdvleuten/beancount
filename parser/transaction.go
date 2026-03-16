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

	if p.match(TXN) {
		// Explicit 'txn' keyword defaults to cleared (*) and does not allow
		// an additional flag token on the same line.
		if p.peek().Line == pos.Line && (p.check(ASTERISK) || p.check(EXCLAIM)) {
			tok := p.peek()
			return nil, p.errorAtToken(tok, "unexpected token %s %q", tok.Type, tok.String(p.source))
		}
		txn.Flag = "*"
	} else if p.match(ASTERISK) {
		txn.Flag = "*"
	} else if p.match(EXCLAIM) {
		txn.Flag = "!"
	} else if p.check(STRING) {
		// Padding transaction (no flag, starts with string)
		// This is allowed in some cases
		txn.Flag = "P"
	} else {
		return nil, p.error("expected transaction flag (* or !) or 'txn'")
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

	postings, err := p.parsePostingBlock()
	if err != nil {
		return err
	}
	txn.Postings = postings
	return nil
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

// parsePostingBlock parses all postings in the transaction's indented body.
func (p *Parser) parsePostingBlock() ([]*ast.Posting, error) {
	postings := make([]*ast.Posting, 0, 4)

	for !p.isAtEnd() {
		tok := p.peek()

		if tok.Type == NEWLINE {
			if !p.shouldConsumeIndentedBlankLine() {
				return postings, nil
			}
			p.advance()
			continue
		}

		if tok.Column <= 1 {
			return postings, nil
		}

		if tok.Type == COMMENT {
			p.advance()
			continue
		}

		if !p.isPostingStartToken(tok) {
			return postings, nil
		}

		posting, err := p.parsePosting()
		if err != nil {
			return nil, err
		}

		postings = append(postings, posting)
	}

	return postings, nil
}

func (p *Parser) startsIndentedMetadataLine() bool {
	if p.isAtEnd() {
		return false
	}
	tok := p.peek()
	return tok.Type != NEWLINE && tok.Column > 1 && p.isMetadataKeyStart(tok)
}

func (p *Parser) shouldConsumeIndentedBlankLine() bool {
	nextTok := p.peekAhead(1)
	return nextTok.Type != EOF && nextTok.Column > 1
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
	postingLine := p.peek().Line

	posting := &ast.Posting{}

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

	// Optional amount (either NUMBER or parenthesized expression)
	hasAmount := p.check(NUMBER) || p.check(EXPRESSION) || p.isExpressionStartToken(p.peek())
	if hasAmount {
		amount, err := p.parseAmount()
		if err != nil {
			return nil, err
		}
		posting.Amount = amount
	}

	// Optional cost specification
	if p.check(LBRACE) || p.check(LDBRACE) {
		cost, err := p.parseCost()
		if err != nil {
			return nil, err
		}
		posting.Cost = cost
	}

	// Optional price (@ or @@)
	if p.match(ATAT) {
		// Total price (@@)
		posting.PriceTotal = true

		// Parse price amount
		amount, err := p.parseAmount()
		if err != nil {
			return nil, err
		}
		posting.Price = amount
	} else if p.match(AT) {
		// Unit price (@)
		posting.PriceTotal = false

		// Parse price amount
		amount, err := p.parseAmount()
		if err != nil {
			return nil, err
		}
		posting.Price = amount
	}

	metadata, err := p.finishMetadataLine(posting, postingLine)
	if err != nil {
		return nil, err
	}
	posting.Metadata = metadata

	return posting, nil
}
