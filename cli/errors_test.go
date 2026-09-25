package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestErrorRenderer_RenderParseErrorWithSourceContext(t *testing.T) {
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

	renderer := NewErrorRenderer(nil)
	output := renderer.Render(parseErr)

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

func TestErrorRenderer_RenderParseErrorWithoutSourceContext(t *testing.T) {
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

	renderer := NewErrorRenderer(nil)
	output := renderer.Render(parseErr)

	// Should fall back to basic position formatting
	expected := "test.beancount:6:49: expected currency"
	assert.Equal(t, expected, output)
}

func TestErrorRenderer_RenderWithSourceContext(t *testing.T) {
	sourceContent := `2024-01-15 * "Test" "Description"
  Expenses:Food                     -10.00 USD
  Assets:Cash`

	pos := ast.Position{
		Filename: "test.beancount",
		Line:     2, // Error on the posting line
		Column:   35,
	}

	renderer := NewErrorRenderer([]byte(sourceContent))
	output := renderer.renderWithSourceContext(pos, "test error message", []byte(sourceContent))

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

func TestCaretPaddingUsesDisplayWidth(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		byteColumn int
		want       int
	}{
		{"ASCII", "abc", 3, 2},
		{"UnicodeNarrow", "éUSD", 3, 1},
		{"UnicodeWide", "界USD", 4, 2},
		{"TabStop", "A\tB", 3, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, caretPadding(tt.line, tt.byteColumn))
		})
	}
}

func TestErrorRenderer_RenderWithContext_AllDirectiveTypes(t *testing.T) {
	date, _ := ast.NewDate("2024-01-15")

	tests := []struct {
		name      string
		directive ast.Directive
		contains  string
	}{
		{
			name:      "commodity",
			directive: ast.NewCommodity(date, "USD"),
			contains:  "commodity USD",
		},
		{
			name:      "price",
			directive: ast.NewPrice(date, "HOOL", ast.NewAmount("500.00", "USD")),
			contains:  "price HOOL",
		},
		{
			name:      "event",
			directive: ast.NewEvent(date, "location", "New York"),
			contains:  "event",
		},
		{
			name:      "custom",
			directive: ast.NewCustom(date, "budget", nil),
			contains:  "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := directiveContext(tt.directive)
			assert.Contains(t, output, tt.contains, "directive context should be rendered")
		})
	}
}

func TestErrorRenderer_RenderWithSourceContext_BoundsChecking(t *testing.T) {
	// Test with error at the beginning of file
	sourceContent := `2024-01-15 * "Test" "Description"
  Expenses:Food                     -10.00 USD`

	pos := ast.Position{
		Filename: "test.beancount",
		Line:     1, // First line
		Column:   10,
	}

	renderer := NewErrorRenderer([]byte(sourceContent))
	output := renderer.renderWithSourceContext(pos, "error", []byte(sourceContent))

	// Should not panic and should include source lines
	assert.Contains(t, output, "2024-01-15")
}

func TestErrorRenderer_UnusedPadShowsPadOnce(t *testing.T) {
	tree := parser.MustParseString(t.Context(), "2020-01-02 pad Assets:Cash Equity:Opening\n")
	pad := tree.Directives[0].(*ast.Pad)

	output := NewErrorRenderer(nil).Render(ledger.NewUnusedPadWarning(pad))

	assert.Equal(t, 1, strings.Count(output, "pad Assets:Cash Equity:Opening"))
	for _, line := range strings.Split(output, "\n") {
		assert.Equal(t, strings.TrimRight(line, " "), line, "no trailing padding")
	}
}

func TestCheckShowsTheDirectiveUnderALedgerError(t *testing.T) {
	// Like bean-check, the transaction is printed under the error, indented
	// so that no context line starts with path:line:.
	path := filepath.Join(t.TempDir(), "unknown.beancount")
	source := "2020-01-01 open Assets:A\n\n2020-01-02 * \"x\"\n  Assets:A  1 USD\n  Assets:B\n"
	assert.NoError(t, os.WriteFile(path, []byte(source), 0o644))

	output := runOurCheck(t, path)
	assert.Contains(t, output, path+":3: Invalid reference to unknown account 'Assets:B'")
	assert.Contains(t, output, "   2020-01-02 * \"x\"\n")
	assert.Contains(t, output, "     Assets:B  -1 USD\n")
	assert.Equal(t, []int{3}, errorLines(path, output))
}
