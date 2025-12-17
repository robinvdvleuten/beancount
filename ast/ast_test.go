package ast

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestLinesWithMultipleItems(t *testing.T) {
	t.Run("EmptyAST", func(t *testing.T) {
		tree := &AST{}
		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 0, len(multiLines))
	})

	t.Run("SingleItemPerLine", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Directives: []Directive{
				&Open{
					Pos:     Position{Line: 1},
					Date:    date,
					Account: account,
				},
				&Open{
					Pos:     Position{Line: 2},
					Date:    date,
					Account: account,
				},
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 0, len(multiLines))
	})

	t.Run("TwoDirectivesOnSameLine", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Directives: []Directive{
				&Open{
					Pos:     Position{Line: 1},
					Date:    date,
					Account: account,
				},
				&Close{
					Pos:     Position{Line: 1}, // Same line as Open
					Date:    date,
					Account: account,
				},
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 1, len(multiLines))
		assert.True(t, multiLines[1])
	})

	t.Run("DirectiveAndCommentOnSameLine", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Directives: []Directive{
				&Open{
					Pos:     Position{Line: 1},
					Date:    date,
					Account: account,
				},
			},
			Comments: []*Comment{
				{
					Pos:     Position{Line: 1}, // Same line as Open
					Content: "; This is a comment",
				},
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 1, len(multiLines))
		assert.True(t, multiLines[1])
	})

	t.Run("DirectiveAndBlankLineOnDifferentLines", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Directives: []Directive{
				&Open{
					Pos:     Position{Line: 1},
					Date:    date,
					Account: account,
				},
			},
			BlankLines: []*BlankLine{
				{Pos: Position{Line: 2}}, // Different line
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 0, len(multiLines))
	})

	t.Run("MultipleItemTypesOnSameLine", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Options: []*Option{
				{
					Pos:   Position{Line: 5},
					Name:  "title",
					Value: "My Ledger",
				},
			},
			Includes: []*Include{
				{
					Pos:      Position{Line: 5}, // Same line as Option
					Filename: "accounts.beancount",
				},
			},
			Directives: []Directive{
				&Open{
					Pos:     Position{Line: 5}, // Same line as Option and Include
					Date:    date,
					Account: account,
				},
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 1, len(multiLines))
		assert.True(t, multiLines[5])
	})

	t.Run("PushtagAndDirectiveOnSameLine", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Pushtags: []*Pushtag{
				{
					Pos: Position{Line: 10},
					Tag: NewTag("vacation"),
				},
			},
			Directives: []Directive{
				&Open{
					Pos:     Position{Line: 10}, // Same line as Pushtag
					Date:    date,
					Account: account,
				},
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 1, len(multiLines))
		assert.True(t, multiLines[10])
	})

	t.Run("MultipleLinesWithMultipleItems", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Directives: []Directive{
				&Open{Pos: Position{Line: 1}, Date: date, Account: account},
				&Close{Pos: Position{Line: 1}, Date: date, Account: account}, // Line 1 has 2 items
				&Open{Pos: Position{Line: 2}, Date: date, Account: account},  // Line 2 has 1 item
				&Open{Pos: Position{Line: 3}, Date: date, Account: account},
				&Close{Pos: Position{Line: 3}, Date: date, Account: account}, // Line 3 has 2 items
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		assert.Equal(t, 2, len(multiLines))
		assert.True(t, multiLines[1])
		assert.False(t, multiLines[2])
		assert.True(t, multiLines[3])
	})

	t.Run("AllItemTypes", func(t *testing.T) {
		date, _ := NewDate("2024-01-01")
		account, _ := NewAccount("Assets:Checking")

		tree := &AST{
			Options: []*Option{
				{Pos: Position{Line: 1}, Name: "title", Value: "Test"},
			},
			Includes: []*Include{
				{Pos: Position{Line: 2}, Filename: "test.beancount"},
			},
			Plugins: []*Plugin{
				{Pos: Position{Line: 3}, Name: "test_plugin"},
			},
			Pushtags: []*Pushtag{
				{Pos: Position{Line: 4}, Tag: NewTag("test")},
			},
			Poptags: []*Poptag{
				{Pos: Position{Line: 5}, Tag: NewTag("test")},
			},
			Pushmetas: []*Pushmeta{
				{Pos: Position{Line: 6}, Key: "key", Value: "value"},
			},
			Popmetas: []*Popmeta{
				{Pos: Position{Line: 7}, Key: "key"},
			},
			Directives: []Directive{
				&Open{Pos: Position{Line: 8}, Date: date, Account: account},
			},
			Comments: []*Comment{
				{Pos: Position{Line: 9}, Content: "; comment"},
			},
			BlankLines: []*BlankLine{
				{Pos: Position{Line: 10}},
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		// All items are on different lines, so no multiple items
		assert.Equal(t, 0, len(multiLines))
	})
}
