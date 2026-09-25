package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
)

const importLedger = `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Food
`

// buildFixtureImporter builds the importer package's fixture Importer.
func buildFixtureImporter(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", path, "../importer/testdata/fixture").CombinedOutput()
	assert.NoError(t, err, string(out))
	return path
}

// runImport runs beancount import against a ledger and statement written to
// a temporary directory, and returns stdout, stderr and the exit code.
func runImport(t *testing.T, importerPath, ledger, statement string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.beancount")
	statementPath := filepath.Join(dir, statement)
	assert.NoError(t, os.WriteFile(ledgerPath, []byte(ledger), 0o600))
	assert.NoError(t, os.WriteFile(statementPath, nil, 0o600))

	var cmds Commands
	var stdout, stderr bytes.Buffer
	parser, err := kong.New(&cmds, kong.Writers(&stdout, &stderr), kong.Bind(&cmds.Globals))
	assert.NoError(t, err)
	ctx, err := parser.Parse([]string{"import", "--with", importerPath, ledgerPath, statementPath})
	assert.NoError(t, err)

	code := 0
	if err := ctx.Run(); err != nil {
		var cmdErr *CommandError
		assert.True(t, errors.As(err, &cmdErr), "unexpected error: %v", err)
		code = cmdErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func TestImportCmd(t *testing.T) {
	importerPath := buildFixtureImporter(t)

	t.Run("PrintsExtractedDirectives", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, `2024-01-15 * "Coffee Shop" "Latte"
    import-id: "TX-001"
    Expenses:Food                    4.50 USD
    Assets:Checking

2024-01-16 balance Assets:Checking  -4.50 USD
`, stdout)
		assert.Contains(t, stderr, "stderr line")
	})

	t.Run("UnbalancedTransaction", func(t *testing.T) {
		t.Setenv("FIXTURE_MODE", "unbalanced")
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "statement.csv (extracted):1:")
	})

	t.Run("ExtractedSyntaxError", func(t *testing.T) {
		t.Setenv("FIXTURE_MODE", "bad-tag")
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "statement.csv (extracted):1:")
		assert.Contains(t, stderr, "parse error")
	})

	t.Run("LedgerErrorsFailTheImport", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, "2024-01-01 open Assets:Checking\n", "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "Expenses:Food")
	})

	t.Run("StatementNotRecognized", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.ofx")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "importer "+filepath.Base(importerPath)+" does not recognize")
	})

	t.Run("ExtractError", func(t *testing.T) {
		t.Setenv("FIXTURE_MODE", "extract-error")
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "importer "+filepath.Base(importerPath)+": statement is truncated")
	})

	t.Run("NotAnImporter", func(t *testing.T) {
		t.Setenv("FIXTURE_MODE", "not-importer")
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "as a beancount Importer")
	})
}
