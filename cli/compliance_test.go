package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
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

			ctx := context.Background()
			ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery())
			result, err := ldr.Load(ctx, fixture.path)
			if err == nil {
				err = ledger.New().Process(ctx, result.AST)
			}
			if err == nil {
				// Fatal load diagnostics fail a check like validation errors do.
				if loadErrs := diagnostic.Errors(result.Diagnostics); len(loadErrs) > 0 {
					err = errors.Join(loadErrs...)
				}
			}

			if fixture.wantPass {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestOfficialBeancountDifferential validates the fixture expectations
// themselves against the official bean-check, so the manifest cannot drift
// from real beancount behavior. Runs whenever bean-check is installed.
func TestOfficialBeancountDifferential(t *testing.T) {
	if _, err := exec.LookPath("bean-check"); err != nil {
		t.Skip("bean-check not found in PATH; install beancount 2.x to run the differential suite")
	}

	version, err := exec.Command("bean-check", "--version").CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(version), "2.", "differential suite targets beancount v2")

	for _, fixture := range loadComplianceFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			err := exec.Command("bean-check", fixture.path).Run()
			officialOK := err == nil
			var exitErr *exec.ExitError
			if err != nil && !errors.As(err, &exitErr) {
				t.Fatalf("run bean-check: %v", err)
			}
			assert.Equal(t, fixture.wantPass, officialOK)
		})
	}
}

// TestOfficialFormatParity compares our formatter's output byte-for-byte
// with bean-format on the fixtures under testdata/compliance/format.
// Runs whenever bean-format is installed.
func TestOfficialFormatParity(t *testing.T) {
	if _, err := exec.LookPath("bean-format"); err != nil {
		t.Skip("bean-format not found in PATH; install beancount 2.x to run the format parity suite")
	}

	paths, err := filepath.Glob(filepath.Join(complianceDir, "format", "*.beancount"))
	assert.NoError(t, err)
	assert.True(t, len(paths) > 0, "no format fixtures found")

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".beancount")
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(path)
			assert.NoError(t, err)

			ctx := context.Background()
			tree, err := parser.ParseBytesWithFilename(ctx, path, source)
			assert.NoError(t, err)
			var ours bytes.Buffer
			assert.NoError(t, formatter.New().Format(ctx, tree, source, &ours))

			official, err := exec.Command("bean-format", path).Output()
			assert.NoError(t, err)

			assert.Equal(t, string(official), ours.String())
		})
	}
}
