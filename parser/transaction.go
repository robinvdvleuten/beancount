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
		if p.peek().Line == pos.Line && (p.check(ASTERISK) || p.check(EXCLAIM) || isFlagToken(p.peek())) {
			tok := p.peek()
			return nil, p.errorAtToken(tok, "unexpected token %s %q", tok.Type, tok.String(p.source))
		}
		txn.Flag = "*"
	} else if p.match(ASTERISK) {
		txn.Flag = "*"
	} else if p.match(EXCLAIM) {
		txn.Flag = "!"
	} else if isFlagToken(p.peek()) {
		txn.Flag = p.advance().String(p.source)
	} else {
		return nil, p.error("expected transaction flag or 'txn'")
	}

	// Parse payee and/or narration
	// If one string: it's the narration
	// If two strings: first is payee, second is narration
	// Strings belong to the transaction header, which ends at EOL in the
	// official grammar; never absorb strings from following lines.
	if p.check(STRING) && p.continuesPreviousLine() {
		first, err := p.parseString()
		if err != nil {
			return nil, err
		}

		if p.check(STRING) && p.continuesPreviousLine() {
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

	tags, links, err := p.parseTagsLinks()
	if err != nil {
		return nil, err
	}
	txn.Tags = tags
	txn.Links = links

	if err := p.finishHeader(txn, txn.Position().Offset); err != nil {
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

// parseLeadingTransactionMetadata parses the indented metadata lines and
// tag/link lines before the first posting, in any order.
func (p *Parser) parseLeadingTransactionMetadata(txn *ast.Transaction) error {
	for {
		switch {
		case p.startsIndentedMetadataLine():
			metadata, err := p.parseMetadata()
			if err != nil {
				return err
			}
			txn.Metadata = append(txn.Metadata, metadata...)
		case p.startsIndentedTagsLinksLine():
			line, err := p.parseTagsLinksLine()
			if err != nil {
				return err
			}
			txn.BodyTagsLinks = append(txn.BodyTagsLinks, line)
		default:
			return nil
		}
	}
}

// parseTagsLinks parses the tags and links (can be intermixed) that close
// the header of a transaction, note or document. They end at EOL in the
// official grammar, so it never absorbs tokens from following lines.
func (p *Parser) parseTagsLinks() ([]ast.Tag, []ast.Link, error) {
	var tags []ast.Tag
	var links []ast.Link
	for (p.check(TAG) || p.check(LINK)) && p.continuesPreviousLine() {
		if p.check(TAG) {
			tag, err := p.parseTag()
			if err != nil {
				return nil, nil, err
			}
			tags = append(tags, tag)
		} else {
			link, err := p.parseLink()
			if err != nil {
				return nil, nil, err
			}
			links = append(links, link)
		}
	}
	return tags, links, nil
}

func (p *Parser) startsIndentedTagsLinksLine() bool {
	tok := p.peek()
	return (tok.Type == TAG || tok.Type == LINK) && tok.Column > 1 && !p.continuesPreviousLine()
}

// parseTagsLinksLine parses an indented line of tags and links, with an
// optional trailing comment.
func (p *Parser) parseTagsLinksLine() (*ast.TagsLinks, error) {
	first := p.peek()
	line := &ast.TagsLinks{}
	line.SetPosition(tokenPosition(first, p.filename))
	for (p.check(TAG) || p.check(LINK)) && p.peek().Line == first.Line {
		if p.check(TAG) {
			tag, err := p.parseTag()
			if err != nil {
				return nil, err
			}
			line.Tags = append(line.Tags, tag)
		} else {
			link, err := p.parseLink()
			if err != nil {
				return nil, err
			}
			line.Links = append(line.Links, link)
		}
	}
	if p.check(COMMENT) && p.continuesPreviousLine() {
		p.advance()
	}
	if next := p.peek(); next.Type != EOF && next.Type != NEWLINE && next.Line == first.Line {
		return nil, p.errorAtToken(next, "unexpected content after tags and links")
	}
	return line, nil
}

// parsePostingBlock parses all postings and trivia in the transaction's indented body.
func (p *Parser) parsePostingBlock(txn *ast.Transaction) error {
	for !p.isAtEnd() {
		tok := p.peek()

		// Like beancount's, a blank or whitespace-only line ends the body:
		// an indented line after it is a syntax error at top level.
		if tok.Type == NEWLINE || tok.Column <= 1 {
			return nil
		}

		if tok.Type == COMMENT {
			comment := p.parseComment()
			txn.BodyItems = append(txn.BodyItems, ast.TransactionBodyItem{Comment: comment})
			continue
		}

		if tok.Type == TAG || tok.Type == LINK {
			if len(txn.Postings) > 0 {
				return p.errorAtToken(tok, "tags or links not allowed after first posting")
			}
			line, err := p.parseTagsLinksLine()
			if err != nil {
				return err
			}
			txn.BodyTagsLinks = append(txn.BodyTagsLinks, line)
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
	// Indented comment lines may lead the metadata line.
	n := p.indentedCommentsBeforeMetadata()
	tok := p.peekAhead(n)
	return tok.Type != NEWLINE && tok.Column > 1 && p.isMetadataKeyAt(n)
}

func (p *Parser) isPostingStartToken(tok Token) bool {
	return tok.Type == ASTERISK || tok.Type == EXCLAIM || tok.Type == ACCOUNT || isFlagToken(tok)
}

// isFlagToken reports whether tok is a flag other than * and !: a FLAG
// (#, &, ?, %) or a capital letter, beancount v3's CAPITAL, which the lexer
// reads as a one-letter IDENT since it is a currency too.
func isFlagToken(tok Token) bool {
	return tok.Type == FLAG || (tok.Type == IDENT && tok.Len() == 1)
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
	} else if isFlagToken(p.peek()) {
		posting.Flag = p.advance().String(p.source)
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

	if err := p.finishHeader(posting, postingTok.Start); err != nil {
		return nil, err
	}
	metadata, err := p.parseMetadata()
	if err != nil {
		return nil, err
	}
	posting.Metadata = metadata

	return posting, nil
}
