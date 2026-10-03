package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
)

var missingOpenDir = filepath.Join(complianceDir, "missing_open")

func missingOpenFixtures(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(missingOpenDir, "*.beancount"))
	assert.NoError(t, err)
	assert.True(t, len(paths) > 0, "no missing_open fixtures found in %s", missingOpenDir)
	return paths
}

// runOurMissingOpen runs doctor missing_open on path and returns its stdout.
func runOurMissingOpen(t *testing.T, path string) string {
	t.Helper()

	var cmds Commands
	var stdout, stderr bytes.Buffer
	parser, err := kong.New(&cmds, kong.Writers(&stdout, &stderr), kong.Bind(&cmds.Globals))
	assert.NoError(t, err)
	ctx, err := parser.Parse([]string{"doctor", "missing_open", path})
	assert.NoError(t, err)
	assert.NoError(t, ctx.Run(), "stderr:\n%s", stderr.String())
	return stdout.String()
}

func TestMissingOpenCmd(t *testing.T) {
	t.Run("PrintsOpensByFirstUse", func(t *testing.T) {
		out := runOurMissingOpen(t, filepath.Join(missingOpenDir, "balance_and_note.beancount"))
		assert.Equal(t, "2024-01-02 open Liabilities:Card\n"+
			"2024-01-03 open Assets:Other\n"+
			"2024-01-04 open Expenses:Zed\n"+
			"2024-01-05 open Expenses:Food\n", out)
	})

	t.Run("NothingMissing", func(t *testing.T) {
		assert.Equal(t, "", runOurMissingOpen(t, filepath.Join(missingOpenDir, "none.beancount")))
		assert.Equal(t, "", runOurMissingOpen(t, filepath.Join(missingOpenDir, "auto_accounts.beancount")))
	})
}

// TestMissingOpenPasteBack pastes each fixture's output into its ledger,
// after which no account it opens is reported as unknown. Like bean-doctor,
// it leaves out a close-only account. It also leaves out an account used
// only by a balance, which bean-doctor opens (#556).
func TestMissingOpenPasteBack(t *testing.T) {
	for _, path := range missingOpenFixtures(t) {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".beancount"), func(t *testing.T) {
			out := runOurMissingOpen(t, path)

			ctx := context.Background()
			result, err := ledgerload.Load(ctx, loader.Source{Path: path})
			assert.NoError(t, err)
			opens, err := parser.ParseBytesWithFilename(ctx, "missing_open", []byte(out))
			assert.NoError(t, err)
			opened := make(map[ast.Account]bool)
			for _, directive := range opens.Directives {
				opened[directive.(*ast.Open).Account] = true
			}

			pasted, err := result.With(ctx, opens.Directives)
			assert.NoError(t, err)
			for _, err := range pasted.Ledger.Diagnostics() {
				var diag *ledger.Diagnostic
				if !errors.As(err, &diag) || diag.Kind() != "AccountNotOpenError" {
					continue
				}
				assert.False(t, opened[diag.GetAccount()], "still unknown after pasting: %v", err)
			}
		})
	}
}

// missingOpenGaps lists missing_open fixtures whose output differs from
// bean-doctor's, with the reason. An entry whose output agrees, or that names
// no fixture, fails the missing_open parity suite.
var missingOpenGaps = map[string]string{}

// TestOfficialMissingOpenParity compares doctor missing_open byte-for-byte
// with bean-doctor missing_open on the fixtures under
// testdata/compliance/missing_open. Runs whenever bean-doctor 3.x is
// installed.
func TestOfficialMissingOpenParity(t *testing.T) {
	requireOfficialTool(t, "bean-doctor", 3)

	var names []string
	for _, path := range missingOpenFixtures(t) {
		name := strings.TrimSuffix(filepath.Base(path), ".beancount")
		names = append(names, name)
		t.Run(name, func(t *testing.T) {
			official, err := exec.Command("bean-doctor", "missing_open", path).Output()
			assert.NoError(t, err)
			ours := runOurMissingOpen(t, path)

			if reason, ok := missingOpenGaps[name]; ok {
				assert.NotEqual(t, string(official), ours, "the output agrees; remove the missingOpenGaps entry (%s)", reason)
				return
			}
			assert.Equal(t, string(official), ours)
		})
	}
	assertGapsNameFixtures(t, missingOpenGaps, names)
}
