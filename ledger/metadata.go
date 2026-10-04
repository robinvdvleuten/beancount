package ledger

import "github.com/robinvdvleuten/beancount/ast"

// resolveMetadataKeys leaves each metadata key once on every directive and
// posting of the tree, as beancount's parser builds its meta dicts. A
// transaction and each of its postings keep a key's first value, and every
// later one is reported, as beancount's grammar checks them; any other
// directive keeps a key's last value, in the key's first place, as a dict
// update does, with no error. A directive other than a transaction is
// replaced by a copy, since the tree shares it with the parsed one.
func resolveMetadataKeys(tree *ast.AST) []error {
	var errs []error
	for i, directive := range tree.Directives {
		if txn, ok := directive.(*ast.Transaction); ok {
			txn.Metadata, errs = keepFirstMetadata(txn, "", txn.Metadata, errs)
			for _, posting := range txn.Postings {
				posting.Metadata, errs = keepFirstMetadata(txn, posting.Account, posting.Metadata, errs)
			}
			continue
		}
		if metadata := directive.GetMetadata(); repeatsKey(metadata) {
			tree.Directives[i] = withMetadata(directive, keepLastMetadata(metadata))
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
// each on the transaction.
func keepFirstMetadata(txn *ast.Transaction, account ast.Account, metadata []*ast.Metadata, errs []error) ([]*ast.Metadata, []error) {
	if !repeatsKey(metadata) {
		return metadata, errs
	}
	kept := make([]*ast.Metadata, 0, len(metadata))
	seen := make(map[string]bool, len(metadata))
	for _, md := range metadata {
		if seen[md.Key] {
			errs = append(errs, newInvalidMetadataError(txn, account, md.Key, md.Value, "duplicate key"))
			continue
		}
		seen[md.Key] = true
		kept = append(kept, md)
	}
	return kept, errs
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
