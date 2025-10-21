package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
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
