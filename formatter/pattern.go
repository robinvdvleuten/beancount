package formatter

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/robinvdvleuten/beancount/ast"
)

// bean-format's line pattern: which lines it aligns, which it leaves as
// written, and how an aligned line splits into prefix, number and the rest
// (beancount/scripts/format.py). These functions take a source line and the
// item parsed from it and hold no formatter state.

// lineKind is what the formatter does with an item's line.
type lineKind int

const (
	// reconstructLine: the item has no source line of its own, so it is
	// printed from the AST.
	reconstructLine lineKind = iota
	// copyLine: bean-format leaves the line as written.
	copyLine
	// alignLine: bean-format pads the prefix and right-aligns the number.
	alignLine
)

// lineLayout is bean-format's reading of one item's line.
type lineLayout struct {
	kind lineKind

	// text is a copied line as the formatter writes it.
	text string

	// prefix, number and currency split an aligned line; rest is the
	// source text from the currency to the end of the line, or "" when
	// what follows the number is printed from the AST.
	prefix, number, currency, rest string

	// prefixWidth and numberWidth are the display widths bean-format
	// measures an aligned line by, its prefix as the source indents it.
	prefixWidth, numberWidth int
}

// plainLayout reads the line of an item with no number to align: a header,
// a metadata, tags or comment line. bean-format leaves it as written.
func plainLayout(line string, owned bool) lineLayout {
	if !owned {
		return lineLayout{kind: reconstructLine}
	}
	return lineLayout{kind: copyLine, text: line}
}

// alignedNumber is how bean-format's line pattern spells a number it
// aligns (NUMBER_RE and PARENTHESIZED_BINARY_OP_RE): an optional sign,
// digits with thousands commas and an optional fraction, or two such
// numbers and one operator in parentheses. Any other expression, repeated
// signs and nested parentheses do not match.
var alignedNumber = regexp.MustCompile(`^(?:\(` + numberPattern + `\s*[-+*/]\s*` + numberPattern + `\)|` + numberPattern + `)$`)

// numberPattern is bean-format's NUMBER_RE.
const numberPattern = `[-+]?\s*[\d,]+(?:\.\d*)?`

// numberText returns an amount's number as the source spells it.
func numberText(amount *ast.Amount) string {
	if amount == nil {
		return ""
	}
	if amount.HasRaw() {
		return amount.Raw
	}
	return amount.Value
}

// isAlignedAmount reports whether bean-format aligns an amount: only a
// plainly spelled number followed by a currency matches its line pattern.
// Any other amount leaves the line as written.
func isAlignedAmount(amount *ast.Amount) bool {
	return amount != nil && amount.Currency != "" && alignedNumber.MatchString(numberText(amount))
}

// gluedToCurrency reports whether a source line spells number directly
// followed by currency, which bean-format's pattern does not align.
func gluedToCurrency(line, number, currency string) bool {
	return number != "" && strings.Contains(line, number+currency)
}

// postingLayout reads a posting's source line. bean-format aligns a
// posting that is not flagged, with a plainly spelled number that the line
// does not glue to its currency; it re-pads only the account and the
// number and copies the rest from the currency on. Any other posting line
// it leaves as written, re-indented to indent when it starts with the
// account, as bean-format re-indents such lines. owned says whether the
// line is the posting's own; indent is the run's posting indent.
func postingLayout(line string, owned bool, p *ast.Posting, indent int) lineLayout {
	aligns := p.Flag == "" && isAlignedAmount(p.Amount) &&
		!gluedToCurrency(line, numberText(p.Amount), p.Amount.Currency)
	text, rest, holds := postingText(line, owned, p)

	if !aligns {
		if (p.Flag != "" || p.Amount != nil) && holds {
			if p.Flag == "" {
				text = strings.Repeat(" ", indent) + text
			} else {
				text = line
			}
			return lineLayout{kind: copyLine, text: text}
		}
		return lineLayout{kind: reconstructLine}
	}

	// bean-format measures the prefix as the source indents it, even
	// though it writes it at the normalized indent.
	sourceIndent := indent
	if column := p.Position().Column; column > 1 {
		sourceIndent = column - 1
	}
	number := numberText(p.Amount)
	layout := lineLayout{
		kind:        alignLine,
		prefix:      strings.Repeat(" ", indent) + string(p.Account),
		number:      number,
		currency:    p.Amount.Currency,
		prefixWidth: sourceIndent + runewidth.StringWidth(string(p.Account)),
		numberWidth: runewidth.StringWidth(number),
	}
	if holds {
		layout.rest = postingRest(rest, p)
	}
	return layout
}

