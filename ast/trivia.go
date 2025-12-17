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
	Pos     Position
	Content string      // Comment text including the semicolon prefix
	Type    CommentType // Type of comment (standalone or section header)
}

func (c *Comment) Position() Position { return c.Pos }

// BlankLine represents a blank line in the source file.
type BlankLine struct {
	Pos Position
}

func (b *BlankLine) Position() Position { return b.Pos }
