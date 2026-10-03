package query

import (
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
)

// dictValue is a Python dict of metadata, the value of beanquery's meta
// columns: its keys in insertion order, as beancount builds a directive's
// or a posting's meta (filename, lineno, then the entries written).
type dictValue struct {
	keys   []string
	values []any
}

// set adds a key, or replaces its value, keeping insertion order.
func (d *dictValue) set(key string, value any) {
	for i, k := range d.keys {
		if k == key {
			d.values[i] = value
			return
		}
	}
	d.keys = append(d.keys, key)
	d.values = append(d.values, value)
}

// get is Python's dict.get: the key's value, NULL for a missing key.
func (d *dictValue) get(key string) any {
	value, _ := d.lookup(key)
	return value
}

// lookup returns the key's value and whether the dict has the key.
func (d *dictValue) lookup(key string) (any, bool) {
	for i, k := range d.keys {
		if k == key {
			return d.values[i], true
		}
	}
	return nil, false
}

// String renders the dict like Python's str() of a dict.
func (d *dictValue) String() string {
	return d.repr(func(string) bool { return true })
}

// metadataString renders the dict like beanquery's MetadataRenderer, the
// renderer of a Transaction's meta: without filename, lineno and the keys
// starting with "__", which beancount reserves for itself.
func (d *dictValue) metadataString() string {
	return d.repr(func(key string) bool {
		return key != "filename" && key != "lineno" && !strings.HasPrefix(key, "__")
	})
}

func (d *dictValue) repr(keep func(key string) bool) string {
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for i, key := range d.keys {
		if !keep(key) {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(pyrepr.String(key))
		b.WriteString(": ")
		b.WriteString(pyValueRepr(d.values[i]))
	}
	b.WriteByte('}')
	return b.String()
}

// newMetaDict builds the meta dict of a source position and its metadata,
// as beancount's parser builds it: filename and lineno first.
func newMetaDict(pos ast.Position, metadata []*ast.Metadata) *dictValue {
	d := &dictValue{}
	d.set("filename", pos.Filename)
	d.set("lineno", int64(pos.Line))
	for _, md := range metadata {
		d.set(md.Key, metaValue(md.Value))
	}
	return d
}

// entryMeta is a directive's meta, the entries table's meta column and a
// Transaction's meta attribute. beancount also records the tolerances it
// inferred for a transaction (__tolerances__), which ours leaves out
// (KNOWN_GAPS.md).
func entryMeta(entry ast.Directive) *dictValue {
	return newMetaDict(entry.Position(), entry.GetMetadata())
}

// postingMeta is the postings table's meta column, the posting's meta:
// NULL for a posting without a source position, such as one FROM's
// summarization creates, whose meta beancount leaves None, and marked
// __automatic__ when Booking interpolated one of its numbers.
func postingMeta(posting *ast.Posting) any {
	pos := posting.Position()
	if pos.Filename == "" {
		return nil
	}
	d := newMetaDict(pos, posting.Metadata)
	if posting.Automatic {
		d.set("__automatic__", true)
	}
	return d
}

// transactionValue is the entry column's value, beancount's Transaction:
// the transaction and the Context its booked postings are read from.
type transactionValue struct {
	txn *ast.Transaction
	ctx *Context
}

// accountSet is the set of accounts a directive names, beancount's
// getters.get_entry_accounts: a transaction's posting accounts.
func accountSet(entry ast.Directive) setValue {
	set := make(setValue)
	if entry, ok := entry.(ast.WithAccounts); ok {
		for _, account := range entry.Accounts() {
			set[string(account)] = struct{}{}
		}
	}
	return set
}

// transactionAttributes are the attributes of beanquery's Transaction
// structure: the Transaction's fields but postings, and accounts.
var transactionAttributes = map[string]attributeDef{
	"meta":      {tMetadata, func(v any) any { return entryMeta(v.(*transactionValue).txn) }},
	"date":      {tDate, func(v any) any { return v.(*transactionValue).txn.Date() }},
	"flag":      {tString, func(v any) any { return v.(*transactionValue).txn.Flag }},
	"payee":     {tString, func(v any) any { return payeeValue(v.(*transactionValue).txn) }},
	"narration": {tString, func(v any) any { return v.(*transactionValue).txn.Narration.String() }},
	"tags":      {tSet, func(v any) any { return tagSet(v.(*transactionValue).txn) }},
	"links":     {tSet, func(v any) any { return linkSet(v.(*transactionValue).txn) }},
	"accounts":  {tAccountSet, func(v any) any { return accountSet(v.(*transactionValue).txn) }},
}

// String renders the transaction like Python's repr() of beancount's
// Transaction, its booked postings included, one per lot a reduction
// was booked against. Its meta lacks the __tolerances__ beancount
// records (KNOWN_GAPS.md).
func (t *transactionValue) String() string {
	txn := t.txn
	var b strings.Builder
	b.WriteString("Transaction(meta=")
	b.WriteString(entryMeta(txn).String())
	b.WriteString(", date=")
	b.WriteString(pyValueRepr(txn.Date()))
	b.WriteString(", flag=")
	b.WriteString(pyValueRepr(txn.Flag))
	b.WriteString(", payee=")
	b.WriteString(pyValueRepr(payeeValue(txn)))
	b.WriteString(", narration=")
	b.WriteString(pyValueRepr(txn.Narration.String()))
	b.WriteString(", tags=")
	b.WriteString(valueString(tagSet(txn)))
	b.WriteString(", links=")
	b.WriteString(valueString(linkSet(txn)))
	b.WriteString(", postings=[")
	first := true
	for _, posting := range txn.Postings {
		for _, position := range postingPositions(t.ctx, posting) {
			if !first {
				b.WriteString(", ")
			}
			first = false
			writePostingRepr(&b, posting, position)
		}
	}
	b.WriteString("])")
	return b.String()
}

// writePostingRepr writes a booked posting like Python's repr() of
// beancount's Posting.
func writePostingRepr(b *strings.Builder, posting *ast.Posting, position *positionValue) {
	b.WriteString("Posting(account=")
	b.WriteString(pyrepr.String(string(posting.Account)))
	b.WriteString(", units=")
	b.WriteString(pyValueRepr(&position.Units))
	b.WriteString(", cost=")
	if position.Cost != nil {
		b.WriteString(pyValueRepr(position.Cost))
	} else {
		b.WriteString("None")
	}
	b.WriteString(", price=")
	b.WriteString(pyValueRepr(postingPrice(posting)))
	b.WriteString(", flag=")
	var flag any
	if posting.Flag != "" {
		flag = posting.Flag
	}
	b.WriteString(pyValueRepr(flag))
	b.WriteString(", meta=")
	b.WriteString(pyValueRepr(postingMeta(posting)))
	b.WriteByte(')')
}

// amountRepr renders an amount like beancount's Amount repr: its number
// in fixed notation, as the default display formatter spells it, then its
// currency.
func amountRepr(a *amountValue) string {
	return decimalLiteral(a.Number) + " " + a.Currency
}
