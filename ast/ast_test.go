package ast

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

// newOpenForTest creates an Open directive for testing.
func newOpenForTest(line int, date *Date, account Account) *Open {
	open := &Open{Account: account}
	open.SetPosition(Position{Line: line})
	open.SetDate(date)
	return open
}

// newCloseForTest creates a Close directive for testing.
func newCloseForTest(line int, date *Date, account Account) *Close {
	close := &Close{Account: account}
	close.SetPosition(Position{Line: line})
	close.SetDate(date)
	return close
}

// newBalanceForTest creates a Balance directive for testing.
func newBalanceForTest(line int, date *Date, account Account) *Balance {
	balance := &Balance{Account: account, Amount: NewAmount("0", "USD")}
	balance.SetPosition(Position{Line: line})
	balance.SetDate(date)
	return balance
}

// newPadForTest creates a Pad directive for testing.
func newPadForTest(line int, date *Date, account, padAccount Account) *Pad {
	pad := &Pad{Account: account, AccountPad: padAccount}
	pad.SetPosition(Position{Line: line})
	pad.SetDate(date)
	return pad
}

// newNoteForTest creates a Note directive for testing.
func newNoteForTest(line int, date *Date, account Account) *Note {
	note := &Note{Account: account, Description: NewRawString("note")}
	note.SetPosition(Position{Line: line})
	note.SetDate(date)
	return note
}

// newDocumentForTest creates a Document directive for testing.
func newDocumentForTest(line int, date *Date, account Account) *Document {
	document := &Document{Account: account, PathToDocument: NewRawString("receipt.pdf")}
	document.SetPosition(Position{Line: line})
	document.SetDate(date)
	return document
}

// newTransactionForTest creates a Transaction directive for testing.
func newTransactionForTest(line int, date *Date) *Transaction {
	transaction := &Transaction{Flag: "*", Narration: NewRawString("test")}
	transaction.SetPosition(Position{Line: line})
	transaction.SetDate(date)
	return transaction
}

// newOptionForTest creates an Option for testing.
func newOptionForTest(line int, name, value RawString) *Option {
	opt := &Option{Name: name, Value: value}
	opt.SetPosition(Position{Line: line})
	return opt
}

// newIncludeForTest creates an Include for testing.
func newIncludeForTest(line int, filename RawString) *Include {
	inc := &Include{Filename: filename}
	inc.SetPosition(Position{Line: line})
	return inc
}

// newPluginForTest creates a Plugin for testing.
func newPluginForTest(line int, name RawString) *Plugin {
	plugin := &Plugin{Name: name}
	plugin.SetPosition(Position{Line: line})
	return plugin
}

// newPushtagForTest creates a Pushtag for testing.
func newPushtagForTest(line int, tag Tag) *Pushtag {
	pt := &Pushtag{Tag: tag}
	pt.SetPosition(Position{Line: line})
	return pt
}

// newPoptagForTest creates a Poptag for testing.
func newPoptagForTest(line int, tag Tag) *Poptag {
	pt := &Poptag{Tag: tag}
	pt.SetPosition(Position{Line: line})
	return pt
}

// newPushmetaForTest creates a Pushmeta for testing.
func newPushmetaForTest(line int, key, value string) *Pushmeta {
	pm := &Pushmeta{Key: key, Value: value}
	pm.SetPosition(Position{Line: line})
	return pm
}

// newPopmetaForTest creates a Popmeta for testing.
func newPopmetaForTest(line int, key string) *Popmeta {
	pm := &Popmeta{Key: key}
	pm.SetPosition(Position{Line: line})
	return pm
}

// newCommentForTest creates a Comment for testing.
func newCommentForTest(line int, content string) *Comment {
	c := &Comment{Content: content}
	c.SetPosition(Position{Line: line})
	return c
}

// newBlankLineForTest creates a BlankLine for testing.
func newBlankLineForTest(line int) *BlankLine {
	bl := &BlankLine{}
	bl.SetPosition(Position{Line: line})
	return bl
}

func TestSortDirectives(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	account, _ := NewAccount("Assets:Checking")
	padAccount, _ := NewAccount("Equity:Opening-Balances")

	open := newOpenForTest(70, date, account)
	balance := newBalanceForTest(60, date, account)
	transaction := newTransactionForTest(30, date)
	pad := newPadForTest(40, date, account, padAccount)
	note := newNoteForTest(50, date, account)
	document := newDocumentForTest(20, date, account)
	close := newCloseForTest(10, date, account)

	tree := &AST{
		Directives: []Directive{
			close,
			document,
			note,
			pad,
			transaction,
			balance,
			open,
		},
	}

	err := SortDirectives(tree)
	assert.NoError(t, err)
	assert.Equal(t, []Directive{
		open,
		balance,
		transaction,
		pad,
		note,
		document,
		close,
	}, tree.Directives)
}

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
				newOpenForTest(1, date, account),
				newOpenForTest(2, date, account),
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
				newOpenForTest(1, date, account),
				newCloseForTest(1, date, account), // Same line as Open
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
				newOpenForTest(1, date, account),
			},
			Comments: []*Comment{
				newCommentForTest(1, "; This is a comment"), // Same line as Open
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
				newOpenForTest(1, date, account),
			},
			BlankLines: []*BlankLine{
				newBlankLineForTest(2), // Different line
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
				newOptionForTest(5, NewRawString("title"), NewRawString("My Ledger")),
			},
			Includes: []*Include{
				newIncludeForTest(5, NewRawString("accounts.beancount")), // Same line as Option
			},
			Directives: []Directive{
				newOpenForTest(5, date, account), // Same line as Option and Include
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
				newPushtagForTest(10, NewTag("vacation")),
			},
			Directives: []Directive{
				newOpenForTest(10, date, account), // Same line as Pushtag
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
				newOpenForTest(1, date, account),
				newCloseForTest(1, date, account), // Line 1 has 2 items
				newOpenForTest(2, date, account),  // Line 2 has 1 item
				newOpenForTest(3, date, account),
				newCloseForTest(3, date, account), // Line 3 has 2 items
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
				newOptionForTest(1, NewRawString("title"), NewRawString("Test")),
			},
			Includes: []*Include{
				newIncludeForTest(2, NewRawString("test.beancount")),
			},
			Plugins: []*Plugin{
				newPluginForTest(3, NewRawString("test_plugin")),
			},
			Pushtags: []*Pushtag{
				newPushtagForTest(4, NewTag("test")),
			},
			Poptags: []*Poptag{
				newPoptagForTest(5, NewTag("test")),
			},
			Pushmetas: []*Pushmeta{
				newPushmetaForTest(6, "key", "value"),
			},
			Popmetas: []*Popmeta{
				newPopmetaForTest(7, "key"),
			},
			Directives: []Directive{
				newOpenForTest(8, date, account),
			},
			Comments: []*Comment{
				newCommentForTest(9, "; comment"),
			},
			BlankLines: []*BlankLine{
				newBlankLineForTest(10),
			},
		}

		multiLines := LinesWithMultipleItems(tree)
		// All items are on different lines, so no multiple items
		assert.Equal(t, 0, len(multiLines))
	})
}
