package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/importer/internal/pb"
	"github.com/robinvdvleuten/beancount/parser"
)

const roundTripLedger = `2024-01-15 * "Coffee Shop" "Latte" #food ^receipt-1
  import-id: "TX-001"
  note: "with \"quotes\""
  when: 2024-01-14
  source: Assets:Checking
  currency: EUR
  label: #tagged
  ref: ^linked
  count: 3
  fee: 1.25 USD
  verified: TRUE
  empty:
  Expenses:Food  4.50 USD
    category: "drinks"
  ! Assets:Checking

2024-01-16 txn "Narration only"
  Assets:Brokerage  10 HOOL {518.73 USD, 2024-01-10, "lot-1"}
  Assets:Brokerage  -5 HOOL {}
  Assets:Brokerage  -1 HOOL {*}
  Assets:Brokerage  2 HOOL {{1000 USD}}
  Assets:Brokerage  3 HOOL {500 # 9.95 USD}
  Assets:Cash  200 EUR @ 1.35 USD
  Assets:Cash  100 EUR @@ 135 USD
  Assets:Cash

2024-01-31 balance Assets:Checking  1250.00 ~ 0.01 USD
  statement: "jan"
`

func TestRoundTripFormatsIdentically(t *testing.T) {
	ctx := context.Background()
	tree, err := parser.ParseString(ctx, roundTripLedger)
	assert.NoError(t, err)

	msgs, err := encodeDirectives(tree.Directives)
	assert.NoError(t, err)
	decoded, err := decodeDirectives(msgs)
	assert.NoError(t, err)

	assert.Equal(t, format(t, tree.Directives), format(t, decoded))
}

func TestImportIDTravelsAsItsOwnField(t *testing.T) {
	tree, err := parser.ParseString(context.Background(), roundTripLedger)
	assert.NoError(t, err)

	msgs, err := encodeDirectives(tree.Directives[:1])
	assert.NoError(t, err)

	txn := msgs[0].GetTransaction()
	assert.Equal(t, "TX-001", txn.GetImportId())
	for _, m := range txn.GetMetadata() {
		assert.NotEqual(t, importIDKey, m.GetKey())
	}
}

func TestEncodeRejects(t *testing.T) {
	date, _ := ast.NewDate("2024-01-01")
	account, _ := ast.NewAccount("Assets:Checking")

	tests := []struct {
		name      string
		directive ast.Directive
		want      string
	}{
		{
			name:      "directive kind outside v1",
			directive: ast.NewPrice(date, "HOOL", ast.NewAmount("1", "USD")),
			want:      "directive 1: protocol v1 carries only transactions and balance assertions, not price",
		},
		{
			name: "import-id that is not a string",
			directive: ast.NewTransaction(date, "x", ast.WithTransactionMetadata(&ast.Metadata{
				Key: importIDKey, Value: &ast.MetadataValue{Account: &account},
			})),
			want: "directive 1: import-id metadata must be a string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encodeDirectives([]ast.Directive{tt.directive})
			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestDecodeRejects(t *testing.T) {
	date := &pb.Date{Year: 2024, Month: 1, Day: 1}
	txn := func(mutate func(*pb.Transaction)) []*pb.Directive {
		msg := &pb.Transaction{
			Date: date,
			Flag: "*",
			Postings: []*pb.Posting{
				{Account: "Assets:Checking", Units: &pb.Amount{Number: "1", Currency: "USD"}},
				{Account: "Expenses:Food"},
			},
		}
		mutate(msg)
		return []*pb.Directive{{Kind: &pb.Directive_Transaction{Transaction: msg}}}
	}

	tests := []struct {
		name string
		msgs []*pb.Directive
		want string
	}{
		{
			name: "invalid number",
			msgs: txn(func(m *pb.Transaction) { m.Postings[0].Units.Number = "1e" }),
			want: `directive 1: posting 1: invalid number "1e"`,
		},
		{
			name: "invalid date",
			msgs: txn(func(m *pb.Transaction) { m.Date = &pb.Date{Year: 2024, Month: 2, Day: 30} }),
			want: "directive 1: invalid date 2024-02-30",
		},
		{
			name: "missing date",
			msgs: txn(func(m *pb.Transaction) { m.Date = nil }),
			want: "directive 1: missing date",
		},
		{
			name: "invalid account",
			msgs: txn(func(m *pb.Transaction) { m.Postings[1].Account = "Expenses:food\nx" }),
			want: "directive 1: posting 2: ",
		},
		{
			name: "import-id both as field and metadata",
			msgs: txn(func(m *pb.Transaction) {
				m.ImportId = "a"
				m.Metadata = []*pb.Metadata{{Key: importIDKey, Value: &pb.MetadataValue{Kind: &pb.MetadataValue_StringValue{StringValue: "b"}}}}
			}),
			want: "directive 1: import-id is set both as the Import ID and as metadata",
		},
		{
			name: "unknown directive kind",
			msgs: []*pb.Directive{{}},
			want: "directive 1: unknown directive kind",
		},
		{
			name: "balance without amount",
			msgs: []*pb.Directive{{Kind: &pb.Directive_Balance{Balance: &pb.Balance{Date: date, Account: "Assets:Checking"}}}},
			want: "directive 1: balance assertion without an amount",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeDirectives(tt.msgs)
			assert.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), tt.want), "got %q", err.Error())
		})
	}
}

func format(t *testing.T, directives []ast.Directive) string {
	t.Helper()
	var sb strings.Builder
	err := formatter.New(formatter.WithIndentation(2)).Format(context.Background(), &ast.AST{Directives: directives}, nil, &sb)
	assert.NoError(t, err)
	return sb.String()
}
