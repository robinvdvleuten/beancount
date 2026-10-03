package ledger

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	sharedconfig "github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/parser"
)

// interpolateLast books every transaction of source but the last, then
// interpolates each Currency group of the last as the booker would: sorted
// into groups, with the positions its reductions booked and the
// transaction's spec tolerances. It returns the groups' results rendered,
// one line per posting and residual, and the errors' messages.
func interpolateLast(t *testing.T, source string) (lines, errs []string) {
	t.Helper()
	tree := parser.MustParseString(context.Background(), source)
	b := newBooker(sharedconfig.New(), newTolerances(nil), tree.Directives)
	var txns []*ast.Transaction
	for _, directive := range tree.Directives {
		if txn, ok := directive.(*ast.Transaction); ok {
			txns = append(txns, txn)
		}
	}
	for _, txn := range txns[:len(txns)-1] {
		_, bookErrs := b.book(txn)
		assert.Equal(t, 0, len(bookErrs))
	}

	txn := txns[len(txns)-1]
	groups, groupErrs := b.categorize(txn)
	assert.Equal(t, 0, len(groupErrs))
	specTolerances := b.tolerances.spec(txn.Postings)
	resolveCurrencies(txn, groups)
	for _, group := range groups {
		scratch := &scratchInventories{booker: b, staged: map[string]*inventory{}, own: map[string]*inventory{}}
		reductions, bookErrs := b.bookReductions(txn, group, scratch)
		assert.Equal(t, 0, len(bookErrs))

		interpolated, interpolateErrs := interpolate(txn, group, reductions, specTolerances)
		for _, err := range interpolateErrs {
			errs = append(errs, err.(*Diagnostic).Message())
		}
		if len(interpolateErrs) > 0 {
			assert.True(t, interpolated == nil, "a Dropped group has no result")
			continue
		}
		assert.Equal(t, len(group.postings), len(interpolated.postings))
		for i, posting := range interpolated.postings {
			assert.True(t, posting.posting == group.postings[i], "one result per posting, in the group's order")
			assert.False(t, posting.posting.Inferred || posting.posting.Automatic, "interpolation must not write to the postings")
			lines = append(lines, group.currency+": "+renderInterpolated(posting))
		}
		for _, currency := range residualCurrencies(nil, interpolated.residuals) {
			lines = append(lines, fmt.Sprintf("%s: residual %s %s", group.currency, interpolated.residuals[currency], currency))
		}
	}
	return lines, errs
}

// renderInterpolated renders what interpolation returns for a posting: its
// account, then only what was completed.
func renderInterpolated(posting interpolatedPosting) string {
	amount := func(a *ast.Amount) string { return a.Value + " " + a.Currency }
	var b strings.Builder
	b.WriteString(string(posting.posting.Account))
	if posting.amount != nil {
		b.WriteString(" units " + amount(posting.amount))
	}
	if cost := posting.cost; cost != nil {
		b.WriteString(" cost {")
		if cost.IsTotal {
			b.WriteString("{")
		}
		if cost.Amount != nil {
			b.WriteString(amount(cost.Amount))
		}
		if cost.Total != nil {
			b.WriteString(" # " + amount(cost.Total))
		}
		if cost.IsTotal {
			b.WriteString("}")
		}
		b.WriteString("}")
	}
	if posting.price != nil {
		b.WriteString(" price " + amount(posting.price))
	}
	if posting.leftOut {
		b.WriteString(" left out")
	}
	if len(posting.amounts) > 0 {
		amounts := make([]string, len(posting.amounts))
		for i, a := range posting.amounts {
			amounts[i] = amount(a)
		}
		b.WriteString(" booked at " + strings.Join(amounts, ", "))
	}
	return b.String()
}

