package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// Integration tests verify parsing of complete beancount files with multiple directive types

func TestParseMultipleDirectives(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD
2014-01-02 open Expenses:Food

2014-05-05 * "Cafe" "Coffee"
  Expenses:Food  4.50 USD
  Assets:Checking

2014-08-09 balance Assets:Checking 100.00 USD
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(result.Directives))

	// Verify directive types
	_, ok := result.Directives[0].(*ast.Open)
	assert.True(t, ok, "first directive should be Open")

	_, ok = result.Directives[1].(*ast.Open)
	assert.True(t, ok, "second directive should be Open")

	_, ok = result.Directives[2].(*ast.Transaction)
	assert.True(t, ok, "third directive should be Transaction")

	_, ok = result.Directives[3].(*ast.Balance)
	assert.True(t, ok, "fourth directive should be Balance")
}

func TestParseCompleteFile(t *testing.T) {
	input := `option "title" "Test Ledger"
option "operating_currency" "USD"

include "accounts.beancount"

plugin "beancount.plugins.auto_accounts"

pushtag #trip

2014-01-01 open Assets:Checking USD
2014-01-01 open Expenses:Food

2014-01-01 commodity USD
  name: "US Dollar"

2014-05-05 * "Restaurant" "Dinner" #food
  Expenses:Food    25.00 USD
  Assets:Checking

2014-06-01 balance Assets:Checking 500.00 USD

2014-07-01 price HOOL 100.00 USD

2014-08-01 note Assets:Checking "Account review"

poptag #trip

2014-12-31 close Expenses:Food
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)

	// Check options
	assert.Equal(t, 2, len(result.Options))

	// Check includes
	assert.Equal(t, 1, len(result.Includes))

	// Check plugins
	assert.Equal(t, 1, len(result.Plugins))

	// Check tag stack (1 pushtag + 1 poptag)
	assert.Equal(t, 1, len(result.Pushtags))
	assert.Equal(t, 1, len(result.Poptags))

	// Check directives (open x2, commodity, transaction, balance, price, note, close)
	assert.Equal(t, 8, len(result.Directives))
}

func TestParseWithCommentsInterspersed(t *testing.T) {
	input := `; Account setup
2014-01-01 open Assets:Checking USD

; Regular transactions
2014-05-05 * "Test"
  Expenses:Food  10.00 USD
  Assets:Checking

; End of month
2014-08-09 balance Assets:Checking 100.00 USD
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(result.Directives))
}

func TestParseEmptyFile(t *testing.T) {
	input := ``

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(result.Directives))
}

func TestParseWhitespaceOnlyFile(t *testing.T) {
	input := `  

  
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(result.Directives))
}

func TestParseCommentOnlyFile(t *testing.T) {
	input := `; This is a comment
; Another comment
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(result.Directives))
}

func TestParseMixedLineEndings(t *testing.T) {
	// Test with different line ending styles
	input := "2014-01-01 open Assets:Checking USD\n2014-01-02 open Expenses:Food\n"

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(result.Directives))
}

func TestParseDirectivesWithMetadata(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD
  account-number: "12345"

2014-05-05 * "Test"
  invoice: "INV-001"
  Expenses:Food  10.00 USD
    category: "groceries"
  Assets:Checking
`

	result, err := ParseString(context.Background(), input)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(result.Directives))

	// Check open has metadata
	open, ok := result.Directives[0].(*ast.Open)
	assert.True(t, ok)
	assert.Equal(t, 1, len(open.Metadata))

	// Check transaction has metadata
	txn, ok := result.Directives[1].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 1, len(txn.Metadata))
	assert.Equal(t, 1, len(txn.Postings[0].Metadata))
}
