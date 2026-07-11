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
// same-named .beancount override). A gap_ prefix marks a known divergence
// from the official tool, documented in KNOWN_GAPS.md; gap fixtures are
// skipped by the parity leg. A numberify_ prefix runs the query with -m.
type queryFixture struct {
	name      string
	query     string
	ledger    string
	numberify bool
	knownGap  bool
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
			knownGap:  strings.HasPrefix(name, "gap_"),
		})
	}
	return fixtures
}

// runOurQuery executes a fixture through the same pipeline as the query
// command and returns what would be written to stdout.
func runOurQuery(t *testing.T, fixture queryFixture, format string, numberify bool) string {
	t.Helper()

	ctx := context.Background()
	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery())
	result, err := ldr.Load(ctx, fixture.ledger)
	assert.NoError(t, err)

	l := ledger.New()
	if err := l.Process(ctx, result.AST); err != nil {
		var validationErrors *ledger.ValidationErrors
		assert.True(t, stdErrors.As(err, &validationErrors), "unexpected process error: %v", err)
	}

	cfg, err := config.FromAST(result.AST)
	assert.NoError(t, err)

	var out strings.Builder
	qctx := &query.Context{Ledger: l, Config: cfg}
	assert.NoError(t, runQuery(ctx, qctx, result.AST, fixture.query, format, numberify, &out))
	return out.String()
}

// TestQueryFixtures runs every fixture through our engine in both formats,
// so the suite exercises the fixtures even without bean-query installed.
// Error fixtures (err_ prefix) must produce an ERROR: line.
func TestQueryFixtures(t *testing.T) {
	for _, fixture := range loadQueryFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			for _, format := range []string{"text", "csv"} {
				output := runOurQuery(t, fixture, format, fixture.numberify)
				if strings.HasPrefix(fixture.name, "err_") {
					assert.True(t, strings.HasPrefix(output, "ERROR: "), "expected an error, got: %s", output)
				} else {
					assert.False(t, strings.HasPrefix(output, "ERROR: "), "unexpected error: %s", output)
				}
			}
		})
	}
}

// TestOfficialQueryParity compares our output byte-for-byte with bean-query
// in both text and csv formats. Runs whenever bean-query 2.x is installed.
func TestOfficialQueryParity(t *testing.T) {
	if _, err := exec.LookPath("bean-query"); err != nil {
		t.Skip("bean-query not found in PATH; install beancount 2.x to run the query parity suite")
	}

	version, err := exec.Command("bean-query", "--version").CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(version), "2.", "query parity suite targets beancount v2")

	for _, fixture := range loadQueryFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			if fixture.knownGap {
				t.Skipf("known gap, see %s", filepath.Join("..", "testdata", "compliance", "KNOWN_GAPS.md"))
			}

			for _, format := range []string{"text", "csv"} {
				args := []string{"-f", format}
				if fixture.numberify {
					args = append(args, "-m")
				}
				args = append(args, fixture.ledger, fixture.query)

				// bean-query prints query errors to stdout and exits zero,
				// so Output() captures everything we compare against.
				official, err := exec.Command("bean-query", args...).Output()
				assert.NoError(t, err)

				ours := runOurQuery(t, fixture, format, fixture.numberify)
				assert.Equal(t, string(official), ours, "format %s, query: %s", format, fixture.query)
			}
		})
	}
}
