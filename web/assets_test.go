//go:build !dev

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestIndexMetadataFollowsTitle(t *testing.T) {
	ledgerFile := filepath.Join(t.TempDir(), "main.beancount")
	err := os.WriteFile(ledgerFile, []byte(`option "title" "Joe's </script> Ledger"`+"\n"), 0600)
	assert.NoError(t, err)

	server := New(8080, ledgerFile)
	_, err = server.reloadLedger(context.Background())
	assert.NoError(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	get := func() string {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/balance-sheet", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	assert.Contains(t, get(), `"title":"Joe's \u003c/script\u003e Ledger"`)

	err = os.WriteFile(ledgerFile, []byte(`option "title" "Renamed"`+"\n"), 0600)
	assert.NoError(t, err)
	_, err = server.reloadLedger(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, get(), `"title":"Renamed"`)
}
