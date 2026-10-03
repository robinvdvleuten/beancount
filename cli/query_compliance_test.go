package cli

import (
	"context"
	stdErrors "errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/query"
)

const queryComplianceDir = "../testdata/compliance/query"

// queryFixture is a .bql file run against the shared ledger.beancount (or a
// same-named .beancount override). A numberify_ prefix runs the query with
// -m.
type queryFixture struct {
	name      string
	query     string
	ledger    string
	numberify bool
}

func loadQueryFixtures(t *testing.T) []queryFixture {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(queryComplianceDir, "*.bql"))
	assert.NoError(t, err)
	assert.True(t, len(paths) > 0, "no query fixtures found in %s", queryComplianceDir)

	var fixtures []queryFixture
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".bql")
		source, err := os.ReadFile(path)
		assert.NoError(t, err)

		ledgerPath := filepath.Join(queryComplianceDir, name+".beancount")
		if _, err := os.Stat(ledgerPath); err != nil {
			ledgerPath = filepath.Join(queryComplianceDir, "ledger.beancount")
		}

		fixtures = append(fixtures, queryFixture{
			name:      name,
			query:     strings.TrimSpace(string(source)),
			ledger:    ledgerPath,
			numberify: strings.HasPrefix(name, "numberify_"),
		})
	}
	return fixtures
}

// queryOutput is what a query run prints and how it exits.
type queryOutput struct {
	stdout   string
	stderr   string
	exitCode int
}

// runOurQuery executes a fixture through the same load, process, run and
// error-reporting steps as the query command and returns what the command
// prints for it and how it exits.
func runOurQuery(t *testing.T, fixture queryFixture, format string, numberify bool) queryOutput {
	t.Helper()

	ctx := context.Background()
	// Like the query command, load the ledger by its absolute path, which
	// the filename and meta columns name.
	path, err := filepath.Abs(fixture.ledger)
	assert.NoError(t, err)
	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery())
	result, err := ldr.Load(ctx, path)
	assert.NoError(t, err)

	l := ledger.New()
	if err := l.Process(ctx, result.AST); err != nil {
		var validationErrors *ledger.ValidationErrors
		assert.True(t, stdErrors.As(err, &validationErrors), "unexpected process error: %v", err)
	}

	cfg, err := config.FromAST(result.AST)
	assert.NoError(t, err)

	var stdout, stderr strings.Builder
	qctx := &query.Context{Ledger: l, Config: cfg, AST: result.AST}
	err = reportQueryError(&stderr, query.Run(ctx, qctx, fixture.query, query.Format(format), numberify, &stdout))
	output := queryOutput{stdout: stdout.String(), stderr: stderr.String()}
	var cmdErr *CommandError
	if stdErrors.As(err, &cmdErr) {
		output.exitCode = cmdErr.ExitCode()
	} else {
		assert.NoError(t, err)
	}
	return output
}

// TestQueryFixtures runs every fixture through our engine in both formats,
// so the suite exercises the fixtures even without bean-query installed.
// Error fixtures (err_ prefix) must fail as a statement that does not
// parse or compile.
func TestQueryFixtures(t *testing.T) {
	for _, fixture := range loadQueryFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			for _, format := range []string{"text", "csv"} {
				output := runOurQuery(t, fixture, format, fixture.numberify)
				assert.Equal(t, strings.HasPrefix(fixture.name, "err_"), isQueryError(output), "output: %+v", output)
			}
		})
	}
}

// isQueryError reports whether output is how the query command reports a
// statement that does not parse or compile: nothing on stdout, beanquery's
// "error: " text on stderr, and exit status 1.
func isQueryError(output queryOutput) bool {
	return output.stdout == "" && strings.HasPrefix(output.stderr, "error: ") && output.exitCode == 1
}

// queryGaps lists query fixtures whose output differs from beanquery's,
// with the issue that closes the gap. An entry
// whose output agrees in both formats, or that names no fixture, fails the
// query parity suite.
var queryGaps = map[string]string{
	"from_open_on_close":          "#582, a deliberate deviation: beanquery's compiler crashes comparing OPEN's date with a dateless CLOSE",
	"negative_zero":               "#408, a deliberate deviation: booking gives a zero residual no sign",
	"err_integer_overflow":        "#589, a deliberate deviation: Python's integers do not overflow, and ours fail the statement where they would",
	"err_regex_invalid":           "#589, a deliberate deviation: RE2 words an invalid pattern apart from Python's re",
	"err_regex_invalid_grep":      "#589, a deliberate deviation: RE2 words an invalid pattern apart from Python's re",
	"err_regex_lookahead":         "#589, a deliberate deviation: RE2 has no lookarounds, so the pattern is invalid here and valid in Python's re",
	"err_from_table_accounts":     "#639, the accounts Table is not built yet",
	"err_from_table_balances":     "#638, the balances Table is not built yet",
	"err_from_table_commodities":  "#635, the commodities Table is not built yet",
	"err_from_table_documents":    "#637, the documents Table is not built yet",
	"err_from_table_events":       "#634, the events Table is not built yet",
	"err_from_table_notes":        "#636, the notes Table is not built yet",
	"err_from_table_prices":       "#633, the prices Table is not built yet",
	"err_from_table_transactions": "#632, the transactions Table is not built yet",
}

