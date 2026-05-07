package formatter

import (
	"bytes"
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFormatTransactionWithInlineComments(t *testing.T) {
	source := `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Food

2024-01-15 * "Test transaction" ; header comment
  Assets:Checking   100.00 USD  ; posting 1 comment
  Expenses:Food    -100.00 USD  ; posting 2 comment
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()

	// Check that comments are preserved in output
	assert.True(t, bytes.Contains([]byte(result), []byte("; header comment")), "output should contain header comment")
	assert.True(t, bytes.Contains([]byte(result), []byte("; posting 1 comment")), "output should contain posting 1 comment")
	assert.True(t, bytes.Contains([]byte(result), []byte("; posting 2 comment")), "output should contain posting 2 comment")
}

func TestFormatDirectiveWithInlineComment(t *testing.T) {
	source := `2024-01-01 open Assets:Checking ; test comment
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()
	assert.True(t, bytes.Contains([]byte(result), []byte("; test comment")), "output should contain test comment")
}

func TestFormatPriceWithInlineComment(t *testing.T) {
	source := `2024-01-01 commodity USD

2024-06-15 price HOOL 500.00 USD ; market close
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()
	assert.True(t, bytes.Contains([]byte(result), []byte("; market close")), "output should contain price inline comment")
}

func TestFormatPreservesCommentPosition(t *testing.T) {
	source := `2024-01-01 open Assets:Checking

2024-01-15 * "Test"
  Assets:Checking   100.00 USD  ; inline comment
  Expenses:Food    -100.00 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()

	// Comment should be on the same line as the posting
	lines := bytes.Split([]byte(result), []byte("\n"))
	var foundCommentLine bool
	for _, line := range lines {
		if bytes.Contains(line, []byte("100.00")) && bytes.Contains(line, []byte("; inline comment")) {
			foundCommentLine = true
			break
		}
	}
	assert.True(t, foundCommentLine, "comment should be on the same line as the posting")
}

func TestFormatRoundTripPreservesComments(t *testing.T) {
	source := `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Food

2024-01-15 * "Test" ; header
  Assets:Checking   100.00 USD  ; comment 1
  Expenses:Food    -100.00 USD  ; comment 2
`

	// First parse
	tree1 := parser.MustParseBytes(context.Background(), []byte(source))

	// First format
	f := New()
	output1 := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree1, []byte(source), output1)
	assert.NoError(t, err)

	// Second parse (formatted output)
	tree2 := parser.MustParseBytes(context.Background(), output1.Bytes())

	// Second format
	output2 := bytes.NewBufferString("")
	err = f.Format(context.Background(), tree2, output1.Bytes(), output2)
	assert.NoError(t, err)

	// The two formatted outputs should match (idempotent)
	assert.Equal(t, output1.String(), output2.String(), "formatting should be idempotent")

	// Both outputs should preserve comments
	assert.True(t, bytes.Contains(output1.Bytes(), []byte("; header")), "output1 should contain header comment")
	assert.True(t, bytes.Contains(output2.Bytes(), []byte("; header")), "output2 should contain header comment")
	assert.True(t, bytes.Contains(output1.Bytes(), []byte("; comment 1")), "output1 should contain comment 1")
	assert.True(t, bytes.Contains(output2.Bytes(), []byte("; comment 1")), "output2 should contain comment 1")
}

func TestFormatWithoutInlineComments(t *testing.T) {
	source := `2024-01-01 open Assets:Checking

2024-01-15 * "Test"
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()

	// Should format correctly without comments
	assert.True(t, bytes.Contains([]byte(result), []byte("2024-01-01 open Assets:Checking")), "output should contain open directive")
	assert.True(t, bytes.Contains([]byte(result), []byte("2024-01-15 * \"Test\"")), "output should contain transaction")
}

func TestFormatPreservesOrgStyleSectionHeaders(t *testing.T) {
	source := `* Options

option "title" "Ledger"
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)
	assert.Equal(t, source, output.String())
}

func TestFormatPreservesTransactionBodyCommentsAndBlankLines(t *testing.T) {
	source := `2024-01-02 * "Test"
  ; comment before first posting
  Assets:Cash  -10 USD
  ; comment between postings

  Expenses:Food  10 USD
  ; comment after postings
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()
	before := bytes.Index([]byte(result), []byte("; comment before first posting"))
	firstPosting := bytes.Index([]byte(result), []byte("Assets:Cash"))
	between := bytes.Index([]byte(result), []byte("; comment between postings"))
	secondPosting := bytes.Index([]byte(result), []byte("Expenses:Food"))
	after := bytes.Index([]byte(result), []byte("; comment after postings"))

	assert.True(t, before >= 0 && before < firstPosting)
	assert.True(t, firstPosting < between && between < secondPosting)
	assert.True(t, secondPosting < after)
	assert.True(t, bytes.Contains([]byte(result), []byte("; comment between postings\n\n    Expenses:Food")))
}

func TestFormatCanDropTransactionBodyTrivia(t *testing.T) {
	source := `2024-01-02 * "Test"
  ; comment before first posting
  Assets:Cash  -10 USD

  Expenses:Food  10 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New(WithPreserveComments(false), WithPreserveBlanks(false))
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()
	assert.True(t, !bytes.Contains([]byte(result), []byte("; comment before first posting")))
	assert.True(t, !bytes.Contains([]byte(result), []byte("\n\n")))
	assert.True(t, bytes.Contains([]byte(result), []byte("Assets:Cash")))
	assert.True(t, bytes.Contains([]byte(result), []byte("Expenses:Food")))
}

func TestFormatPreservesOneLegTransaction(t *testing.T) {
	source := `2024-01-02 * "In progress"
  Assets:Cash  -10 USD
`

	tree := parser.MustParseBytes(context.Background(), []byte(source))

	f := New()
	output := bytes.NewBufferString("")
	err := f.Format(context.Background(), tree, []byte(source), output)
	assert.NoError(t, err)

	result := output.String()
	assert.True(t, bytes.Contains([]byte(result), []byte("2024-01-02 * \"In progress\"")))
	assert.True(t, bytes.Contains([]byte(result), []byte("Assets:Cash")))
}

func TestFormatLeadingBlankTriviaAfterTransactionIsIdempotent(t *testing.T) {
	for _, source := range []string{
		"0001-01-01 !\n\n \n ;",
		"0001-01-01 !\n ;\n\n \n ;",
	} {
		tree1 := parser.MustParseBytes(context.Background(), []byte(source))
		f := New()

		output1 := bytes.NewBufferString("")
		err := f.Format(context.Background(), tree1, []byte(source), output1)
		assert.NoError(t, err)

		tree2 := parser.MustParseBytes(context.Background(), output1.Bytes())
		output2 := bytes.NewBufferString("")
		err = f.Format(context.Background(), tree2, output1.Bytes(), output2)
		assert.NoError(t, err)

		assert.Equal(t, output1.String(), output2.String())
	}
}
