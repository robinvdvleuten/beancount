package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
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
		Msg: "expected currency",
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
		Msg: "expected currency",
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

func TestCheckShowsBookedPostingsUnderAnError(t *testing.T) {
	// Like bean-check, an error's transaction shows its postings as booked:
	// a total cost per unit and dated, a reduction one posting per lot.
	path, err := filepath.Abs(filepath.Join(complianceDir, "booked_context.fail.beancount"))
	assert.NoError(t, err)
	want := map[int]string{
		7: "   2020-01-07 * \"x\"\n" +
			"     Assets:A       2 HOOL {5.50 USD, 2020-01-07} @ 6.00 USD\n" +
			"     Equity:E  -11.00 USD\n",
		19: "   2020-01-10 * \"sell\"\n" +
			"     Assets:B  -2 HOOL {10 USD, 2020-01-08}\n" +
			"     Assets:B  -3 HOOL {12 USD, 2020-01-09}\n" +
			"     Assets:C  56 USD\n",
	}
	assert.Equal(t, want, errorContexts(path, runOurCheck(t, path)))

	if !hasOfficialTool(t, "bean-check", 3) {
		return
	}
	out, _ := exec.Command("bean-check", path).CombinedOutput()
	assert.Equal(t, want, errorContexts(path, string(out)))
}

// errorContexts returns the context lines printed under each error in
// check's output that is reported in path, keyed by the error's line.
func errorContexts(path, output string) map[int]string {
	contexts := map[int]string{}
	current := 0
	for line := range strings.Lines(output) {
		if rest, ok := strings.CutPrefix(line, path+":"); ok {
			number, _, _ := strings.Cut(rest, ":")
			current, _ = strconv.Atoi(number)
			continue
		}
		if current != 0 && strings.HasPrefix(line, "   ") {
			contexts[current] += line
		}
	}
	return contexts
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
	tree := parser.MustParseString(t.Context(), "2020-01-01 open Assets:Cash\n2020-01-01 open Equity:Opening\n2020-01-02 pad Assets:Cash Equity:Opening\n")
	l := ledger.New()
	_, err := l.Process(t.Context(), tree)
	assert.NoError(t, err)
	diagnostics := l.Diagnostics()
	assert.Equal(t, 1, len(diagnostics))

	output := NewErrorRenderer(nil).Render(diagnostics[0])

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

func TestCheckIgnoresAnIncludedPluginDirective(t *testing.T) {
	// Like bean-check, an included file's plugin never runs, so an unknown
	// one reports nothing.
	mainPath, _ := writeLedgerWithInclude(t,
		"include \"sub.beancount\"\n\n2020-01-01 open Assets:Cash\n",
		"plugin \"beancount.plugins.nope\"\n\n2020-01-01 open Assets:Bank\n")

	assert.Equal(t, "", runOurCheck(t, mainPath))
}

func TestCheckShowsAMainFilePluginErrorInTheMainFile(t *testing.T) {
	mainPath, _ := writeLedgerWithInclude(t,
		"plugin \"beancount.plugins.nope\"\n\ninclude \"sub.beancount\"\n\n2020-01-01 open Assets:Cash\n",
		"2020-01-01 open Assets:Bank\n")

	// Like an error beancount raises with no entry, it is printed alone.
	output := runOurCheck(t, mainPath)
	assert.Equal(t, mainPath+":1: Error importing \"beancount.plugins.nope\"\n\n"+
		"✗ 1 validation error(s) found\n", output)
}

func TestCheckShowsAnIncludedSyntaxErrorWithoutContext(t *testing.T) {
	mainPath, subPath := writeLedgerWithInclude(t,
		"include \"sub.beancount\"\n\n2020-01-01 open Assets:Cash\n",
		"2020-01-01 open Assets:Bank\n\n2020-01-02 * \"x\"\n  Assets:Bank  1 USD USD\n  Assets:Cash\n")

	output := runOurCheck(t, mainPath)
	assert.Equal(t, subPath+":4:22: unexpected token IDENT \"USD\"\n", output)
}

func TestErrorRendererShowsAPositionedErrorInItsOwnFile(t *testing.T) {
	renderer := NewErrorRenderer(map[string][]byte{
		"main.beancount": []byte("option \"title\" \"Main\"\n\ninclude \"sub.beancount\"\n"),
		"sub.beancount":  []byte("; sub\n\nplugin \"a\"\n\n2020-01-01 open Assets:Bank\n"),
	})
	err := positionedError{pos: ast.Position{Filename: "sub.beancount", Line: 3}, message: "sub.beancount:3: boom"}

	output := renderer.Render(err)
	assert.Contains(t, output, "sub.beancount:3: boom\n\n"+
		"   ; sub\n"+
		"   \n"+
		"   plugin \"a\"\n"+
		"   \n"+
		"   2020-01-01 open Assets:Bank\n")
	assert.NotContains(t, output, "option \"title\"")
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

// TestPositionedErrors checks the shape of every positioned error outside
// package ledger (whose kinds TestErrorKinds covers): its kind, and that its
// text is its Error line, ": " and its message.
func TestPositionedErrors(t *testing.T) {
	tree, err := parser.ParseBytesWithFilename(context.Background(), "main.beancount", []byte(
		"option \"inferred_tolerance_multiplier\" \"x\"\ninclude \"sub*.beancount\"\n"))
	assert.NoError(t, err)
	option, include := tree.Options[0], tree.Includes[0]
	pos := ast.Position{Filename: "main.beancount", Line: 6, Column: 49}

	for _, tt := range []struct {
		err       error
		kind      string
		errorLine string
		message   string
		severity  diagnostic.Severity
	}{
		{&config.RenamedOptionError{Option: option}, "RenamedOptionError", "main.beancount:1", "Renamed to 'tolerance_multiplier'.", diagnostic.SeverityError},
		{&config.DeprecatedOptionError{Option: option, Msg: "going away"}, "DeprecatedOptionError", "main.beancount:1", "going away", diagnostic.SeverityError},
		{&config.InvalidOptionError{Option: option}, "InvalidOptionError", "main.beancount:1", "Invalid option: 'inferred_tolerance_multiplier'", diagnostic.SeverityError},
		{&config.InvalidOptionError{Option: option, Reserved: true}, "InvalidOptionError", "main.beancount:1", "Option 'inferred_tolerance_multiplier' may not be set", diagnostic.SeverityError},
		{&config.OptionValueError{Option: option, Err: errors.New("boom")}, "OptionValueError", "main.beancount:1", "Error for option 'tolerance_multiplier': boom", diagnostic.SeverityError},
		{&loader.IncludedOptionWarning{Option: option}, "IncludedOptionWarning", "main.beancount:1", `option "inferred_tolerance_multiplier" from included file is ignored`, diagnostic.SeverityWarning},
		{&loader.IncludeGlobNoMatchError{Include: include}, "IncludeGlobNoMatchError", "main.beancount:2", `File glob "sub*.beancount" does not match any files`, diagnostic.SeverityError},
		{&loader.DuplicateIncludeError{Include: include, Path: "sub.beancount"}, "DuplicateIncludeError", "main.beancount:2", `Duplicate filename parsed: "sub.beancount"`, diagnostic.SeverityError},
		{&loader.DocumentRootError{Option: option, Dir: "/docs"}, "DocumentRootError", "main.beancount:1", "Document root '/docs' does not exist", diagnostic.SeverityError},
		// A syntax error's Error line has the column.
		{&parser.ParseError{Pos: pos, Msg: "expected currency"}, "ParseError", "main.beancount:6:49", "expected currency", diagnostic.SeverityError},
		{&ast.PushPopError{Pos: pos, Msg: "Unbalanced pushed tag: 'trip'"}, "PushPopError", "main.beancount:6", "Unbalanced pushed tag: 'trip'", diagnostic.SeverityError},
		// bean-check prints a missing file without an Error line.
		{newMissingFileError("/x.beancount"), "MissingFileError", "", `File "/x.beancount" does not exist`, diagnostic.SeverityError},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			shaped, ok := tt.err.(diagnostic.Positioned)
			assert.True(t, ok, "%T lacks the shape", tt.err)
			assert.Equal(t, tt.kind, shaped.Kind())
			assert.Equal(t, tt.message, shaped.Message())
			assert.Equal(t, tt.severity, diagnostic.SeverityOf(tt.err))

			want := tt.message
			if tt.errorLine != "" {
				want = tt.errorLine + ": " + tt.message
				assert.Equal(t, strings.SplitN(tt.errorLine, ":", 2)[0], shaped.GetPosition().Filename)
			}
			assert.Equal(t, want, tt.err.Error())
		})
	}
}

