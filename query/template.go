package query

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode"

	"github.com/robinvdvleuten/beancount/internal/pyrepr"
)

// pyMaxGroups is Python's re MAXGROUPS on a 64-bit build: a \g<n>
// reference at or above it is invalid whatever the pattern.
const pyMaxGroups = 1<<30 - 1

// templatePart is a piece of a parsed replacement: literal text, or the
// text of a group when group is not negative.
type templatePart struct {
	literal string
	group   int
}

// pySub replaces every match of re in s with template, read as Python's
// re.sub reads a replacement (re._parser.parse_template): \1 to \99 and
// \g<n> or \g<name> insert a group, \g<0> the whole match, and \n, \t,
// \\ and the other escapes Python's re knows insert their character; a
// group that took no part in a match inserts nothing, and $ is literal.
// Like re.sub, it parses template before it matches, so an invalid one
// fails the statement with Python's message even where nothing matches.
func pySub(re *regexp.Regexp, template, s string) string {
	parts := parseTemplate(re, template)
	var b strings.Builder
	last := 0
	for _, loc := range re.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(s[last:loc[0]])
		for _, part := range parts {
			if part.group < 0 {
				b.WriteString(part.literal)
			} else if start := loc[2*part.group]; start >= 0 {
				b.WriteString(s[start:loc[2*part.group+1]])
			}
		}
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// templateTokenizer is re._parser.Tokenizer over a replacement: each
// token is a character, or a backslash and the character after it.
type templateTokenizer struct {
	src   []rune
	index int
	next  []rune // nil at the end
}

func newTemplateTokenizer(template string) *templateTokenizer {
	t := &templateTokenizer{src: []rune(template)}
	t.advance()
	return t
}

func (t *templateTokenizer) advance() {
	index := t.index
	if index >= len(t.src) {
		t.next = nil
		return
	}
	char := []rune{t.src[index]}
	if char[0] == '\\' {
		index++
		if index >= len(t.src) {
			t.failAt("bad escape (end of pattern)", len(t.src)-1)
		}
		char = append(char, t.src[index])
	}
	t.index = index + 1
	t.next = char
}

func (t *templateTokenizer) get() []rune {
	this := t.next
	t.advance()
	return this
}

func (t *templateTokenizer) match(char rune) bool {
	if len(t.next) == 1 && t.next[0] == char {
		t.advance()
		return true
	}
	return false
}

// nextIn reports whether the next token is one character of set.
func (t *templateTokenizer) nextIn(set string) bool {
	return len(t.next) == 1 && strings.ContainsRune(set, t.next[0])
}

func (t *templateTokenizer) tell() int {
	return t.index - len(t.next)
}

// getuntil reads up to terminator, which it consumes.
func (t *templateTokenizer) getuntil(terminator rune, name string) string {
	var result []rune
	for {
		c := t.next
		t.advance()
		if c == nil {
			if len(result) == 0 {
				t.failAt("missing "+name, t.tell())
			}
			t.failAt(fmt.Sprintf("missing %c, unterminated name", terminator), t.tell()-len(result))
		}
		if len(c) == 1 && c[0] == terminator {
			if len(result) == 0 {
				t.failAt("missing "+name, t.tell()-1)
			}
			return string(result)
		}
		result = append(result, c...)
	}
}

// error fails the statement with re.error's message for msg at offset
// characters before the tokenizer's position.
func (t *templateTokenizer) error(msg string, offset int) {
	t.failAt(msg, t.tell()-offset)
}

// failAt fails the statement with str() of re.error(msg, template, pos):
// the position, and its line and column when the template spans lines.
func (t *templateTokenizer) failAt(msg string, pos int) {
	msg = fmt.Sprintf("%s at position %d", msg, pos)
	before := t.src[:pos]
	if strings.ContainsRune(string(t.src), '\n') {
		line := strings.Count(string(before), "\n") + 1
		col := pos + 1
		for i := len(before) - 1; i >= 0; i-- {
			if before[i] == '\n' {
				col = pos - i
				break
			}
		}
		msg = fmt.Sprintf("%s (line %d, column %d)", msg, line, col)
	}
	fail("%s", msg)
}

const (
	pyDigits    = "0123456789"
	pyOctDigits = "01234567"
)

// templateEscapes are the escapes re._parser.ESCAPES gives a character.
var templateEscapes = map[rune]rune{
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\',
}

// parseTemplate is re._parser.parse_template: it splits template into
// literal text and group references to re, failing the statement as
// Python raises re.error (or IndexError, for an unknown group name).
func parseTemplate(re *regexp.Regexp, template string) []templatePart {
	t := newTemplateTokenizer(template)
	var parts []templatePart
	var literal strings.Builder
	addgroup := func(index int64, pos int) {
		if index > int64(re.NumSubexp()) {
			t.error(fmt.Sprintf("invalid group reference %d", index), pos)
		}
		if literal.Len() > 0 {
			parts = append(parts, templatePart{literal: literal.String(), group: -1})
			literal.Reset()
		}
		parts = append(parts, templatePart{group: int(index)})
	}
	for {
		this := t.get()
		if this == nil {
			break
		}
		if this[0] != '\\' {
			literal.WriteRune(this[0])
			continue
		}
		c := this[1]
		switch {
		case c == 'g':
			if !t.match('<') {
				t.error("missing <", 0)
			}
			name := t.getuntil('>', "group name")
			length := len([]rune(name))
			var index int64
			if isPyIdentifier(name) {
				index = int64(re.SubexpIndex(name))
				if index < 0 {
					fail("unknown group name %s", pyrepr.String(name))
				}
			} else {
				n, ok := pyInt(name)
				if !ok || n.Sign() < 0 {
					t.error(fmt.Sprintf("bad character in group name %s", pyrepr.String(name)), length+1)
				}
				if n.Cmp(big.NewInt(pyMaxGroups)) >= 0 {
					t.error(fmt.Sprintf("invalid group reference %s", n), length+1)
				}
				index = n.Int64()
			}
			addgroup(index, length+1)
		case c == '0':
			digits := string(this[1:])
			if t.nextIn(pyOctDigits) {
				digits += string(t.get())
				if t.nextIn(pyOctDigits) {
					digits += string(t.get())
				}
			}
			literal.WriteRune(rune(octal(digits) & 0xff))
		case strings.ContainsRune(pyDigits, c):
			digits := string(this[1:])
			isOctal := false
			if t.nextIn(pyDigits) {
				digits += string(t.get())
				if strings.ContainsRune(pyOctDigits, c) && strings.ContainsRune(pyOctDigits, rune(digits[1])) && t.nextIn(pyOctDigits) {
					digits += string(t.get())
					isOctal = true
					value := octal(digits)
					if value > 0o377 {
						t.error(fmt.Sprintf(`octal escape value \%s outside of range 0-0o377`, digits), len(digits)+1)
					}
					literal.WriteRune(rune(value))
				}
			}
			if !isOctal {
				var index int64
				for _, d := range digits {
					index = index*10 + int64(d-'0')
				}
				addgroup(index, len(digits))
			}
		default:
			if r, ok := templateEscapes[c]; ok {
				literal.WriteRune(r)
			} else if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				t.error(`bad escape \`+string(c), 2)
			} else {
				literal.WriteString(string(this))
			}
		}
	}
	if literal.Len() > 0 {
		parts = append(parts, templatePart{literal: literal.String(), group: -1})
	}
	return parts
}

// octal reads ASCII octal digits.
func octal(digits string) int {
	value := 0
	for _, d := range digits {
		value = value*8 + int(d-'0')
	}
	return value
}

// isPyIdentifier approximates Python's str.isidentifier: a letter or
// underscore, then letters, digits, underscores and combining marks.
func isPyIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		start := r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
		if i == 0 && !start {
			return false
		}
		if !start && !unicode.In(r, unicode.Nd, unicode.Mn, unicode.Mc, unicode.Pc) {
			return false
		}
	}
	return true
}

// pyInt reads s as Python's int(s) does: surrounding whitespace, an
// optional sign, and decimal digits of any script, single underscores
// allowed between them.
func pyInt(s string) (*big.Int, bool) {
	s = strings.TrimFunc(s, unicode.IsSpace)
	negative := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	n := new(big.Int)
	ten := big.NewInt(10)
	sawDigit, lastUnderscore := false, false
	for _, r := range s {
		if r == '_' {
			if !sawDigit || lastUnderscore {
				return nil, false
			}
			lastUnderscore = true
			continue
		}
		d, ok := digitValue(r)
		if !ok {
			return nil, false
		}
		n.Mul(n, ten).Add(n, big.NewInt(int64(d)))
		sawDigit, lastUnderscore = true, false
	}
	if !sawDigit || lastUnderscore {
		return nil, false
	}
	if negative {
		n.Neg(n)
	}
	return n, true
}
