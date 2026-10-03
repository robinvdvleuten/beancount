package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/query"
)

// snapshot is everything the server knows of the ledger at one moment: what
// the last load that went on loaded and, when the last load could not go
// on, that error. It is never changed once built: a reload builds another
// (snapshot.reload) and the server swaps it in, so a handler that takes one
// (Server.snapshot) reads a consistent ledger from it without a lock.
type snapshot struct {
	// ledger is the processed ledger of the last load that went on, an
	// empty one until then, so the reports of a ledger that never loaded
	// are empty.
	ledger *ledger.Ledger
	// tree is the processed tree the ledger's records are keyed by, nil
	// until a load goes on.
	tree *ast.AST
	// root is the absolute path of the ledger file, empty before the
	// first load.
	root string
	// includes are the absolute paths of the files the root includes: the
	// loaded ones or, after a load that could not go on, the ones the last
	// load that went on loaded, or the ones the root file names itself when
	// none went on.
	includes []string
	// errors are the errors of the load this snapshot was built from, as
	// the web API sends them: the load's, then the ledger's.
	errors []error
	// loadErr is the error that stopped the load this snapshot was built
	// from, such as an unreadable file. The ledger and tree are then the
	// previous snapshot's.
	loadErr error
}

// newSnapshot returns the snapshot of a server that has loaded nothing yet.
func newSnapshot() *snapshot {
	return &snapshot{ledger: ledger.New()}
}

// reload loads the ledger at path and returns the snapshot that follows
// snap. Like check and query, a syntax error drops the directive it is in
// and the rest of the ledger still loads. When the load cannot go on, the
// new snapshot keeps snap's ledger, so the reports still answer from it,
// and its includes, so they can still be edited and watched (the root's
// own includes when no load has gone on yet), and carries the error as its
// loadErr. The error returned is that of a path that cannot be resolved,
// which leaves no snapshot to build.
func (snap *snapshot) reload(ctx context.Context, path string) (*snapshot, error) {
	result, loadErr := ledgerload.Load(ctx, loader.Source{Path: path})
	if loadErr != nil {
		root, err := absolutePath(path)
		if err != nil {
			return nil, err
		}
		includes := snap.includes
		if snap.tree == nil {
			includes = rootIncludes(ctx, root)
		}
		return &snapshot{
			ledger:   snap.ledger,
			tree:     snap.tree,
			root:     root,
			includes: includes,
			loadErr:  loadErr,
		}, nil
	}

	// Diagnostics returns a new list, so the snapshot owns its errors.
	errs := diagnostic.Errors(result.Diagnostics())
	for i, err := range errs {
		errs[i] = jsonSafeSourceError(err)
	}
	return &snapshot{
		ledger:   result.Ledger,
		tree:     result.AST,
		root:     result.Root,
		includes: result.Includes,
		errors:   errs,
	}, nil
}

func absolutePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %s: %w", path, err)
	}
	return abs, nil
}

// rootIncludes returns the absolute paths the include directives of the
// file at root name, as written: a ledger that fails to load still names
// them. A root that cannot be read names none.
func rootIncludes(ctx context.Context, root string) []string {
	includes := []string{}
	data, err := os.ReadFile(root)
	if err != nil {
		return includes
	}
	// A syntax error leaves the rest of the file parsed.
	tree, _ := parser.ParseBytesWithFilename(ctx, root, data)
	if tree == nil {
		return includes
	}
	baseDir := filepath.Dir(root)
	includes = make([]string, 0, len(tree.Includes))
	for _, inc := range tree.Includes {
		includePath := inc.Filename.Value
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}
		absPath, err := filepath.Abs(includePath)
		if err != nil {
			continue
		}
		includes = append(includes, absPath)
	}
	return includes
}

// queryContext returns a context to run one query against the snapshot's
// ledger, or nil when no load has gone on yet. A Context holds per-run
// state (FROM's summarization), so each query gets its own.
func (snap *snapshot) queryContext() *query.Context {
	if snap.tree == nil {
		return nil
	}
	return &query.Context{Ledger: snap.ledger, Config: snap.ledger.Config(), AST: snap.tree}
}

// files returns the ledger's files: its root, then its includes.
func (snap *snapshot) files() []string {
	if snap.root == "" {
		return nil
	}
	return append([]string{snap.root}, snap.includes...)
}

// allows reports whether path is a file of the ledger: its root or one of
// its includes.
func (snap *snapshot) allows(path string) bool {
	return path == snap.root || slices.Contains(snap.includes, path)
}

// resolve returns the absolute path of the ledger file path names, the
// root when it is empty. Only a file of the ledger resolves.
func (snap *snapshot) resolve(path string) (string, error) {
	if path == "" {
		if snap.root == "" {
			return "", fmt.Errorf("no filepath provided and no root file configured")
		}
		return snap.root, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid filepath: %w", err)
	}
	if !snap.allows(absPath) {
		return "", fmt.Errorf("access denied: file not in ledger")
	}
	return absPath, nil
}

// sourceResponse returns the response for a file of the ledger with the
// given content: the error that stopped the load or else the load's errors,
// and the ledger's files.
func (snap *snapshot) sourceResponse(source []byte) *SourceResponse {
	includes := snap.includes
	if includes == nil {
		includes = []string{}
	}
	errors := []error{}
	if snap.loadErr != nil {
		errors = []error{jsonSafeSourceError(snap.loadErr)}
	} else {
		errors = append(errors, snap.errors...)
	}
	return &SourceResponse{
		Source:      string(source),
		Fingerprint: computeFingerprint(source),
		Errors:      errors,
		Files: Files{
			Root:     snap.root,
			Includes: includes,
		},
	}
}
