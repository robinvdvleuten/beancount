package ledger

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
)

// currencyGroup is a Currency group: the postings of a transaction that
// balance in currency, in source order. The amount-less posting, if any,
// belongs to every group; Booking completes each group on its own.
type currencyGroup struct {
	currency string
	postings []*ast.Posting
}

// missingCurrency marks a currency the source leaves out of an amount, cost
// or price that is present; "" means the amount, cost or price is absent.
const missingCurrency = "\x00"

// currencyRefs are the currencies a posting states for its units, cost and
// price, as beancount's Refer.
type currencyRefs struct {
	index              int
	units, cost, price string
}

// bucket is the currency a posting balances in: its cost currency, else its
// price currency, else (without cost or price) its units currency; "" when
// that is not known yet. This is beancount's get_bucket_currency.
func (r currencyRefs) bucket() string {
	switch {
	case isCurrency(r.cost):
		return r.cost
	case isCurrency(r.price):
		return r.price
	case r.cost == "" && r.price == "" && isCurrency(r.units):
		return r.units
	}
	return ""
}

func isCurrency(c string) bool { return c != "" && c != missingCurrency }

// statedCurrency is the currency an amount, cost or price states.
func statedCurrency(present bool, currency string) string {
	switch {
	case !present:
		return ""
	case currency == "":
		return missingCurrency
	}
	return currency
}

// categorize sorts a transaction's postings into Currency groups, like
// beancount's categorize_by_currency: a posting whose currencies the source
// leaves out takes them from the transaction's only group, or else from its
// account's inventory. The errors mean the transaction cannot be booked at
// all.
func (b *booker) categorize(txn *ast.Transaction) ([]currencyGroup, []error) {
	var errs []error
	groups := make(map[string][]currencyRefs)
	var currencies []string // In the order groups are created
	firstIndex := make(map[string]int)
	add := func(currency string, r currencyRefs) {
		if _, ok := groups[currency]; !ok {
			currencies = append(currencies, currency)
			firstIndex[currency] = r.index
		}
		groups[currency] = append(groups[currency], r)
	}

	var autos, unknown []currencyRefs
	for i, posting := range txn.Postings {
		r := currencyRefs{index: i}
		if posting.Amount != nil {
			r.units = statedCurrency(true, posting.Amount.Currency)
		}
		if posting.Cost != nil {
			r.cost = statedCurrency(true, costCurrency(posting.Cost))
		}
		if posting.Price != nil {
			r.price = statedCurrency(true, posting.Price.Currency)
		}
		// A cost and a price must be in the same currency.
		if r.cost == missingCurrency && isCurrency(r.price) {
			r.cost = r.price
		}
		if r.price == missingCurrency && isCurrency(r.cost) {
			r.price = r.cost
		}

		if posting.Amount == nil && r.price == "" {
			autos = append(autos, r)
		} else if currency := r.bucket(); currency != "" {
			add(currency, r)
		} else {
			unknown = append(unknown, r)
		}
	}

	// A single unknown posting next to a single group belongs to it.
	if len(unknown) == 1 && len(groups) == 1 {
		r := unknown[0]
		unknown = nil
		other := currencies[0]
		if r.cost == "" && r.price == "" {
			r.units = other
		} else {
			if r.price == missingCurrency {
				r.price = other
			}
			if r.cost == missingCurrency {
				r.cost = other
			}
		}
		add(r.bucket(), r)
	}

	// Otherwise the account's inventory decides, when it holds one currency.
	for _, r := range unknown {
		posting := txn.Postings[r.index]
		units, costs := b.heldCurrencies(posting.Account)
		if r.units == missingCurrency && len(units) == 1 {
			r.units = units[0]
		}
		if r.cost == missingCurrency || r.price == missingCurrency {
			if len(costs) == 1 {
				if r.price == missingCurrency {
					r.price = costs[0]
				}
				if r.cost == missingCurrency {
					r.cost = costs[0]
				}
			}
		}
		if currency := r.bucket(); currency != "" {
			add(currency, r)
			continue
		}
		errs = append(errs, NewCurrencyGroupError(txn, posting, fmt.Sprintf("Failed to categorize posting %d", r.index+1)))
	}

	// A units currency still left out comes from the account's inventory.
	for _, currency := range currencies {
		refs := groups[currency]
		for i, r := range refs {
			if r.units != missingCurrency {
				continue
			}
			if units, _ := b.heldCurrencies(txn.Postings[r.index].Account); len(units) == 1 {
				refs[i].units = units[0]
			}
		}
	}

	if len(autos) > 1 {
		last := txn.Postings[autos[len(autos)-1].index]
		errs = append(errs, NewCurrencyGroupError(txn, last, "You may not have more than one auto-posting per currency"))
		autos = autos[:1]
	}
	for _, auto := range autos {
		for _, currency := range currencies {
			groups[currency] = append(groups[currency], currencyRefs{index: auto.index, units: currency})
		}
	}

	for _, currency := range currencies {
		for _, r := range groups[currency] {
			for _, part := range []struct{ currency, name string }{{r.units, "units"}, {r.cost, "cost"}, {r.price, "price"}} {
				if part.currency == missingCurrency {
					errs = append(errs, NewCurrencyGroupError(txn, txn.Postings[r.index], fmt.Sprintf("Could not resolve %s currency", part.name)))
				}
			}
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}

	slices.SortStableFunc(currencies, func(a, b string) int { return cmp.Compare(firstIndex[a], firstIndex[b]) })
	result := make([]currencyGroup, 0, len(currencies))
	for _, currency := range currencies {
		refs := groups[currency]
		slices.SortStableFunc(refs, func(a, b currencyRefs) int { return cmp.Compare(a.index, b.index) })
		group := currencyGroup{currency: currency, postings: make([]*ast.Posting, len(refs))}
		for i, r := range refs {
			group.postings[i] = txn.Postings[r.index]
		}
		result = append(result, group)
	}
	return result, nil
}

// heldCurrencies returns the currencies an account's inventory holds, and
// the cost currencies of its lots held at cost.
func (b *booker) heldCurrencies(account ast.Account) (units, costs []string) {
	inv, ok := b.inventories[string(account)]
	if !ok {
		return nil, nil
	}
	return inv.Currencies(), inv.costCurrencies()
}
