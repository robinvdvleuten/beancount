package ledger

import (
	"context"
	"testing"

	"github.com/robinvdvleuten/beancount/parser"
)

// BenchmarkProcessTransaction benchmarks a simple 2-posting transaction
func BenchmarkProcessTransaction(b *testing.B) {
	input := `
2021-01-01 open Assets:Cash USD
2021-01-01 open Expenses:Food USD

2021-01-02 * "Simple transaction"
  Assets:Cash      -50.00 USD
  Expenses:Food     50.00 USD
`

	ast := parser.MustParseString(context.Background(), input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := New()
		_ = l.Process(context.Background(), ast)
	}
}

// BenchmarkProcessTransactionWithCost benchmarks transaction with cost basis
func BenchmarkProcessTransactionWithCost(b *testing.B) {
	input := `
2021-01-01 open Assets:Cash USD
2021-01-01 open Assets:Stock
2021-01-01 open Expenses:Commission USD

2021-01-02 * "Buy stock"
  Assets:Cash         -1000.00 USD
  Assets:Stock             10 AAPL {100.00 USD}
  Expenses:Commission       5.00 USD
`

	ast := parser.MustParseString(context.Background(), input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := New()
		_ = l.Process(context.Background(), ast)
	}
}

// BenchmarkProcessTransactionWithInference benchmarks amount inference
func BenchmarkProcessTransactionWithInference(b *testing.B) {
	input := `
2021-01-01 open Assets:Cash USD
2021-01-01 open Expenses:Food USD

2021-01-02 * "Inferred amount"
  Assets:Cash      -50.00 USD
  Expenses:Food
`

	ast := parser.MustParseString(context.Background(), input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := New()
		_ = l.Process(context.Background(), ast)
	}
}

// BenchmarkProcessTransactionComplex benchmarks a complex multi-posting transaction
func BenchmarkProcessTransactionComplex(b *testing.B) {
	input := `
2021-01-01 open Assets:Checking USD
2021-01-01 open Assets:Savings USD
2021-01-01 open Assets:Stock
2021-01-01 open Expenses:Commission USD
2021-01-01 open Income:Salary USD
2021-01-01 open Expenses:Taxes USD

2021-01-02 * "Payroll and investment"
  Assets:Checking     1500.00 USD
  Assets:Savings       500.00 USD
  Assets:Stock          10 VTSAX {100.00 USD}
  Expenses:Commission    5.00 USD
  Expenses:Taxes       400.00 USD
  Income:Salary      -3405.00 USD
`

	ast := parser.MustParseString(context.Background(), input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := New()
		_ = l.Process(context.Background(), ast)
	}
}

// BenchmarkProcessBalance benchmarks balance checking
func BenchmarkProcessBalance(b *testing.B) {
	input := `
2021-01-01 open Assets:Cash USD
2021-01-01 open Expenses:Food USD

2021-01-02 * "Transaction"
  Assets:Cash      -50.00 USD
  Expenses:Food     50.00 USD

2021-01-03 balance Assets:Cash  -50.00 USD
`

	ast := parser.MustParseString(context.Background(), input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := New()
		_ = l.Process(context.Background(), ast)
	}
}

// BenchmarkProcessPad benchmarks pad directive processing
func BenchmarkProcessPad(b *testing.B) {
	input := `
2021-01-01 open Assets:Checking USD
2021-01-01 open Equity:Opening USD

2021-01-02 * "Initial"
  Assets:Checking   100.00 USD
  Equity:Opening   -100.00 USD

2021-01-05 pad Assets:Checking Equity:Opening
2021-01-06 balance Assets:Checking  500.00 USD
`

	ast := parser.MustParseString(context.Background(), input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := New()
		_ = l.Process(context.Background(), ast)
	}
}
