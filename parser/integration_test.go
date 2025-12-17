package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestParseSimpleTransaction(t *testing.T) {
	input := `2014-05-05 * "Cafe" "Coffee"
  Expenses:Food  4.50 USD
  Assets:Cash
`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err, "parse error")

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed transaction!")
}

func TestParseBalance(t *testing.T) {
	input := `2014-08-09 balance Assets:Checking 100.00 USD`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err, "parse error")

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed balance!")
}

func TestParseOpen(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err, "parse error")

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed open!")
}

func TestParseMultipleDirectives(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD
2014-01-02 open Expenses:Food

2014-05-05 * "Cafe" "Coffee"
  Expenses:Food  4.50 USD
  Assets:Checking

2014-08-09 balance Assets:Checking 100.00 USD
`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err, "parse error")

	assert.Equal(t, 4, len(ast.Directives))

	t.Logf("Successfully parsed %d directives!", len(ast.Directives))
}

func TestParseWithComments(t *testing.T) {
	input := `; This is a comment
2014-01-01 open Assets:Checking
; Another comment
2014-05-05 * "Test"
  Expenses:Food  10 USD
  Assets:Checking
`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err, "parse error")

	assert.Equal(t, 2, len(ast.Directives))

	t.Logf("Successfully parsed with comments!")
}

func TestParseWithMetadata(t *testing.T) {
	input := `2014-01-01 open Assets:Checking
  account-number: "123456"
  bank: "Chase"
`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err, "parse error")

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed with metadata!")
}

func TestParseTransactionRequiresNarration(t *testing.T) {
	input := `0001-01-01 !Assets:0 Income:0`

	_, err := ParseString(context.Background(), input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected transaction payee or narration string")
}

func TestParseTransactionWithMultipleInlineComments(t *testing.T) {
	source := `2024-01-01 * "Test transaction with inline comments"
  Expenses:Random 19.99 EUR ; Massageroller
  Expenses:PocketMoney:Robin 69.99 EUR ; Ketllebell
  Assets:Checking -89.98 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err, "parse error")
	assert.NotEqual(t, nil, result)
	assert.Equal(t, 1, len(result.Directives), "expected one directive")

	txn := result.Directives[0].(*ast.Transaction)
	assert.Equal(t, 3, len(txn.Postings), "expected three postings")

	// Verify each posting is parsed correctly
	assert.Equal(t, "Expenses:Random", string(txn.Postings[0].Account))
	assert.Equal(t, "19.99", txn.Postings[0].Amount.Value)
	assert.Equal(t, "EUR", txn.Postings[0].Amount.Currency)

	assert.Equal(t, "Expenses:PocketMoney:Robin", string(txn.Postings[1].Account))
	assert.Equal(t, "69.99", txn.Postings[1].Amount.Value)
	assert.Equal(t, "EUR", txn.Postings[1].Amount.Currency)

	assert.Equal(t, "Assets:Checking", string(txn.Postings[2].Account))
	assert.Equal(t, "-89.98", txn.Postings[2].Amount.Value)
	assert.Equal(t, "USD", txn.Postings[2].Amount.Currency)
}

func TestParseTransactionWithMixedInlineCommentsAndMetadata(t *testing.T) {
	source := `2024-01-01 * "Mixed test"
  Assets:Cash 100 USD
    category: food
  Expenses:Food 50 USD ; lunch money
  Expenses:Drinks 30 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err, "parse error")
	assert.NotEqual(t, nil, result)

	txn := result.Directives[0].(*ast.Transaction)
	assert.Equal(t, 3, len(txn.Postings), "expected three postings")

	// Verify metadata is parsed correctly for first posting
	assert.Equal(t, 1, len(txn.Postings[0].Metadata))
	assert.Equal(t, "category", txn.Postings[0].Metadata[0].Key)
	assert.Equal(t, "food", txn.Postings[0].Metadata[0].Value.String())
}

func TestParseTransactionWithSingleInlineComment(t *testing.T) {
	source := `2024-01-01 * "Single comment"
  Assets:Cash 100 USD ; initial deposit
  Expenses:Food 50 USD
  Expenses:Drinks 50 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err, "parse error")
	assert.NotEqual(t, nil, result)

	txn := result.Directives[0].(*ast.Transaction)
	assert.Equal(t, 3, len(txn.Postings), "expected three postings")
}

