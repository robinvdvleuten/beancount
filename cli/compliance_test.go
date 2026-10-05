package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
)

const complianceDir = "../testdata/compliance"

// complianceFixture is a .beancount file whose expected check outcome is
// encoded in its filename: <name>.pass.beancount or <name>.fail.beancount.
// A gap_ prefix marks a fixture our implementation does not yet satisfy;
// see testdata/compliance/KNOWN_GAPS.md. Gap fixtures are skipped by the
// in-process leg but still verified against the official tools, so the
// expectations stay honest and closing a gap only requires renaming the file.
type complianceFixture struct {
	name     string
	path     string
	wantPass bool
	knownGap bool
}

func loadComplianceFixtures(t *testing.T) []complianceFixture {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(complianceDir, "*.beancount"))
	assert.NoError(t, err)

	var fixtures []complianceFixture
	for _, path := range paths {
		base := strings.TrimSuffix(filepath.Base(path), ".beancount")
		var wantPass bool
		switch {
		case strings.HasSuffix(base, ".pass"):
			wantPass = true
		case strings.HasSuffix(base, ".fail"):
			wantPass = false
		default:
			continue // Helper file (e.g. an include target), not a fixture.
		}
		name := strings.TrimSuffix(strings.TrimSuffix(base, ".pass"), ".fail")
		fixtures = append(fixtures, complianceFixture{
			name:     name,
			path:     path,
			wantPass: wantPass,
			knownGap: strings.HasPrefix(name, "gap_"),
		})
	}

	assert.True(t, len(fixtures) > 0, "no fixtures found in %s", complianceDir)
	return fixtures
}

