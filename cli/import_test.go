package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
)

const importLedger = `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Food
`

// buildImporter builds the Importer in pkg.
func buildImporter(t *testing.T, pkg string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), filepath.Base(pkg))
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", path, pkg).CombinedOutput()
	assert.NoError(t, err, string(out))
	return path
}

// runImport runs beancount import against a ledger and statement written to
// a temporary directory, and returns stdout, stderr and the exit code.
func runImport(t *testing.T, importerPath, ledger, statement string, flags ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.beancount")
	statementPath := filepath.Join(dir, statement)
	assert.NoError(t, os.WriteFile(ledgerPath, []byte(ledger), 0o600))
	assert.NoError(t, os.WriteFile(statementPath, nil, 0o600))
	return runImportFiles(t, importerPath, ledgerPath, statementPath, flags...)
}

func runImportFiles(t *testing.T, importerPath, ledgerPath, statementPath string, flags ...string) (string, string, int) {
	t.Helper()
	var cmds Commands
	var stdout, stderr bytes.Buffer
	parser, err := kong.New(&cmds, kong.Writers(&stdout, &stderr), kong.Bind(&cmds.Globals))
	assert.NoError(t, err)
	args := append([]string{"import", "--with", importerPath}, flags...)
	ctx, err := parser.Parse(append(args, ledgerPath, statementPath))
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
	importerPath := buildImporter(t, "../importer/testdata/fixture")

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

func TestImportCmdDuplicates(t *testing.T) {
	importerPath := buildImporter(t, "../importer/testdata/fixture")
	const latte = `2024-01-15 * "Coffee Shop" "Latte"`
	const balance = `2024-01-16 balance Assets:Checking  -4.50 USD`
	ledger := func(entries string) string {
		return "2024-01-01 open Assets:Checking\n2024-01-01 open Expenses:Food\n2024-01-01 open Equity:Opening\n\n" + entries
	}

	t.Run("SameImportID", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, ledger(`2024-01-15 * "Latte"
  import-id: "TX-001"
  Expenses:Food 4.50 USD
  Assets:Checking
`), "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, "2024-01-16 balance Assets:Checking  -4.50 USD\n", stdout)
		assert.Contains(t, stderr, "ledger.beancount:5: Duplicate left out: "+latte+"\n")
	})

	t.Run("NoImportIDOneDayApart", func(t *testing.T) {
		// The ledger's Assets:Checking posting is interpolated.
		stdout, stderr, code := runImport(t, importerPath, ledger(`2024-01-14 * "Coffee"
  Expenses:Food 4.5 USD
  Assets:Checking
`), "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, "2024-01-16 balance Assets:Checking  -4.50 USD\n", stdout)
		assert.Contains(t, stderr, "ledger.beancount:5: Duplicate left out: "+latte+"\n")
	})

	t.Run("DifferentImportIDs", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, ledger(`2024-01-15 * "Latte"
  import-id: "TX-000"
  Expenses:Food 4.50 USD
  Equity:Opening
`), "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Contains(t, stdout, latte)
		assert.NotContains(t, stderr, "Duplicate")
	})

	t.Run("ThreeDaysApart", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, ledger(`2024-01-12 * "Coffee"
  Expenses:Food 4.50 USD
  Equity:Opening
`), "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Contains(t, stdout, latte)
		assert.NotContains(t, stderr, "Duplicate")
	})

	t.Run("IdenticalInOneStatement", func(t *testing.T) {
		t.Setenv("FIXTURE_MODE", "twice")
		stdout, stderr, code := runImport(t, importerPath, ledger(""), "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, `2024-01-15 * "Latte"
    Expenses:Food  4.50 USD
    Assets:Checking

2024-01-15 * "Latte"
    Expenses:Food  4.50 USD
    Assets:Checking
`, stdout)
		assert.NotContains(t, stderr, "Duplicate")
	})

	t.Run("IdenticalBalance", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, ledger(balance+"\n"), "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Contains(t, stdout, latte)
		assert.NotContains(t, stdout, "balance")
		assert.Contains(t, stderr, "ledger.beancount:5: Duplicate left out: "+balance+"\n")
	})

	t.Run("BalanceWithDifferentAmount", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, ledger("2024-01-16 balance Assets:Checking -4.00 USD\n"), "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.NotContains(t, stderr, "Duplicate left out")
		assert.Contains(t, stderr, "statement.csv (extracted):6: Duplicate balance assertion with different amounts")
	})
}

func TestImportCmdUnknownAccount(t *testing.T) {
	importerPath := buildImporter(t, "../importer/testdata/fixture")
	t.Setenv("FIXTURE_MODE", "single-posting")

	t.Run("Default", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, importLedger+"2024-01-01 open Expenses:Unknown\n", "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, `2024-01-15 * "Latte"
    Assets:Checking  -4.50 USD
    Expenses:Unknown
`, stdout)
	})

	t.Run("Flag", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, importLedger+"2024-01-01 open Expenses:Uncategorized\n", "statement.csv", "--unknown-account", "Expenses:Uncategorized")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, `2024-01-15 * "Latte"
    Assets:Checking  -4.50 USD
    Expenses:Uncategorized
`, stdout)
	})

	t.Run("NotOpened", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "statement.csv (extracted):1: Invalid reference to unknown account 'Expenses:Unknown'")
	})

	t.Run("DuplicateBeforeUnknownPosting", func(t *testing.T) {
		// Duplicates are looked up before the Unknown posting is added, so
		// the lone Assets:Checking posting's accounts are nested in the
		// ledger transaction's.
		stdout, stderr, code := runImport(t, importerPath, importLedger+`
2024-01-14 * "Coffee"
  Expenses:Food 4.50 USD
  Assets:Checking
`, "statement.csv")
		assert.Equal(t, 0, code, stderr)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, `ledger.beancount:4: Duplicate left out: 2024-01-15 * "Latte"`)
	})

	t.Run("InvalidAccount", func(t *testing.T) {
		stdout, stderr, code := runImport(t, importerPath, importLedger, "statement.csv", "--unknown-account", "unknown")
		assert.Equal(t, 1, code)
		assert.Equal(t, "", stdout)
		assert.Contains(t, stderr, "invalid --unknown-account")
	})
}

func TestImportCmdCSVExample(t *testing.T) {
	importerPath := buildImporter(t, "../_examples/csv_importer")
	dir := "../_examples/csv_importer"

	stdout, stderr, code := runImportFiles(t, importerPath, filepath.Join(dir, "ledger.beancount"), filepath.Join(dir, "transactions.csv"))
	assert.Equal(t, 0, code, stderr)

	expected, err := os.ReadFile(filepath.Join(dir, "expected.beancount"))
	assert.NoError(t, err)
	// Windows checkouts may turn the golden file's newlines into CRLF.
	assert.Equal(t, strings.ReplaceAll(string(expected), "\r\n", "\n"), stdout)
}
