package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ledger"
)

func TestAPISource(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.beancount")
	assert.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testContent := "2024-01-01 * \"Test transaction\"\n  Assets:Checking  100 USD\n  Expenses:Food   -100 USD"
	_, err = tmpFile.WriteString(testContent)
	assert.NoError(t, err)
	_ = tmpFile.Close()

	server := New(8080, tmpFile.Name())
	err = server.reloadLedger(context.Background())
	assert.NoError(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	t.Run("WithDefaultFile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/source", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, testContent, response["source"].(string))
		assert.NotEqual(t, nil, response["errors"])
		// Verify files are included in response
		files := response["files"].(map[string]interface{})
		assert.True(t, strings.HasSuffix(files["root"].(string), tmpFile.Name()))
		assert.NotEqual(t, nil, files["includes"])
	})

	t.Run("WithQueryParameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+tmpFile.Name(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, testContent, response["source"].(string))
	})

	t.Run("FileNotInAllowlist", func(t *testing.T) {
		// Create a file that exists but is not in the allowlist
		otherFile, err := os.CreateTemp("", "other-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(otherFile.Name()) }()
		_ = otherFile.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+otherFile.Name(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, strings.Contains(rec.Body.String(), "access denied"))
	})

	t.Run("NoFilepathNoDefault", func(t *testing.T) {
		serverNoDefault := New(8080, "")
		muxNoDefault, err := serverNoDefault.setupRouter()
		assert.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/api/source", nil)
		rec := httptest.NewRecorder()

		muxNoDefault.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("PutUpdateContent", func(t *testing.T) {
		updatedContent := "2024-01-02 * \"Updated transaction\"\n  Assets:Checking  200 USD\n  Expenses:Food   -200 USD"
		requestBody := map[string]string{
			"source": updatedContent,
		}
		bodyBytes, err := json.Marshal(requestBody)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/api/source", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, updatedContent, response["source"].(string))
		// Verify files are included in PUT response
		files := response["files"].(map[string]interface{})
		assert.True(t, strings.HasSuffix(files["root"].(string), tmpFile.Name()))

		content, err := os.ReadFile(tmpFile.Name())
		assert.NoError(t, err)
		assert.Equal(t, updatedContent, string(content))
	})

	t.Run("PutToFileNotInAllowlist", func(t *testing.T) {
		// Try to write to a file that's not in the allowlist
		otherFile, err := os.CreateTemp("", "other-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(otherFile.Name()) }()
		_ = otherFile.Close()

		requestBody := map[string]string{
			"filepath": otherFile.Name(),
			"source":   "malicious content",
		}
		bodyBytes, err := json.Marshal(requestBody)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/api/source", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, strings.Contains(rec.Body.String(), "access denied"))
	})

	t.Run("PutWithParseErrorStillSavesFile", func(t *testing.T) {
		invalidContent := "this is not valid beancount syntax @@@"
		requestBody := map[string]string{
			"source": invalidContent,
		}
		bodyBytes, err := json.Marshal(requestBody)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/api/source", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		// Should still return 200 — file was saved even if parse fails
		assert.Equal(t, http.StatusOK, rec.Code)

		// File should contain the new content
		content, err := os.ReadFile(tmpFile.Name())
		assert.NoError(t, err)
		assert.Equal(t, invalidContent, string(content))

		// Response should still be valid JSON with source
		var response map[string]interface{}
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, invalidContent, response["source"].(string))
	})

	t.Run("PutInvalidJSON", func(t *testing.T) {
		body := strings.NewReader(`invalid json`)
		req := httptest.NewRequest(http.MethodPut, "/api/source", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("GetWithValidationError", func(t *testing.T) {
		// Create a new server with a file containing validation errors
		tmpFileErr, err := os.CreateTemp("", "test-validation-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(tmpFileErr.Name()) }()

		invalidContent := "2024-01-01 * \"Using unopened account\"\n  Assets:Checking  100 USD\n  Expenses:Food   -100 USD"
		_, err = tmpFileErr.WriteString(invalidContent)
		assert.NoError(t, err)
		_ = tmpFileErr.Close()

		serverErr := New(8080, tmpFileErr.Name())
		err = serverErr.reloadLedger(context.Background())
		assert.NoError(t, err)
		muxErr, err := serverErr.setupRouter()
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/source", nil)
		rec := httptest.NewRecorder()

		muxErr.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		errors := response["errors"].([]interface{})
		assert.True(t, len(errors) > 0, "Expected validation errors for unopened accounts")
	})
}

func TestAPIAccounts(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.beancount")
	assert.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testContent := `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Food
2024-01-01 open Liabilities:CreditCard
2024-01-02 * "Test transaction"
  Assets:Checking  100 USD
  Expenses:Food   -100 USD`
	_, err = tmpFile.WriteString(testContent)
	assert.NoError(t, err)
	_ = tmpFile.Close()

	server := New(8080, tmpFile.Name())
	err = server.reloadLedger(context.Background())
	assert.NoError(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	t.Run("ReturnsSortedAccounts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var response AccountsResponse
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		assert.Equal(t, 3, len(response.Accounts))

		// Verify accounts are sorted alphabetically
		assert.Equal(t, "Assets:Checking", response.Accounts[0].Name)
		assert.Equal(t, "Assets", response.Accounts[0].Type)

		assert.Equal(t, "Expenses:Food", response.Accounts[1].Name)
		assert.Equal(t, "Expenses", response.Accounts[1].Type)

		assert.Equal(t, "Liabilities:CreditCard", response.Accounts[2].Name)
		assert.Equal(t, "Liabilities", response.Accounts[2].Type)
	})

	t.Run("EmptyArrayWhenNoFile", func(t *testing.T) {
		serverNoFile := New(8080, "")
		// Manually initialize empty ledger for testing (since we're not calling Start())
		serverNoFile.ledger = ledger.New()
		muxNoFile, err := serverNoFile.setupRouter()
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		rec := httptest.NewRecorder()

		muxNoFile.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response AccountsResponse
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		assert.Equal(t, 0, len(response.Accounts))
		assert.NotEqual(t, nil, response.Accounts) // Should be empty array, not nil
	})

	t.Run("ValidJSONStructure", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		accounts, ok := response["accounts"].([]interface{})
		assert.True(t, ok, "accounts should be an array")
		assert.True(t, len(accounts) > 0)

		firstAccount := accounts[0].(map[string]interface{})
		_, hasName := firstAccount["name"]
		_, hasType := firstAccount["type"]
		assert.True(t, hasName, "account should have 'name' field")
		assert.True(t, hasType, "account should have 'type' field")
	})
}
