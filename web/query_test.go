package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestAPIQuery(t *testing.T) {
	ledgerFile := filepath.Join(t.TempDir(), "main.beancount")
	writeLedger := func(amount string) {
		t.Helper()
		err := os.WriteFile(ledgerFile, []byte(`2024-01-01 open Assets:Checking
2024-01-01 open Equity:Opening

2024-01-15 * "Opening balance"
  Assets:Checking  `+amount+` USD
  Equity:Opening
`), 0600)
		assert.NoError(t, err)
	}
	writeLedger("1000.00")

	server := New(8080, ledgerFile)
	server.ReadOnly = true
	_, err := server.reloadLedger(context.Background())
	assert.NoError(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	run := func(t *testing.T, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			return rec.Code, rec.Body.String()
		}
		var response QueryResponse
		assert.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
		return rec.Code, response.Output
	}

	t.Run("Text", func(t *testing.T) {
		code, output := run(t, `{"query": " select account, sum(position) group by account order by account "}`)
		assert.Equal(t, http.StatusOK, code)
		// beancount query's bytes, which match beanquery's
		assert.Equal(t, "    account      sum(position\n"+
			"---------------  ------------\n"+
			"Assets:Checking   1000.00 USD\n"+
			"Equity:Opening   -1000.00 USD\n", output)
	})

	t.Run("CSV", func(t *testing.T) {
		code, output := run(t, `{"query": "select account order by account", "format": "csv"}`)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "account\r\nAssets:Checking\r\nEquity:Opening\r\n", output)
	})

	t.Run("BQLErrorIsOutput", func(t *testing.T) {
		code, output := run(t, `{"query": "select nosuchcolumn"}`)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "ERROR: Invalid column name 'nosuchcolumn' in targets/column context.\n", output)
	})

	t.Run("UnknownFormat", func(t *testing.T) {
		code, _ := run(t, `{"query": "select account", "format": "html"}`)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("InvalidBody", func(t *testing.T) {
		code, _ := run(t, `not json`)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("SeesReloadedLedger", func(t *testing.T) {
		writeLedger("250.00")
		_, err := server.reloadLedger(context.Background())
		assert.NoError(t, err)

		code, output := run(t, `{"query": "select sum(position) where account = 'Assets:Checking'"}`)
		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, output, "250.00 USD")
	})
}

func TestAPIQueryWithoutLoadedLedger(t *testing.T) {
	ledgerFile := filepath.Join(t.TempDir(), "main.beancount")
	assert.NoError(t, os.WriteFile(ledgerFile, []byte("2024-01-01 open\n"), 0600))

	server := New(8080, ledgerFile)
	_, err := server.reloadLedger(context.Background())
	assert.Error(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/query", strings.NewReader(`{"query": "select 1"}`)))
	assert.Equal(t, http.StatusConflict, rec.Code)
}
