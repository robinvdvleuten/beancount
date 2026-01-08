package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

func TestAPIBalances(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.beancount")
	assert.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testContent := `
2024-01-01 open Assets:Checking USD
2024-01-01 open Assets:Savings USD
2024-01-01 open Liabilities:CreditCard USD
2024-01-01 open Equity:Opening USD
2024-01-01 open Income:Salary USD
2024-01-01 open Expenses:Food USD

2024-01-15 * "Opening balance"
  Assets:Checking  1000.00 USD
  Equity:Opening

2024-01-20 * "Transfer to savings"
  Assets:Checking  -200.00 USD
  Assets:Savings    200.00 USD

2024-02-01 * "Salary"
  Assets:Checking  3000.00 USD
  Income:Salary

2024-02-15 * "Groceries"
  Expenses:Food     150.00 USD
  Assets:Checking
`
	_, err = tmpFile.WriteString(testContent)
	assert.NoError(t, err)
	_ = tmpFile.Close()

	server := New(8080, tmpFile.Name())
	err = server.reloadLedger(context.Background())
	assert.NoError(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	t.Run("TrialBalance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var response BalancesResponse
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Should have 5 roots (all account types with activity)
		assert.Equal(t, 5, len(response.Roots))

		// Verify currencies
		assert.Equal(t, 1, len(response.Currencies))
		assert.Equal(t, "USD", response.Currencies[0])

		// No dates for current state
		assert.Equal(t, (*string)(nil), response.StartDate)
		assert.Equal(t, (*string)(nil), response.EndDate)
	})

	t.Run("FilterByTypes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?types=Assets,Liabilities", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response BalancesResponse
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Should have 2 roots (Assets and Liabilities)
		assert.Equal(t, 2, len(response.Roots))
		assert.Equal(t, "Assets", response.Roots[0].Name)
		assert.Equal(t, "Liabilities", response.Roots[1].Name)
	})

	t.Run("PointInTimeBalance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?types=Assets&startDate=2024-01-31&endDate=2024-01-31", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response BalancesResponse
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Should have 1 root (Assets)
		assert.Equal(t, 1, len(response.Roots))

		// Verify dates are set
		assert.NotEqual(t, (*string)(nil), response.StartDate)
		assert.NotEqual(t, (*string)(nil), response.EndDate)
		assert.Equal(t, "2024-01-31", *response.StartDate)
		assert.Equal(t, "2024-01-31", *response.EndDate)

		// Assets as of Jan 31: 1000 - 200 + 200 = 1000 (only opening and transfer)
		assetsRoot := response.Roots[0]
		assert.Equal(t, "Assets", assetsRoot.Name)
		usdBalance := assetsRoot.Balance["USD"]
		assert.True(t, usdBalance.Equal(decimal.NewFromInt(1000)), "Assets should be 1000 as of Jan 31, got %s", usdBalance)
	})

	t.Run("PeriodBalance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?types=Income,Expenses&startDate=2024-02-01&endDate=2024-02-28", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response BalancesResponse
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Should have 2 roots (Income and Expenses)
		assert.Equal(t, 2, len(response.Roots))

		// Find Income and Expenses roots
		var incomeRoot, expensesRoot *BalanceNodeResponse
		for i := range response.Roots {
			switch response.Roots[i].Name {
			case "Income":
				incomeRoot = response.Roots[i]
			case "Expenses":
				expensesRoot = response.Roots[i]
			}
		}

		assert.True(t, incomeRoot != nil, "Income root should exist")
		assert.True(t, expensesRoot != nil, "Expenses root should exist")

		// Income in February: -3000 (credited)
		assert.True(t, incomeRoot.Balance["USD"].Equal(decimal.NewFromInt(-3000)))

		// Expenses in February: 150
		assert.True(t, expensesRoot.Balance["USD"].Equal(decimal.NewFromInt(150)))
	})

	t.Run("HierarchicalStructure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?types=Assets", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response BalancesResponse
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Assets root should have children
		assetsRoot := response.Roots[0]
		assert.Equal(t, "Assets", assetsRoot.Name)
		assert.Equal(t, "", assetsRoot.Account) // Virtual root
		assert.Equal(t, 0, assetsRoot.Depth)
		assert.True(t, len(assetsRoot.Children) > 0, "Assets should have children")

		// Children should have proper structure
		for _, child := range assetsRoot.Children {
			assert.True(t, child.Depth > 0, "Children should have depth > 0")
			assert.NotEqual(t, "", child.Account, "Children should have account path")
		}
	})

	t.Run("InvalidAccountType", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?types=invalid", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, rec.Body.String() != "", "Should have error message")
	})

	t.Run("InvalidDateFormat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?startDate=invalid&endDate=2024-01-31", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, rec.Body.String() != "", "Should have error message")
	})

	t.Run("MissingEndDate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?startDate=2024-01-01", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, rec.Body.String() != "", "Should have error message about both dates")
	})

	t.Run("MissingStartDate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?endDate=2024-01-31", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, rec.Body.String() != "", "Should have error message about both dates")
	})

	t.Run("StartDateAfterEndDate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?startDate=2024-02-01&endDate=2024-01-01", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, rec.Body.String() != "", "Should have error message")
	})

	t.Run("EmptyLedger", func(t *testing.T) {
		// Create a temp file with just an open directive (no transactions)
		tmpFileEmpty, err := os.CreateTemp("", "test-empty-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(tmpFileEmpty.Name()) }()

		_, err = tmpFileEmpty.WriteString("2024-01-01 open Assets:Checking USD\n")
		assert.NoError(t, err)
		_ = tmpFileEmpty.Close()

		serverEmpty := New(8080, tmpFileEmpty.Name())
		err = serverEmpty.reloadLedger(context.Background())
		assert.NoError(t, err)
		muxEmpty, err := serverEmpty.setupRouter()
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/balances", nil)
		rec := httptest.NewRecorder()

		muxEmpty.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response BalancesResponse
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Account exists but has zero balance - still included (Fava behavior)
		assert.Equal(t, 1, len(response.Roots))
		assert.NotEqual(t, nil, response.Roots)
	})

	t.Run("ValidJSONStructure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/balances?types=Assets", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		// Verify top-level structure
		_, hasRoots := response["roots"]
		_, hasCurrencies := response["currencies"]
		assert.True(t, hasRoots, "response should have 'roots' field")
		assert.True(t, hasCurrencies, "response should have 'currencies' field")

		// Verify node structure
		roots := response["roots"].([]interface{})
		assert.True(t, len(roots) > 0)

		firstRoot := roots[0].(map[string]interface{})
		_, hasName := firstRoot["name"]
		_, hasDepth := firstRoot["depth"]
		_, hasBalance := firstRoot["balance"]
		assert.True(t, hasName, "node should have 'name' field")
		assert.True(t, hasDepth, "node should have 'depth' field")
		assert.True(t, hasBalance, "node should have 'balance' field")
	})
}
