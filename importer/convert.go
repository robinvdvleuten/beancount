package importer

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/importer/internal/pb"
)

// importIDKey is the metadata key the host writes an Import ID to.
const importIDKey = "import-id"

// encoders turn each directive kind protocol v1 carries into its message.
var encoders = map[ast.DirectiveKind]func(ast.Directive) (*pb.Directive, error){
	ast.KindTransaction: func(d ast.Directive) (*pb.Directive, error) {
		txn, err := encodeTransaction(d.(*ast.Transaction))
		if err != nil {
			return nil, err
		}
		return &pb.Directive{Kind: &pb.Directive_Transaction{Transaction: txn}}, nil
	},
	ast.KindBalance: func(d ast.Directive) (*pb.Directive, error) {
		return &pb.Directive{Kind: &pb.Directive_Balance{Balance: encodeBalance(d.(*ast.Balance))}}, nil
	},
}

// encodeDirectives converts an Importer's directives to their messages,
// keeping their order.
func encodeDirectives(directives []ast.Directive) ([]*pb.Directive, error) {
	out := make([]*pb.Directive, len(directives))
	for i, d := range directives {
		encode, ok := encoders[d.Kind()]
		if !ok {
			return nil, fmt.Errorf("directive %d: protocol v1 carries only transactions and balance assertions, not %s", i+1, d.Kind())
		}
		msg, err := encode(d)
		if err != nil {
			return nil, fmt.Errorf("directive %d: %w", i+1, err)
		}
		out[i] = msg
	}
	return out, nil
}

// decodeDirectives converts messages from an Importer back to directives.
// It validates what the messages carry, since they cross a process boundary.
func decodeDirectives(msgs []*pb.Directive) ([]ast.Directive, error) {
	out := make([]ast.Directive, len(msgs))
	for i, msg := range msgs {
		var (
			d   ast.Directive
			err error
		)
		switch kind := msg.GetKind().(type) {
		case *pb.Directive_Transaction:
			d, err = decodeTransaction(kind.Transaction)
		case *pb.Directive_Balance:
			d, err = decodeBalance(kind.Balance)
		default:
			err = fmt.Errorf("unknown directive kind")
		}
		if err != nil {
			return nil, fmt.Errorf("directive %d: %w", i+1, err)
		}
		out[i] = d
	}
	return out, nil
}

func encodeTransaction(txn *ast.Transaction) (*pb.Transaction, error) {
	msg := &pb.Transaction{
		Date:      encodeDate(txn.Date()),
		Flag:      txn.Flag,
		Narration: txn.Narration.Value,
		Postings:  make([]*pb.Posting, len(txn.Postings)),
	}
	if !txn.Payee.IsEmpty() || txn.Payee.HasRaw() {
		payee := txn.Payee.Value
		msg.Payee = &payee
	}
	for _, tag := range txn.AllTags() {
		msg.Tags = append(msg.Tags, string(tag))
	}
	for _, link := range txn.AllLinks() {
		msg.Links = append(msg.Links, string(link))
	}
	for _, m := range txn.Metadata {
		if m.Key != importIDKey {
			msg.Metadata = append(msg.Metadata, encodeMetadata(m))
			continue
		}
		if m.Value == nil || m.Value.StringValue == nil {
			return nil, fmt.Errorf("%s metadata must be a string", importIDKey)
		}
		msg.ImportId = m.Value.StringValue.Value
	}
	for i, p := range txn.Postings {
		msg.Postings[i] = encodePosting(p)
	}
	return msg, nil
}

func decodeTransaction(msg *pb.Transaction) (*ast.Transaction, error) {
	date, err := decodeDate(msg.GetDate())
	if err != nil {
		return nil, err
	}
	txn := ast.NewTransaction(date, msg.GetNarration(), ast.WithFlag(msg.GetFlag()), ast.WithTags(msg.GetTags()...), ast.WithLinks(msg.GetLinks()...))
	if msg.Payee != nil {
		txn.Payee = ast.NewRawString(msg.GetPayee())
		if msg.GetPayee() == "" {
			txn.Payee = ast.NewRawStringWithRaw(`""`, "")
		}
	}
	metadata, err := decodeMetadataList(msg.GetMetadata())
	if err != nil {
		return nil, err
	}
	if id := msg.GetImportId(); id != "" {
		for _, m := range metadata {
			if m.Key == importIDKey {
				return nil, fmt.Errorf("%s is set both as the Import ID and as metadata", importIDKey)
			}
		}
		metadata = append([]*ast.Metadata{ast.NewMetadata(importIDKey, id)}, metadata...)
	}
	txn.AddMetadata(metadata...)
	for i, p := range msg.GetPostings() {
		posting, err := decodePosting(p)
		if err != nil {
			return nil, fmt.Errorf("posting %d: %w", i+1, err)
		}
		txn.Postings = append(txn.Postings, posting)
	}
	return txn, nil
}

