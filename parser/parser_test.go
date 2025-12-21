package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestMustParseBytes(t *testing.T) {
	ctx := context.Background()
	data := []byte(`2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Groceries
2024-01-15 * "Buy groceries"
  Assets:Checking     -45.60 USD
  Expenses:Groceries   45.60 USD
`)

	// Should not panic on valid input
	ast := MustParseBytes(ctx, data)
	assert.True(t, ast != nil)
	assert.Equal(t, len(ast.Directives), 3)
}

func TestMustParseString(t *testing.T) {
	ctx := context.Background()
	source := `2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Groceries
2024-01-15 * "Buy groceries"
  Assets:Checking     -45.60 USD
  Expenses:Groceries   45.60 USD
`

	// Should not panic on valid input
	ast := MustParseString(ctx, source)
	assert.True(t, ast != nil)
	assert.Equal(t, len(ast.Directives), 3)
}

func TestMustParseBytesWithFilename(t *testing.T) {
	ctx := context.Background()
	filename := "test.beancount"
	data := []byte(`2024-01-01 open Assets:Checking
2024-01-01 open Expenses:Groceries`)

	// Should not panic on valid input
	ast := MustParseBytesWithFilename(ctx, filename, data)
	assert.True(t, ast != nil)
	assert.Equal(t, len(ast.Directives), 2)
}

func TestMustParseBytesInvalidPanics(t *testing.T) {
	ctx := context.Background()
	// Invalid syntax error - unclosed string
	data := []byte(`2024-01-01 open Assets:Checking "unclosed`)

	// Verify that invalid input causes a panic
	assert.Panics(t, func() {
		MustParseBytes(ctx, data)
	})
}

func TestMustParseStringEmpty(t *testing.T) {
	ctx := context.Background()

	// Empty input should parse successfully (empty AST)
	ast := MustParseString(ctx, "")
	assert.True(t, ast != nil)
	assert.Equal(t, len(ast.Directives), 0)
}

func TestMustParseWithComments(t *testing.T) {
	ctx := context.Background()
	source := `; This is a section comment

2024-01-01 open Assets:Checking  ; Inline comment

; Another comment
2024-01-15 * "Transaction"
  Assets:Checking -100.00 USD
  Expenses:Other   100.00 USD
`

	// Should parse comments and directives correctly
	ast := MustParseString(ctx, source)
	assert.True(t, ast != nil)
	assert.True(t, len(ast.Comments) > 0)
	assert.Equal(t, len(ast.Directives), 2)
}
