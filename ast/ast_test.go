package ast

import (
	"fmt"
	"os"
	"path/filepath"
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
	raw := NewRawString(value)
	pm := &Pushmeta{Key: key, MetaValue: &MetadataValue{StringValue: &raw}}
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

// Like beancount v3, a note takes the tags pushed around it, after its own.
func TestApplyPushPopDirectivesTagsNotes(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	account, _ := NewAccount("Assets:Checking")

	inside := newNoteForTest(2, date, account)
	inside.Tags = []Tag{"own"}
	// A pushed tag the note already has is kept twice; beancount holds a
	// set, and the printer writes each tag once.
	repeated := newNoteForTest(3, date, account)
	repeated.Tags = []Tag{"trip"}
	outside := newNoteForTest(5, date, account)

	tree := &AST{
		Pushtags:   []*Pushtag{newPushtagForTest(1, NewTag("trip"))},
		Poptags:    []*Poptag{newPoptagForTest(4, NewTag("trip"))},
		Directives: []Directive{inside, repeated, outside},
	}

	assert.Equal(t, 0, len(ApplyPushPopDirectives(tree)))
	assert.Equal(t, []Tag{"own", "trip"}, inside.Tags)
	assert.Equal(t, []Tag{"trip", "trip"}, repeated.Tags)
	assert.Equal(t, 0, len(outside.Tags))
}

// Beancount resolves a document's path when parsing, so its errors and its
// printer show the same one.
func TestDocumentResolvedPath(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	assert.NoError(t, err)

	// Absolute on every OS, and not cleaned.
	sep := string(filepath.Separator)
	absolute := dir + sep + "docs" + sep + ".." + sep + "x.pdf"

	tests := []struct {
		name     string
		filename string
		path     string
		want     string
	}{
		{"AbsoluteAsWritten", filepath.Join(dir, "main.beancount"), absolute, absolute},
		{"RelativeToItsFile", filepath.Join(dir, "sub", "main.beancount"), "../docs/x.pdf", filepath.Join(dir, "docs", "x.pdf")},
		{"EmptyNamesItsFilesDirectory", filepath.Join(dir, "main.beancount"), "", dir},
		// Like beancount's load_string, whose "<string>" has no directory.
		{"StdinResolvesAgainstWorkingDirectory", "<stdin>", "docs/x.pdf", filepath.Join(cwd, "docs", "x.pdf")},
		// Built in code: no file to resolve against.
		{"NoSourceAsWritten", "", "docs/x.pdf", "docs/x.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := &Document{PathToDocument: NewRawString(tt.path)}
			doc.SetPosition(Position{Filename: tt.filename, Line: 1})
			assert.Equal(t, tt.want, doc.ResolvedPath())
		})
	}
}

// Like beancount, a document takes the tags pushed around it, after its own.
func TestApplyPushPopDirectivesTagsDocuments(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	account, _ := NewAccount("Assets:Checking")

	inside := newDocumentForTest(2, date, account)
	inside.Tags = []Tag{"own"}
	untagged := newDocumentForTest(3, date, account)
	outside := newDocumentForTest(5, date, account)

	tree := &AST{
		Pushtags:   []*Pushtag{newPushtagForTest(1, NewTag("trip"))},
		Poptags:    []*Poptag{newPoptagForTest(4, NewTag("trip"))},
		Directives: []Directive{inside, untagged, outside},
	}

	assert.Equal(t, 0, len(ApplyPushPopDirectives(tree)))
	assert.Equal(t, []Tag{"own", "trip"}, inside.Tags)
	assert.Equal(t, []Tag{"trip"}, untagged.Tags)
	assert.Equal(t, 0, len(outside.Tags))
}

func TestApplyPushPopDirectivesReportsImbalance(t *testing.T) {
	at := func(line int) Position { return Position{Filename: "f.beancount", Line: line} }
	pushtag := func(line int, tag Tag) *Pushtag { p := &Pushtag{Tag: tag}; p.SetPosition(at(line)); return p }
	poptag := func(line int, tag Tag) *Poptag { p := &Poptag{Tag: tag}; p.SetPosition(at(line)); return p }
	pushmeta := func(line int, key, value string) *Pushmeta {
		raw := NewRawString(value)
		p := &Pushmeta{Key: key, MetaValue: &MetadataValue{StringValue: &raw}}
		p.SetPosition(at(line))
		return p
	}
	popmeta := func(line int, key string) *Popmeta { p := &Popmeta{Key: key}; p.SetPosition(at(line)); return p }

	// Messages follow beancount: a tag pushed twice and popped once stays
	// open, pops need a matching push, and leftover metadata lists every
	// value still pushed for its key.
	tree := &AST{
		Pushtags:  []*Pushtag{pushtag(1, "trip"), pushtag(2, "trip")},
		Poptags:   []*Poptag{poptag(3, "trip"), poptag(4, "absent")},
		Pushmetas: []*Pushmeta{pushmeta(5, "key", "v"), pushmeta(6, "key", "w")},
		Popmetas:  []*Popmeta{popmeta(7, "missing")},
	}
	var got []string
	for _, err := range ApplyPushPopDirectives(tree) {
		got = append(got, err.Error())
	}
	assert.Equal(t, []string{
		"f.beancount:4: Attempting to pop absent tag: 'absent'",
		"f.beancount:7: Attempting to pop absent metadata key: 'missing'",
		"f.beancount:2: Unbalanced pushed tag: 'trip'",
		"f.beancount:5: Unbalanced metadata key 'key'; leftover metadata 'v, w'",
	}, got)
}

// Like beancount, a transaction's metadata holds the pushed keys first, in
// push order (a key popped off entirely and pushed again goes last), then its
// own; an own key that was pushed takes the pushed key's place.
func TestApplyPushPopDirectivesMetadataOrder(t *testing.T) {
	date, _ := NewDate("2024-01-01")
	own := func(line int, key string) *Metadata {
		s := NewRawString(key)
		m := &Metadata{Key: key, Value: &MetadataValue{StringValue: &s}}
		m.SetPosition(Position{Line: line})
		return m
	}

	first := newTransactionForTest(4, date)
	first.Metadata = []*Metadata{own(5, "mine"), own(6, "b")}
	second := newTransactionForTest(9, date)

	tree := &AST{
		Pushmetas: []*Pushmeta{
			newPushmetaForTest(1, "a", "1"),
			newPushmetaForTest(2, "b", "2"),
			newPushmetaForTest(3, "c", "3"),
			newPushmetaForTest(8, "a", "4"),
		},
		Popmetas:   []*Popmeta{newPopmetaForTest(7, "a"), newPopmetaForTest(10, "a"), newPopmetaForTest(11, "b"), newPopmetaForTest(12, "c")},
		Directives: []Directive{first, second},
	}
	assert.Equal(t, 0, len(ApplyPushPopDirectives(tree)))

	describe := func(txn *Transaction) []string {
		var got []string
		for _, m := range txn.Metadata {
			got = append(got, fmt.Sprintf("%s=%s@%d", m.Key, m.Value, m.Position().Line))
		}
		return got
	}
	assert.Equal(t, []string{"a=1@0", "b=b@6", "c=3@0", "mine=mine@5"}, describe(first))
	assert.Equal(t, []string{"b=2@0", "c=3@0", "a=4@0"}, describe(second))
}
