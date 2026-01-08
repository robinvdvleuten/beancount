package ledger

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// Balance represents the balance of an account across one or more currencies.
// It stores amounts in a sorted slice for deterministic iteration and display.
type Balance struct {
	entries []*CurrencyAmount
}

// CurrencyAmount represents an amount in a specific currency.
type CurrencyAmount struct {
	Currency string
	Amount   decimal.Decimal
}

// NewBalance creates an empty balance.
func NewBalance() *Balance {
	return &Balance{entries: []*CurrencyAmount{}}
}

// NewBalanceFromMap converts a map[string]decimal.Decimal to a sorted Balance.
func NewBalanceFromMap(m map[string]decimal.Decimal) *Balance {
	if len(m) == 0 {
		return NewBalance()
	}

	entries := make([]*CurrencyAmount, 0, len(m))
	for currency, amount := range m {
		entries = append(entries, &CurrencyAmount{
			Currency: currency,
			Amount:   amount,
		})
	}

	// Sort by currency code for deterministic order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Currency < entries[j].Currency
	})

	return &Balance{entries: entries}
}

// Get returns the amount for a specific currency, or zero if not found.
func (b *Balance) Get(currency string) decimal.Decimal {
	for _, e := range b.entries {
		if e.Currency == currency {
			return e.Amount
		}
	}
	return decimal.Zero
}

// Set sets or updates the amount for a currency.
func (b *Balance) Set(currency string, amount decimal.Decimal) {
	for _, e := range b.entries {
		if e.Currency == currency {
			e.Amount = amount
			return
		}
	}

	// Not found, add new entry and re-sort
	b.entries = append(b.entries, &CurrencyAmount{
		Currency: currency,
		Amount:   amount,
	})
	sort.Slice(b.entries, func(i, j int) bool {
		return b.entries[i].Currency < b.entries[j].Currency
	})
}

// Add adds an amount to an existing currency balance.
func (b *Balance) Add(currency string, amount decimal.Decimal) {
	current := b.Get(currency)
	b.Set(currency, current.Add(amount))
}

// IsZero returns true if all amounts are zero or balance is empty.
func (b *Balance) IsZero() bool {
	for _, e := range b.entries {
		if !e.Amount.IsZero() {
			return false
		}
	}
	return true
}

// Currencies returns a sorted list of all currencies in this balance.
func (b *Balance) Currencies() []string {
	currencies := make([]string, len(b.entries))
	for i, e := range b.entries {
		currencies[i] = e.Currency
	}
	return currencies
}

// Entries returns the underlying sorted list of currency amounts.
func (b *Balance) Entries() []*CurrencyAmount {
	return b.entries
}

// ToMap converts balance to map[string]decimal.Decimal for convenience.
func (b *Balance) ToMap() map[string]decimal.Decimal {
	m := make(map[string]decimal.Decimal)
	for _, e := range b.entries {
		m[e.Currency] = e.Amount
	}
	return m
}

// String returns a human-readable representation of the balance.
func (b *Balance) String() string {
	if len(b.entries) == 0 {
		return "(empty)"
	}

	var parts []string
	for _, e := range b.entries {
		parts = append(parts, e.Amount.String()+" "+e.Currency)
	}
	return strings.Join(parts, ", ")
}

// Merge combines another balance into this one by adding amounts.
func (b *Balance) Merge(other *Balance) {
	if other == nil {
		return
	}
	for _, e := range other.entries {
		b.Add(e.Currency, e.Amount)
	}
}

// Copy creates a deep copy of this balance.
func (b *Balance) Copy() *Balance {
	if b == nil {
		return NewBalance()
	}
	entries := make([]*CurrencyAmount, len(b.entries))
	for i, e := range b.entries {
		entries[i] = &CurrencyAmount{
			Currency: e.Currency,
			Amount:   e.Amount,
		}
	}
	return &Balance{entries: entries}
}

// BalanceTree represents a hierarchical view of account balances.
// Used for generating balance sheets, income statements, and trial balances.
//
// The tree is organized by account type (Assets, Liabilities, etc.) with
// each type serving as a virtual root node. Balances are aggregated bottom-up
// so parent nodes include the sum of all their descendants.
type BalanceTree struct {
	// Roots contains the top-level nodes (account type roots like "Assets", "Liabilities").
	// Each root's Balance is the aggregated total of all accounts under that type.
	Roots []*BalanceNode

	// Currencies lists all currencies present in the tree, sorted alphabetically.
	Currencies []string

	// StartDate and EndDate define the period for the balance calculation.
	// When StartDate == EndDate, this is a point-in-time balance (balance sheet).
	// When StartDate < EndDate, this is a period change (income statement).
	// When both are nil, this represents the current inventory state.
	StartDate *string
	EndDate   *string
}

// BalanceNode represents a single node in the balance tree hierarchy.
// Can be either an account type root (e.g., "Assets") or an actual account.
type BalanceNode struct {
	// Name is the display name for this node.
	// For account type roots: "Assets", "Liabilities", etc.
	// For accounts: the full account name like "Assets:US:Checking".
	Name string

	// Account is the full account path, empty for virtual root nodes.
	Account string

	// Depth indicates the nesting level (0 for roots, 1+ for accounts).
	Depth int

	// Balance is the aggregated balance for this node and all descendants.
	// For leaf accounts, this is the account's own balance.
	// For parent accounts and roots, this includes all children's balances.
	Balance *Balance

	// Children contains direct child nodes, sorted by name.
	Children []*BalanceNode
}
