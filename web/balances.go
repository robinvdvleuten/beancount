package web

import (
	"net/http"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
)

// BalancesResponse is the JSON response structure for the balances endpoint.
type BalancesResponse struct {
	Roots      []*BalanceNodeResponse `json:"roots"`
	Currencies []string               `json:"currencies"`
	// OperatingCurrencies lists the ledger's operating_currency options in
	// declaration order, without duplicates.
	OperatingCurrencies []string `json:"operatingCurrencies"`
	StartDate           *string  `json:"startDate,omitempty"`
	EndDate             *string  `json:"endDate,omitempty"`
}

// BalanceNodeResponse represents a node in the balance tree for JSON serialization.
type BalanceNodeResponse struct {
	Name     string                 `json:"name"`
	Account  string                 `json:"account,omitempty"`
	Depth    int                    `json:"depth"`
	Balance  map[string]string      `json:"balance"`
	Children []*BalanceNodeResponse `json:"children,omitempty"`
}

// handleGetBalances handles GET requests to /api/balances.
//
// Query parameters:
//   - types: Comma-separated account types (Assets,Liabilities,Equity,Income,Expenses).
//     Must match configured account names. If omitted, returns all types (trial balance).
//   - startDate: Start date in YYYY-MM-DD format.
//   - endDate: End date in YYYY-MM-DD format.
//
// Date semantics (both dates inclusive, an omitted one leaves that side open):
//   - Both omitted: Current inventory state (all postings).
//   - Only endDate: Balance as of endDate (balance sheet).
//   - Only startDate: Change from startDate on.
//   - Both: Change within the period (income statement); startDate == endDate
//     is that one day.
//   - startDate after endDate: 400.
//
// Examples:
//   - GET /api/balances - Trial balance (all types, current state)
//   - GET /api/balances?types=Assets,Liabilities,Equity&endDate=2024-01-31 - Balance sheet
//   - GET /api/balances?types=Income,Expenses&startDate=2024-01-01&endDate=2024-01-31 - Income statement
func (s *Server) handleGetBalances(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Parse account types
	var accountTypes []ast.AccountType
	if typesParam := r.URL.Query().Get("types"); typesParam != "" {
		for _, t := range strings.Split(typesParam, ",") {
			typeName := strings.TrimSpace(t)
			accountType, ok := s.ledger.GetAccountTypeFromName(typeName)
			if !ok {
				http.Error(w, "invalid account type: "+t, http.StatusBadRequest)
				return
			}
			accountTypes = append(accountTypes, accountType)
		}
	}

	// Parse dates
	var startDate, endDate *ast.Date
	if startParam := r.URL.Query().Get("startDate"); startParam != "" {
		d, err := ast.NewDate(startParam)
		if err != nil {
			http.Error(w, "invalid startDate format (expected YYYY-MM-DD): "+startParam, http.StatusBadRequest)
			return
		}
		startDate = d
	}
	if endParam := r.URL.Query().Get("endDate"); endParam != "" {
		d, err := ast.NewDate(endParam)
		if err != nil {
			http.Error(w, "invalid endDate format (expected YYYY-MM-DD): "+endParam, http.StatusBadRequest)
			return
		}
		endDate = d
	}

	// Get balance tree from ledger
	tree, err := s.ledger.GetBalanceTree(accountTypes, startDate, endDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert to response format
	response := convertBalanceTree(tree)
	response.OperatingCurrencies = operatingCurrencies(s.config)
	writeJSONResponse(w, response)
}

// convertBalanceTree converts a ledger.BalanceTree to a BalancesResponse.
func convertBalanceTree(tree *ledger.BalanceTree) *BalancesResponse {
	roots := make([]*BalanceNodeResponse, len(tree.Roots))
	for i, root := range tree.Roots {
		roots[i] = convertBalanceNode(root)
	}

	return &BalancesResponse{
		Roots:      roots,
		Currencies: tree.Currencies,
		StartDate:  tree.StartDate,
		EndDate:    tree.EndDate,
	}
}

// operatingCurrencies returns cfg's operating currencies once each, since
// beancount keeps an operating_currency declared twice twice.
func operatingCurrencies(cfg *config.Config) []string {
	currencies := make([]string, 0, len(cfg.OperatingCurrencies))
	for _, currency := range cfg.OperatingCurrencies {
		if !slices.Contains(currencies, currency) {
			currencies = append(currencies, currency)
		}
	}
	return currencies
}

// convertBalanceNode recursively converts a ledger.BalanceNode to a BalanceNodeResponse.
func convertBalanceNode(node *ledger.BalanceNode) *BalanceNodeResponse {
	var children []*BalanceNodeResponse
	if len(node.Children) > 0 {
		children = make([]*BalanceNodeResponse, len(node.Children))
		for i, child := range node.Children {
			children[i] = convertBalanceNode(child)
		}
	}

	return &BalanceNodeResponse{
		Name:     node.Name,
		Account:  node.Account,
		Depth:    node.Depth,
		Balance:  convertBalance(node.Balance),
		Children: children,
	}
}

func convertBalance(balance *ledger.Balance) map[string]string {
	if balance == nil {
		return map[string]string{}
	}

	entries := balance.Entries()
	converted := make(map[string]string, len(entries))
	for _, entry := range entries {
		converted[entry.Currency] = entry.Amount.String()
	}
	return converted
}
