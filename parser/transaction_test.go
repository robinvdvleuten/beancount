package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// TestParseTransactionBasic tests parsing a basic two-posting transaction
func TestParseTransactionBasic(t *testing.T) {
	source := `2024-01-15 * "Basic transaction"
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))
	assert.Equal(t, "Assets:Checking", string(txn.Postings[0].Account))
	assert.Equal(t, "Expenses:Food", string(txn.Postings[1].Account))
}

// TestParseTransactionWithPayeeAndNarration tests transaction with both payee and narration
func TestParseTransactionWithPayeeAndNarration(t *testing.T) {
	source := `2024-01-15 * "Payee Name" "Transaction narration"
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, "Payee Name", txn.Payee.Value)
	assert.Equal(t, "Transaction narration", txn.Narration.Value)
}

// TestParseTransactionWithExclamationFlag tests pending transaction flag
func TestParseTransactionWithExclamationFlag(t *testing.T) {
	source := `2024-01-15 ! "Pending transaction"
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, "!", txn.Flag)
}

// TestParseTransactionWithThreePostings tests transaction with three postings
func TestParseTransactionWithThreePostings(t *testing.T) {
	source := `2024-01-15 * "Three-way split"
  Assets:Checking   100.00 USD
  Expenses:Food      50.00 USD
  Expenses:Transport -50.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 3, len(txn.Postings))
	assert.Equal(t, "Assets:Checking", string(txn.Postings[0].Account))
	assert.Equal(t, "Expenses:Food", string(txn.Postings[1].Account))
	assert.Equal(t, "Expenses:Transport", string(txn.Postings[2].Account))
}

// TestParseTransactionWithImplicitPosting tests transaction with missing amount (implicit posting)
func TestParseTransactionWithImplicitPosting(t *testing.T) {
	source := `2024-01-15 * "Implicit amount"
  Assets:Checking   100.00 USD
  Expenses:Food
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))
	assert.True(t, txn.Postings[0].Amount != nil)
	assert.Equal(t, (*ast.Amount)(nil), txn.Postings[1].Amount)
}

// TestParseTransactionWithTrailingWhitespace tests that trailing whitespace after amounts
// doesn't break the parser. This is a regression test for a bug where trailing spaces
// would cause the lexer to emit unwanted NEWLINE tokens, breaking the parsePostings loop.
// See: INVESTIGATION_ERROR4_ROOT_CAUSE.md
func TestParseTransactionWithTrailingWhitespace(t *testing.T) {
	// Simulates the BITVAVO transaction bug: trailing space after "4.00 EUR"
	// would cause the lexer to emit a NEWLINE token, breaking the parser
	source := "2025-05-21 * \"BITVAVO\"\n" +
		"  Liabilities:CreditCard:Clix   -400 EUR\n" +
		"  Expenses:Bank:Costs           4.00 EUR \n" + // Note: trailing space before newline
		"  Assets:Bitvavo:Investments:Cash\n"

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err, "should parse successfully despite trailing whitespace")

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok, "directive should be a transaction")

	// All 3 postings should be parsed, not just 2
	assert.Equal(t, 3, len(txn.Postings), "should parse all 3 postings despite trailing space")

	assert.Equal(t, "Liabilities:CreditCard:Clix", string(txn.Postings[0].Account))
	assert.Equal(t, "Expenses:Bank:Costs", string(txn.Postings[1].Account))
	assert.Equal(t, "Assets:Bitvavo:Investments:Cash", string(txn.Postings[2].Account))

	// Third posting should have no amount (for implicit inference)
	assert.Equal(t, (*ast.Amount)(nil), txn.Postings[2].Amount, "third posting should have no explicit amount")
}

// TestParseTransactionWithBlankLinesBetweenPostings tests parsing when blank lines appear between postings
func TestParseTransactionWithBlankLinesBetweenPostings(t *testing.T) {
	source := `2024-01-15 * "With blank lines"
  Assets:Checking   100.00 USD

  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	// Blank lines should be skipped gracefully
	assert.Equal(t, 2, len(txn.Postings))
}

// TestParseTransactionWithCost tests transaction with explicit cost specification
func TestParseTransactionWithCost(t *testing.T) {
	source := `2024-01-15 * "Buy shares"
  Assets:Stocks        10 GOOG {100.00 USD}
  Assets:Cash      -1000.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))

	// First posting should have cost
	assert.True(t, txn.Postings[0].Cost != nil)
	assert.True(t, txn.Postings[0].Cost.Amount != nil)
	assert.Equal(t, "100.00", txn.Postings[0].Cost.Amount.Value)
	assert.Equal(t, "USD", txn.Postings[0].Cost.Amount.Currency)
}

// TestParseTransactionWithPrice tests transaction with price specification
func TestParseTransactionWithPrice(t *testing.T) {
	source := `2024-01-15 * "Exchange currency"
  Assets:Checking     100.00 EUR @ 1.10 USD
  Assets:USD         -110.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))

	// First posting should have price
	assert.True(t, txn.Postings[0].Price != nil)
	assert.Equal(t, "1.10", txn.Postings[0].Price.Value)
	assert.Equal(t, "USD", txn.Postings[0].Price.Currency)
	assert.Equal(t, false, txn.Postings[0].PriceTotal)
}