func TestInterpolate(t *testing.T) {
	const lots = `
2024-01-01 open Assets:Stock "FIFO"

2024-01-02 * "Buy"
  Assets:Stock  5 HOOL {100.00 USD}
  Assets:Cash  -500.00 USD

2024-01-03 * "Buy"
  Assets:Stock  5 HOOL {110.00 USD}
  Assets:Cash  -550.00 USD
`

	tests := []struct {
		name     string
		source   string
		want     []string
		wantErrs []string
	}{
		{
			name: "nothing missing",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.00 USD
  Assets:Cash    -50.00 USD
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash",
			},
		},
		{
			name: "a residual beyond the tolerance",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.00 USD
  Assets:Cash    -40.00 USD
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash",
				"USD: residual 10 USD",
			},
		},
		{
			// Amounts at 3 and 4 decimals: the coarsest gives 0.0005, and a
			// residual of 0.0005 is not greater than it.
			name: "a residual within the tolerance",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.001 USD
  Assets:Cash    -50.0005 USD
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash",
			},
		},
		{
			// 0.0001 is greater than the 0.00005 four decimals give.
			name: "a residual just beyond the tolerance",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.0001 USD
  Assets:Cash    -50.0000 USD
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash",
				"USD: residual 0.0001 USD",
			},
		},
		{
			name: "an amount-less posting",
			source: `
2024-01-15 * "Test"
  Expenses:Food   33.33 USD
  Expenses:Food   33.33 USD
  Expenses:Food   33.34 USD
  Assets:Cash
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Expenses:Food",
				"USD: Expenses:Food",
				"USD: Assets:Cash units -100.00 USD booked at -100.00 USD",
			},
		},
		{
			name: "an amount-less posting is rounded to the tolerance",
			source: `
2024-01-15 * "Test"
  Expenses:Food   10.00 USD
  Expenses:Food   -3.333 USD
  Assets:Cash
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Expenses:Food",
				"USD: Assets:Cash units -6.67 USD booked at -6.67 USD",
			},
		},
		{
			name: "an amount-less posting over two currencies",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.00 USD
  Expenses:Food   30.00 EUR
  Assets:Cash
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash units -50.00 USD booked at -50.00 USD",
				"EUR: Expenses:Food",
				"EUR: Assets:Cash units -30.00 EUR booked at -30.00 EUR",
			},
		},
		{
			name: "an amount-less posting a group leaves nothing",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.00 USD
  Assets:Cash    -50.00 USD
  Equity:Rounding
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash",
				"USD: Equity:Rounding",
			},
		},
		{
			name: "a missing units number",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.00 USD
  Assets:Cash           USD
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash units -50.00 USD",
			},
		},
		{
			name: "a missing units number at cost",
			source: `
2024-01-15 * "Test"
  Assets:Stock     HOOL {100.00 USD}
  Assets:Cash   -1000.00 USD
`,
			want: []string{
				"USD: Assets:Stock units 10 HOOL",
				"USD: Assets:Cash",
			},
		},
		{
			name: "a missing cost number",
			source: `
2024-01-15 * "Test"
  Assets:Stock  10 HOOL {USD}
  Assets:Cash   -1000.00 USD
`,
			want: []string{
				"USD: Assets:Stock cost {100.00 USD}",
				"USD: Assets:Cash",
			},
		},
		{
			name: "a missing total cost number",
			source: `
2024-01-15 * "Test"
  Assets:Stock  10 HOOL {{USD}}
  Assets:Cash   -1000.00 USD
`,
			want: []string{
				"USD: Assets:Stock cost {{1000.00 USD}}",
				"USD: Assets:Cash",
			},
		},
		{
			name: "a missing per-unit number of a compound cost",
			source: `
2024-01-15 * "Test"
  Assets:Stock  10 HOOL {# 5.00 USD}
  Assets:Cash   -1005.00 USD
`,
			want: []string{
				"USD: Assets:Stock cost {100.00 USD # 5.00 USD}",
				"USD: Assets:Cash",
			},
		},
		{
			name: "a missing price number",
			source: `
2024-01-15 * "Test"
  Assets:Euros   100.00 EUR @ USD
  Assets:Cash   -120.00 USD
`,
			want: []string{
				"USD: Assets:Euros price 1.2 USD",
				"USD: Assets:Cash",
			},
		},
		{
			name: "a missing total price number",
			source: `
2024-01-15 * "Test"
  Assets:Euros   100.00 EUR @@ USD
  Assets:Cash   -120.00 USD
`,
			want: []string{
				"USD: Assets:Euros price 120 USD",
				"USD: Assets:Cash",
			},
		},
		{
			name: "missing units that interpolate to a zero weight are left out",
			source: `
2024-01-15 * "Test"
  Expenses:Food   50.00 USD
  Assets:Cash    -50.00 USD
  Equity:Rounding       USD
`,
			want: []string{
				"USD: Expenses:Food",
				"USD: Assets:Cash",
				"USD: Equity:Rounding units 0.00 USD left out",
			},
		},
		{
			name: "a missing cost on zero units is left out",
			source: `
2024-01-15 * "Test"
  Assets:Stock    0 HOOL {USD}
  Expenses:Food   50.00 USD
  Assets:Cash    -50.00 USD
`,
			want: []string{
				"USD: Assets:Stock left out",
				"USD: Expenses:Food",
				"USD: Assets:Cash",
			},
		},
		{
			name: "two missing numbers",
			source: `
2024-01-15 * "Test"
  Assets:Stock  10 HOOL {USD}
  Assets:Stock   5 AAPL {USD}
  Assets:Cash   -1000.00 USD
`,
			wantErrs: []string{"Too many missing numbers for currency group 'USD'"},
		},
		{
			name: "two missing numbers on one posting",
			source: `
2024-01-15 * "Test"
  Assets:Stock     HOOL {USD}
  Assets:Cash   -1000.00 USD
`,
			wantErrs: []string{"Too many missing numbers for currency group 'USD'"},
		},
		{
			name: "a missing number in one group only drops that group",
			source: `
2024-01-15 * "Test"
  Assets:Stock  10 HOOL {USD}
  Assets:Stock   5 AAPL {USD}
  Assets:Cash   -1000.00 USD
  Expenses:Food   30.00 EUR
  Assets:Euros   -30.00 EUR
`,
			want: []string{
				"EUR: Expenses:Food",
				"EUR: Assets:Euros",
			},
			wantErrs: []string{"Too many missing numbers for currency group 'USD'"},
		},
		{
			name: "missing units a total cost does not determine",
			source: `
2024-01-15 * "Test"
  Assets:Stock     HOOL {{1000.00 USD}}
  Assets:Cash   -1000.00 USD
`,
			wantErrs: []string{"Transaction does not balance: (-1000 USD)"},
		},
		{
			name: "a reduction with an empty cost spec booked against two lots",
			source: lots + `
2024-01-15 * "Sell"
  Assets:Stock  -8 HOOL {}
  Assets:Cash    900.00 USD
  Income:Gains
`,
			want: []string{
				"USD: Assets:Stock",
				"USD: Assets:Cash",
				"USD: Income:Gains units -70.00 USD booked at -70.00 USD",
			},
		},
		{
			name: "a reduction against two lots leaves its residual",
			source: lots + `
2024-01-15 * "Sell"
  Assets:Stock  -8 HOOL {}
  Assets:Cash    900.00 USD
`,
			want: []string{
				"USD: Assets:Stock",
				"USD: Assets:Cash",
				"USD: residual 70 USD",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, errs := interpolateLast(t, tt.source)
			assert.Equal(t, tt.want, lines)
			assert.Equal(t, tt.wantErrs, errs)
		})
	}
}
