package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// newTestHandler returns the handler of a server for the ledger file at
// path, loaded as Start loads it, so a test reads: file in, request,
// response out. It fails the test when the load cannot go on.
func newTestHandler(t *testing.T, path string) http.Handler {
	t.Helper()
	_, handler := newTestServer(t, path)
	return handler
}

// newTestServer is newTestHandler for a test that needs the server too: to
// reload it after an edit, to watch its files, or to set its options.
func newTestServer(t *testing.T, path string) (*Server, http.Handler) {
	t.Helper()
	server := New(8080, path)
	_, _, err := server.reloadLedger(context.Background())
	assert.NoError(t, err)
	return server, router(t, server)
}

// newFailedTestHandler returns the handler of a server whose first load of
// the ledger file at path could not go on, which Start serves all the
// same. It fails the test when the load goes on.
func newFailedTestHandler(t *testing.T, path string) http.Handler {
	t.Helper()
	server := New(8080, path)
	_, _, err := server.reloadLedger(context.Background())
	assert.Error(t, err)
	return router(t, server)
}

// newUnloadedTestHandler returns the handler of a server that has no
// ledger file and has loaded nothing.
func newUnloadedTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return router(t, New(8080, ""))
}

func router(t *testing.T, server *Server) http.Handler {
	t.Helper()
	handler, err := server.setupRouter()
	assert.NoError(t, err)
	return handler
}

// writeLedger writes source as main.beancount in a directory of its own and
// returns its path.
func writeLedger(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.beancount")
	assert.NoError(t, os.WriteFile(path, []byte(source), 0600))
	return path
}

// reload reloads the server's ledger from disk, as a save and the watcher
// do, and fails the test when the load cannot go on.
func reload(t *testing.T, server *Server) {
	t.Helper()
	_, _, err := server.reloadLedger(context.Background())
	assert.NoError(t, err)
}