// TestOfficialQueryParity compares our output byte-for-byte with bean-query
// in both text and csv formats: stdout and exit status always, and for a
// statement beanquery fails, stderr with the text its interactive shell
// prints (beanqueryShellError). Runs whenever beanquery 0.2 is installed.
func TestOfficialQueryParity(t *testing.T) {
	requireBeanquery(t)
	python := beanqueryPython(t)

	fixtures := loadQueryFixtures(t)
	names := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		names[i] = fixture.name
		t.Run(fixture.name, func(t *testing.T) {
			_, gap := queryGaps[fixture.name]
			agrees := true
			shellError := ""
			for _, format := range []string{"text", "csv"} {
				args := []string{"-f", format}
				if fixture.numberify {
					args = append(args, "-m")
				}
				args = append(args, fixture.ledger, fixture.query)

				var official queryOutput
				stdout, err := exec.Command("bean-query", args...).Output()
				official.stdout = string(stdout)
				if err != nil {
					var exitErr *exec.ExitError
					assert.True(t, stdErrors.As(err, &exitErr), "bean-query: %v", err)
					official.exitCode = exitErr.ExitCode()
					// One-shot bean-query prints a traceback for a failed
					// statement, so its shell's text stands in for it.
					if shellError == "" {
						shellError = runBeanqueryShellError(t, python, fixture)
					}
					official.stderr = shellError
				}
				ours := runOurQuery(t, fixture, format, fixture.numberify)

				if gap {
					agrees = agrees && official == ours
					continue
				}
				assert.Equal(t, official, ours, "format %s, query: %s", format, fixture.query)
			}
			if reason, ok := queryGaps[fixture.name]; ok {
				assert.False(t, agrees, "the output agrees; remove the queryGaps entry (%s)", reason)
			}
		})
	}
	assertGapsNameFixtures(t, queryGaps, names)
}

// beanqueryShellError prints what beanquery's interactive shell prints on
// stderr for a statement it fails (shell.py): "error: " and
// render_exception's text. For an error raised without a node, where the
// shell prints a traceback, it prints the message alone, as we do
// (KNOWN_GAPS.md), so only that line is compared.
// It exits with shellErrorExit, so a Python failure of its own, such as an
// ImportError, is never taken for the shell's text.
const beanqueryShellError = `
import sys
import beanquery
from beanquery.shell import BQLShell, render_exception

shell = BQLShell('beancount:' + sys.argv[1], sys.stdout, errors=False)
try:
    shell.onecmd(sys.argv[2])
except Exception as exc:
    if isinstance(exc, (beanquery.CompilationError, beanquery.ParseError)) and exc.parseinfo:
        message = render_exception(exc)
    else:
        message = str(exc)
    print('error: ' + message, file=sys.stderr)
    sys.exit(3)  # shellErrorExit
`

// shellErrorExit is the status beanqueryShellError exits with after
// printing the shell's text.
const shellErrorExit = 3

// beanqueryPython returns the Python interpreter bean-query runs on: the
// one its shebang names, which pip writes as an absolute path, or else the
// python next to it in its virtual environment (pipx's included). It fails
// the test when that interpreter cannot import beanquery's shell.
func beanqueryPython(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("bean-query")
	assert.NoError(t, err)
	path, err = filepath.EvalSymlinks(path)
	assert.NoError(t, err)

	python := filepath.Join(filepath.Dir(path), "python")
	if script, err := os.ReadFile(path); err == nil {
		shebang, _, _ := strings.Cut(string(script), "\n")
		// The shebang may pass the interpreter flags (pip writes `python -E`).
		if line, ok := strings.CutPrefix(shebang, "#!"); ok {
			if fields := strings.Fields(line); len(fields) > 0 && strings.HasPrefix(filepath.Base(fields[0]), "python") {
				python = fields[0]
			}
		}
	}
	if output, err := exec.Command(python, "-c", "import beanquery.shell").CombinedOutput(); err != nil {
		t.Fatalf("%s, bean-query's interpreter, cannot import beanquery.shell: %v\n%s", python, err, output)
	}
	return python
}

// runBeanqueryShellError returns what beanquery's shell prints on stderr
// for a fixture's statement, which it fails.
func runBeanqueryShellError(t *testing.T, python string, fixture queryFixture) string {
	t.Helper()

	cmd := exec.Command(python, "-c", beanqueryShellError, fixture.ledger, fixture.query)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	assert.True(t, stdErrors.As(err, &exitErr) && exitErr.ExitCode() == shellErrorExit && strings.HasPrefix(stderr.String(), "error: "),
		"beanquery's shell does not report an error for %s: %v\n%s", fixture.name, err, stderr.String())
	return stderr.String()
}

// requireBeanquery skips the test when bean-query is not on PATH, and fails
// it when bean-query is not beanquery 0.2, whose output the query fixtures
// are held to.
func requireBeanquery(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("bean-query"); err != nil {
		t.Skip("bean-query not found in PATH; install beanquery 0.2 to run this suite")
	}
	version, err := exec.Command("bean-query", "--version").Output()
	assert.NoError(t, err)
	if !isBeanquery02(string(version)) {
		t.Fatalf("bean-query reports %q, but this suite targets beanquery 0.2", strings.TrimSpace(string(version)))
	}
}

// isBeanquery02 reports whether version, bean-query's --version output,
// names beanquery 0.2 ("beanquery 0.2.0, beancount 3.2.3"); bean-query 2.x
// reports "Beancount 2.3.6".
func isBeanquery02(version string) bool {
	return strings.HasPrefix(version, "beanquery 0.2.")
}

func TestIsBeanquery02(t *testing.T) {
	assert.True(t, isBeanquery02("beanquery 0.2.0, beancount 3.2.3\n"))
	assert.False(t, isBeanquery02("beanquery 0.1.0, beancount 3.0.0\n"))
	assert.False(t, isBeanquery02("Beancount 2.3.6\n"))
}
