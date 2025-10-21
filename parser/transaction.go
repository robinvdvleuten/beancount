package parser

import "github.com/robinvdvleuten/beancount/ast"

// Transaction parsing - the most complex directive type.
// Transactions have postings, which are indented on subsequent lines.

// parseTransaction parses a transaction:
// DATE [txn] FLAG [PAYEE] NARRATION [TAG]* [LINK]*
//
//	POSTING*
func (p *Parser) parseTransaction(pos ast.Position, date *ast.Date) (*ast.Transaction, error) {
	txn := &ast.Transaction{
		Pos:  pos,
		Date: date,
	}

	// Handle optional 'txn' keyword and flag
	// Valid forms:
	//   DATE txn * "narration"
	//   DATE txn ! "narration"
	//   DATE * "narration"
	//   DATE ! "narration"

	if p.match(TXN) {
		// Explicit 'txn' keyword
		if p.match(ASTERISK) {
			txn.Flag = "*"
		} else if p.match(EXCLAIM) {
			txn.Flag = "!"
		} else {
			return nil, p.error("expected flag (* or !) after 'txn'")
		}
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

	// Parse transaction-level metadata
	txn.Metadata = p.parseMetadata()

	// Parse postings (indented lines)
	postings, err := p.parsePostings()
	if err != nil {
		return nil, err
	}
	txn.Postings = postings

	return txn, nil
}

// parsePostings parses all postings for a transaction.
// Postings are indented lines following the transaction header.
func (p *Parser) parsePostings() ([]*ast.Posting, error) {
	postings := make([]*ast.Posting, 0, 4)

	// Postings must be indented (column > 1)
	// We detect them by checking if the next token is on a new line,
	// is indented, and looks like it could start a posting
	for !p.isAtEnd() {
		tok := p.peek()

		// Postings must be indented (not at column 1)
		// This distinguishes them from org-mode headers like "* Credit-Cards"
		if tok.Column <= 1 {
			break
		}

		// Posting can start with:
		// - Optional flag (* or !)
		// - Account name
		// If we see anything else, it's not a posting
		if tok.Type != ASTERISK && tok.Type != EXCLAIM && tok.Type != ACCOUNT {
			break
		}

		posting, err := p.parsePosting()
		if err != nil {
			return nil, err
		}

		postings = append(postings, posting)
	}

	return postings, nil
}

// parsePosting parses a single posting:
// [FLAG] ACCOUNT [AMOUNT] [COST] [PRICE]
//
//	[METADATA]*
func (p *Parser) parsePosting() (*ast.Posting, error) {
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

	// Optional amount (either NUMBER or expression starting with '(')
	tok := p.peek()
	hasAmount := p.check(NUMBER) || (tok.Start < len(p.source) && p.source[tok.Start] == '(')
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

	// Parse posting-level metadata
	posting.Metadata = p.parseMetadata()

	return posting, nil
}
