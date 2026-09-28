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

	renderer := NewErrorRenderer(nil)
	output := renderer.renderWithSourceContext(pos, "test error message", ast.SplitSourceLines(sourceContent))

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

func TestDirectiveContextIsValidBeancount(t *testing.T) {
	// The context is the directive as beancount prints it, indented: an
	// open's booking method stays quoted, so the lines parse again.
	source := "2020-01-01 open Assets:Stock HOOL, USD \"FIFO\"\n"
	open := parser.MustParseString(t.Context(), source).Directives[0]

	output := directiveContext(open)
	assert.Equal(t, "   2020-01-01 open Assets:Stock                                    HOOL,USD \"FIFO\"\n", output)

	reparsed, err := parser.ParseString(t.Context(), strings.TrimPrefix(output, "   "))
	assert.NoError(t, err)
	assert.Equal(t, "FIFO", reparsed.Directives[0].(*ast.Open).BookingMethod)
}

func TestCheckShowsADroppedGroupUnderItsBookingError(t *testing.T) {
	// Like bean-check, a booking error shows the transaction as written,
	// with the postings of the group it drops from the ledger.
	output := runOurCheck(t, filepath.Join(complianceDir, "average_account.fail.beancount"))
	assert.Contains(t, output, "   2020-04-01 * \"ambiguous sell under AVERAGE\"\n"+
		"     Assets:Brokerage      -5 HOOL {}\n"+
		"     Assets:Cash       600.00 USD\n"+
		"     Income:Gains      -75.00 USD\n")
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

	renderer := NewErrorRenderer(nil)
	output := renderer.renderWithSourceContext(pos, "error", ast.SplitSourceLines(sourceContent))

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

// writeLedgerWithInclude writes main.beancount, which includes
// sub.beancount, into a temporary directory and returns both paths.
func writeLedgerWithInclude(t *testing.T, mainSource, subSource string) (mainPath, subPath string) {
	t.Helper()
	dir := t.TempDir()
	mainPath = filepath.Join(dir, "main.beancount")
	subPath = filepath.Join(dir, "sub.beancount")
	assert.NoError(t, os.WriteFile(mainPath, []byte(mainSource), 0o644))
	assert.NoError(t, os.WriteFile(subPath, []byte(subSource), 0o644))
	return mainPath, subPath
}

func TestCheckShowsAnIncludedPluginErrorInItsOwnFile(t *testing.T) {
	mainPath, subPath := writeLedgerWithInclude(t,
		"option \"title\" \"Main\"\n\ninclude \"sub.beancount\"\n\n2020-01-01 open Assets:Cash\n",
		"; sub\n\nplugin \"beancount.plugins.nope\"\n\n2020-01-01 open Assets:Bank\n")

	output := runOurCheck(t, mainPath)
	assert.Contains(t, output, subPath+":3: Error importing \"beancount.plugins.nope\"\n\n"+
		"   ; sub\n"+
		"   \n"+
		"   plugin \"beancount.plugins.nope\"\n"+
		"   ^\n"+
		"   \n"+
		"   2020-01-01 open Assets:Bank\n")
	assert.NotContains(t, output, "include \"sub.beancount\"")
	assert.NotContains(t, output, "option \"title\"")
	assert.Equal(t, []int{3}, errorLines(subPath, output))
}

func TestCheckShowsAMainFilePluginErrorInTheMainFile(t *testing.T) {
	mainPath, _ := writeLedgerWithInclude(t,
		"plugin \"beancount.plugins.nope\"\n\ninclude \"sub.beancount\"\n\n2020-01-01 open Assets:Cash\n",
		"2020-01-01 open Assets:Bank\n")

	output := runOurCheck(t, mainPath)
	assert.Contains(t, output, mainPath+":1: Error importing \"beancount.plugins.nope\"\n\n"+
		"   plugin \"beancount.plugins.nope\"\n"+
		"   ^\n"+
		"   \n"+
		"   include \"sub.beancount\"\n")
}

func TestCheckShowsAnIncludedSyntaxErrorWithoutContext(t *testing.T) {
	mainPath, subPath := writeLedgerWithInclude(t,
		"include \"sub.beancount\"\n\n2020-01-01 open Assets:Cash\n",
		"2020-01-01 open Assets:Bank\n\n2020-01-02 * \"x\"\n  Assets:Bank  1 USD USD\n  Assets:Cash\n")

	output := runOurCheck(t, mainPath)
	assert.Equal(t, subPath+":4:22: unexpected token IDENT \"USD\"\n", output)
}

func TestErrorRendererShowsNoContextForAFileItDoesNotHave(t *testing.T) {
	renderer := NewErrorRenderer(map[string][]byte{"main.beancount": []byte("plugin \"a\"\n")})
	err := positionedError{pos: ast.Position{Filename: "sub.beancount", Line: 1}, message: "sub.beancount:1: boom"}

	assert.Equal(t, "sub.beancount:1: boom", renderer.Render(err))
}

type positionedError struct {
	pos     ast.Position
	message string
}

func (e positionedError) Error() string             { return e.message }
func (e positionedError) GetPosition() ast.Position { return e.pos }
