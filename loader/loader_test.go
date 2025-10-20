package loader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestLoadSingleFile(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err := os.WriteFile(mainFile, []byte(`
2024-01-01 open Assets:Checking USD
2024-01-02 * "Test"
  Assets:Checking  100.00 USD
  Equity:Opening-Balances
`), 0644)
	assert.NoError(t, err)

	// Test without FollowIncludes
	ldr := New()
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(tree.Directives))

	// Test with FollowIncludes (should behave the same for single file)
	ldr = New(WithFollowIncludes())
	tree, err = ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(tree.Directives))
}

func TestLoadWithInclude_NoFollow(t *testing.T) {
	tmpDir := t.TempDir()

	// Create included file
	includedFile := filepath.Join(tmpDir, "included.beancount")
	err := os.WriteFile(includedFile, []byte(`
2024-01-01 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create main file with include
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err = os.WriteFile(mainFile, []byte(`
include "included.beancount"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load without following includes
	ldr := New()
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Should have 1 directive (only from main file)
	assert.Equal(t, 1, len(tree.Directives))

	// Should preserve the include directive
	assert.Equal(t, 1, len(tree.Includes))
	assert.Equal(t, "included.beancount", tree.Includes[0].Filename)
}

func TestLoadWithInclude_WithFollow(t *testing.T) {
	tmpDir := t.TempDir()

	// Create included file
	includedFile := filepath.Join(tmpDir, "included.beancount")
	err := os.WriteFile(includedFile, []byte(`
2024-01-01 open Assets:Savings USD
2024-01-03 open Income:Salary USD
`), 0644)
	assert.NoError(t, err)

	// Create main file with include
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err = os.WriteFile(mainFile, []byte(`
include "included.beancount"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Should have 3 directives (merged from both files)
	assert.Equal(t, 3, len(tree.Directives))

	// Includes should be nil (all resolved)
	assert.True(t, tree.Includes == nil)

	// Verify directives are sorted by date
	open1 := tree.Directives[0].(*ast.Open)
	open2 := tree.Directives[1].(*ast.Open)
	open3 := tree.Directives[2].(*ast.Open)

	assert.Equal(t, "Assets:Savings", string(open1.Account))
	assert.Equal(t, "Assets:Checking", string(open2.Account))
	assert.Equal(t, "Income:Salary", string(open3.Account))
}

func TestLoadNestedIncludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file C
	fileC := filepath.Join(tmpDir, "c.beancount")
	err := os.WriteFile(fileC, []byte(`
2024-01-03 open Expenses:Food USD
`), 0644)
	assert.NoError(t, err)

	// Create file B that includes C
	fileB := filepath.Join(tmpDir, "b.beancount")
	err = os.WriteFile(fileB, []byte(`
include "c.beancount"

2024-01-02 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create file A that includes B
	fileA := filepath.Join(tmpDir, "a.beancount")
	err = os.WriteFile(fileA, []byte(`
include "b.beancount"

2024-01-01 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load A with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), fileA)
	assert.NoError(t, err)

	// Should have 3 directives (from A, B, and C)
	assert.Equal(t, 3, len(tree.Directives))

	// Verify all accounts are present and sorted
	accounts := make([]string, 3)
	for i, dir := range tree.Directives {
		open := dir.(*ast.Open)
		accounts[i] = string(open.Account)
	}

	assert.Equal(t, []string{"Assets:Checking", "Assets:Savings", "Expenses:Food"}, accounts)
}

func TestLoadMultipleIncludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file B
	fileB := filepath.Join(tmpDir, "b.beancount")
	err := os.WriteFile(fileB, []byte(`
2024-01-02 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create file C
	fileC := filepath.Join(tmpDir, "c.beancount")
	err = os.WriteFile(fileC, []byte(`
2024-01-03 open Income:Salary USD
`), 0644)
	assert.NoError(t, err)

	// Create file A that includes both B and C
	fileA := filepath.Join(tmpDir, "a.beancount")
	err = os.WriteFile(fileA, []byte(`
include "b.beancount"
include "c.beancount"

2024-01-01 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load A with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), fileA)
	assert.NoError(t, err)

	// Should have 3 directives
	assert.Equal(t, 3, len(tree.Directives))
}

func TestLoadCircularInclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file A that includes B
	fileA := filepath.Join(tmpDir, "a.beancount")
	err := os.WriteFile(fileA, []byte(`
include "b.beancount"

2024-01-01 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Create file B that includes A (circular)
	fileB := filepath.Join(tmpDir, "b.beancount")
	err = os.WriteFile(fileB, []byte(`
include "a.beancount"

2024-01-02 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Load A - circular includes are handled via deduplication (not an error)
	// A is loaded first, then B is loaded, then A is requested again but already visited
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), fileA)
	assert.NoError(t, err)

	// Should have 2 directives (one from A, one from B)
	// The second include of A is skipped due to deduplication
	assert.Equal(t, 2, len(tree.Directives))
}

func TestLoadRelativePaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectory
	subDir := filepath.Join(tmpDir, "accounts")
	err := os.MkdirAll(subDir, 0755)
	assert.NoError(t, err)

	// Create included file in subdirectory
	includedFile := filepath.Join(subDir, "savings.beancount")
	err = os.WriteFile(includedFile, []byte(`
2024-01-01 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create main file with relative include
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err = os.WriteFile(mainFile, []byte(`
include "accounts/savings.beancount"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Should have 2 directives
	assert.Equal(t, 2, len(tree.Directives))
}

func TestLoadAbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create included file
	includedFile := filepath.Join(tmpDir, "included.beancount")
	err := os.WriteFile(includedFile, []byte(`
2024-01-01 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create main file with absolute include path
	mainFile := filepath.Join(tmpDir, "main.beancount")
	// Convert to forward slashes for beancount syntax (works on all platforms)
	includePath := filepath.ToSlash(includedFile)
	err = os.WriteFile(mainFile, []byte(`
include "`+includePath+`"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Should have 2 directives
	assert.Equal(t, 2, len(tree.Directives))
}

func TestLoadSameFileTwice(t *testing.T) {
	tmpDir := t.TempDir()

	// Create included file
	includedFile := filepath.Join(tmpDir, "common.beancount")
	err := os.WriteFile(includedFile, []byte(`
2024-01-01 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create main file that includes the same file twice
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err = os.WriteFile(mainFile, []byte(`
include "common.beancount"
include "common.beancount"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Should have 2 directives (not 3 - deduplication should work)
	// Actually, looking at the implementation, we visit each include directive,
	// so it would try to include twice. But the second time it should be skipped
	// because it's already in visited map.
	assert.Equal(t, 2, len(tree.Directives))
}

func TestLoadNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create main file with non-existent include
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err := os.WriteFile(mainFile, []byte(`
include "does-not-exist.beancount"

2024-01-01 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load should fail
	ldr := New(WithFollowIncludes())
	_, err = ldr.Load(context.Background(), mainFile)
	assert.Error(t, err)
}

func TestLoadOptionsPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create included file with options
	includedFile := filepath.Join(tmpDir, "included.beancount")
	err := os.WriteFile(includedFile, []byte(`
option "title" "Included File"
option "operating_currency" "EUR"

2024-01-01 open Assets:Savings EUR
`), 0644)
	assert.NoError(t, err)

	// Create main file with different options
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err = os.WriteFile(mainFile, []byte(`
option "title" "Main File"
option "operating_currency" "USD"

include "included.beancount"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Main file options should take precedence
	assert.Equal(t, 2, len(tree.Options))

	// Find and verify options
	optionsMap := make(map[string]string)
	for _, opt := range tree.Options {
		optionsMap[opt.Name] = opt.Value
	}

	assert.Equal(t, "Main File", optionsMap["title"])
	assert.Equal(t, "USD", optionsMap["operating_currency"])
}

func TestLoadPluginsMerged(t *testing.T) {
	tmpDir := t.TempDir()

	// Create included file with plugin
	includedFile := filepath.Join(tmpDir, "included.beancount")
	err := os.WriteFile(includedFile, []byte(`
plugin "beancount.plugins.auto_accounts"

2024-01-01 open Assets:Savings USD
`), 0644)
	assert.NoError(t, err)

	// Create main file with different plugin
	mainFile := filepath.Join(tmpDir, "main.beancount")
	err = os.WriteFile(mainFile, []byte(`
plugin "beancount.plugins.check_commodity"

include "included.beancount"

2024-01-02 open Assets:Checking USD
`), 0644)
	assert.NoError(t, err)

	// Load with following includes
	ldr := New(WithFollowIncludes())
	tree, err := ldr.Load(context.Background(), mainFile)
	assert.NoError(t, err)

	// Both plugins should be present
	assert.Equal(t, 2, len(tree.Plugins))

	// Verify plugin names
	pluginNames := make([]string, 2)
	for i, plugin := range tree.Plugins {
		pluginNames[i] = plugin.Name
	}

	// Main file plugins come first
	assert.Equal(t, "beancount.plugins.check_commodity", pluginNames[0])
	assert.Equal(t, "beancount.plugins.auto_accounts", pluginNames[1])
}

func TestLoadBytes(t *testing.T) {
	t.Run("BasicLoadBytes", func(t *testing.T) {
		testData := []byte(`
2024-01-01 open Assets:Checking USD
2024-01-02 * "Test"
  Assets:Checking  100.00 USD
  Equity:Opening-Balances
`)

		// Test without FollowIncludes
		ldr := New()
		tree, err := ldr.LoadBytes(context.Background(), "test.beancount", testData)
		assert.NoError(t, err)
		assert.Equal(t, 2, len(tree.Directives))

		// Test with FollowIncludes (should work the same for data without includes)
		ldr = New(WithFollowIncludes())
		tree, err = ldr.LoadBytes(context.Background(), "test.beancount", testData)
		assert.NoError(t, err)
		assert.Equal(t, 2, len(tree.Directives))
	})

	t.Run("LoadBytesWithIncludesNoFollow", func(t *testing.T) {
		testData := []byte(`
include "accounts.beancount"

2024-01-01 open Assets:Checking USD
`)

		// Without FollowIncludes, includes should be preserved
		ldr := New()
		tree, err := ldr.LoadBytes(context.Background(), "main.beancount", testData)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(tree.Includes))
		assert.Equal(t, "accounts.beancount", tree.Includes[0].Filename)
	})

	t.Run("LoadBytesWithIncludesFollowStdin", func(t *testing.T) {
		testData := []byte(`
include "accounts.beancount"

2024-01-01 open Assets:Checking USD
`)

		// With FollowIncludes and stdin filename, should error
		ldr := New(WithFollowIncludes())
		_, err := ldr.LoadBytes(context.Background(), "<stdin>", testData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "include directives are not supported when reading from stdin")
	})

	t.Run("LoadBytesWithIncludesFollowFile", func(t *testing.T) {
		testData := []byte(`
include "accounts.beancount"

2024-01-01 open Assets:Checking USD
`)

		// With FollowIncludes and file filename, should error (for simplicity)
		ldr := New(WithFollowIncludes())
		_, err := ldr.LoadBytes(context.Background(), "/path/to/main.beancount", testData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "include directives found; use Load() instead of LoadBytes() to resolve includes")
	})

	t.Run("LoadBytesStdinFilename", func(t *testing.T) {
		testData := []byte(`
2024-01-01 open Assets:Checking USD
`)

		// Test with stdin filename
		ldr := New()
		tree, err := ldr.LoadBytes(context.Background(), "<stdin>", testData)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(tree.Directives))
	})

	t.Run("LoadBytesParseError", func(t *testing.T) {
		testData := []byte(`2024-01-01 invalid directive`)

		ldr := New()
		_, err := ldr.LoadBytes(context.Background(), "test.beancount", testData)
		assert.Error(t, err)
		// Should be a ParseError
		var parseErr *parser.ParseError
		assert.True(t, errors.As(err, &parseErr))
	})
}
