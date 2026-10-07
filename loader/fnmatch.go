package loader

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// fnmatch returns a matcher of names against a glob pattern segment, as
// Python's fnmatch.fnmatchcase matches them for glob.glob, which beancount
// expands includes with: "*" and "?" match any characters, a class "[...]"
// is negated by a leading "!" (a leading "^" is a literal), a "]" first in a
// class is a literal, a "[" that no "]" closes is a literal bracket, and
// nothing is escaped by a backslash. The class is read as Python 3.9's
// fnmatch.translate hands it to Python's re. A range that re rejects, such
// as "z-a", makes glob raise, so beancount fails on it; here the pattern
// matches nothing (KNOWN_GAPS.md).
func fnmatch(pattern string) func(name string) bool {
	re, ok := translateGlob([]rune(pattern))
	if !ok {
		return func(string) bool { return false }
	}
	return re.MatchString
}

// translateGlob is Python 3.9's fnmatch.translate, written as an RE2
// regular expression; it reports false for a class Python's re rejects.
func translateGlob(pat []rune) (*regexp.Regexp, bool) {
	var b strings.Builder
	b.WriteString(`(?s)\A`)
	i, n := 0, len(pat)
	for i < n {
		c := pat[i]
		i++
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i
			if j < n && pat[j] == '!' {
				j++
			}
			if j < n && pat[j] == ']' {
				j++
			}
			for j < n && pat[j] != ']' {
				j++
			}
			if j >= n {
				b.WriteString(`\[`)
				continue
			}
			class, ok := globClass(pat, i, j)
			if !ok {
				return nil, false
			}
			b.WriteString(class)
			i = j + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`\z`)
	return regexp.MustCompile(b.String()), true
}

// globAtom is one character of a class as Python's re reads it: literal
// when translate escaped it, else possibly a range's hyphen.
type globAtom struct {
	r       rune
	escaped bool
}

// globClass translates the class pat[i:j], between its brackets, as
// fnmatch.translate does (a hyphen inside a chunk of "--" is a literal, one
// joining two chunks makes a range), then reads it as Python's re reads a
// set, and writes it as an RE2 class of escaped code points.
func globClass(pat []rune, i, j int) (string, bool) {
	// The chunks translate joins by range hyphens; without "--" the class
	// is one chunk whose hyphens stay unescaped.
	var chunks [][]rune
	if !strings.Contains(string(pat[i:j]), "--") {
		chunks = [][]rune{pat[i:j]}
	} else {
		k := i + 1
		if pat[i] == '!' {
			k = i + 2
		}
		for k < j {
			m := slices.Index(pat[k:j], '-')
			if m < 0 {
				break
			}
			found := k + m
			chunks = append(chunks, pat[i:found])
			i = found + 1
			k = found + 3
		}
		chunks = append(chunks, pat[i:j])
	}
	var atoms []globAtom
	for c, chunk := range chunks {
		if c > 0 {
			atoms = append(atoms, globAtom{r: '-'})
		}
		for _, r := range chunk {
			// Without "--", a hyphen is left for re to read as a range.
			atoms = append(atoms, globAtom{r: r, escaped: len(chunks) > 1 && r == '-'})
		}
	}

	var b strings.Builder
	b.WriteByte('[')
	switch atoms[0].r {
	case '!':
		b.WriteByte('^')
		atoms = atoms[1:]
	case '^', '[':
		atoms[0].escaped = true
	}
	for a := 0; a < len(atoms); a++ {
		lo := atoms[a].r
		// Like re, an unescaped hyphen between two characters makes a range;
		// at the end of the set it is a literal.
		if a+2 < len(atoms) && atoms[a+1].r == '-' && !atoms[a+1].escaped {
			hi := atoms[a+2].r
			if hi < lo {
				return "", false
			}
			fmt.Fprintf(&b, `\x{%x}-\x{%x}`, lo, hi)
			a += 2
			continue
		}
		fmt.Fprintf(&b, `\x{%x}`, lo)
	}
	b.WriteByte(']')
	return b.String(), true
}
