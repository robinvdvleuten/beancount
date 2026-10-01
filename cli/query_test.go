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
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/query"
)

func TestQueryShell(t *testing.T) {
	ctx := context.Background()
	ldr := loader.New(loader.WithFollowIncludes())
	result, err := ldr.Load(ctx, "../testdata/compliance/query/ledger.beancount")
	assert.NoError(t, err)

	l := ledger.New()
	assert.NoError(t, l.Process(ctx, result.AST))
	cfg, err := config.FromAST(result.AST)
	assert.NoError(t, err)
	qctx := &query.Context{Ledger: l, Config: cfg, AST: result.AST}

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
	ldr := loader.New(loader.WithFollowIncludes())
	result, err := ldr.Load(ctx, "../testdata/compliance/query/ledger.beancount")
	assert.NoError(t, err)

	l := ledger.New()
	assert.NoError(t, l.Process(ctx, result.AST))
	cfg, err := config.FromAST(result.AST)
	assert.NoError(t, err)

	var out strings.Builder
	assert.NoError(t, runShell(ctx, &query.Context{Ledger: l, Config: cfg, AST: result.AST},
		query.FormatText, false, strings.NewReader(""), &out, &out, nil, nil))
}
