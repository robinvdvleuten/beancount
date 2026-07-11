package cli

import (
	"context"
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
	qctx := &query.Context{Ledger: l, Config: cfg}

	in := strings.NewReader("help\nerrors\nselect count(date);\nbogus query\nexit\n")
	var out strings.Builder
	assert.NoError(t, runShell(ctx, qctx, result.AST, "text", false, in, &out, nil, nil))

	output := out.String()
	assert.Contains(t, output, `Input file: "Query Compliance Ledger"`)
	assert.Contains(t, output, "Ready with 26 directives (22 postings in 10 transactions).")
	assert.Contains(t, output, "beancount> ")
	assert.Contains(t, output, "Commands: errors")
	assert.Contains(t, output, "(no errors)")
	assert.Contains(t, output, "22")         // count(date) result
	assert.Contains(t, output, "ERROR: ")    // bogus query reports, shell continues
	assert.NotContains(t, output, "bogus\n") // and does not echo the input
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
	assert.NoError(t, runShell(ctx, &query.Context{Ledger: l, Config: cfg}, result.AST,
		"text", false, strings.NewReader(""), &out, nil, nil))
}
