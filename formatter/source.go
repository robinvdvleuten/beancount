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
}

// newSourceView reads source, the text tree was parsed from. The lines are
// split on \r\n, \r and \n to match the lexer's lineBreakLenAt semantics.
func newSourceView(source []byte, tree *ast.AST) *sourceView {
	s := &sourceView{
		lines:    ast.SplitSourceLines(string(source)),
		multiple: ast.LinesWithMultipleItems(tree),
		starts:   make(map[int]bool),
	}
	s.markStarts(tree)
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

// itemLine returns the source line of an item starting at line and column,
// and whether the item owns it: the line holds this item and nothing else.
// It does when no other top-level item starts on it, the item starts it,
// and the item ends on it, because the next line starts a node of its own
// or only whitespace follows. A directive whose tokens or strings run onto
// the next line owns no line.
func (s *sourceView) itemLine(line, column int) (text string, owned bool) {
	text = s.line(line)
	if line < 1 || line > len(s.lines) || s.multiple[line] || firstColumn(text) != column {
		return text, false
	}
	if line < s.lastContent && !s.starts[line+1] {
		return text, false
	}
	return text, true
}

// directiveLine is itemLine for a dated directive, which starts at its
// date: the parser requires the date in column 1, on its keyword's line.
func (s *sourceView) directiveLine(d ast.Directive) (text string, owned bool) {
	return s.itemLine(d.Position().Line, 1)
}

// firstColumn is the 1-indexed column of a line's first non-blank byte.
func firstColumn(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t")) + 1
}

func (s *sourceView) markStart(pos ast.Position) {
	if pos.Line > 0 && firstColumn(s.line(pos.Line)) == pos.Column {
		s.starts[pos.Line] = true
	}
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
			s.markStart(m.Position())
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
				s.markStart(m.Position())
			}
		}
		for _, item := range txn.BodyItems {
			switch {
			case item.Comment != nil:
				s.markStart(item.Comment.Position())
			case item.BlankLine != nil:
				s.starts[item.BlankLine.Position().Line] = true
			}
		}
	}
}