func encodePosting(p *ast.Posting) *pb.Posting {
	msg := &pb.Posting{
		Flag:    p.Flag,
		Account: string(p.Account),
		Units:   encodeAmount(p.Amount),
		Cost:    encodeCost(p.Cost),
	}
	if p.Price != nil {
		msg.Price = &pb.Price{Amount: encodeAmount(p.Price), Total: p.PriceTotal}
	}
	for _, m := range p.Metadata {
		msg.Metadata = append(msg.Metadata, encodeMetadata(m))
	}
	return msg
}

func decodePosting(msg *pb.Posting) (*ast.Posting, error) {
	account, err := ast.NewAccount(msg.GetAccount())
	if err != nil {
		return nil, err
	}
	posting := ast.NewPosting(account, ast.WithPostingFlag(msg.GetFlag()))
	if posting.Amount, err = decodeAmount(msg.GetUnits()); err != nil {
		return nil, err
	}
	if posting.Cost, err = decodeCost(msg.GetCost()); err != nil {
		return nil, fmt.Errorf("cost: %w", err)
	}
	if price := msg.GetPrice(); price != nil {
		if posting.Price, err = decodeAmount(price.GetAmount()); err != nil {
			return nil, fmt.Errorf("price: %w", err)
		}
		posting.PriceTotal = price.GetTotal()
	}
	metadata, err := decodeMetadataList(msg.GetMetadata())
	if err != nil {
		return nil, err
	}
	posting.AddMetadata(metadata...)
	return posting, nil
}

func encodeCost(c *ast.Cost) *pb.Cost {
	if c == nil {
		return nil
	}
	return &pb.Cost{
		Merge:         c.IsMerge,
		Total:         c.IsTotal,
		Amount:        encodeAmount(c.Amount),
		CompoundTotal: encodeAmount(c.Total),
		Date:          encodeDate(c.Date),
		Label:         c.Label,
	}
}

