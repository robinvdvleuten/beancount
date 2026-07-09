package web

import (
	"net/http"
	"slices"
)

// AccountInfo represents basic information about a ledger account.
type AccountInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// AccountsResponse is the JSON response structure for the accounts endpoint.
type AccountsResponse struct {
	Accounts []AccountInfo `json:"accounts"`
}

// handleGetAccounts handles GET requests to /api/accounts.
// Returns all accounts from the ledger, sorted alphabetically by name.
func (s *Server) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Pre-allocate slice with capacity hint
	accounts := make([]AccountInfo, 0, len(s.ledger.Accounts()))

	for name, account := range s.ledger.Accounts() {
		accounts = append(accounts, AccountInfo{
			Name: name,
			Type: account.Type, // Type is now a string (account root name)
		})
	}

	// Sort alphabetically using standard library
	slices.SortFunc(accounts, func(a, b AccountInfo) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})

	writeJSONResponse(w, &AccountsResponse{Accounts: accounts})
}
