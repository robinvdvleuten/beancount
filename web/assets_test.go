//go:build !dev

package web

import (
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

	server, mux := newTestServer(t, ledgerFile)

	get := func() string {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/balance-sheet", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	assert.Contains(t, get(), `"title":"Joe's \u003c/script\u003e Ledger"`)

	err = os.WriteFile(ledgerFile, []byte(`option "title" "Renamed"`+"\n"), 0600)
	assert.NoError(t, err)
	reload(t, server)
	assert.Contains(t, get(), `"title":"Renamed"`)
}

func TestIndexAfterFailedFirstLoad(t *testing.T) {
	// A ledger that never loaded still serves the editor, under the
	// default title.
	root := filepath.Join(t.TempDir(), "main.beancount")
	assert.NoError(t, os.Mkdir(root, 0700))

	mux := newFailedTestHandler(t, root)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Beancount"`)
}
