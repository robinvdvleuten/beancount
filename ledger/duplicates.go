package ledger

import (
	"slices"
	"time"

	"github.com/robinvdvleuten/beancount/ast"
)

// duplicateDays is how many days apart a transaction without an Import ID
// may be dated from the one it duplicates.
const duplicateDays = 2

// duplicateFinders find the ledger directive an Extracted directive
// duplicates, by kind. A kind without a finder never has a Duplicate.
var duplicateFinders = map[ast.DirectiveKind]func(*Ledger, ast.Directive) ast.Directive{
	ast.KindTransaction: func(l *Ledger, d ast.Directive) ast.Directive {
		if txn := l.duplicateTransaction(d.(*ast.Transaction)); txn != nil {
			return txn
		}
		return nil
	},
	ast.KindBalance: func(l *Ledger, d ast.Directive) ast.Directive {
		if balance := l.duplicateBalance(d.(*ast.Balance)); balance != nil {
			return balance
		}
		return nil
	},
}

// Duplicate reports whether the ledger already records directive, an
// Extracted directive that is not part of the ledger, and returns the
// ledger directive it matched.
//
// A transaction is a Duplicate of a ledger transaction with the same Import
// ID, or else of a ledger transaction without an Import ID that has a
// posting on the same account with the same units, dated at most two days
// apart, and whose accounts include the transaction's or are included in
// them, like beangulp's heuristic_comparator. Padding transactions count as
// ledger transactions. A balance assertion is a Duplicate of one with the
// same account, date and amount.
func (l *Ledger) Duplicate(directive ast.Directive) (ast.Directive, bool) {
	find, ok := duplicateFinders[directive.Kind()]
	if !ok {
		return nil, false
	}
	match := find(l, directive)
	return match, match != nil
}

// duplicateTransaction returns the first applied transaction txn
// duplicates, or nil.
func (l *Ledger) duplicateTransaction(txn *ast.Transaction) *ast.Transaction {
	if id, ok := importID(txn); ok && id != "" {
		if match, ok := l.importIDs[id]; ok {
			return match
		}
	}

	for _, posting := range txn.Postings {
		if posting.Amount == nil {
			continue
		}
		account, ok := l.accounts[string(posting.Account)]
		if !ok {
			account, ok = l.unopened[string(posting.Account)]
		}
		if !ok {
			continue
		}
		for _, recorded := range account.postings {
			if _, ok := importID(recorded.transaction); ok {
				continue
			}
			if withinDays(txn.Date(), recorded.transaction.Date(), duplicateDays) &&
				sameAmount(posting.Amount, recorded.posting.Amount) &&
				nestedAccounts(txn, recorded.transaction) {
				return recorded.transaction
			}
		}
	}
	return nil
}

// nestedAccounts reports whether the accounts one transaction posts to are
// all among the other's.
func nestedAccounts(a, b *ast.Transaction) bool {
	return subsetOf(a.Accounts(), b.Accounts()) || subsetOf(b.Accounts(), a.Accounts())
}

func subsetOf(sub, super []ast.Account) bool {
	for _, account := range sub {
		if !slices.Contains(super, account) {
			return false
		}
	}
	return true
}

// duplicateBalance returns the first balance assertion balance duplicates,
// or nil.
func (l *Ledger) duplicateBalance(balance *ast.Balance) *ast.Balance {
	for _, recorded := range l.balances[balanceKeyOf(balance)] {
		if sameNumber(balance.Amount, recorded.Amount) {
			return recorded
		}
	}
	return nil
}

// importID returns the text of txn's import-id metadata and whether it has
// one. A value of any type counts, so a hand-written import-id: 12345 is
// the Import ID "12345".
func importID(txn *ast.Transaction) (string, bool) {
	for _, meta := range txn.Metadata {
		if meta.Key == ast.ImportIDKey {
			return meta.Value.String(), true
		}
	}
	return "", false
}

// sameAmount reports whether two amounts have the same currency and number.
func sameAmount(a, b *ast.Amount) bool {
	return a != nil && b != nil && a.Currency == b.Currency && sameNumber(a, b)
}

// withinDays reports whether two dates are at most days apart.
func withinDays(a, b *ast.Date, days int) bool {
	diff := a.Sub(b.Time)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(days)*24*time.Hour
}
