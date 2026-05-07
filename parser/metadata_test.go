package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestParseMetadataValueTypes(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantType   string
		wantString string
	}{
		{
			name:       "String",
			source:     "2024-01-01 * \"Test\"\n  key: \"INV-2024-001\"\n  Assets:Cash  100 USD",
			wantType:   "string",
			wantString: "INV-2024-001",
		},
		{
			name:       "Date",
			source:     "2024-01-01 * \"Test\"\n  trip-start: 2024-01-15\n  Assets:Cash  100 USD",
			wantType:   "date",
			wantString: "2024-01-15",
		},
		{
			name:       "Account",
			source:     "2024-01-01 * \"Test\"\n  linked: Assets:Checking\n  Assets:Cash  100 USD",
			wantType:   "account",
			wantString: "Assets:Checking",
		},
		{
			name:       "Currency",
			source:     "2024-01-01 * \"Test\"\n  target: USD\n  Assets:Cash  100 USD",
			wantType:   "currency",
			wantString: "USD",
		},
		{
			name:       "Tag",
			source:     "2024-01-01 * \"Test\"\n  category: #vacation\n  Assets:Cash  100 USD",
			wantType:   "tag",
			wantString: "vacation",
		},
		{
			name:       "Link",
			source:     "2024-01-01 * \"Test\"\n  ref: ^invoice123\n  Assets:Cash  100 USD",
			wantType:   "link",
			wantString: "invoice123",
		},
		{
			name:       "Number",
			source:     "2024-01-01 * \"Test\"\n  quantity: 42\n  Assets:Cash  100 USD",
			wantType:   "number",
			wantString: "42",
		},
		{
			name:       "NumberDecimal",
			source:     "2024-01-01 * \"Test\"\n  quantity: 42.5\n  Assets:Cash  100 USD",
			wantType:   "number",
			wantString: "42.5",
		},
		{
			name:       "NumberNegative",
			source:     "2024-01-01 * \"Test\"\n  quantity: -42.5\n  Assets:Cash  100 USD",
			wantType:   "number",
			wantString: "-42.5",
		},
		{
			name:       "Amount",
			source:     "2024-01-01 * \"Test\"\n  budget: 1000.00 USD\n  Assets:Cash  100 USD",
			wantType:   "amount",
			wantString: "1000.00 USD",
		},
		{
			name:       "BooleanTrue",
			source:     "2024-01-01 * \"Test\"\n  active: TRUE\n  Assets:Cash  100 USD",
			wantType:   "boolean",
			wantString: "TRUE",
		},
		{
			name:       "BooleanFalse",
			source:     "2024-01-01 * \"Test\"\n  active: FALSE\n  Assets:Cash  100 USD",
			wantType:   "boolean",
			wantString: "FALSE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseString(context.Background(), tt.source)
			assert.NoError(t, err)
			assert.NotEqual(t, nil, parsed)
			assert.Equal(t, 1, len(parsed.Directives))

			txn, ok := parsed.Directives[0].(*ast.Transaction)
			assert.True(t, ok, "expected transaction")
			assert.Equal(t, 1, len(txn.Metadata))

			meta := txn.Metadata[0]
			assert.Equal(t, tt.wantType, meta.Value.Type())
			assert.Equal(t, tt.wantString, meta.Value.String())
		})
	}
}

func TestParseMetadataMultipleTypes(t *testing.T) {
	source := `
2024-01-01 * "Test transaction with various metadata"
  invoice: "INV-2024-001"
  trip-start: 2024-01-15
  linked-account: Assets:Checking
  target-currency: USD
  category: #vacation
  ref: ^invoice123
  quantity: 42
  budget: 1000.00 EUR
  active: TRUE
  Assets:Cash  -1000 USD
  Expenses:Travel
`

	parsed, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsed.Directives))

	txn, ok := parsed.Directives[0].(*ast.Transaction)
	assert.True(t, ok)

	assert.Equal(t, 9, len(txn.Metadata))

	// Check each metadata type
	expectedTypes := []string{"string", "date", "account", "currency", "tag", "link", "number", "amount", "boolean"}
	for i, expected := range expectedTypes {
		if i < len(txn.Metadata) {
			assert.Equal(t, expected, txn.Metadata[i].Value.Type(), "metadata at index %d", i)
		}
	}
}

func TestParsePostingMetadata(t *testing.T) {
	source := `
2024-01-01 * "Test"
  Assets:Cash  100 USD
    confirmation: "CONF123"
    ref-num: 42
  Expenses:Food
`

	parsed, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsed.Directives))

	txn, ok := parsed.Directives[0].(*ast.Transaction)
	assert.True(t, ok)
	assert.Equal(t, 2, len(txn.Postings))

	posting := txn.Postings[0]
	assert.Equal(t, 2, len(posting.Metadata))
	assert.Equal(t, "string", posting.Metadata[0].Value.Type())
	assert.Equal(t, "CONF123", posting.Metadata[0].Value.String())
	assert.Equal(t, "number", posting.Metadata[1].Value.Type())
	assert.Equal(t, "42", posting.Metadata[1].Value.String())
}

