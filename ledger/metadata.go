package ledger

import (
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
)

// resolveMetadataKeys leaves each metadata key once on every directive and
// posting of the tree, as beancount's parser builds its meta dicts. A
// transaction and each of its postings keep a key's first value, and every
// later one that is not the same Python object is reported, as beancount's
// grammar checks them (sameObject); any other directive keeps a key's last
// value, in the key's first place, as a dict update does, with no error. A
// directive other than a transaction is replaced by a copy, since the tree
// shares it with the parsed one.
func resolveMetadataKeys(tree *ast.AST) []error {
	var errs []error
	// An amount whose key is dropped was still read: like the amounts of a
	// dropped directive, it counts towards display precision.
	drop := func(written, kept []*ast.Metadata) []*ast.Metadata {
		for _, md := range written {
			if md.Value != nil && md.Value.Amount != nil && !slices.Contains(kept, md) {
				tree.DroppedAmounts = append(tree.DroppedAmounts, md.Value.Amount)
			}
		}
		return kept
	}
	for i, directive := range tree.Directives {
		if txn, ok := directive.(*ast.Transaction); ok {
			var kept []*ast.Metadata
			kept, errs = keepFirstMetadata(txn, "", txn.Metadata, errs)
			txn.Metadata = drop(txn.Metadata, kept)
			for _, posting := range txn.Postings {
				kept, errs = keepFirstMetadata(txn, posting.Account, posting.Metadata, errs)
				posting.Metadata = drop(posting.Metadata, kept)
			}
			continue
		}
		if metadata := directive.GetMetadata(); repeatsKey(metadata) {
			tree.Directives[i] = withMetadata(directive, drop(metadata, keepLastMetadata(metadata)))
		}
	}
	return errs
}

// repeatsKey reports whether a key appears more than once; a directive
// holds a few keys, so comparing each pair is cheaper than a map.
func repeatsKey(metadata []*ast.Metadata) bool {
	for i, md := range metadata {
		for _, other := range metadata[:i] {
			if other.Key == md.Key {
				return true
			}
		}
	}
	return false
}

// keepFirstMetadata drops every repeated key after its first, reporting
// on the transaction each whose value is not the same object as the first.
func keepFirstMetadata(txn *ast.Transaction, account ast.Account, metadata []*ast.Metadata, errs []error) ([]*ast.Metadata, []error) {
	if !repeatsKey(metadata) {
		return metadata, errs
	}
	kept := make([]*ast.Metadata, 0, len(metadata))
	first := make(map[string]*ast.MetadataValue, len(metadata))
	for _, md := range metadata {
		if value, ok := first[md.Key]; ok {
			if !sameObject(value, md.Value) {
				errs = append(errs, newInvalidMetadataError(txn, account, md.Key, md.Value, "duplicate key"))
			}
			continue
		}
		first[md.Key] = md.Value
		kept = append(kept, md)
	}
	return kept, errs
}

// sameObject reports whether beancount's parser holds two metadata values
// as one Python object, which is how its grammar tells a repeated key from
// a duplicate (value is not posting_or_kv.value): None and the booleans are
// singletons, accounts are interned, and CPython caches the empty string
// and every string of one Latin-1 character, whether written as a string,
// a currency or a tag. Numbers, dates, amounts and longer strings are new
// objects each time they are read.
func sameObject(a, b *ast.MetadataValue) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Boolean != nil || b.Boolean != nil {
		return a.Boolean != nil && b.Boolean != nil && *a.Boolean == *b.Boolean
	}
	if a.Account != nil || b.Account != nil {
		return a.Account != nil && b.Account != nil && *a.Account == *b.Account
	}
	x, xCached := cachedString(a)
	y, yCached := cachedString(b)
	return xCached && yCached && x == y
}

// cachedString returns the text of a string, currency or tag value that
// CPython caches: the empty string or one character below U+0100.
func cachedString(value *ast.MetadataValue) (string, bool) {
	var text string
	switch {
	case value.StringValue != nil:
		text = value.StringValue.Value
	case value.Currency != nil:
		text = *value.Currency
	case value.Tag != nil:
		text = string(*value.Tag)
	default:
		return "", false
	}
	runes := []rune(text)
	return text, len(runes) == 0 || len(runes) == 1 && runes[0] < 0x100
}

// keepLastMetadata gives each key its last value, in its first place.
func keepLastMetadata(metadata []*ast.Metadata) []*ast.Metadata {
	kept := make([]*ast.Metadata, 0, len(metadata))
	place := make(map[string]int, len(metadata))
	for _, md := range metadata {
		if i, ok := place[md.Key]; ok {
			kept[i] = md
			continue
		}
		place[md.Key] = len(kept)
		kept = append(kept, md)
	}
	return kept
}

// withMetadata returns a copy of a directive other than a transaction,
// holding the metadata given.
func withMetadata(directive ast.Directive, metadata []*ast.Metadata) ast.Directive {
	copyOf, ok := metadataCopiers[directive.Kind()]
	if !ok {
		return directive
	}
	return copyOf(directive, metadata)
}

// metadataCopiers copies each kind of directive but a transaction with
// other metadata.
var metadataCopiers = map[ast.DirectiveKind]func(ast.Directive, []*ast.Metadata) ast.Directive{
	ast.KindCommodity: copyWithMetadata[ast.Commodity],
	ast.KindOpen:      copyWithMetadata[ast.Open],
	ast.KindClose:     copyWithMetadata[ast.Close],
	ast.KindBalance:   copyWithMetadata[ast.Balance],
	ast.KindPad:       copyWithMetadata[ast.Pad],
	ast.KindNote:      copyWithMetadata[ast.Note],
	ast.KindDocument:  copyWithMetadata[ast.Document],
	ast.KindPrice:     copyWithMetadata[ast.Price],
	ast.KindEvent:     copyWithMetadata[ast.Event],
	ast.KindQuery:     copyWithMetadata[ast.Query],
	ast.KindCustom:    copyWithMetadata[ast.Custom],
}

// copyWithMetadata copies a directive of type T and gives the copy the
// metadata given.
func copyWithMetadata[T any, P interface {
	*T
	ast.Directive
	SetMetadata([]*ast.Metadata)
}](directive ast.Directive, metadata []*ast.Metadata) ast.Directive {
	copied := *directive.(P)
	P(&copied).SetMetadata(metadata)
	return P(&copied)
}
