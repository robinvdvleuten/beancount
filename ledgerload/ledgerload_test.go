package ledgerload

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alecthomas/assert/v2"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.beancount")
	assert.NoError(t, os.WriteFile(path, []byte(`option "title" "Test"
2024-01-01 open Assets:Cash
2024-01-02 garbage
2024-01-03 * "spent"
  Assets:Cash  -5 USD
  Expenses:Food
`), 0o600))

	result, err := Load(context.Background(), loader.Source{Path: path})
	assert.NoError(t, err)
	// The syntax error is a load diagnostic, and the rest is processed.
	assert.Equal(t, 1, len(result.LoadDiagnostics))
	assert.Equal(t, 1, len(result.Ledger.Diagnostics()), "%v", result.Ledger.Diagnostics())
	// Diagnostics lists the load's, then the ledger's.
	assert.Equal(t, append(slices.Clone(result.LoadDiagnostics), result.Ledger.Diagnostics()...), result.Diagnostics())
	assert.Equal(t, "Test", result.Ledger.Config().Title)
	txn := result.AST.Directives[1].(*ast.Transaction)
	assert.Equal(t, 1, len(result.Ledger.BookedPositions(txn.Postings[1])), "the processed tree's postings are booked")
	assert.Equal(t, path, result.Root)
}

func TestWith(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.beancount")
	assert.NoError(t, os.WriteFile(path, []byte(`2024-01-01 open Assets:Cash
2024-01-03 * "spent"
  Assets:Cash  -5 USD
  Expenses:Food
`), 0o600))
	ctx := context.Background()
	result, err := Load(ctx, loader.Source{Path: path})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.Ledger.Diagnostics()))

	opens, err := parser.ParseString(ctx, "2024-01-01 open Expenses:Food\n")
	assert.NoError(t, err)
	with, err := result.With(ctx, opens.Directives)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(with.Ledger.Diagnostics()), "%v", with.Ledger.Diagnostics())
	assert.Equal(t, 3, len(with.AST.Directives))

	// The result it was made from is left as it was.
	assert.Equal(t, 1, len(result.Ledger.Diagnostics()))
	assert.Equal(t, 2, len(result.AST.Directives))
}
