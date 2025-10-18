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
	assert.NoError(t, err)

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed transaction!")
}

func TestParseBalance(t *testing.T) {
	input := `2014-08-09 balance Assets:Checking 100.00 USD`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed balance!")
}

func TestParseOpen(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

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
	assert.NoError(t, err)

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
	assert.NoError(t, err)

	assert.Equal(t, 2, len(ast.Directives))

	t.Logf("Successfully parsed with comments!")
}

func TestParseWithMetadata(t *testing.T) {
	input := `2014-01-01 open Assets:Checking
  account-number: "123456"
  bank: "Chase"
`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed with metadata!")
}

func TestParseAmountWithoutCurrency(t *testing.T) {
	input := `2023-06-02 * "buy stocks"
  Assets:Investments:Stock  100 STOCK {}
  Assets:Cash -1600.00
`

	ast, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(ast.Directives))

	t.Logf("Successfully parsed transaction with amount without currency!")
}

func TestParseTransaction_WithExpressions(t *testing.T) {
	// Test the example from TODO.txt
	input := `2014-10-05 * "Split bill"
  Liabilities:CreditCard         -45.00 USD
  Assets:Receivable:John         ((40.00/3) + 5) USD
  Assets:Receivable:Michael      40.00/3 USD
  Assets:Receivable:Peter        40.00/3 USD
`

	tree, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(tree.Directives))

	txn, ok := tree.Directives[0].(*ast.Transaction)
	assert.True(t, ok, "expected Transaction, got %T", tree.Directives[0])

	assert.Equal(t, 4, len(txn.Postings))

	// Check that expressions were evaluated correctly
	tests := []struct {
		posting int
		want    string
	}{
		{0, "-45.00"},              // Simple negative preserves formatting
		{1, "18.3333333333333333"}, // ((40.00/3) + 5)
		{2, "13.3333333333333333"}, // 40.00/3
		{3, "13.3333333333333333"}, // 40.00/3
	}

	for _, tt := range tests {
		assert.NotEqual(t, nil, txn.Postings[tt.posting].Amount, "posting %d: amount is nil", tt.posting)
		if txn.Postings[tt.posting].Amount != nil {
			got := txn.Postings[tt.posting].Amount.Value
			assert.Equal(t, tt.want, got, "posting %d amount mismatch", tt.posting)
		}
	}

	t.Logf("Successfully parsed transaction with expression amounts!")
}

func TestParseTransaction_MixedAmounts(t *testing.T) {
	// Test mix of simple and expression amounts
	input := `2023-01-01 * "Test mixed amounts"
  Assets:Bank            100.00 USD
  Expenses:Food          (20 + 5) * 2 USD
  Expenses:Transport     -10.50 USD
  Assets:Cash
`

	tree, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(tree.Directives))

	txn, ok := tree.Directives[0].(*ast.Transaction)
	assert.True(t, ok, "expected Transaction, got %T", tree.Directives[0])

	assert.Equal(t, 4, len(txn.Postings))

	// Verify amounts
	assert.Equal(t, "100.00", txn.Postings[0].Amount.Value, "posting 0")

	// (20 + 5) * 2 = 25 * 2 = 50
	assert.Equal(t, "50", txn.Postings[1].Amount.Value, "posting 1")

	// Negative amount (simple negative, preserves formatting)
	assert.Equal(t, "-10.50", txn.Postings[2].Amount.Value, "posting 2")

	// No amount (will be inferred)
	assert.Equal(t, nil, txn.Postings[3].Amount, "posting 3: expected nil amount")

	t.Logf("Successfully parsed transaction with mixed amount types!")
}

func TestParseTransaction_NegativeExpressions(t *testing.T) {
	// Test negative expressions (bug fix verification)
	input := `2023-01-15 * "Test negative expressions"
  Assets:Bank            -5 + 10 USD
  Expenses:Food          -10 * 2 USD
  Expenses:Transport     -100 / 2 EUR
  Assets:Cash
`

	tree, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(tree.Directives))

	txn, ok := tree.Directives[0].(*ast.Transaction)
	assert.True(t, ok, "expected Transaction, got %T", tree.Directives[0])

	assert.Equal(t, 4, len(txn.Postings))

	// Verify negative expression amounts were evaluated correctly
	tests := []struct {
		posting int
		want    string
	}{
		{0, "5"},   // -5 + 10 = 5
		{1, "-20"}, // -10 * 2 = -20
		{2, "-50"}, // -100 / 2 = -50
	}

	for _, tt := range tests {
		assert.NotEqual(t, nil, txn.Postings[tt.posting].Amount, "posting %d: amount is nil", tt.posting)
		if txn.Postings[tt.posting].Amount != nil {
			got := txn.Postings[tt.posting].Amount.Value
			assert.Equal(t, tt.want, got, "posting %d amount mismatch", tt.posting)
		}
	}

	t.Logf("Successfully parsed transaction with negative expression amounts!")
}
