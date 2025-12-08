package web

import (
	"net/http"
	"sort"
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
			Type: account.Type.String(),
		})
	}

	// Sort alphabetically using standard library
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Name < accounts[j].Name
	})

	writeJSONResponse(w, &AccountsResponse{Accounts: accounts})
}