func TestParseCommodityMetadata(t *testing.T) {
	source := `
2024-01-01 commodity USD
  name: "US Dollar"
  asset-class: "cash"
  precision: 2
`

	parsed, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsed.Directives))

	commodity, ok := parsed.Directives[0].(*ast.Commodity)
	assert.True(t, ok)
	assert.Equal(t, 3, len(commodity.Metadata))

	// All should be strings or numbers
	assert.Equal(t, "string", commodity.Metadata[0].Value.Type())
	assert.Equal(t, "US Dollar", commodity.Metadata[0].Value.String())
	assert.Equal(t, "string", commodity.Metadata[1].Value.Type())
	assert.Equal(t, "cash", commodity.Metadata[1].Value.String())
	assert.Equal(t, "number", commodity.Metadata[2].Value.Type())
	assert.Equal(t, "2", commodity.Metadata[2].Value.String())
}

func TestParseMetadataOnAllSupportedEntries(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		metadata func(*ast.AST) []*ast.Metadata
	}{
		{
			name: "commodity",
			source: `2024-01-01 commodity USD
  meta: "commodity"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Commodity).Metadata
			},
		},
		{
			name: "open",
			source: `2024-01-01 open Assets:Cash USD
  meta: "open"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Open).Metadata
			},
		},
		{
			name: "close",
			source: `2024-01-01 close Assets:Cash
  meta: "close"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Close).Metadata
			},
		},
		{
			name: "balance",
			source: `2024-01-01 balance Assets:Cash 10.00 USD
  meta: "balance"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Balance).Metadata
			},
		},
		{
			name: "pad",
			source: `2024-01-01 pad Assets:Cash Equity:Opening-Balances
  meta: "pad"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Pad).Metadata
			},
		},
		{
			name: "note",
			source: `2024-01-01 note Assets:Cash "Called the bank"
  meta: "note"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Note).Metadata
			},
		},
		{
			name: "document",
			source: `2024-01-01 document Assets:Cash "/documents/statement.pdf"
  meta: "document"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Document).Metadata
			},
		},
		{
			name: "price",
			source: `2024-01-01 price USD 1.10 EUR
  meta: "price"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Price).Metadata
			},
		},
		{
			name: "event",
			source: `2024-01-01 event "location" "Amsterdam"
  meta: "event"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Event).Metadata
			},
		},
		{
			name: "query",
			source: `2024-01-01 query "cash" "SELECT account"
  meta: "query"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Query).Metadata
			},
		},
		{
			name: "custom",
			source: `2024-01-01 custom "budget" "monthly"
  meta: "custom"
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Custom).Metadata
			},
		},
		{
			name: "transaction",
			source: `2024-01-01 * "Dinner"
  meta: "transaction"
  Assets:Cash  -10.00 USD
  Expenses:Food  10.00 USD
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Transaction).Metadata
			},
		},
		{
			name: "posting",
			source: `2024-01-01 * "Dinner"
  Assets:Cash  -10.00 USD
    meta: "posting"
  Expenses:Food  10.00 USD
`,
			metadata: func(tree *ast.AST) []*ast.Metadata {
				return tree.Directives[0].(*ast.Transaction).Postings[0].Metadata
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseString(context.Background(), tt.source)
			assert.NoError(t, err)
			assert.Equal(t, 1, len(parsed.Directives))

			metadata := tt.metadata(parsed)
			assert.Equal(t, 1, len(metadata))
			assert.Equal(t, "meta", metadata[0].Key)
			assert.Equal(t, tt.name, metadata[0].Value.String())
		})
	}
}

