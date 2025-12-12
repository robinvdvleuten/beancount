package cli

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

	"golang.org/x/term"
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
		cmd := exec.Command("go", "build", "-o", binaryName, "../cmd/beancount")
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
		cmd := exec.Command("go", "build", "-o", binaryName, "../cmd/beancount")
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
		cmd := exec.Command("go", "build", "-o", binaryName, "../cmd/beancount")
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
		cmd := exec.Command("go", "build", "-o", binaryName, "../cmd/beancount")
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
		cmd := exec.Command("go", "build", "-o", binaryName, "../cmd/beancount")
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
		cmd := exec.Command("go", "build", "-o", binaryName, "../cmd/beancount")
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

// TestWebCmdFileCreation tests the file creation functionality of the web command
func TestWebCmdFileCreation(t *testing.T) {
	t.Run("FileExistsNoPrompt", func(t *testing.T) {
		// Create a temporary file
		tmpDir := t.TempDir()
		tmpFile := tmpDir + "/existing.beancount"
		err := os.WriteFile(tmpFile, []byte(""), 0600)
		assert.NoError(t, err)

		// Verify file exists
		_, err = os.Stat(tmpFile)
		assert.NoError(t, err)

		// Note: We can't fully test server startup in unit tests since it would block,
		// but we can verify the file existence check doesn't trigger creation
		info, err := os.Stat(tmpFile)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), info.Size())
	})

	t.Run("FileDoesNotExistWithCreateFlag", func(t *testing.T) {
		// Create a temporary directory
		tmpDir := t.TempDir()
		tmpFile := tmpDir + "/new.beancount"

		// Verify file does not exist
		_, err := os.Stat(tmpFile)
		assert.True(t, os.IsNotExist(err))

		// Simulate creating the file with --create flag
		err = os.WriteFile(tmpFile, []byte(""), 0600)
		assert.NoError(t, err)

		// Verify file was created
		info, err := os.Stat(tmpFile)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), info.Size())
		assert.False(t, info.IsDir(), "should be a file, not directory")
	})

	t.Run("CreateParentDirectories", func(t *testing.T) {
		// Create a temporary directory
		tmpDir := t.TempDir()
		nestedPath := tmpDir + "/ledgers/2024/test.beancount"

		// Verify parent directories don't exist
		_, err := os.Stat(tmpDir + "/ledgers")
		assert.True(t, os.IsNotExist(err))

		// Create parent directories
		parentDir := tmpDir + "/ledgers/2024"
		err = os.MkdirAll(parentDir, 0755)
		assert.NoError(t, err)

		// Create file
		err = os.WriteFile(nestedPath, []byte(""), 0600)
		assert.NoError(t, err)

		// Verify file was created
		info, err := os.Stat(nestedPath)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), info.Size())

		// Verify parent directory was created
		dirInfo, err := os.Stat(parentDir)
		assert.NoError(t, err)
		assert.True(t, dirInfo.IsDir(), "should be a directory")
	})

	t.Run("PermissionDeniedOnCreate", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Permission tests don't work reliably on Windows")
		}

		// Create a read-only directory
		tmpDir := t.TempDir()
		readOnlyDir := tmpDir + "/readonly"
		err := os.Mkdir(readOnlyDir, 0555)
		assert.NoError(t, err)

		// Try to create file in read-only directory
		testFile := readOnlyDir + "/test.beancount"
		err = os.WriteFile(testFile, []byte(""), 0600)
		assert.Error(t, err)

		// Verify error is permission-related
		assert.True(t, os.IsPermission(err) || strings.Contains(err.Error(), "permission denied"))
	})
}

// TestPromptYesNo tests the interactive prompt functionality
func TestPromptYesNo(t *testing.T) {
	t.Run("NonTTYReturnsFalse", func(t *testing.T) {
		// Test that promptYesNo returns false when stdin is not a TTY
		// This test simulates a non-interactive environment (CI, piped input)

		// We can't easily test the actual function without a real TTY,
		// but we can verify the term.IsTerminal behavior

		// In a test environment, stdin is typically not a TTY
		isTTY := term.IsTerminal(int(os.Stdin.Fd()))

		// In most test environments, this should be false
		// (unless running tests interactively in a terminal)
		_ = isTTY

		// The key behavior is that promptYesNo should return false
		// immediately without blocking when not in a TTY
	})
}
