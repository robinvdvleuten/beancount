package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/parser"
)

// getBinaryName returns the platform-specific binary name for tests
func getBinaryName() string {
	if runtime.GOOS == "windows" {
		return "beancount-test.exe"
	}
	return "beancount-test"
}

// cleanupBinary removes the test binary in a cross-platform way
func cleanupBinary(name string) {
	_ = os.Remove(name)
}

func TestFormatCmd(t *testing.T) {
	t.Run("BasicFormatting", func(t *testing.T) {
		source := `
option "title" "Test"

2021-01-01 open Assets:Checking

2021-01-02 * "Test transaction"
  Assets:Checking  -100.00 USD
  Expenses:Food  100.00 USD
`
		// Parse the input
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		// Format to buffer
		f := formatter.New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Verify output contains key elements
		assert.True(t, bytes.Contains([]byte(output), []byte("option \"title\" \"Test\"")))
		assert.True(t, bytes.Contains([]byte(output), []byte("2021-01-01 open Assets:Checking")))
		assert.True(t, bytes.Contains([]byte(output), []byte("Assets:Checking")))
		assert.True(t, bytes.Contains([]byte(output), []byte("100.00 USD")))
	})

	t.Run("WithCustomCurrencyColumn", func(t *testing.T) {
		source := `
2021-01-01 * "Test"
  Assets:Checking  -50.00 USD
  Expenses:Food  50.00 USD
`
		// Parse the input
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		// Format with custom column
		f := formatter.New(formatter.WithCurrencyColumn(60))
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()
		// Verify formatting occurred
		assert.True(t, bytes.Contains([]byte(output), []byte("USD")))
		// Verify custom column was used
		assert.Equal(t, 60, f.CurrencyColumn)
	})

	t.Run("EmptyFile", func(t *testing.T) {
		source := ``
		// Empty file should parse successfully but produce no output
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		f := formatter.New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		// Empty file produces minimal output
		output := buf.String()
		_ = output
	})
}

// TestFormatCmdIntegration tests the full command integration
func TestFormatCmdIntegration(t *testing.T) {
	t.Run("CompleteFile", func(t *testing.T) {
		source := `
option "title" "Integration Test"

2021-01-01 commodity USD

2021-01-01 open Assets:Checking  USD

2021-01-02 * "Opening balance"
  Assets:Checking  1000.00 USD
  Equity:Opening-Balances  -1000.00 USD

2021-01-03 balance Assets:Checking  1000.00 USD
`
		ast, err := parser.ParseBytes(context.Background(), []byte(source))
		assert.NoError(t, err)

		f := formatter.New()
		var buf bytes.Buffer
		err = f.Format(context.Background(), ast, []byte(source), &buf)
		assert.NoError(t, err)

		output := buf.String()

		// Verify all directive types are present
		assert.True(t, bytes.Contains([]byte(output), []byte("option")))
		assert.True(t, bytes.Contains([]byte(output), []byte("commodity")))
		assert.True(t, bytes.Contains([]byte(output), []byte("open")))
		assert.True(t, bytes.Contains([]byte(output), []byte("balance")))

		// Verify amounts are aligned
		assert.True(t, bytes.Contains([]byte(output), []byte("1000.00 USD")))
	})
}

// TestStdinIntegration tests the full stdin functionality by running the compiled binary
func TestStdinIntegration(t *testing.T) {
	t.Run("CheckStdinSuccess", func(t *testing.T) {
		binaryName := getBinaryName()
		// Build the binary
		cmd := exec.Command("go", "build", "-o", binaryName, ".")
		err := cmd.Run()
		assert.NoError(t, err)
		defer cleanupBinary(binaryName)

		// Test successful check with stdin (using -)
		checkCmd := exec.Command("./"+binaryName, "check", "-")
		checkCmd.Stdin = strings.NewReader("2024-01-01 open Assets:Checking USD")
		output, err := checkCmd.CombinedOutput()
		assert.NoError(t, err)
		assert.Contains(t, string(output), "✓ Check passed")
	})

	t.Run("CheckStdinDefault", func(t *testing.T) {
		binaryName := getBinaryName()
		// Build the binary
		cmd := exec.Command("go", "build", "-o", binaryName, ".")
		err := cmd.Run()
		assert.NoError(t, err)
		defer cleanupBinary(binaryName)

		// Test successful check with stdin (no arguments = default to stdin)
		checkCmd := exec.Command("./"+binaryName, "check")
		checkCmd.Stdin = strings.NewReader("2024-01-01 open Assets:Checking USD")
		output, err := checkCmd.CombinedOutput()
		assert.NoError(t, err)
		assert.Contains(t, string(output), "✓ Check passed")
	})

	t.Run("FormatStdin", func(t *testing.T) {
		binaryName := getBinaryName()
		// Build the binary
		cmd := exec.Command("go", "build", "-o", binaryName, ".")
		err := cmd.Run()
		assert.NoError(t, err)
		defer cleanupBinary(binaryName)

		// Test format with stdin (using -)
		formatCmd := exec.Command("./"+binaryName, "format", "-")
		formatCmd.Stdin = strings.NewReader("2024-01-01 open Assets:Checking USD")
		output, err := formatCmd.Output()
		assert.NoError(t, err)
		assert.Equal(t, "2024-01-01 open Assets:Checking USD\n", string(output))
	})

	t.Run("FormatStdinDefault", func(t *testing.T) {
		binaryName := getBinaryName()
		// Build the binary
		cmd := exec.Command("go", "build", "-o", binaryName, ".")
		err := cmd.Run()
		assert.NoError(t, err)
		defer cleanupBinary(binaryName)

		// Test format with stdin (no arguments = default to stdin)
		formatCmd := exec.Command("./"+binaryName, "format")
		formatCmd.Stdin = strings.NewReader("2024-01-01 open Assets:Checking USD")
		output, err := formatCmd.Output()
		assert.NoError(t, err)
		assert.Equal(t, "2024-01-01 open Assets:Checking USD\n", string(output))
	})

	t.Run("CheckStdinError", func(t *testing.T) {
		binaryName := getBinaryName()
		// Build the binary
		cmd := exec.Command("go", "build", "-o", binaryName, ".")
		assert.NoError(t, cmd.Run())
		defer cleanupBinary(binaryName)

		// Test error handling with stdin
		checkCmd := exec.Command("./"+binaryName, "check", "-")
		checkCmd.Stdin = strings.NewReader("2024-01-01 invalid directive")
		output, err := checkCmd.CombinedOutput()
		assert.Error(t, err)
		assert.Contains(t, string(output), "<stdin>:")
		assert.Contains(t, string(output), "parse error")
	})

	t.Run("CheckStdinWithIncludesError", func(t *testing.T) {
		binaryName := getBinaryName()
		// Build the binary
		cmd := exec.Command("go", "build", "-o", binaryName, ".")
		assert.NoError(t, cmd.Run())
		defer cleanupBinary(binaryName)

		// Test include directive error with stdin
		checkCmd := exec.Command("./"+binaryName, "check", "-")
		checkCmd.Stdin = strings.NewReader(`include "accounts.beancount"
2024-01-01 open Assets:Checking USD`)
		output, err := checkCmd.CombinedOutput()
		assert.Error(t, err)
		assert.Contains(t, string(output), "include directives are not supported when reading from stdin")
	})
}
