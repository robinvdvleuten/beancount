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
	hasNarration := false
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
			hasNarration = true
		} else {
			// One string: just narration
			txn.Narration = first
			hasNarration = true
		}
	}

	if !hasNarration {
		return nil, p.error("expected transaction payee or narration string")
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

	// Capture inline comment at end of transaction header line
	if !p.isAtEnd() && p.peek().Type == COMMENT && p.peek().Line == txn.Pos.Line {
		txn.SetComment(p.parseComment())
	}

	// Parse transaction-level metadata (only if on new line and properly indented)
	if !p.isAtEnd() && p.peek().Line > txn.Pos.Line && p.peek().Column > 1 {
		txn.Metadata = p.parseMetadataFromLine(txn.Pos.Line)
	}

	// Parse postings (indented lines)
	postings, err := p.parsePostings(txn.Pos.Line)
	if err != nil {
		return nil, err
	}
	txn.Postings = postings

	return txn, nil
}

// parsePostings parses all postings for a transaction.
// Postings are indented lines following the transaction header.
func (p *Parser) parsePostings(headerLine int) ([]*ast.Posting, error) {
	postings := make([]*ast.Posting, 0, 4)

	// Postings must be indented (column > 1)
	// We detect them by checking if the next token is on a new line,
	// is indented, and looks like it could start a posting
	for !p.isAtEnd() {
		tok := p.peek()

		if tok.Line == headerLine && (tok.Type == ASTERISK || tok.Type == EXCLAIM || tok.Type == ACCOUNT) {
			return nil, p.errorAtToken(tok, "postings must start on a new line")
		}

		// Skip blank lines (NEWLINE tokens) that might appear between postings
		// This handles cases like trailing whitespace that creates unwanted blank lines
		// Must check NEWLINE before column check since blank lines have column 1
		// HOWEVER: Don't consume a NEWLINE if it's followed by a directive or end-of-file,
		// as it's a blank line that should be preserved in the AST, not part of the transaction
		if tok.Type == NEWLINE {
			// Peek ahead to see what comes after the blank line
			nextIdx := p.pos + 1
			if nextIdx < len(p.tokens) {
				nextTok := p.tokens[nextIdx]
				// If the next token is at column <= 1 or is EOF, this blank line marks
				// the end of the transaction and should NOT be consumed here
				if nextTok.Column <= 1 || nextTok.Type == EOF {
					break // Don't consume this blank line - let the main parser handle it
				}
			}
			// Safe to consume - it's a blank line between postings
			p.advance() // consume the blank line and continue
			continue
		}

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
			if tok.Type == COMMENT {
				p.advance() // consume comment and continue
				continue
			}
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

	// Capture inline comment at end of posting line
	if !p.isAtEnd() && p.peek().Type == COMMENT && p.peek().Line == postingLine {
		posting.SetComment(p.parseComment())
	}

	// Parse posting-level metadata
	posting.Metadata = p.parseMetadataFromLine(postingLine)

	return posting, nil
}