// TestCheckDirectoryReportsTheLoadError pins that a load that cannot go
// on is reported even when its source cannot be read again for context.
func TestCheckDirectoryReportsTheLoadError(t *testing.T) {
	dir := t.TempDir()
	_, stderr, err := runCommand(t, "check", dir)
	var cmdErr *CommandError
	assert.True(t, errors.As(err, &cmdErr), "got %v", err)
	assert.Contains(t, stderr, "failed to read "+dir)
	assert.Contains(t, stderr, "is a directory")
	assert.NotContains(t, stderr, "error context")
}

// TestCheckShowsAnEntrylessErrorAlone pins check's layout against
// bean-check's for an error beancount raises with no entry ("Cannot infer
// price ...", from interpolate_group): the error line alone, while an error
// about the transaction shows it under the line.
func TestCheckShowsAnEntrylessErrorAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.beancount")
	assert.NoError(t, os.WriteFile(path, []byte(`option "booking_method" "HIFO"
2020-01-01 open Assets:Cash
2020-01-01 open Assets:Stock
2020-01-01 open Equity:Open

2020-01-03 * "Lot b"
  Assets:Stock  10 HOOL {20 USD, "lotb"}
  Equity:Open

2020-02-01 *
  Assets:Cash   0 USD
  Assets:Stock  -3 HOOL {} @ USD
`), 0o644))

	output := runOurCheck(t, path)
	assert.Equal(t, path+":12: Cannot infer price for postings with units held at cost\n\n"+
		path+":10: Transaction does not balance: (-60 USD)\n\n"+
		"   2020-02-01 * \n"+
		"     Assets:Cash    0 USD\n"+
		"     Assets:Stock  -3 HOOL {20 USD, 2020-01-03, \"lotb\"} @  USD\n\n\n"+
		"✗ 2 validation error(s) found\n", output)
}