// TestComplianceFixtures checks every fixture through the same load+process
// pipeline the check command uses and asserts the outcome encoded in the
// fixture's filename.
func TestComplianceFixtures(t *testing.T) {
	for _, fixture := range loadComplianceFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			if fixture.knownGap {
				t.Skipf("known gap, see %s", filepath.Join(complianceDir, "KNOWN_GAPS.md"))
			}

			result, err := ledgerload.Load(context.Background(), loader.Source{Path: fixture.path})
			if err == nil {
				// Fatal load diagnostics fail a check like validation
				// diagnostics do.
				err = errors.Join(diagnostic.Errors(result.Diagnostics())...)
			}

			if fixture.wantPass {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// hasOfficialTool reports whether tool is on PATH, and fails the test when it
// is but its --version does not report the given major version of beancount.
func hasOfficialTool(t *testing.T, tool string, major int) bool {
	t.Helper()

	if _, err := exec.LookPath(tool); err != nil {
		return false
	}
	version, err := exec.Command(tool, "--version").Output()
	assert.NoError(t, err)
	if !isBeancountMajor(string(version), major) {
		t.Fatalf("%s reports %q, but this suite targets beancount %d.x",
			tool, strings.TrimSpace(string(version)), major)
	}
	return true
}

// requireOfficialTool skips the test when tool is not on PATH.
func requireOfficialTool(t *testing.T, tool string, major int) {
	t.Helper()

	if !hasOfficialTool(t, tool, major) {
		t.Skipf("%s not found in PATH; install beancount %d.x to run this suite", tool, major)
	}
}

// isBeancountMajor reports whether version, a tool's --version output, names
// the given major version of beancount ("Beancount 3.2.3").
func isBeancountMajor(version string, major int) bool {
	return strings.HasPrefix(version, fmt.Sprintf("Beancount %d.", major))
}

func TestIsBeancountMajor(t *testing.T) {
	assert.True(t, isBeancountMajor("Beancount 3.2.3\n", 3))
	assert.True(t, isBeancountMajor("Beancount 2.3.6\n", 2))
	assert.False(t, isBeancountMajor("Beancount 3.2.3\n", 2))
	assert.False(t, isBeancountMajor("Beancount 2.3.6\n", 3))
	assert.False(t, isBeancountMajor("beanquery 0.2.0, beancount 3.2.3\n", 2))
}

// assertGapsNameFixtures fails the test for every entry of gaps that names
// none of the fixtures.
func assertGapsNameFixtures(t *testing.T, gaps map[string]string, fixtures []string) {
	t.Helper()

	for name := range gaps {
		assert.True(t, slices.Contains(fixtures, name), "gap entry %q names no fixture; remove it", name)
	}
}

// TestOfficialBeancountDifferential validates the fixture expectations
// themselves against the official bean-check, so the manifest cannot drift
// from real beancount behavior, and compares the lines errors are reported
// on, in check's text and --json output. Most messages are worded apart, so
// --json is held to bean-check --json's filenames and lines, not its bytes.
// Runs whenever bean-check 3.x is installed.
func TestOfficialBeancountDifferential(t *testing.T) {
	requireOfficialTool(t, "bean-check", 3)

	var names []string
	for _, fixture := range loadComplianceFixtures(t) {
		names = append(names, fixture.name)
		t.Run(fixture.name, func(t *testing.T) {
			abs, err := filepath.Abs(fixture.path)
			assert.NoError(t, err)
			out, err := exec.Command("bean-check", "--json", abs).Output()
			officialOK := err == nil
			var exitErr *exec.ExitError
			if err != nil && !errors.As(err, &exitErr) {
				t.Fatalf("run bean-check: %v", err)
			}
			assert.Equal(t, fixture.wantPass, officialOK)

			if fixture.knownGap {
				return
			}
			oursJSON := runOurCheckJSON(t, abs)
			if fixture.wantPass {
				assert.Equal(t, string(out), oursJSON)
				return
			}
			ours := runOurCheck(t, abs)
			officialLines, ourLines := jsonErrorLines(t, abs, out), errorLines(abs, ours)
			if reason, ok := lineGaps[fixture.name]; ok {
				assert.NotEqual(t, officialLines, ourLines, "the lines agree; remove the lineGaps entry (%s)", reason)
				return
			}
			assert.Equal(t, officialLines, ourLines, "bean-check:\n%s\nours:\n%s", out, ours)
			assert.Equal(t, officialLines, jsonErrorLines(t, abs, []byte(oursJSON)), "bean-check:\n%s\nours:\n%s", out, oursJSON)
		})
	}
	assertGapsNameFixtures(t, lineGaps, names)
}

// lineGaps lists .fail fixtures whose error lines differ from bean-check's,
// with the reason. An entry whose lines agree, or that names no fixture,
// fails the differential suite; one naming a .pass or gap_ fixture is not
// checked.
var lineGaps = map[string]string{
	// Deliberate: we read what beancount's lexer rejects (KNOWN_GAPS.md).
	"crlf_one_letter_currency": "#704: a one-letter currency before \\r is an invalid token in beancount only",
	// Deliberate: our line is more precise (KNOWN_GAPS.md).
	"body_tags_after_posting":       "we blame the tag's line, beancount the transaction's",
	"applied_tag_after_posting":     "we blame the link's line, beancount the transaction's",
	"documents_missing_root":        "we blame the option's line, beancount line 0",
	"duplicate_include":             "we blame the include's line, beancount <load>:0",
	"include_glob_no_match":         "we blame the include's line, beancount <load>:0",
	"include_missing_file":          "we blame the include's line, beancount <load>:0",
	"plugin_auto_accounts_config":   "we blame the plugin's line, beancount <load>:0",
	"plugin_empty_config":           "we blame the plugin's line, beancount <load>:0",
	"plugin_implicit_prices_config": "we blame the plugin's line, beancount <load>:0",
	"plugin_removed_in_v3":          "we blame the plugin's line, beancount <load>:0",
	"plugin_unknown_module":         "we blame the plugin's line, beancount <load>:0",
	"unbalanced_pushmeta":           "we blame the pushmeta's line, beancount line 0",
	"unbalanced_pushtag":            "we blame the pushtag's line, beancount line 0",
}

// runOurCheck runs the check command on path and returns its stderr.
func runOurCheck(t *testing.T, path string) string {
	t.Helper()

	var cmds Commands
	var stdout, stderr bytes.Buffer
	parser, err := kong.New(&cmds, kong.Writers(&stdout, &stderr), kong.Bind(&cmds.Globals))
	assert.NoError(t, err)
	ctx, err := parser.Parse([]string{"check", path})
	assert.NoError(t, err)
	_ = ctx.Run()
	return stderr.String()
}

// runOurCheckJSON runs check --json on path and returns its stdout.
func runOurCheckJSON(t *testing.T, path string) string {
	t.Helper()

	var cmds Commands
	var stdout, stderr bytes.Buffer
	parser, err := kong.New(&cmds, kong.Writers(&stdout, &stderr), kong.Bind(&cmds.Globals))
	assert.NoError(t, err)
	ctx, err := parser.Parse([]string{"check", "--json", path})
	assert.NoError(t, err)
	_ = ctx.Run()
	return stdout.String()
}

// jsonErrorLines returns the sorted, distinct lines of the errors in
// check --json output that are reported in path.
func jsonErrorLines(t *testing.T, path string, output []byte) []int {
	t.Helper()

	var result struct {
		Errors []struct {
			Filename *string `json:"filename"`
			Lineno   *int    `json:"lineno"`
		} `json:"errors"`
	}
	assert.NoError(t, json.Unmarshal(output, &result), "%s", output)
	var lines []int
	for _, e := range result.Errors {
		if e.Filename != nil && *e.Filename == path && e.Lineno != nil && !slices.Contains(lines, *e.Lineno) {
			lines = append(lines, *e.Lineno)
		}
	}
	slices.Sort(lines)
	return lines
}

// errorLines returns the sorted, distinct line numbers of the output lines
// that start with "<path>:<line>:", ignoring any column that follows.
func errorLines(path, output string) []int {
	var lines []int
	for line := range strings.Lines(output) {
		rest, ok := strings.CutPrefix(line, path+":")
		if !ok {
			continue
		}
		number, _, ok := strings.Cut(rest, ":")
		if n, err := strconv.Atoi(number); ok && err == nil && !slices.Contains(lines, n) {
			lines = append(lines, n)
		}
	}
	slices.Sort(lines)
	return lines
}

// formatGaps lists format fixtures whose output differs from bean-format's,
// with the reason. An entry whose output agrees, or that names no fixture,
// fails the format parity suite.
var formatGaps = map[string]string{
	"line_in_string": "#664: bean-format realigns a line inside a string spanning lines; we copy the string",
	// Deliberate (#663): beancount's lexer reads a lone \r as whitespace,
	// so we keep it inside its line; bean-format reads the file with
	// Python's universal newlines and writes it as a line break.
	"comment_after_string_spanning_cr": "bean-format turns a lone \\r into a line break; we keep it (#663)",
}

// TestOfficialFormatParity compares our formatter's output byte-for-byte
// with bean-format on the fixtures under testdata/compliance/format.
// Runs whenever bean-format 3.x is installed.
func TestOfficialFormatParity(t *testing.T) {
	requireOfficialTool(t, "bean-format", 3)

	paths, err := filepath.Glob(filepath.Join(complianceDir, "format", "*.beancount"))
	assert.NoError(t, err)
	assert.True(t, len(paths) > 0, "no format fixtures found")

	var names []string
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".beancount")
		names = append(names, name)
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(path)
			assert.NoError(t, err)

			ctx := context.Background()
			tree, err := parser.ParseBytesWithFilename(ctx, path, source)
			assertParsesWhole(t, err)
			// The format command loads through the loader, which applies
			// pushed tags and metadata; none may reach the output.
			assert.Zero(t, ast.ApplyPushPopDirectives(tree))
			var ours bytes.Buffer
			assert.NoError(t, formatter.New().Format(ctx, tree, source, &ours))

			official, err := exec.Command("bean-format", path).Output()
			assert.NoError(t, err)

			if reason, ok := formatGaps[name]; ok {
				assert.NotEqual(t, string(official), ours.String(), "the output agrees; remove the formatGaps entry (%s)", reason)
				return
			}
			assert.Equal(t, string(official), ours.String())
		})
	}
	assertGapsNameFixtures(t, formatGaps, names)
}

