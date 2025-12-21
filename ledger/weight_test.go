package ledger

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/shopspring/decimal"
)

func TestCalculateWeights_SimpleCost(t *testing.T) {
	// Parse a transaction with cost
	input := `
		2021-01-01 * "Buy stock"
		  Assets:Cash       -500.00 USD
		  Assets:Stock         5 AAPL {100.00 USD}
	`

	tree := parser.MustParseString(context.Background(), input)
	assert.Equal(t, 1, len(tree.Directives))

	txn, ok := tree.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))

	// Test cash posting (no cost)
	cashWeights, err := calculateWeights(txn.Postings[0])
	assert.NoError(t, err)
	assert.Equal(t, 1, len(cashWeights))
	assert.Equal(t, "USD", cashWeights[0].Currency)
	assert.Equal(t, "-500", cashWeights[0].Amount.String())

	// Test stock posting (with cost)
	stockWeights, err := calculateWeights(txn.Postings[1])
	assert.NoError(t, err)
	t.Logf("Stock weights: %+v", stockWeights)
	t.Logf("Posting.Cost: %+v", txn.Postings[1].Cost)

	// With cost, only the cost currency weight is contributed (not the commodity!)
	assert.Equal(t, 1, len(stockWeights), "should have 1 weight (cost only)")

	// Only weight: +500 USD (5 * 100)
	assert.Equal(t, "USD", stockWeights[0].Currency)
	assert.Equal(t, "500", stockWeights[0].Amount.String())
}

func TestBalanceWeights(t *testing.T) {
	// Test that balanceWeights correctly sums weights
	allWeights := []weightSet{
		// Cash posting
		{{Amount: mustDecimal("-500"), Currency: "USD"}},
		// Stock posting with cost (only cost weight, not commodity!)
		{{Amount: mustDecimal("500"), Currency: "USD"}},
	}

	balance := balanceWeights(allWeights)
	defer putBalanceMap(balance)

	t.Logf("Balance: %+v", balance)

	// Should balance to USD: 0
	assert.Equal(t, "0", balance["USD"].String())
}

func mustDecimal(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

func TestFullTransactionWithCost(t *testing.T) {
	// Integration test: full transaction processing with cost
	input := `
		2021-01-01 open Assets:Cash
		2021-01-01 open Assets:Stock

		2021-01-02 * "Buy stock"
		  Assets:Cash   -500.00 USD
		  Assets:Stock     5 AAPL {100.00 USD}
	`

	ast := parser.MustParseString(context.Background(), input)

	l := New()
	err := l.Process(context.Background(), ast)

	// Should have NO errors - transaction should balance!
	if err != nil {
		t.Logf("Errors: %v", err)
		if valErr, ok := err.(*ValidationErrors); ok {
			for _, e := range valErr.Errors {
				t.Logf("  - %v", e)
			}
		}
	}
	assert.NoError(t, err, "transaction with cost should balance")
}

func TestCalculateWeights_Price(t *testing.T) {
	// Parse a transaction with price
	input := `
		2021-01-01 * "Sell stock"
		  Assets:Stock     -10 AAPL {150.00 USD} @ 160.00 USD
		  Assets:Cash     1600.00 USD
	`

	tree := parser.MustParseString(context.Background(), input)

	txn, ok := tree.Directives[0].(*ast.Transaction)
	assert.True(t, ok)

	// Test stock posting (with cost and price)
	stockWeights, err := calculateWeights(txn.Postings[0])
	assert.NoError(t, err)

	// When BOTH cost and price are present, COST is used for weight (price is informational)
	// This is correct: the cost basis determines the accounting weight
	assert.Equal(t, 1, len(stockWeights))

	// Only weight: -1500 USD (-10 * 150 from COST, not price)
	assert.Equal(t, "USD", stockWeights[0].Currency)
	assert.Equal(t, "-1500", stockWeights[0].Amount.String())
}
