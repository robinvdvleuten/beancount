package ast

// Trivia represents non-semantic content like comments and blank lines that should be
// preserved during formatting. These are not processed by the ledger but are important
// for maintaining the original structure and readability of the file.

// CommentType represents the type of comment in a beancount file.
type CommentType int

const (
	// StandaloneComment appears on its own line
	StandaloneComment CommentType = iota
	// SectionComment is a standalone comment followed by a blank line (section header)
	SectionComment
)

// Comment represents a comment line in the source file (lines starting with ;).
type Comment struct {
	pos     Position
	Content string      // Comment text including the semicolon prefix
	Type    CommentType // Type of comment (standalone or section header)
}

func (c *Comment) Position() Position { return c.pos }

// SetPosition sets the position (for use by parser/builders in ast package)
func (c *Comment) SetPosition(pos Position) { c.pos = pos }

// BlankLine represents a blank line in the source file.
type BlankLine struct {
	pos Position
}

func (b *BlankLine) Position() Position { return b.pos }

// SetPosition sets the position (for use by parser/builders in ast package)
func (b *BlankLine) SetPosition(pos Position) { b.pos = pos }