func decodeCost(msg *pb.Cost) (*ast.Cost, error) {
	if msg == nil {
		return nil, nil
	}
	c := &ast.Cost{IsMerge: msg.GetMerge(), IsTotal: msg.GetTotal(), Label: msg.GetLabel()}
	var err error
	if c.Amount, err = decodeAmount(msg.GetAmount()); err != nil {
		return nil, err
	}
	if c.Total, err = decodeAmount(msg.GetCompoundTotal()); err != nil {
		return nil, err
	}
	if msg.GetDate() != nil {
		if c.Date, err = decodeDate(msg.GetDate()); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func encodeBalance(b *ast.Balance) *pb.Balance {
	msg := &pb.Balance{
		Date:      encodeDate(b.Date()),
		Account:   string(b.Account),
		Amount:    encodeAmount(b.Amount),
		Tolerance: encodeAmount(b.Tolerance),
	}
	for _, m := range b.Metadata {
		msg.Metadata = append(msg.Metadata, encodeMetadata(m))
	}
	return msg
}

func decodeBalance(msg *pb.Balance) (*ast.Balance, error) {
	date, err := decodeDate(msg.GetDate())
	if err != nil {
		return nil, err
	}
	account, err := ast.NewAccount(msg.GetAccount())
	if err != nil {
		return nil, err
	}
	amount, err := decodeAmount(msg.GetAmount())
	if err != nil {
		return nil, err
	}
	if amount == nil {
		return nil, fmt.Errorf("balance assertion without an amount")
	}
	b := ast.NewBalance(date, account, amount)
	if b.Tolerance, err = decodeAmount(msg.GetTolerance()); err != nil {
		return nil, fmt.Errorf("tolerance: %w", err)
	}
	metadata, err := decodeMetadataList(msg.GetMetadata())
	if err != nil {
		return nil, err
	}
	b.AddMetadata(metadata...)
	return b, nil
}

func encodeAmount(a *ast.Amount) *pb.Amount {
	if a == nil {
		return nil
	}
	return &pb.Amount{Number: a.Value, Currency: a.Currency}
}

func decodeAmount(msg *pb.Amount) (*ast.Amount, error) {
	if msg == nil {
		return nil, nil
	}
	if number := msg.GetNumber(); number != "" {
		if _, err := decimal.NewFromString(number); err != nil {
			return nil, fmt.Errorf("invalid number %q", number)
		}
	}
	return ast.NewAmount(msg.GetNumber(), msg.GetCurrency()), nil
}

func encodeDate(d *ast.Date) *pb.Date {
	if d == nil {
		return nil
	}
	return &pb.Date{Year: int32(d.Year()), Month: int32(d.Month()), Day: int32(d.Day())}
}

func decodeDate(msg *pb.Date) (*ast.Date, error) {
	if msg == nil {
		return nil, fmt.Errorf("missing date")
	}
	t := time.Date(int(msg.GetYear()), time.Month(msg.GetMonth()), int(msg.GetDay()), 0, 0, 0, 0, time.UTC)
	if t.Year() != int(msg.GetYear()) || int32(t.Month()) != msg.GetMonth() || int32(t.Day()) != msg.GetDay() {
		return nil, fmt.Errorf("invalid date %04d-%02d-%02d", msg.GetYear(), msg.GetMonth(), msg.GetDay())
	}
	return ast.NewDateFromTime(t), nil
}

func encodeMetadata(m *ast.Metadata) *pb.Metadata {
	return &pb.Metadata{Key: m.Key, Value: encodeMetadataValue(m.Value)}
}

func encodeMetadataValue(v *ast.MetadataValue) *pb.MetadataValue {
	switch {
	case v == nil:
		return nil
	case v.StringValue != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_StringValue{StringValue: v.StringValue.Value}}
	case v.Date != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Date{Date: encodeDate(v.Date)}}
	case v.Account != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Account{Account: string(*v.Account)}}
	case v.Currency != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Currency{Currency: *v.Currency}}
	case v.Tag != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Tag{Tag: string(*v.Tag)}}
	case v.Link != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Link{Link: string(*v.Link)}}
	case v.Number != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Number{Number: *v.Number}}
	case v.Amount != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Amount{Amount: encodeAmount(v.Amount)}}
	case v.Boolean != nil:
		return &pb.MetadataValue{Kind: &pb.MetadataValue_Boolean{Boolean: *v.Boolean}}
	}
	return nil
}

func decodeMetadataList(msgs []*pb.Metadata) ([]*ast.Metadata, error) {
	out := make([]*ast.Metadata, len(msgs))
	for i, msg := range msgs {
		value, err := decodeMetadataValue(msg.GetValue())
		if err != nil {
			return nil, fmt.Errorf("metadata %q: %w", msg.GetKey(), err)
		}
		out[i] = &ast.Metadata{Key: msg.GetKey(), Value: value}
	}
	return out, nil
}

func decodeMetadataValue(msg *pb.MetadataValue) (*ast.MetadataValue, error) {
	switch kind := msg.GetKind().(type) {
	case nil:
		return nil, nil
	case *pb.MetadataValue_StringValue:
		s := ast.NewRawString(kind.StringValue)
		return &ast.MetadataValue{StringValue: &s}, nil
	case *pb.MetadataValue_Date:
		date, err := decodeDate(kind.Date)
		return &ast.MetadataValue{Date: date}, err
	case *pb.MetadataValue_Account:
		account, err := ast.NewAccount(kind.Account)
		return &ast.MetadataValue{Account: &account}, err
	case *pb.MetadataValue_Currency:
		return &ast.MetadataValue{Currency: &kind.Currency}, nil
	case *pb.MetadataValue_Tag:
		tag := ast.Tag(kind.Tag)
		return &ast.MetadataValue{Tag: &tag}, nil
	case *pb.MetadataValue_Link:
		link := ast.Link(kind.Link)
		return &ast.MetadataValue{Link: &link}, nil
	case *pb.MetadataValue_Number:
		if _, err := decimal.NewFromString(kind.Number); err != nil {
			return nil, fmt.Errorf("invalid number %q", kind.Number)
		}
		return &ast.MetadataValue{Number: &kind.Number}, nil
	case *pb.MetadataValue_Amount:
		amount, err := decodeAmount(kind.Amount)
		return &ast.MetadataValue{Amount: amount}, err
	case *pb.MetadataValue_Boolean:
		return &ast.MetadataValue{Boolean: &kind.Boolean}, nil
	}
	return nil, fmt.Errorf("unknown metadata value kind")
}
