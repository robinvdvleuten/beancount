// Package ledgerload loads a ledger and processes it, the one way every
// command and the web server do: following includes, discovering documents
// and recovering from syntax errors, like bean-check. Callers only decide
// how to report what it returns.
package ledgerload

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// Result is a loaded and processed ledger.
type Result struct {
	// AST is the processed tree, which the Ledger's records are keyed by:
	// booked postings, and the directives Plugins and pads add.
	AST *ast.AST
	// Ledger holds the processed state, its options (Ledger.Config) and its
	// validation diagnostics (Ledger.Diagnostics).
	Ledger *ledger.Ledger
	// LoadDiagnostics are the problems loading went on past: syntax
	// errors, includes that match nothing, ignored options and the like.
	LoadDiagnostics []error
	// Root is the absolute path of the loaded file.
	Root string
	// Includes are the absolute paths of the files it includes.
	Includes []string
	// Sources holds the text of every file read, keyed by the filename its
	// positions carry, so an error can be shown in its own file's context.
	Sources map[string][]byte

	// loaded is the tree as loaded, which With processes again.
	loaded *ast.AST
}

// Load loads the ledger at src and processes it. Its error is a load that
// cannot go on, such as an unreadable file, or processing that fails; the
// problems loading and validation go on past are in the Result.
func Load(ctx context.Context, src loader.Source) (*Result, error) {
	timer := telemetry.FromContext(ctx).Start("ledgerload.load " + filepath.Base(src.Path))
	defer timer.End()

	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery())
	loaded, err := ldr.Load(ctx, src)
	if err != nil {
		return nil, err
	}
	result := &Result{
		LoadDiagnostics: loaded.Diagnostics,
		Root:            loaded.Root,
		Includes:        loaded.Includes,
		Sources:         loaded.Sources,
		loaded:          loaded.AST,
	}
	if err := result.process(ctx, loaded.AST); err != nil {
		return nil, err
	}
	return result, nil
}

// Diagnostics returns everything the ledger reports, errors and warnings
// alike, as one new list in the order they are shown: the LoadDiagnostics,
// then the Ledger's. It is for a caller that reports both alike, such as
// check --json and the web server; one that renders them apart reads each.
func (r *Result) Diagnostics() []error {
	return slices.Concat(r.LoadDiagnostics, r.Ledger.Diagnostics())
}

// With returns the ledger processed again with directives added to it, as
// they would be if the loaded files held them, in a Ledger of its own. The
// load is not repeated: Process leaves the loaded tree as it was.
func (r *Result) With(ctx context.Context, directives []ast.Directive) (*Result, error) {
	tree := *r.loaded
	tree.Directives = append(slices.Clip(r.loaded.Directives), directives...)
	result := *r
	if err := result.process(ctx, &tree); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Result) process(ctx context.Context, tree *ast.AST) error {
	l := ledger.New()
	processed, err := l.Process(ctx, tree)
	if err != nil {
		return err
	}
	r.AST = processed
	r.Ledger = l
	return nil
}
