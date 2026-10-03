package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/loader"
)

// BenchmarkCheckScaling runs check's load, booking, validation and error
// rendering on ledgers growing 4× per step. Linear work keeps MB/s flat
// across a scenario's steps; MB/s falling about 4× per step means it has
// turned quadratic.
//
//	go test ./cli -run '^$' -bench CheckScaling
func BenchmarkCheckScaling(b *testing.B) {
	scenarios := []struct {
		name string
		// txn writes the i-th transaction.
		txn func(buf *strings.Builder, date string, i int)
		// errors is how many errors check reports for n transactions.
		errors func(n int) int
	}{
		{
			name:   "spending",
			txn:    spendingTxn,
			errors: func(int) int { return 0 },
		},
		{
			// Every purchase has its own cost, so the brokerage
			// inventory holds one lot per purchase.
			name: "lots",
			txn: func(buf *strings.Builder, date string, i int) {
				fmt.Fprintf(buf, "%s * \"Buy\"\n  Assets:Brokerage  2 STOCK {%d.%02d USD}\n  Assets:Checking\n\n", date, 100+i/100, i%100)
			},
			errors: func(int) int { return 0 },
		},
		{
			name: "syntax_errors",
			txn: func(buf *strings.Builder, date string, i int) {
				if i%4 != 0 {
					spendingTxn(buf, date, i)
					return
				}
				fmt.Fprintf(buf, "%s * \"Broken\"\n  Expenses:Food  USD 12.34\n  Assets:Checking\n\n", date)
			},
			errors: func(n int) int { return (n + 3) / 4 },
		},
	}

	for _, sc := range scenarios {
		for _, n := range []int{1000, 4000, 16000} {
			b.Run(fmt.Sprintf("%s/txns=%d", sc.name, n), func(b *testing.B) {
				source := scalingLedger(n, sc.txn)
				path := filepath.Join(b.TempDir(), "ledger.beancount")
				if err := os.WriteFile(path, source, 0o644); err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(source)))

				ctx := context.Background()
				for b.Loop() {
					result, err := ledgerload.Load(ctx, loader.Source{Path: path})
					if err != nil {
						b.Fatal(err)
					}
					errorCount := checkLedger(io.Discard, result, path)
					if want := sc.errors(n); errorCount != want {
						b.Fatalf("got %d errors, want %d", errorCount, want)
					}
				}
			})
		}
	}
}

func spendingTxn(buf *strings.Builder, date string, _ int) {
	fmt.Fprintf(buf, "%s * \"Groceries\"\n  Expenses:Food  12.34 USD\n  Assets:Checking\n\n", date)
}

// scalingLedger opens the accounts and writes n transactions, four a day.
func scalingLedger(n int, txn func(buf *strings.Builder, date string, i int)) []byte {
	var buf strings.Builder
	buf.WriteString(`2020-01-01 open Assets:Checking USD
2020-01-01 open Assets:Brokerage
2020-01-01 open Expenses:Food USD

`)
	start := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range n {
		txn(&buf, start.AddDate(0, 0, i/4).Format("2006-01-02"), i)
	}
	return []byte(buf.String())
}
