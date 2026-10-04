package formatter

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// bean-format's line pattern: which lines it aligns, which it leaves as
// written, and how an aligned line splits into prefix, number and the rest
// (beancount/scripts/format.py). These functions take a source line and
// hold no formatter state.

// lineKind is what the formatter does with an item's line.
type lineKind int

const (
	// unownedLine: the line is not the item's own, so the formatter has
	// nothing to copy or align and fails.
	unownedLine lineKind = iota
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

	// prefix and number split an aligned line; rest is its source text
	// from the currency to the end, comment and trailing whitespace
	// included.
	prefix, number, rest string

	// prefixWidth and numberWidth are the display widths bean-format
	// measures an aligned line by, its prefix as the source indents it.
	prefixWidth, numberWidth int
}

// plainLayout reads the line of an item with no number to align: a
// metadata, tags or comment line, or an undated directive. bean-format
// leaves it as written.
func plainLayout(line string, owned bool) lineLayout {
	if !owned {
		return lineLayout{kind: unownedLine}
	}
	return lineLayout{kind: copyLine, text: line}
}

// The patterns bean-format reads a line with, in Go's syntax.
const (
	// numberPattern is format.py's NUMBER_RE.
	numberPattern = `[-+]?\s*[\d,]+(?:\.\d*)?`
	// parenthesizedPattern is format.py's PARENTHESIZED_BINARY_OP_RE.
	parenthesizedPattern = `\(` + numberPattern + `\s*[-+*/]\s*` + numberPattern + `\)`
	// accountPattern is beancount's account.ACCOUNT_RE: every component
	// starts with an uppercase letter, or a digit after the first, so
	// Assets:日本 is not an account to it.
	accountPattern = `(?:\p{Lu}[\p{L}\p{Nd}\-]*)(?::[\p{Lu}\p{Nd}][\p{L}\p{Nd}\-]*)+`
	// currencyPattern is beancount's amount.CURRENCY_RE.
	currencyPattern = `[A-Z][A-Z0-9'._\-]*[A-Z0-9]?\b|/[A-Z0-9'._\-]*[A-Z](?:[A-Z0-9'._\-]*[A-Z0-9])?`
)

// alignedLine is the pattern of a line bean-format aligns: a prefix that
// is a dated line's start, short of any quote or semicolon, or an indented
// account; a plainly spelled number, or two joined by one operator in
// parentheses; then a currency and the rest of the line.
var alignedLine = regexp.MustCompile(`^(\d[^";]*?|\s+` + accountPattern + `)\s+(` +
	parenthesizedPattern + `|` + numberPattern + `)\s+((?:` + currencyPattern + `)\b.*)`)

// postingLine is the pattern of a line whose indent bean-format
// normalizes: an indent, then an account.
var postingLine = regexp.MustCompile(`^([ \t]+)(` + accountPattern + `.*)`)

// splitFirstLine splits text at its first line break: an item's text runs
// over the lines a string spanning them adds, and bean-format reads each
// line alone.
func splitFirstLine(text string) (first, tail string) {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i], text[i:]
	}
	return text, ""
}

// postingIndent returns the indent of a line bean-format re-indents, an
// indented account, and reports whether the line is one.
func postingIndent(line string) (int, bool) {
	m := postingLine.FindStringSubmatchIndex(line)
	if m == nil {
		return 0, false
	}
	return m[3], true
}

// layout reads an item's text as bean-format reads its lines. The first
// line is aligned when it matches bean-format's pattern, its prefix at
// indent when it is an indented account; otherwise the text is copied,
// re-indented to indent when it starts with an indented account. Every
// line after the first is copied. owned says whether the text is the
// item's own; an item that does not own it has no layout.
func layout(text string, owned bool, indent int) lineLayout {
	if !owned {
		return lineLayout{kind: unownedLine}
	}
	line, tail := splitFirstLine(text)
	m := alignedLine.FindStringSubmatch(line)
	if m == nil {
		if width, ok := postingIndent(line); ok {
			text = strings.Repeat(" ", indent) + text[width:]
		}
		return lineLayout{kind: copyLine, text: text}
	}

	prefix := strings.TrimRight(m[1], " \t")
	// bean-format measures the prefix as the source indents it, even
	// though it writes it at the normalized indent.
	prefixWidth := utf8.RuneCountInString(prefix)
	if width, ok := postingIndent(prefix); ok {
		prefixWidth = width + utf8.RuneCountInString(prefix[width:])
		prefix = strings.Repeat(" ", indent) + prefix[width:]
	}
	return lineLayout{
		kind:        alignLine,
		prefix:      prefix,
		number:      m[2],
		rest:        m[3] + tail,
		prefixWidth: prefixWidth,
		numberWidth: utf8.RuneCountInString(m[2]),
	}
}