// TestFormatFixturesReachFixedPoint formats every format fixture three
// times: the third pass must leave the second one's output as it is. Like
// bean-format, a first pass can still move numbers, since it measures a
// posting's prefix as the source indents it.
func TestFormatFixturesReachFixedPoint(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(complianceDir, "format", "*.beancount"))
	assert.NoError(t, err)
	assert.True(t, len(paths) > 0, "no format fixtures found")

	format := func(t *testing.T, source []byte) string {
		t.Helper()
		ctx := context.Background()
		tree, err := parser.ParseBytes(ctx, source)
		assertParsesWhole(t, err)
		var out bytes.Buffer
		assert.NoError(t, formatter.New().Format(ctx, tree, source, &out))
		return out.String()
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".beancount"), func(t *testing.T) {
			source, err := os.ReadFile(path)
			assert.NoError(t, err)
			twice := format(t, []byte(format(t, source)))
			assert.Equal(t, twice, format(t, []byte(twice)))
		})
	}
}

// assertParsesWhole fails unless every directive of a source parsed, as the
// format command requires: an error that keeps its directive, such as a
// deprecated pipe, is one the format command leaves out too.
func assertParsesWhole(t *testing.T, err error) {
	t.Helper()
	var syntaxErrs parser.ParseErrors
	if errors.As(err, &syntaxErrs) {
		assert.Zero(t, syntaxErrs.FirstDropping())
		return
	}
	assert.NoError(t, err)
}

// TestNoFollowOnErrors checks the applied_ fixtures, whose one erroneous
// directive is still applied like beancount applies it: the later directives
// that depend on it must pass, so both implementations report exactly one
// error, a syntax error that keeps its directive or a validation error.
// Exit codes alone cannot show a follow-on error.
func TestNoFollowOnErrors(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(complianceDir, "applied_*.fail.beancount"))
	assert.NoError(t, err)
	assert.True(t, len(paths) > 0, "no applied_ fixtures found in %s", complianceDir)

	official := hasOfficialTool(t, "bean-check", 3)
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".fail.beancount"), func(t *testing.T) {
			result, err := ledgerload.Load(context.Background(), loader.Source{Path: path})
			assert.NoError(t, err)
			loadErrors, validationErrors := diagnostic.Errors(result.LoadDiagnostics), result.Ledger.Diagnostics()
			assert.Equal(t, 1, len(loadErrors)+len(validationErrors), "%v %v", loadErrors, validationErrors)

			if !official {
				return
			}
			// bean-check prints each error as "<absolute path>:<line>: <message>".
			abs, err := filepath.Abs(path)
			assert.NoError(t, err)
			out, _ := exec.Command("bean-check", abs).CombinedOutput()
			reported := 0
			for line := range strings.Lines(string(out)) {
				if strings.HasPrefix(line, abs+":") {
					reported++
				}
			}
			assert.Equal(t, 1, reported, "bean-check output:\n%s", out)
		})
	}
}