func TestParseMetadataEdgeCases(t *testing.T) {
	t.Run("EmptyString", func(t *testing.T) {
		source := `2024-01-01 * "Test"
  key: ""
  Assets:Cash  100 USD`

		parsed, err := ParseString(context.Background(), source)
		assert.NoError(t, err)
		txn := parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "string", txn.Metadata[0].Value.Type())
		assert.Equal(t, "", txn.Metadata[0].Value.String())
	})

	t.Run("AccountVsCurrency", func(t *testing.T) {
		// Without colon = currency
		source1 := `2024-01-01 * "Test"
  curr: USD
  Assets:Cash  100 USD`

		parsed, err := ParseString(context.Background(), source1)
		assert.NoError(t, err)
		txn := parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "currency", txn.Metadata[0].Value.Type())

		// With colon = account
		source2 := `2024-01-01 * "Test"
  acct: Assets:Cash
  Assets:Cash  100 USD`

		parsed, err = ParseString(context.Background(), source2)
		assert.NoError(t, err)
		txn = parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "account", txn.Metadata[0].Value.Type())
	})

	t.Run("NumberVsAmount", func(t *testing.T) {
		// Just number
		source1 := `2024-01-01 * "Test"
  qty: 42
  Assets:Cash  100 USD`

		parsed, err := ParseString(context.Background(), source1)
		assert.NoError(t, err)
		txn := parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "number", txn.Metadata[0].Value.Type())

		// Number with currency = amount
		source2 := `2024-01-01 * "Test"
  amount: 42 USD
  Assets:Cash  100 USD`

		parsed, err = ParseString(context.Background(), source2)
		assert.NoError(t, err)
		txn = parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "amount", txn.Metadata[0].Value.Type())
	})

	t.Run("BooleanVsCurrency", func(t *testing.T) {
		// TRUE/FALSE = boolean
		source1 := `2024-01-01 * "Test"
  flag: TRUE
  Assets:Cash  100 USD`

		parsed, err := ParseString(context.Background(), source1)
		assert.NoError(t, err)
		txn := parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "boolean", txn.Metadata[0].Value.Type())

		// Other uppercase ident = currency
		source2 := `2024-01-01 * "Test"
  curr: EUR
  Assets:Cash  100 USD`

		parsed, err = ParseString(context.Background(), source2)
		assert.NoError(t, err)
		txn = parsed.Directives[0].(*ast.Transaction)
		assert.Equal(t, "currency", txn.Metadata[0].Value.Type())
	})
}

func TestParseMetadataInvalidStringReturnsError(t *testing.T) {
	source := "2024-01-01 commodity USD\n  name: \"bad\\qescape\"\n"

	_, err := ParseString(context.Background(), source)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid string literal")
}

func TestParseMetadataRejectsUnsupportedUnquotedValues(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "LowercaseWord",
			source: "2024-01-01 commodity USD\n  name: foo\n",
			want:   `unsupported metadata value "foo"`,
		},
		{
			name:   "MultiwordUnquoted",
			source: "2024-01-01 commodity USD\n  name: some value\n",
			want:   `unsupported metadata value "some"`,
		},
		{
			name:   "UnterminatedString",
			source: "2024-01-01 commodity USD\n  name: \"unterminated\n",
			want:   "unterminated string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseString(context.Background(), tt.source)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestParseMetadataAllowsOmittedValue(t *testing.T) {
	source := "2024-01-01 commodity USD\n  name:\n"

	parsed, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsed.Directives))

	commodity, ok := parsed.Directives[0].(*ast.Commodity)
	assert.True(t, ok)
	assert.Equal(t, 1, len(commodity.Metadata))
	assert.Equal(t, "name", commodity.Metadata[0].Key)
	assert.Equal(t, (*ast.MetadataValue)(nil), commodity.Metadata[0].Value)
}

func TestParseMetadataAllowsOmittedValueBeforeInlineComment(t *testing.T) {
	source := "2024-01-01 commodity USD\n  name: ; pending value\n"

	parsed, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsed.Directives))

	commodity, ok := parsed.Directives[0].(*ast.Commodity)
	assert.True(t, ok)
	assert.Equal(t, 1, len(commodity.Metadata))
	assert.Equal(t, "name", commodity.Metadata[0].Key)
	assert.Equal(t, (*ast.MetadataValue)(nil), commodity.Metadata[0].Value)
}

func TestParseRestOfLineOptimization(t *testing.T) {
	// pushmeta uses parseRestOfLine; verify it still works with builder
	source := `pushmeta key: some value here
2024-01-01 open Assets:Checking
popmeta key:
`
	result, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Pushmetas))
	assert.Equal(t, "some value here", result.Pushmetas[0].Value)
}

func BenchmarkParseRestOfLine(b *testing.B) {
	source := `pushmeta key: some value with multiple tokens here
2024-01-01 open Assets:Checking
popmeta key:
`
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseString(ctx, source)
	}
}

func TestParseMetadataWithPrecision(t *testing.T) {
	source := `
2024-01-01 * "High precision test"
  price: 0.00000001 BTC
  ratio: 3.141592653589793
  Assets:Cash  100 USD
  Expenses:Food
`

	parsed, err := ParseString(context.Background(), source)
	assert.NoError(t, err)
	txn := parsed.Directives[0].(*ast.Transaction)

	// Amount with high precision
	assert.Equal(t, "amount", txn.Metadata[0].Value.Type())
	assert.Equal(t, "0.00000001 BTC", txn.Metadata[0].Value.String())

	// Number with high precision
	assert.Equal(t, "number", txn.Metadata[1].Value.Type())
	assert.Equal(t, "3.141592653589793", txn.Metadata[1].Value.String())
}
