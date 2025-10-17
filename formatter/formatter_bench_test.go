package formatter

import (
	"context"
	"bytes"
	"testing"

	"github.com/robinvdvleuten/beancount/parser"
)

// BenchmarkFormat benchmarks the formatter with various file sizes
func BenchmarkFormat(b *testing.B) {
	b.Run("SmallFile", func(b *testing.B) {
		source := `
option "title" "Test Ledger"

2021-01-01 open Assets:Checking USD
2021-01-01 open Expenses:Food

2021-01-02 * "Grocery Store" "Weekly groceries"
  Assets:Checking  -75.50 USD
  Expenses:Food  75.50 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		if err != nil {
			b.Fatal(err)
		}

		f := New()
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			if err := f.Format(context.Background(), ast, []byte(source), &buf); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("MediumFile", func(b *testing.B) {
		// Generate a medium-sized ledger with multiple transactions
		var source string
		source += `option "title" "Test Ledger"
option "operating_currency" "USD"

2021-01-01 open Assets:Checking USD
2021-01-01 open Assets:Savings USD
2021-01-01 open Expenses:Food USD
2021-01-01 open Expenses:Transportation USD
2021-01-01 open Income:Salary USD

`
		// Add 100 transactions
		for i := 1; i <= 100; i++ {
			source += `2021-01-02 * "Store" "Purchase"
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD

`
		}

		ast, err := parser.ParseString(context.Background(), source)
		if err != nil {
			b.Fatal(err)
		}

		f := New()
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			if err := f.Format(context.Background(), ast, []byte(source), &buf); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("LargeFile", func(b *testing.B) {
		// Generate a large ledger with many transactions
		var source string
		source += `option "title" "Large Test Ledger"

2021-01-01 open Assets:US:BofA:Checking USD
2021-01-01 open Expenses:Food:Restaurant USD

`
		// Add 1000 transactions
		for i := 1; i <= 1000; i++ {
			source += `2021-01-02 * "Restaurant Name" "Lunch meeting"
  Assets:US:BofA:Checking  -125.75 USD
  Expenses:Food:Restaurant  125.75 USD

`
		}

		ast, err := parser.ParseString(context.Background(), source)
		if err != nil {
			b.Fatal(err)
		}

		f := New()
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			if err := f.Format(context.Background(), ast, []byte(source), &buf); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkFormatWithComments benchmarks formatting files that contain comments
func BenchmarkFormatWithComments(b *testing.B) {
	b.Run("SmallFileWithComments", func(b *testing.B) {
		source := `; Header comment
option "title" "Test Ledger"

; Account section
2021-01-01 open Assets:Checking  USD
2021-01-01 open Expenses:Food

; Transactions
2021-01-02 * "Grocery Store" "Weekly groceries"
  Assets:Checking  -75.50 USD
  Expenses:Food  75.50 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		if err != nil {
			b.Fatal(err)
		}

		f := New()
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			if err := f.Format(context.Background(), ast, []byte(source), &buf); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("DisabledCommentPreservation", func(b *testing.B) {
		source := `; Header comment
option "title" "Test Ledger"

2021-01-01 open Assets:Checking  USD
2021-01-02 * "Test"
  Assets:Checking  -75.50 USD
  Expenses:Food  75.50 USD
`
		ast, err := parser.ParseString(context.Background(), source)
		if err != nil {
			b.Fatal(err)
		}

		f := New(WithPreserveComments(false), WithPreserveBlanks(false))
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			if err := f.Format(context.Background(), ast, []byte(source), &buf); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCurrencyColumnCalculation benchmarks just the currency column calculation
func BenchmarkCurrencyColumnCalculation(b *testing.B) {
	var source string
	for i := 1; i <= 100; i++ {
		source += `2021-01-02 * "Test"
  Assets:Checking  -100.00 USD
  Expenses:Food  100.00 USD

`
	}

	ast, err := parser.ParseString(context.Background(), source)
	if err != nil {
		b.Fatal(err)
	}

	f := New()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		f.calculateCurrencyColumn(ast)
	}
}

// BenchmarkStringBuilderVsConcat demonstrates the performance difference
// This is for documentation purposes to show the improvement
func BenchmarkStringBuilderVsConcat(b *testing.B) {
	b.Run("StringBuilder", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			for j := 0; j < 100; j++ {
				buf.WriteString("test string ")
				buf.WriteString("another string ")
				buf.WriteByte('\n')
			}
			_ = buf.String()
		}
	})

	b.Run("StringConcat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			output := ""
			for j := 0; j < 100; j++ {
				output += "test string "
				output += "another string "
				output += "\n"
			}
			_ = output
		}
	})
}