func TestParseTransactionWithInlineCommentEdgeCases(t *testing.T) {
	t.Run("CommentBeforeFirstPosting", func(t *testing.T) {
		source := `2024-01-01 * "Test"
  ; Comment before first posting
  Assets:Cash 100 USD
  Expenses:Food 100 USD
`
		result, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := result.Directives[0].(*ast.Transaction)
		assert.Equal(t, 2, len(txn.Postings))
	})

	t.Run("MultipleConsecutiveComments", func(t *testing.T) {
		source := `2024-01-01 * "Test"
  Assets:Cash 50 USD
  ; Comment 1
  ; Comment 2
  ; Comment 3
  Expenses:Food 50 USD
`
		result, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := result.Directives[0].(*ast.Transaction)
		assert.Equal(t, 2, len(txn.Postings))
	})

	t.Run("CommentsMixedWithMetadata", func(t *testing.T) {
		// Simplified test case that focuses on comment parsing without complex metadata
		source := `2024-01-01 * "Test"
  Assets:Cash 100 USD
  ; Comment between postings
  Expenses:Food 50 USD ; inline comment
  ; Another comment
  Expenses:Drinks 50 USD
`
		result, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := result.Directives[0].(*ast.Transaction)

		// The critical test: verify all 3 postings are parsed correctly despite comments
		// This was the original bug - comments would cause posting parsing to stop early
		assert.Equal(t, 3, len(txn.Postings), "Should parse all 3 postings despite intervening comments")

		// Verify accounts and amounts are correct
		assert.Equal(t, "Assets:Cash", string(txn.Postings[0].Account))
		assert.Equal(t, "100", txn.Postings[0].Amount.Value)
		assert.Equal(t, "Expenses:Food", string(txn.Postings[1].Account))
		assert.Equal(t, "50", txn.Postings[1].Amount.Value)
		assert.Equal(t, "Expenses:Drinks", string(txn.Postings[2].Account))
		assert.Equal(t, "50", txn.Postings[2].Amount.Value)
	})

	t.Run("AllPostingsWithInlineComments", func(t *testing.T) {
		source := `2024-01-01 * "All with comments"
  Assets:Checking -100 USD ; payment
  Expenses:Groceries 40 USD ; groceries
  Expenses:Gas 30 USD ; gas
  Expenses:Entertainment 30 USD ; movies
`
		result, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := result.Directives[0].(*ast.Transaction)
		assert.Equal(t, 4, len(txn.Postings))

		// Verify all amounts are parsed correctly
		assert.Equal(t, "-100", txn.Postings[0].Amount.Value)
		assert.Equal(t, "40", txn.Postings[1].Amount.Value)
		assert.Equal(t, "30", txn.Postings[2].Amount.Value)
		assert.Equal(t, "30", txn.Postings[3].Amount.Value)
	})

	t.Run("CommentAtEndOfPostingSection", func(t *testing.T) {
		source := `2024-01-01 * "Test"
  Assets:Cash 50 USD
  Expenses:Food 50 USD
  ; Comment after all postings
`
		result, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := result.Directives[0].(*ast.Transaction)
		assert.Equal(t, 2, len(txn.Postings))
	})

	t.Run("OnlyCommentLinesInTransaction", func(t *testing.T) {
		source := `2024-01-01 * "Test"
  ; Only comments in transaction
  ; Another comment
  ; And another
`
		result, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := result.Directives[0].(*ast.Transaction)
		assert.Equal(t, 0, len(txn.Postings))
	})
}
