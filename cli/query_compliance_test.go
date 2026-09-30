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

// runOurQuery executes a fixture through the same load, process and run
// steps as the query command and returns what it writes to stdout.
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
	qctx := &query.Context{Ledger: l, Config: cfg, AST: result.AST}
	assert.NoError(t, query.Run(ctx, qctx, fixture.query, query.Format(format), numberify, &out))
	return out.String()
}

// TestQueryFixtures runs every fixture through our engine in both formats,
// so the suite exercises the fixtures even without bean-query installed.
// Error fixtures (err_ prefix) must produce an error message.
func TestQueryFixtures(t *testing.T) {
	for _, fixture := range loadQueryFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			for _, format := range []string{"text", "csv"} {
				output := runOurQuery(t, fixture, format, fixture.numberify)
				assert.Equal(t, strings.HasPrefix(fixture.name, "err_"), isQueryError(output), "output: %s", output)
			}
		})
	}
}

// isQueryError reports whether output is an error message. bean-query
// prints most errors behind "ERROR: ", but a few parse errors without it.
func isQueryError(output string) bool {
	for _, prefix := range []string{"ERROR: ", "Empty FROM expression", "Unknown token: "} {
		if strings.HasPrefix(output, prefix) {
			return true
		}
	}
	return false
}

// queryGaps lists query fixtures whose output differs from beanquery's,
// with the #562 topic, or the other issue, that closes the gap. An entry
// whose output agrees in both formats, or that names no fixture, fails the
// query parity suite.
var queryGaps = map[string]string{
	"balance_where_aggregate":         "#562 operator typing: beanquery rejects = between inventories",
	"balance_where_equal":             "#562 operator typing: beanquery rejects = between inventories",
	"balance_where_inventory":         "#562 operator typing: beanquery rejects = between inventories",
	"balance_where_not_equal":         "#562 operator typing: beanquery rejects != between inventories",
	"balance_where_rows":              "#562 operator typing: beanquery rejects = between inventories",
	"compare_mixed_types_equality":    "#562 operator typing: beanquery rejects != between int and str",
	"compare_mixed_types_ordering":    "#562 operator typing: beanquery rejects < between int and str",
	"compare_number_string_from":      "#562 operator typing: beanquery rejects = between int and str",
	"compare_number_string_where":     "#562 operator typing: beanquery rejects = between int and str",
	"err_group_by_aggregate_expr":     "#562 operator typing: beanquery rejects != between int and date first",
	"metadata":                        "#562 operator typing: beanquery rejects != NULL for IS NOT NULL",
	"err_aggregate_arg_count":         "#562 error output: beanquery's words on stderr",
	"err_aggregate_arg_type":          "#562 error output: beanquery's words on stderr",
	"err_bad_column":                  "#562 error output: beanquery's words on stderr",
	"err_bad_function":                "#562 error output: beanquery's words on stderr",
	"err_empty_from":                  "#562 error output: beanquery's words on stderr",
	"err_from_close_before_open":      "#562 error output: beanquery's words on stderr",
	"err_from_context":                "#562 error output: beanquery's words on stderr",
	"err_function_arg_type":           "#562 error output: beanquery's words on stderr",
	"err_function_arg_types":          "#562 error output: beanquery's words on stderr",
	"err_group_by_inventory":          "#562 error output: beanquery's words on stderr",
	"err_group_coverage":              "#562 error output: beanquery's words on stderr",
	"err_mixed_aggregate":             "#562 error output: beanquery's words on stderr",
	"err_nested_aggregate":            "#562 error output: beanquery's words on stderr",
	"err_null_argument":               "#562 error output: beanquery's words on stderr",
	"err_object_argument":             "#562 error output: beanquery's words on stderr",
	"err_order_by_index":              "#562 error output: beanquery's words on stderr",
	"err_pivot_by_expression":         "#562 error output: beanquery's words on stderr",
	"err_print_close_before_open":     "#562 error output: beanquery's words on stderr",
	"err_unknown_token":               "#562 error output: beanquery's words on stderr",
	"err_unterminated":                "#562 error output: beanquery's words on stderr",
	"err_where_aggregate":             "#562 error output: beanquery's words on stderr",
	"err_identifier_digits":           "#562 grammar: identifiers take digits",
	"err_signed_number_after_operand": "#562 grammar: number -1 is a subtraction",
	"err_syntax_near":                 "#562 grammar: count(*) is accepted",
	"err_unary_minus":                 "#562 grammar: unary minus applies to any expression",
	"column_names_unique":             "#562 ORDER BY: a name binds the last target it names",
	"order_by_desc":                   "#562 ORDER BY: each term takes its own direction",
	"err_having":                      "#562 HAVING and PIVOT BY: HAVING is supported",
	"err_pivot":                       "#562 HAVING and PIVOT BY: PIVOT BY is supported",
	"err_pivot_by_after_checks":       "#562 HAVING and PIVOT BY: PIVOT BY takes two columns",
	"balance_where_running":           "#562 functions: balance accumulates where it is evaluated, WHERE included",
	"err_function_arg_count":          "#562 functions: root takes one argument too",
	"err_where_has_account":           "#562 functions: has_account is allowed in WHERE",
	"str_composites":                  "#562 functions: every function is NULL-strict and str(TRUE) is TRUE",
	"from_open_kept_entries":          "#560: beanquery's printer hides __implicit_prices__ metadata",
	"print_document_pushed_tags":      "#560: beanquery's printer spaces a document's tags and links",
	"print_implicit_price_scale":      "#560: beanquery's printer hides __implicit_prices__ metadata",
	"print_implicit_price_zero_units": "#560: beanquery's printer hides __implicit_prices__ metadata",
	"print_implicit_prices":           "#560: beanquery's printer hides __implicit_prices__ metadata",
	"print_implicit_prices_precision": "#560: beanquery's printer hides __implicit_prices__ metadata",
	"negative_zero":                   "#408, a deliberate deviation: booking gives a zero residual no sign",
}

// TestOfficialQueryParity compares our output byte-for-byte with bean-query
// in both text and csv formats. Runs whenever beanquery 0.2 is installed.
func TestOfficialQueryParity(t *testing.T) {
	requireBeanquery(t)

	fixtures := loadQueryFixtures(t)
	names := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		names[i] = fixture.name
		t.Run(fixture.name, func(t *testing.T) {
			agrees := true
			for _, format := range []string{"text", "csv"} {
				args := []string{"-f", format}
				if fixture.numberify {
					args = append(args, "-m")
				}
				args = append(args, fixture.ledger, fixture.query)

				// beanquery writes query errors to stderr and exits 1, so
				// only stdout is compared, and only a gap may fail.
				official, err := exec.Command("bean-query", args...).Output()
				ours := runOurQuery(t, fixture, format, fixture.numberify)

				if _, ok := queryGaps[fixture.name]; ok {
					agrees = agrees && err == nil && string(official) == ours
					continue
				}
				assert.NoError(t, err, "format %s, query: %s", format, fixture.query)
				assert.Equal(t, string(official), ours, "format %s, query: %s", format, fixture.query)
			}
			if reason, ok := queryGaps[fixture.name]; ok {
				assert.False(t, agrees, "the output agrees; remove the queryGaps entry (%s)", reason)
			}
		})
	}
	assertGapsNameFixtures(t, queryGaps, names)
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