// TestParseTransactionWithTotalPrice tests transaction with total price (@@)
func TestParseTransactionWithTotalPrice(t *testing.T) {
	source := `2024-01-15 * "Exchange with total"
  Assets:Checking     100.00 EUR @@ 110.00 USD
  Assets:USD         -110.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)

	// First posting should have total price
	assert.True(t, txn.Postings[0].Price != nil)
	assert.Equal(t, true, txn.Postings[0].PriceTotal)
}

// TestParseTransactionWithEmptyCost tests transaction with empty cost specification {}
func TestParseTransactionWithEmptyCost(t *testing.T) {
	source := `2024-01-15 * "Sell shares with empty cost"
  Assets:Stocks        -10 GOOG {}
  Assets:Cash       1000.00 USD
  Income:Gains
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)

	// First posting should have empty cost
	assert.True(t, txn.Postings[0].Cost != nil)
	assert.Equal(t, true, txn.Postings[0].Cost.IsEmpty())
}

// TestParseTransactionWithFlags tests posting-level flags
func TestParseTransactionWithFlags(t *testing.T) {
	source := `2024-01-15 * "With posting flags"
  * Assets:Checking   100.00 USD
  ! Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, "*", txn.Postings[0].Flag)
	assert.Equal(t, "!", txn.Postings[1].Flag)
}

// TestParseTransactionWithMetadata tests transaction with metadata
func TestParseTransactionWithMetadata(t *testing.T) {
	source := `2024-01-15 * "With metadata"
  invoice: "INV-123"
  category: "groceries"
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Metadata))
}

// TestParseTransactionWithManyPostings tests transaction with many postings
func TestParseTransactionWithManyPostings(t *testing.T) {
	source := `2024-01-15 * "Multi-way split"
  Assets:Checking    200.00 USD
  Expenses:Food       50.00 USD
  Expenses:Transport  30.00 USD
  Expenses:Housing    80.00 USD
  Expenses:Health     20.00 USD
  Expenses:Other     -20.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 6, len(txn.Postings))
}

// TestParseTransactionWithComplexStructure tests complex transaction with costs, prices, and metadata
func TestParseTransactionWithComplexStructure(t *testing.T) {
	source := `2024-01-15 * "Complex transaction"
  Assets:Stocks      10 GOOG {100.00 USD}
    cost-basis: "1000.00"
  Assets:Cash     -1000.00 USD
    fee: "5.00"
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))
	assert.True(t, txn.Postings[0].Cost != nil)
	assert.Equal(t, 1, len(txn.Postings[0].Metadata))
	assert.Equal(t, 1, len(txn.Postings[1].Metadata))
}

// TestParseTransactionWithTags tests transaction with tags
func TestParseTransactionWithTags(t *testing.T) {
	source := `2024-01-15 * "Tagged transaction" #trip #food
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Tags))
}

// TestParseTransactionWithLinks tests transaction with links
func TestParseTransactionWithLinks(t *testing.T) {
	source := `2024-01-15 * "Linked transaction" ^invoice-123
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 1, len(txn.Links))
}

// TestParseMultipleTransactions tests parsing multiple transactions in sequence
func TestParseMultipleTransactions(t *testing.T) {
	source := `2024-01-15 * "First"
  Assets:Checking   100.00 USD
  Expenses:Food    -100.00 USD

2024-01-16 * "Second"
  Assets:Checking    50.00 USD
  Expenses:Gas      -50.00 USD

2024-01-17 * "Third"
  Assets:Checking    75.00 USD
  Expenses:Other    -75.00 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(result.Directives))

	for i, directive := range result.Directives {
		txn, ok := directive.(*ast.Transaction)
		assert.True(t, ok, "directive %d should be a transaction", i)
		assert.Equal(t, 2, len(txn.Postings))
	}
}

// TestParseTransactionWithExpressions tests postings with arithmetic expressions
func TestParseTransactionWithExpressions(t *testing.T) {
	source := `2024-01-15 * "With expressions"
   Assets:Checking      (100 + 50) USD
   Expenses:Food       -(100 + 50) USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))
	assert.True(t, txn.Postings[0].Amount != nil)
}

// TestParseTransactionWithCommaSeparatedAmounts tests parsing numbers with comma thousands separators
func TestParseTransactionWithCommaSeparatedAmounts(t *testing.T) {
	source := `2024-01-15 * "Large transaction"
  Assets:Bank       1,000 USD
  Expenses:Food    -1,000 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Directives))

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))
	// The amount value should have commas stripped during parsing
	assert.Equal(t, "1000", txn.Postings[0].Amount.Value)
	assert.Equal(t, "-1000", txn.Postings[1].Amount.Value)
}

// TestParseTransactionWithLargeCommaSeparatedAmounts tests multi-comma separated numbers
func TestParseTransactionWithLargeCommaSeparatedAmounts(t *testing.T) {
	source := `2024-01-15 * "Million dollar transaction"
  Assets:Bank       1,234,567.89 USD
  Expenses:Food    -1,234,567.89 USD
`

	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	txn, ok := result.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))
	assert.Equal(t, "1234567.89", txn.Postings[0].Amount.Value)
	assert.Equal(t, "-1234567.89", txn.Postings[1].Amount.Value)
}
