package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/query"
)

func TestQueryShell(t *testing.T) {
	ctx := context.Background()
	qctx := loadQueryLedger(t)

	in := strings.NewReader("help\nerrors\nselect count(date);\nbogus query\nexit\n")
	var out, errOut strings.Builder
	assert.NoError(t, runShell(ctx, qctx, query.FormatText, false, in, &out, &errOut, nil, nil))

	output := out.String()
	assert.Contains(t, output, `Input file: "Query Compliance Ledger"`)
	assert.Contains(t, output, "Ready with 26 directives (22 postings in 10 transactions).")
	assert.Contains(t, output, "beancount> ")
	assert.Contains(t, output, "Commands: errors")
	assert.Contains(t, output, "(no errors)")
	assert.Contains(t, output, "22") // count(date) result
	// bogus query reports on stderr, and the shell continues.
	assert.Equal(t, "error: syntax error\n| bogus query\n| ^\n", errOut.String())
}

// TestQueryErrorOnStderr checks that, like bean-query, a statement that
// does not compile prints nothing on stdout, its error on stderr, and
// fails with exit status 1, leaving no -o file.
func TestQueryErrorOnStderr(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "out.txt")
	for _, args := range [][]string{
		{"query", "../testdata/compliance/query/ledger.beancount", "SELECT bogus"},
		{"query", "-o", outFile, "../testdata/compliance/query/ledger.beancount", "SELECT bogus"},
	} {
		stdout, stderr, err := runCommand(t, args...)
		assert.Equal(t, "", stdout)
		assert.Equal(t, "error: column \"bogus\" not found in table \"postings\"\n"+
			"| SELECT bogus\n"+
			"|        ^^^^^\n", stderr)
		var cmdErr *CommandError
		assert.True(t, errors.As(err, &cmdErr), "want a CommandError, got %v", err)
		assert.Equal(t, 1, cmdErr.ExitCode())
	}
	_, err := os.Stat(outFile)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "want no %s, got %v", outFile, err)
}

// TestQueryOutputFile checks that -o writes the result to the file and,
// like bean-query, creates none for a result that prints nothing.
func TestQueryOutputFile(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := "../testdata/compliance/query/ledger.beancount"

	_, _, err := runCommand(t, "query", "-o", filepath.Join(dir, "rows.txt"), ledgerPath, "SELECT 1 AS n LIMIT 1")
	assert.NoError(t, err)
	rows, err := os.ReadFile(filepath.Join(dir, "rows.txt"))
	assert.NoError(t, err)
	assert.Equal(t, "n\n-\n1\n", string(rows))

	_, _, err = runCommand(t, "query", "-o", filepath.Join(dir, "empty.txt"), ledgerPath, "SELECT account WHERE account = 'x'")
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "empty.txt"))
	assert.True(t, errors.Is(err, fs.ErrNotExist), "want no empty.txt, got %v", err)
}

// TestQueryEmpty checks that, like bean-query, an empty query prints
// nothing and succeeds, without reading a query from stdin.
func TestQueryEmpty(t *testing.T) {
	for _, query := range []string{"", "  ", ";"} {
		stdout, stderr, err := runCommand(t, "query", "../testdata/compliance/query/ledger.beancount", query)
		assert.NoError(t, err, query)
		assert.Equal(t, "", stdout, query)
		assert.Equal(t, "", stderr, query)
	}
}

func TestQueryShellEOF(t *testing.T) {
	ctx := context.Background()
	var out strings.Builder
	assert.NoError(t, runShell(ctx, loadQueryLedger(t),
		query.FormatText, false, strings.NewReader(""), &out, &out, nil, nil))
}

// loadQueryLedger loads the shared query ledger like the query command,
// which reports no errors for it.
func loadQueryLedger(t *testing.T) *query.Context {
	t.Helper()
	var stderr strings.Builder
	qctx, result, err := loadQueryContext(context.Background(), &stderr, &FileOrStdin{Filename: "../testdata/compliance/query/ledger.beancount"})
	assert.NoError(t, err)
	assert.Equal(t, "", stderr.String())
	assert.Equal(t, 0, len(result.Ledger.Diagnostics()))
	return qctx
}

// TestQueryLedgerErrorsOnStderr pins the layout bean-query gives the errors
// it loads a ledger with: beancount's print_errors follows each one, plain
// or with its directive, with a blank line.
func TestQueryLedgerErrorsOnStderr(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "one plain error",
			source: "include \"missing.beancount\"\n",
			want:   "FILE:1: File glob \"missing.beancount\" does not match any files\n\n",
		},
		{
			name:   "an error with its directive",
			source: "2024-01-01 note Assets:Z \"x\"\n",
			want: "FILE:1: Invalid reference to unknown account 'Assets:Z'\n\n" +
				"   2024-01-01 note Assets:Z \"x\"\n\n\n",
		},
		{
			name: "two errors",
			source: "include \"missing.beancount\"\n" +
				"2024-01-01 open Assets:A\n" +
				"2024-01-02 *\n  Assets:A  1 USD\n  Assets:Z\n" +
				"2024-01-03 note Assets:Y \"x\"\n",
			want: "FILE:1: File glob \"missing.beancount\" does not match any files\n\n" +
				"FILE:3: Invalid reference to unknown account 'Assets:Z'\n\n" +
				"   2024-01-02 * \n     Assets:A   1 USD\n     Assets:Z  -1 USD\n\n\n" +
				"FILE:6: Invalid reference to unknown account 'Assets:Y'\n\n" +
				"   2024-01-03 note Assets:Y \"x\"\n\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLedger(t, t.TempDir(), "main.beancount", tc.source)
			_, stderr, err := runCommand(t, "query", path, "select 1")
			assert.NoError(t, err)
			assert.Equal(t, tc.want, strings.ReplaceAll(stderr, path, "FILE"))
		})
	}
}
