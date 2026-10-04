package formatter

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
)

// sourceView is the source a run formats, read through the AST's
// positions. It answers once, with one rule, whether an item has a source
// line of its own that bean-format would leave as written.
type sourceView struct {
	lines []string

	// multiple marks the lines where more than one top-level item starts.
	multiple map[int]bool

	// starts marks the lines a node starts at their first non-blank
	// column, so the line before them ends there.
	starts map[int]bool

	// lastContent is the last line holding more than whitespace.
	lastContent int

	// unterminated is the source's last line when it holds only
	// whitespace and no line break ends it: no item stands for it, but
	// bean-format, which reads lines, writes it with one.
	unterminated string
}

// newSourceView reads source, the text tree was parsed from. The lines are
// split as the lexer breaks them (ast.SplitSourceLines).
func newSourceView(source []byte, tree *ast.AST) *sourceView {
	s := &sourceView{
		lines:    ast.SplitSourceLines(string(source)),
		multiple: ast.LinesWithMultipleItems(tree),
		starts:   make(map[int]bool),
	}
	s.markStarts(tree)
	if n := len(s.lines); n > 0 && !strings.HasSuffix(string(source), "\n") && !strings.HasSuffix(string(source), "\r") &&
		s.lines[n-1] != "" && strings.TrimSpace(s.lines[n-1]) == "" {
		s.unterminated = s.lines[n-1]
	}
	for n := len(s.lines); n > 0; n-- {
		if strings.TrimSpace(s.lines[n-1]) != "" {
			s.lastContent = n
			break
		}
	}
	return s
}

// line returns source line n (1-indexed), or "" past either end.
func (s *sourceView) line(n int) string {
	if n < 1 || n > len(s.lines) {
		return ""
	}
	return s.lines[n-1]
}

// itemLine returns the source text of an item starting at line and column,
// and whether the item owns it: the text holds this item and nothing else.
// It does when no other top-level item starts on the line and the item
// starts it. The item ends where the next node starts or only whitespace
// follows; since only a string spanning lines carries an item past its
// line, the text runs over every line up to there, which bean-format
// leaves as written.
func (s *sourceView) itemLine(line, column int) (text string, owned bool) {
	text = s.line(line)
	if line < 1 || line > len(s.lines) || s.multiple[line] || firstColumn(text) != column {
		return text, false
	}
	end := line
	for end < s.lastContent && !s.starts[end+1] {
		end++
	}
	return strings.Join(s.lines[line-1:end], "\n"), true
}

// directiveLine is itemLine for a dated directive, which starts at its
// date: the parser requires the date to start its keyword's line, in
// column 1 or after leading whitespace holding a lone \r, which is no
// indent to beancount.
func (s *sourceView) directiveLine(d ast.Directive) (text string, owned bool) {
	line := d.Position().Line
	return s.itemLine(line, firstColumn(s.line(line)))
}

// firstColumn is the 1-indexed column of a line's first non-blank byte,
// blank as the lexer reads it: a space, a tab or a lone \r.
func firstColumn(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t\r")) + 1
}

func (s *sourceView) markStart(pos ast.Position) {
	if pos.Line > 0 && firstColumn(s.line(pos.Line)) == pos.Column {
		s.starts[pos.Line] = true
	}
}

// markMetadata records the lines a metadata entry and the comments leading
// it start.
func (s *sourceView) markMetadata(m *ast.Metadata) {
	for _, c := range m.Comments {
		s.markStart(c.Position())
	}
	s.markStart(m.Position())
}

// markStarts records the lines every node of tree starts. A blank line
// starts its line whatever its column.
func (s *sourceView) markStarts(tree *ast.AST) {
	for _, n := range tree.Options {
		s.markStart(n.Position())
	}
	for _, n := range tree.Includes {
		s.markStart(n.Position())
	}
	for _, n := range tree.Plugins {
		s.markStart(n.Position())
	}
	for _, n := range tree.Pushtags {
		s.markStart(n.Position())
	}
	for _, n := range tree.Poptags {
		s.markStart(n.Position())
	}
	for _, n := range tree.Pushmetas {
		s.markStart(n.Position())
	}
	for _, n := range tree.Popmetas {
		s.markStart(n.Position())
	}
	for _, n := range tree.Comments {
		s.markStart(n.Position())
	}
	for _, n := range tree.BlankLines {
		s.starts[n.Position().Line] = true
	}
	for _, d := range tree.Directives {
		s.markStart(ast.Position{Line: d.Position().Line, Column: 1})
		for _, m := range d.GetMetadata() {
			s.markMetadata(m)
		}
		txn, ok := d.(*ast.Transaction)
		if !ok {
			continue
		}
		for _, line := range txn.BodyTagsLinks {
			s.markStart(line.Position())
		}
		for _, p := range txn.Postings {
			s.markStart(p.Position())
			for _, m := range p.Metadata {
				s.markMetadata(m)
			}
		}
		for _, item := range txn.BodyItems {
			if item.Comment != nil {
				s.markStart(item.Comment.Position())
			}
		}
	}
}
