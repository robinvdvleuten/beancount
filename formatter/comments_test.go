package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestCommentPreservation(t *testing.T) {
	t.Run("StandaloneComments", func(t *testing.T) {
		source := `; This is a header comment
option "title" "Test"

; Comment before directive
2021-01-01 open Assets:Checking
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Verify comments are preserved
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; This is a header comment")),
			"Header comment should be preserved")
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; Comment before directive")),
			"Comment before directive should be preserved")
	})

	t.Run("BlankLinePreservation", func(t *testing.T) {
		source := `option "title" "Test"

2021-01-01 open Assets:Checking

2021-01-02 open Assets:Savings
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Count newlines - should have blank lines preserved
		lines := bytes.Split(buf.Bytes(), []byte("\n"))
		hasBlankLines := false
		for i := 0; i < len(lines)-1; i++ {
			if len(bytes.TrimSpace(lines[i])) > 0 && len(bytes.TrimSpace(lines[i+1])) == 0 {
				hasBlankLines = true
				break
			}
		}
		assert.True(t, hasBlankLines, "Blank lines should be preserved, got: %s", output)
	})

	t.Run("SectionComments", func(t *testing.T) {
		source := `; Opening accounts

2021-01-01 open Assets:Checking

; Transactions

2021-01-02 * "Test"
  Assets:Checking  100.00 USD
  Equity:Opening-Balances  -100.00 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; Opening accounts")),
			"Section comment should be preserved")
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; Transactions")),
			"Section comment should be preserved")
	})

	t.Run("DisableCommentPreservation", func(t *testing.T) {
		source := `; This comment should not appear
option "title" "Test"
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New(WithPreserveComments(false))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Comment should not be in output
		assert.False(t, bytes.Contains(buf.Bytes(), []byte("; This comment should not appear")),
			"Comment should not be preserved when disabled")
	})

	t.Run("DisableBlankPreservation", func(t *testing.T) {
		source := `option "title" "Test"

2021-01-01 open Assets:Checking
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New(WithPreserveBlanks(false))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Should have minimal blank lines
		output := buf.String()
		lines := bytes.Split(buf.Bytes(), []byte("\n"))
		consecutiveBlanks := 0
		for _, line := range lines {
			if len(bytes.TrimSpace(line)) == 0 {
				consecutiveBlanks++
			} else {
				consecutiveBlanks = 0
			}
		}
		// With blank preservation disabled, shouldn't have consecutive blanks
		assert.True(t, consecutiveBlanks <= 1, "Should not have multiple consecutive blanks, got: %s", output)
	})

	t.Run("MultipleComments", func(t *testing.T) {
		source := `; Comment 1
; Comment 2
; Comment 3
option "title" "Test"
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// All comments should be preserved
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; Comment 1")))
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; Comment 2")))
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("; Comment 3")))
	})
}

func TestExtractCommentsAndBlanks(t *testing.T) {
	t.Run("ExtractComments", func(t *testing.T) {
		source := []byte(`; Comment 1
option "title" "Test"
; Comment 2
2021-01-01 open Assets:Checking
`)
		comments, _ := extractCommentsAndBlanks(source)

		assert.Equal(t, 2, len(comments))
		assert.Equal(t, "; Comment 1", comments[0].Content)
		assert.Equal(t, 1, comments[0].Line)
		assert.Equal(t, "; Comment 2", comments[1].Content)
		assert.Equal(t, 3, comments[1].Line)
	})

	t.Run("ExtractBlanks", func(t *testing.T) {
		// Note: no trailing newline to avoid counting it as a blank line
		source := []byte("option \"title\" \"Test\"\n\n2021-01-01 open Assets:Checking\n\n2021-01-02 open Assets:Savings")
		_, blanks := extractCommentsAndBlanks(source)

		assert.Equal(t, 2, len(blanks))
		assert.Equal(t, 2, blanks[0].Line)
		assert.Equal(t, 4, blanks[1].Line)
	})

	t.Run("SectionCommentType", func(t *testing.T) {
		source := []byte(`; Section header

2021-01-01 open Assets:Checking
`)
		comments, _ := extractCommentsAndBlanks(source)

		assert.Equal(t, 1, len(comments))
		assert.Equal(t, SectionComment, comments[0].Type, "Comment followed by blank should be section comment")
	})

	t.Run("StandaloneCommentType", func(t *testing.T) {
		source := []byte(`; Regular comment
2021-01-01 open Assets:Checking
`)
		comments, _ := extractCommentsAndBlanks(source)

		assert.Equal(t, 1, len(comments))
		assert.Equal(t, StandaloneComment, comments[0].Type, "Comment not followed by blank should be standalone")
	})

	t.Run("HashLinePreservation", func(t *testing.T) {
		source := []byte(`# Options

option "title" "Test"

# Accounts
2021-01-01 open Assets:Checking
`)
		comments, _ := extractCommentsAndBlanks(source)

		assert.Equal(t, 2, len(comments))
		assert.Equal(t, "# Options", comments[0].Content)
		assert.Equal(t, 1, comments[0].Line)
		assert.Equal(t, "# Accounts", comments[1].Content)
		assert.Equal(t, 5, comments[1].Line)
	})
}

func TestHashLineFormatting(t *testing.T) {
	t.Run("PreserveOrgModeHeaders", func(t *testing.T) {
		source := `# Options

option "operating_currency" "EUR"

# Commodities

2022-01-01 commodity EUR

# Accounts

2023-01-01 open Assets:Checking EUR
`
		ast, err := parser.ParseString(context.Background(), source)
		assert.NoError(t, err)

		f := New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Verify hash headers are preserved
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("# Options")))
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("# Commodities")))
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("# Accounts")))

		// Verify directives are formatted
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("option \"operating_currency\" \"EUR\"")))
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("2022-01-01 commodity EUR")))
		assert.True(t, bytes.Contains(buf.Bytes(), []byte("2023-01-01 open Assets:Checking EUR")))
	})
}
