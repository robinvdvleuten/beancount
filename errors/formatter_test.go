package errors

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestTextFormatter_FormatParseErrorWithSourceContext(t *testing.T) {
	// Create source content with a parse error
	sourceContent := `2024-01-15 * "Cafe purchase" "Lunch at cafe"
  Expenses:Food:Cafe                     -25.00 USD
  Assets:Checking

2024-01-16 * "Another transaction" "Test transaction"
  Expenses:Food:Restaurant                -30.00
  Assets:Checking`

	parseErr := &parser.ParseError{
		Pos: ast.Position{
			Filename: "test.beancount",
			Line:     6, // 1-based line number (0-based index 5)
			Column:   49,
		},
		Message: "expected currency",
		SourceRange: parser.SourceRange{
			StartOffset: 0,
			EndOffset:   len(sourceContent),
			Source:      []byte(sourceContent),
		},
	}

	formatter := NewTextFormatter(nil)
	output := formatter.Format(parseErr)

	// Verify the output contains the error message
	assert.Contains(t, output, "expected currency")

	// Verify the output contains the filename and position
	assert.Contains(t, output, "test.beancount:6:49")

	// Verify the output contains source lines
	assert.Contains(t, output, "Expenses:Food:Restaurant")

	// Verify the caret is present
	assert.Contains(t, output, "^")

	// Verify the source lines are indented with 3 spaces
	lines := strings.Split(output, "\n")
	foundIndentedLine := false
	for _, line := range lines {
		if strings.HasPrefix(line, "   ") && strings.Contains(line, "Expenses:Food:Restaurant") {
			foundIndentedLine = true
			break
		}
	}
	assert.True(t, foundIndentedLine, "Expected indented source lines")
}

func TestTextFormatter_FormatParseErrorWithoutSourceContext(t *testing.T) {
	// Create a parse error without source range (fallback behavior)
	parseErr := &parser.ParseError{
		Pos: ast.Position{
			Filename: "test.beancount",
			Line:     6,
			Column:   49,
		},
		Message: "expected currency",
		// SourceRange is empty (Source is nil)
	}

	formatter := NewTextFormatter(nil)
	output := formatter.Format(parseErr)

	// Should fall back to basic position formatting
	expected := "test.beancount:6:49: expected currency"
	assert.Equal(t, expected, output)
}

func TestTextFormatter_FormatWithSourceContext(t *testing.T) {
	sourceContent := `2024-01-15 * "Test" "Description"
  Expenses:Food                     -10.00 USD
  Assets:Cash`

	pos := ast.Position{
		Filename: "test.beancount",
		Line:     2, // Error on the posting line
		Column:   35,
	}

	formatter := NewTextFormatter(nil)
	output := formatter.formatWithSourceContext(pos, "test error message", []byte(sourceContent))

	// Verify error message is included
	assert.Contains(t, output, "test error message")

	// Verify source lines are included
	assert.Contains(t, output, "Expenses:Food")

	// Verify caret is present
	assert.Contains(t, output, "^")

	// Count lines to verify context range
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Should have: error message + blank line + source lines + caret
	assert.True(t, len(lines) >= 5, "Expected at least 5 lines in output")
}

func TestTextFormatter_FormatWithSourceContext_BoundsChecking(t *testing.T) {
	// Test with error at the beginning of file
	sourceContent := `2024-01-15 * "Test" "Description"
  Expenses:Food                     -10.00 USD`

	pos := ast.Position{
		Filename: "test.beancount",
		Line:     1, // First line
		Column:   10,
	}

	formatter := NewTextFormatter(nil)
	output := formatter.formatWithSourceContext(pos, "error", []byte(sourceContent))

	// Should not panic and should include source lines
	assert.Contains(t, output, "2024-01-15")
}

func TestParseError_SourceRangeIntegration(t *testing.T) {
	// Test that parse errors created by the parser include source range
	// This tests the integration through the parser.ParseBytesWithFilename path

	input := `2024-01-15 * "Test transaction" "Description"
  Expenses:Food                     -10.00
  Assets:Cash`

	// This should create a parse error with source range
	_, err := parser.ParseBytesWithFilename(context.Background(), "test.beancount", []byte(input))
	assert.NotZero(t, err, "Expected parse error, but parsing succeeded")

	// Check that it's a ParseError with source range
	parseErr, ok := err.(*parser.ParseError)
	assert.True(t, ok, "Expected *parser.ParseError")
	assert.NotZero(t, parseErr.SourceRange.Source, "Expected ParseError to have source range")

	// Verify the source contains the original input
	sourceText := string(parseErr.SourceRange.Source)
	assert.Contains(t, sourceText, "Expenses:Food")
}
