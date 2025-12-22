package ast

import "sort"

// EnrichedAST wraps an AST with pre-extracted semantic information.
// This enables building a graph skeleton without inspecting directives.
type EnrichedAST struct {
	*AST
	Currencies map[string]bool // Set of all currency/commodity codes seen
	Accounts   map[string]bool // Set of all account names seen
}

// Enrich extracts currencies and accounts from an AST in a single pass.
// Returns a new EnrichedAST with semantic information pre-computed.
func (a *AST) Enrich() *EnrichedAST {
	currencies := make(map[string]bool)
	accounts := make(map[string]bool)

	for _, directive := range a.Directives {
		if stateful, ok := directive.(Stateful); ok {
			for _, nodeID := range stateful.AffectedNodes() {
				// Heuristic: if it looks like an account (has ':'), it's an account; else currency
				if isAccountName(nodeID) {
					accounts[nodeID] = true
				} else {
					currencies[nodeID] = true
				}
			}
		}
	}

	return &EnrichedAST{
		AST:        a,
		Currencies: currencies,
		Accounts:   accounts,
	}
}

// isAccountName returns true if the node ID looks like an account name.
// Accounts have at least one colon (Assets:Cash), currencies are uppercase codes (USD, EUR).
func isAccountName(id string) bool {
	for _, ch := range id {
		if ch == ':' {
			return true
		}
	}
	return false
}

// CurrencyList returns all currencies as a sorted slice.
func (e *EnrichedAST) CurrencyList() []string {
	return mapKeys(e.Currencies)
}

// AccountList returns all accounts as a sorted slice.
func (e *EnrichedAST) AccountList() []string {
	return mapKeys(e.Accounts)
}

// mapKeys extracts keys from a boolean map and returns them sorted.
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