// postingText returns a posting's owned source line trimmed of its indent,
// and the text after its account. It reports false unless the line holds
// the whole posting: its flag, its account, then its amount.
func postingText(line string, owned bool, p *ast.Posting) (text, rest string, ok bool) {
	if !owned {
		return "", "", false
	}
	text = strings.TrimLeft(line, " \t")
	body, ok := strings.CutPrefix(text, p.Flag)
	if !ok {
		return "", "", false
	}
	rest, ok = strings.CutPrefix(strings.TrimLeft(body, " \t"), string(p.Account))
	if !ok {
		return "", "", false
	}
	if p.Amount != nil && (!strings.Contains(rest, numberText(p.Amount)) || !strings.Contains(rest, p.Amount.Currency)) {
		return "", "", false
	}
	return text, rest, true
}

// postingRest returns the source text of an aligned posting line from its
// units currency to the end of the line, cost, price, comment and trailing
// whitespace included, or "" when the text after the account does not
// continue with the number, whitespace and the currency.
func postingRest(afterAccount string, p *ast.Posting) string {
	afterNumber, ok := strings.CutPrefix(strings.TrimLeft(afterAccount, " \t"), numberText(p.Amount))
	if !ok {
		return ""
	}
	suffix := strings.TrimLeft(afterNumber, " \t")
	if !isValidNumericValue(p.Amount.Value) || len(suffix) == len(afterNumber) || !strings.HasPrefix(suffix, p.Amount.Currency) {
		return ""
	}
	return suffix
}

// datedLayout reads a dated directive's line that ends in an amount: head
// is its date, keyword and subject, text its amount spelled without the
// currency, lastNumber the number before the currency. bean-format aligns
// it when some suffix of text is a plainly spelled number and the line
// does not glue that number to the currency ("10USD"); otherwise it
// leaves the line as written.
func datedLayout(line string, owned bool, head, text, lastNumber, currency string) lineLayout {
	if currency != "" && !gluedToCurrency(line, lastNumber, currency) {
		// The source may glue the amount text to the subject: BA- 1 USD
		// is BA's price at - 1, whose minus bean-format reads as part of
		// the prefix.
		glued := text != "" && strings.Contains(line, head[strings.LastIndexByte(head, ' ')+1:]+text[:1])
		if prefix, number, ok := datedAmountLayout(head, text, glued); ok {
			return lineLayout{
				kind:        alignLine,
				prefix:      prefix,
				number:      number,
				currency:    currency,
				prefixWidth: runewidth.StringWidth(prefix),
				numberWidth: runewidth.StringWidth(number),
			}
		}
	}
	return plainLayout(line, owned)
}

// datedAmountLayout splits a dated directive's amount text like
// bean-format's line pattern, whose prefix is the shortest one followed by
// a plainly spelled number and the currency: head plus any text before
// that number is the prefix. In "100.00 ~ 0.05" the tolerance is aligned,
// in "50 + 50" the "+ 50". It reports false when no suffix is a number.
//
// glued says the source spells text right after head, with no space
// between, so no number starts text: its start belongs to the prefix.
func datedAmountLayout(head, text string, glued bool) (prefix, number string, ok bool) {
	separator := " "
	if glued {
		separator = ""
	}
	// Candidate numbers start after a run of spaces; the prefix before
	// them is right-trimmed, like bean-format's prefix.rstrip().
	for i := 0; i < len(text); i++ {
		if i > 0 && text[i-1] != ' ' || text[i] == ' ' || i == 0 && glued {
			continue
		}
		if suffix := text[i:]; alignedNumber.MatchString(suffix) {
			if before := strings.TrimRight(text[:i], " "); before != "" {
				return head + separator + before, suffix, true
			}
			return head, suffix, true
		}
	}
	return "", "", false
}

// spelledDate spells date as a directive's source line does: with slashes
// when the line starts with it so, otherwise with dashes.
func spelledDate(line string, date *ast.Date) string {
	dashed := date.String()
	if slashed := strings.ReplaceAll(dashed, "-", "/"); strings.HasPrefix(line, slashed) {
		return slashed
	}
	return dashed
}

// isValidNumericValue checks if a value looks like a valid numeric amount.
func isValidNumericValue(value string) bool {
	if value == "" {
		return false
	}

	i := 0
	if value[0] == '+' || value[0] == '-' {
		i = 1
	}

	if i >= len(value) {
		return false
	}

	hasDigit := false
	for i < len(value) {
		c := value[i]
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c == '.' || c == ',' {
			// Allow decimal separators
		} else {
			return false
		}
		i++
	}

	return hasDigit
}
