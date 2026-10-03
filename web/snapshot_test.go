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
	"time"

	"github.com/alecthomas/assert/v2"
)

// request sends one request to handler and returns the status and body.
func request(t *testing.T, handler http.Handler, method, target, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// putSource saves source to the ledger file at path (the root when empty)
// and returns the decoded response.
func putSource(t *testing.T, handler http.Handler, path, source string) SourceJSON {
	t.Helper()
	body, err := json.Marshal(map[string]string{"filepath": path, "source": source})
	assert.NoError(t, err)
	code, response := request(t, handler, http.MethodPut, "/api/source", string(body))
	assert.Equal(t, http.StatusOK, code, response)
	return decodeSource(t, response)
}

// SourceJSON is a response of /api/source as a client reads it.
type SourceJSON struct {
	Source string `json:"source"`
	Errors []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"errors"`
	Files Files `json:"files"`
}

func decodeSource(t *testing.T, body string) SourceJSON {
	t.Helper()
	var response SourceJSON
	assert.NoError(t, json.Unmarshal([]byte(body), &response), body)
	return response
}

const selectAccounts = `{"query": "select account, sum(position) group by account order by account"}`

// TestFailedFirstLoad pins what a server answers whose first load could not
// go on because its root file cannot be read: empty reports, no ledger to
// query, and no source to show.
func TestFailedFirstLoad(t *testing.T) {
	roots := map[string]func(t *testing.T) string{
		"UnreadableRoot": func(t *testing.T) string {
			root := writeLedger(t, "2024-01-01 open Assets:Cash\n")
			assert.NoError(t, os.Chmod(root, 0))
			t.Cleanup(func() { _ = os.Chmod(root, 0600) })
			if _, err := os.ReadFile(root); err == nil {
				t.Skip("a file without permissions is still readable here")
			}
			return root
		},
		"RootIsADirectory": func(t *testing.T) string {
			root := filepath.Join(t.TempDir(), "main.beancount")
			assert.NoError(t, os.Mkdir(root, 0700))
			return root
		},
	}
	for name, makeRoot := range roots {
		t.Run(name, func(t *testing.T) {
			handler := newFailedTestHandler(t, makeRoot(t))

			code, body := request(t, handler, http.MethodGet, "/api/balances", "")
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, `{"roots":[],"currencies":[],"operatingCurrencies":[]}`+"\n", body)

			code, body = request(t, handler, http.MethodGet, "/api/balances?types=Assets,Liabilities,Equity&closed=true", "")
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, `{"roots":[],"currencies":[],"operatingCurrencies":[]}`+"\n", body)

			code, body = request(t, handler, http.MethodGet, "/api/accounts", "")
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, `{"accounts":[]}`+"\n", body)

			code, body = request(t, handler, http.MethodPost, "/api/query", selectAccounts)
			assert.Equal(t, http.StatusConflict, code)
			assert.Equal(t, "the ledger failed to load, so there is nothing to query\n", body)

			code, body = request(t, handler, http.MethodGet, "/api/source", "")
			assert.Equal(t, http.StatusInternalServerError, code)
			assert.Equal(t, "Failed to read file\n", body)
		})
	}
}

// TestFailedFirstLoadListsTheRootsIncludes pins that a ledger whose first
// load could not go on still names the root's own includes, as written, so
// they can be read and saved, and that saving a fix loads the ledger.
func TestFailedFirstLoadListsTheRootsIncludes(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.beancount")
	include := filepath.Join(dir, "include.beancount")
	broken := filepath.Join(dir, "broken.beancount")
	rootSource := "include \"include.beancount\"\ninclude \"sub/*.beancount\"\ninclude \"broken.beancount\"\n"
	assert.NoError(t, os.WriteFile(root, []byte(rootSource), 0600))
	assert.NoError(t, os.WriteFile(include, []byte("2024-01-01 open Assets:Cash\n"), 0600))
	assert.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0700))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "a.beancount"), []byte("2024-01-01 open Assets:Bank\n"), 0600))
	// An include that is a directory cannot be read, which stops the load.
	assert.NoError(t, os.Mkdir(broken, 0700))

	handler := newFailedTestHandler(t, root)

	code, body := request(t, handler, http.MethodGet, "/api/source", "")
	assert.Equal(t, http.StatusOK, code)
	source := decodeSource(t, body)
	assert.Equal(t, rootSource, source.Source)
	assert.Equal(t, 1, len(source.Errors))
	assert.Equal(t, "LoadError", source.Errors[0].Type)
	assert.Contains(t, source.Errors[0].Message, broken)
	// The includes are the root's own as written: a glob is not expanded.
	assert.Equal(t, Files{Root: root, Includes: []string{include, filepath.Join(dir, "sub", "*.beancount"), broken}}, source.Files)

	code, body = request(t, handler, http.MethodGet, "/api/source?filepath="+include, "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "2024-01-01 open Assets:Cash\n", decodeSource(t, body).Source)

	code, _ = request(t, handler, http.MethodGet, "/api/source?filepath="+filepath.Join(dir, "sub", "a.beancount"), "")
	assert.Equal(t, http.StatusBadRequest, code)

	code, body = request(t, handler, http.MethodGet, "/api/accounts", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, `{"accounts":[]}`+"\n", body)

	// Saving an include reloads, and still fails.
	saved := putSource(t, handler, include, "2024-01-01 open Assets:Wallet\n")
	assert.Equal(t, 1, len(saved.Errors))
	assert.Equal(t, "LoadError", saved.Errors[0].Type)

	// Saving a root without the broken include loads the ledger.
	saved = putSource(t, handler, "", "include \"include.beancount\"\ninclude \"sub/*.beancount\"\n")
	assert.Equal(t, 0, len(saved.Errors))
	assert.Equal(t, root, saved.Files.Root)
	includes := saved.Files.Includes
	assert.Equal(t, 2, len(includes))
	assert.SliceContains(t, includes, include)
	assert.SliceContains(t, includes, filepath.Join(dir, "sub", "a.beancount"))

	code, body = request(t, handler, http.MethodGet, "/api/accounts", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, `{"accounts":[{"name":"Assets:Bank","type":"Assets"},{"name":"Assets:Wallet","type":"Assets"}]}`+"\n", body)
}

// TestFailedReloadKeepsThePreviousLedger pins what a server answers after a
// reload that could not go on: the reports and queries still answer from
// the ledger loaded before, /api/source reports the failure, and a later
// reload that goes on replaces both.
func TestFailedReloadKeepsThePreviousLedger(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.beancount")
	include := filepath.Join(dir, "include.beancount")
	rootSource := "option \"title\" \"Kept\"\n" +
		"include \"include.beancount\"\n" +
		"2024-01-01 open Assets:Cash\n" +
		"2024-01-02 * \"pay\"\n" +
		"  Assets:Cash  10 USD\n" +
		"  Income:Salary\n"
	assert.NoError(t, os.WriteFile(root, []byte(rootSource), 0600))
	assert.NoError(t, os.WriteFile(include, []byte("2024-01-01 open Income:Salary\n"), 0600))

	handler := newTestHandler(t, root)

	reports := func() map[string]string {
		t.Helper()
		bodies := map[string]string{}
		for _, target := range []string{"/api/balances", "/api/balances?types=Assets,Liabilities,Equity&closed=true", "/api/accounts"} {
			code, body := request(t, handler, http.MethodGet, target, "")
			assert.Equal(t, http.StatusOK, code)
			bodies[target] = body
		}
		code, body := request(t, handler, http.MethodPost, "/api/query", selectAccounts)
		assert.Equal(t, http.StatusOK, code)
		bodies["/api/query"] = body
		return bodies
	}

	loaded := reports()
	assert.Equal(t, `{"accounts":[{"name":"Assets:Cash","type":"Assets"},{"name":"Income:Salary","type":"Income"}]}`+"\n", loaded["/api/accounts"])
	assert.Contains(t, loaded["/api/balances"], `"balance":{"USD":"10"}`)
	assert.Contains(t, loaded["/api/query"], "Assets:Cash")

	// The include becomes a directory, which no load can read, and a save
	// of the root reloads.
	assert.NoError(t, os.Remove(include))
	assert.NoError(t, os.Mkdir(include, 0700))
	saved := putSource(t, handler, "", rootSource)
	assert.Equal(t, 1, len(saved.Errors))
	assert.Equal(t, "LoadError", saved.Errors[0].Type)
	assert.Contains(t, saved.Errors[0].Message, include)

	assert.Equal(t, loaded, reports(), "the reports answer from the ledger loaded before")

	code, body := request(t, handler, http.MethodGet, "/api/source", "")
	assert.Equal(t, http.StatusOK, code)
	source := decodeSource(t, body)
	assert.Equal(t, rootSource, source.Source)
	assert.Equal(t, saved.Errors, source.Errors)
	assert.Equal(t, Files{Root: root, Includes: []string{include}}, source.Files)

	// The include is a file again, with another account: the next reload
	// goes on, and everything answers from it.
	assert.NoError(t, os.Remove(include))
	assert.NoError(t, os.WriteFile(include, []byte("2024-01-01 open Income:Salary\n2024-01-01 open Assets:Bank\n"), 0600))
	saved = putSource(t, handler, "", rootSource)
	assert.Equal(t, 0, len(saved.Errors))
	assert.Equal(t, Files{Root: root, Includes: []string{include}}, saved.Files)

	recovered := reports()
	assert.Equal(t, `{"accounts":[{"name":"Assets:Bank","type":"Assets"},{"name":"Assets:Cash","type":"Assets"},{"name":"Income:Salary","type":"Income"}]}`+"\n", recovered["/api/accounts"])
}

// TestWatcherFollowsTheIncludesOfAReload pins that a reload hands the
// watcher the include lists before and after it: an include the root gains
// is watched from then on.
func TestWatcherFollowsTheIncludesOfAReload(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.beancount")
	added := filepath.Join(dir, "added.beancount")
	assert.NoError(t, os.WriteFile(root, []byte("2024-01-01 open Assets:Cash\n"), 0600))
	assert.NoError(t, os.WriteFile(added, []byte("2024-01-01 open Assets:Bank\n"), 0600))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, handler := newTestServer(t, root)
	assert.NoError(t, server.startWatcher(ctx))

	events := make(chan string, 10)
	server.sseMu.Lock()
	server.sseClients[events] = struct{}{}
	server.sseMu.Unlock()
	awaitReload := func(why string) {
		t.Helper()
		select {
		case event := <-events:
			assert.Equal(t, "reload", event)
		case <-time.After(5 * time.Second):
			t.Fatal("no reload after " + why)
		}
	}
	// One write can reload more than once, when its events straddle the
	// watcher's debounce, so a reload event does not say which write it
	// follows: wait for the account itself.
	awaitAccount := func(account, why string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			code, body := request(t, handler, http.MethodGet, "/api/accounts", "")
			assert.Equal(t, http.StatusOK, code)
			if strings.Contains(body, `"`+account+`"`) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("no %s after %s: %s", account, why, body)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	assert.NoError(t, os.WriteFile(root, []byte("include \"added.beancount\"\n2024-01-01 open Assets:Cash\n"), 0600))
	awaitReload("the root gained an include")
	awaitAccount("Assets:Bank", "the root gained an include")

	assert.NoError(t, os.WriteFile(added, []byte("2024-01-01 open Assets:Bank\n2024-01-01 open Assets:Wallet\n"), 0600))
	awaitAccount("Assets:Wallet", "the new include changed")
}

// TestSnapshotIsNotChangedByAReload is of the snapshot itself: a handler
// that took one before a reload still reads the ledger it took.
func TestSnapshotIsNotChangedByAReload(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.beancount")
	include := filepath.Join(dir, "include.beancount")
	assert.NoError(t, os.WriteFile(root, []byte("include \"include.beancount\"\n2024-01-01 open Assets:Cash\n"), 0600))
	assert.NoError(t, os.WriteFile(include, []byte("2024-01-01 open Assets:Bank\n"), 0600))

	server, _ := newTestServer(t, root)
	before := server.snapshot()
	beforeSource := *before.sourceResponse(nil)
	assert.Equal(t, []string{include}, before.includes)
	assert.Equal(t, 2, len(before.ledger.Accounts()))

	// A reload that goes on, then one that cannot.
	assert.NoError(t, os.WriteFile(root, []byte("2024-01-01 open Assets:Cash\n2024-01-02 open\n"), 0600))
	prev, loaded, err := server.reloadLedger(context.Background())
	assert.NoError(t, err)
	assert.True(t, prev == before, "a reload returns the snapshot it replaced")
	assert.True(t, loaded == server.snapshot(), "and the one now served")
	assert.Equal(t, 0, len(loaded.includes))
	assert.Equal(t, 1, len(loaded.errors))

	assert.NoError(t, os.Remove(root))
	prev, failed, err := server.reloadLedger(context.Background())
	assert.Error(t, err)
	assert.True(t, prev == loaded)
	assert.True(t, failed == server.snapshot())
	assert.Equal(t, err, failed.loadErr)
	assert.True(t, failed.ledger == loaded.ledger, "a failed reload keeps the ledger loaded before")
	assert.True(t, failed.queryContext().AST == loaded.tree)

	assert.Equal(t, beforeSource, *before.sourceResponse(nil))
	assert.Equal(t, []string{include}, before.includes)
	assert.Equal(t, 2, len(before.ledger.Accounts()))
	assert.Equal(t, 1, len(loaded.errors))
	assert.Equal(t, nil, loaded.loadErr)
}
