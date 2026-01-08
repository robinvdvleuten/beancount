package web

import (
	"net/http"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/shopspring/decimal"
)

// BalancesResponse is the JSON response structure for the balances endpoint.
type BalancesResponse struct {
	Roots      []*BalanceNodeResponse `json:"roots"`
	Currencies []string               `json:"currencies"`
	StartDate  *string                `json:"startDate,omitempty"`
	EndDate    *string                `json:"endDate,omitempty"`
}

// BalanceNodeResponse represents a node in the balance tree for JSON serialization.
type BalanceNodeResponse struct {
	Name     string                     `json:"name"`
	Account  string                     `json:"account,omitempty"`
	Depth    int                        `json:"depth"`
	Balance  map[string]decimal.Decimal `json:"balance"`
	Children []*BalanceNodeResponse     `json:"children,omitempty"`
}

// handleGetBalances handles GET requests to /api/balances.
//
// Query parameters:
//   - types: Comma-separated account types (Assets,Liabilities,Equity,Income,Expenses).
//     Must match configured account names. If omitted, returns all types (trial balance).
//   - startDate: Start date in YYYY-MM-DD format.
//   - endDate: End date in YYYY-MM-DD format.
//
// Date semantics:
//   - Both omitted: Current inventory state (all postings).
//   - startDate == endDate: Point-in-time balance (balance sheet).
//   - startDate < endDate: Period change (income statement).
//
// Examples:
//   - GET /api/balances - Trial balance (all types, current state)
//   - GET /api/balances?types=Assets,Liabilities,Equity&startDate=2024-01-31&endDate=2024-01-31 - Balance sheet
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

	// Validate date consistency
	if (startDate == nil) != (endDate == nil) {
		http.Error(w, "both startDate and endDate must be provided together, or neither", http.StatusBadRequest)
		return
	}

	// Get balance tree from ledger
	tree, err := s.ledger.GetBalanceTree(accountTypes, startDate, endDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert to response format
	response := convertBalanceTree(tree)
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
		Balance:  node.Balance.ToMap(),
		Children: children,
	}
}
