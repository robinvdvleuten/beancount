package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestClassifyPostings(t *testing.T) {
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")
	stocks, _ := ast.NewAccount("Assets:Stocks")

	tests := []struct {
		name                 string
		postings             []*ast.Posting
		wantWithAmounts      int
		wantWithoutAmounts   int
		wantWithEmptyCosts   int
		wantWithExplicitCost int
	}{
		{
			name: "all postings have amounts",
			postings: []*ast.Posting{
				ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
				ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   0,
			wantWithEmptyCosts:   0,
			wantWithExplicitCost: 0,
		},
		{
			name: "one posting without amount",
			postings: []*ast.Posting{
				ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
				ast.NewPosting(checking),
			},
			wantWithAmounts:      1,
			wantWithoutAmounts:   1,
			wantWithEmptyCosts:   0,
			wantWithExplicitCost: 0,
		},
		{
			name: "posting with empty cost",
			postings: []*ast.Posting{
				ast.NewPosting(stocks,
					ast.WithAmount("10", "HOOL"),
					ast.WithCost(ast.NewEmptyCost()),
				),
				ast.NewPosting(checking, ast.WithAmount("-5000", "USD")),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   0,
			wantWithEmptyCosts:   1,
			wantWithExplicitCost: 0,
		},
		{
			name: "posting with explicit cost",
			postings: []*ast.Posting{
				ast.NewPosting(stocks,
					ast.WithAmount("10", "HOOL"),
					ast.WithCost(ast.NewCost(ast.NewAmount("500", "USD"))),
				),
				ast.NewPosting(checking, ast.WithAmount("-5000", "USD")),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   0,
			wantWithEmptyCosts:   0,
			wantWithExplicitCost: 1,
		},
		{
			name: "mixed posting types",
			postings: []*ast.Posting{
				ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
				ast.NewPosting(stocks,
					ast.WithAmount("5", "HOOL"),
					ast.WithCost(ast.NewEmptyCost()),
				),
				ast.NewPosting(checking),
			},
			wantWithAmounts:      2,
			wantWithoutAmounts:   1,
			wantWithEmptyCosts:   1,
			wantWithExplicitCost: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := classifyPostings(tt.postings)

			assert.Equal(t, tt.wantWithAmounts, len(pc.withAmounts))

			assert.Equal(t, tt.wantWithoutAmounts, len(pc.withoutAmounts))

			assert.Equal(t, tt.wantWithEmptyCosts, len(pc.withEmptyCosts))

			assert.Equal(t, tt.wantWithExplicitCost, len(pc.withExplicitCost))
		})
	}
}

func BenchmarkClassifyPostings(b *testing.B) {
	checking, _ := ast.NewAccount("Assets:Checking")
	expenses, _ := ast.NewAccount("Expenses:Groceries")

	postings := []*ast.Posting{
		ast.NewPosting(expenses, ast.WithAmount("50.00", "USD")),
		ast.NewPosting(checking, ast.WithAmount("-50.00", "USD")),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		classifyPostings(postings)
	}
}
