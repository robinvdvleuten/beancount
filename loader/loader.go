// Package loader provides functionality for loading Beancount files with support for
// include directives. It can recursively resolve and merge multiple files into a
// single AST, handling relative paths and deduplication.
//
// The loader supports two modes of operation:
//   - Simple mode: parses a single raw source AST with include directives preserved
//   - Follow mode: recursively loads all included files and merges them into one processed AST
//
// When following includes, the loader resolves relative paths from the directory of
// the file containing the include directive, and deduplicates files that are included
// multiple times.
//
// Example usage:
//
//	// Load a single file without following includes
//	loader := loader.New()
//	ast, err := loader.Load("main.beancount")
//
//	// Load with recursive include resolution
//	loader := loader.New(loader.WithFollowIncludes())
//	ast, err := loader.Load("main.beancount")
package loader

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// LoadResult contains the result of loading a beancount file.
type LoadResult struct {
	// AST is the parsed abstract syntax tree.
	AST *ast.AST
	// Root is the absolute path of the root file that was loaded.
	Root string
	// Includes contains the absolute paths of all included files (not including the root).
	// This is only populated when FollowIncludes is enabled.
	Includes []string
	// Diagnostics contains non-fatal warnings produced while loading.
	Diagnostics []error
}

// IncludedOptionWarning reports an option ignored because it came from an included file.
type IncludedOptionWarning struct {
	Option *ast.Option
}

func (w *IncludedOptionWarning) Error() string {
	position := w.Option.Position()
	return fmt.Sprintf("%s:%d: option %q from included file is ignored", position.Filename, position.Line, w.Option.Name.Value)
}

func (w *IncludedOptionWarning) Severity() diagnostic.Severity {
	return diagnostic.SeverityWarning
}

func (w *IncludedOptionWarning) GetPosition() ast.Position { return w.Option.Position() }

// DocumentRootError reports a documents option pointing at a missing directory.
type DocumentRootError struct {
	Option *ast.Option
	Dir    string
}

func (e *DocumentRootError) Error() string {
	pos := e.Option.Position()
	return fmt.Sprintf("%s:%d: Document root '%s' does not exist", pos.Filename, pos.Line, e.Dir)
}

// Severity is fatal: official beancount reports a missing document root as an error.
func (e *DocumentRootError) Severity() diagnostic.Severity { return diagnostic.SeverityError }

func (e *DocumentRootError) GetPosition() ast.Position { return e.Option.Position() }

// Loader handles loading and parsing of Beancount files with optional include resolution.
// It provides configurable behavior for handling include directives, supporting both simple
// single-file parsing and recursive loading with file merging.
//
// Configure the loader using functional options passed to New:
//
//	loader := New(WithFollowIncludes())
type Loader struct {
	// FollowIncludes determines whether to recursively load included files.
	// When false, only the specified file is parsed and ast.Includes is preserved.
	// When true, all included files are recursively loaded and merged into a single AST.
	FollowIncludes bool

	// DiscoverDocuments generates Document directives from the directory
	// trees declared by the documents option, like beancount's default
	// beancount.ops.documents plugin. Formatting-only consumers should
	// leave this off: bean-format never runs document discovery.
	DiscoverDocuments bool
}

// Option configures how files are loaded.
type Option func(*Loader)

// WithFollowIncludes configures the loader to recursively load and merge all included files.
// When enabled:
//   - All include directives are recursively resolved and loaded
//   - Relative paths are resolved from the directory of the including file
//   - All directives, options, and plugins are merged into a single AST
//   - The returned AST has ast.Includes set to nil (all includes resolved)
//
// When disabled (default):
//   - Only the specified file is parsed
//   - Include directives remain in ast.Includes
//   - No path resolution or validation occurs
func WithFollowIncludes() Option {
	return func(l *Loader) {
		l.FollowIncludes = true
	}
}

// WithDocumentsDiscovery enables generation of Document directives from the
// directory trees declared by the documents option.
func WithDocumentsDiscovery() Option {
	return func(l *Loader) {
		l.DiscoverDocuments = true
	}
}

// New creates a new Loader with the given options.
func New(opts ...Option) *Loader {
	l := &Loader{
		FollowIncludes: false, // Default: don't follow includes
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Load parses a beancount file with optional recursive include resolution.
func (l *Loader) Load(ctx context.Context, filename string) (*LoadResult, error) {
	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

	// Get absolute path for the root file
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", filename, err)
	}

	if !l.FollowIncludes {
		// Simple case: just parse the single file
		parseTimer := collector.Start(fmt.Sprintf("loader.parse %s", filepath.Base(filename)))
		defer parseTimer.End()

		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", filename, err)
		}
		result, err := parser.ParseBytesWithFilename(ctx, filename, data)
		if err != nil {
			// Wrap parser errors for consistent formatting
			return nil, parser.NewParseErrorWithSource(filename, err, data)
		}
		var diagnostics []error
		if l.DiscoverDocuments {
			diagnostics = discoverDocuments(result, absPath)
		}
		return &LoadResult{
			AST:         result,
			Root:        absPath,
			Includes:    nil,
			Diagnostics: diagnostics,
		}, nil
	}

	// Recursive loading with include resolution
	// Use root timer for hierarchy if available, otherwise create flat timers
	rootTimer := telemetry.RootTimerFromContext(ctx)
	state := &loaderState{
		visited:   make(map[string]bool),
		collector: collector,
		rootTimer: rootTimer,
		root:      absPath,
	}

	ast, err := state.loadRecursive(ctx, filename)
	if err != nil {
		return nil, err
	}

	// Extract includes from visited map (excluding the root file)
	var includes []string
	for path := range state.visited {
		if path != absPath {
			includes = append(includes, path)
		}
	}

	if l.DiscoverDocuments {
		state.diagnostics = append(state.diagnostics, discoverDocuments(ast, absPath)...)
	}

	return &LoadResult{
		AST:         ast,
		Root:        absPath,
		Includes:    includes,
		Diagnostics: state.diagnostics,
	}, nil
}

// documentFilenamePattern matches dated document filenames, using the same
// expression as beancount's ops/documents.py (the separator after the date
// is deliberately any character).
var documentFilenamePattern = regexp.MustCompile(`^(\d\d\d\d)-(\d\d)-(\d\d).(.*)$`)

// discoverDocuments generates Document directives from the directory trees
// declared by the documents option, mirroring beancount's default
// beancount.ops.documents plugin: directories resolve relative to the root
// ledger file, dated files under account-shaped subpaths become Document
// directives for accounts the ledger mentions, and a missing root directory
// is a fatal diagnostic.
func discoverDocuments(tree *ast.AST, rootFile string) []error {
	var diagnostics []error
	var accounts map[string]bool

	for _, option := range tree.Options {
		if option.Name.Value != "documents" {
			continue
		}

		dir := option.Value.Value
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(rootFile), dir)
		}
		dir = filepath.Clean(dir)

		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			diagnostics = append(diagnostics, &DocumentRootError{Option: option, Dir: dir})
			continue
		}

		if accounts == nil {
			accounts = tree.Enrich().Accounts
		}

		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			match := documentFilenamePattern.FindStringSubmatch(entry.Name())
			if match == nil {
				return nil
			}
			relDir, err := filepath.Rel(dir, filepath.Dir(path))
			if err != nil || relDir == "." {
				return nil
			}
			accountName := strings.ReplaceAll(relDir, string(filepath.Separator), ":")
			if !accounts[accountName] {
				return nil // Like beancount's non-strict mode: skip unknown accounts.
			}
			date, err := ast.NewDate(fmt.Sprintf("%s-%s-%s", match[1], match[2], match[3]))
			if err != nil {
				return nil
			}

			doc := &ast.Document{
				Account:        ast.Account(accountName),
				PathToDocument: ast.NewRawString(path),
			}
			doc.SetDate(date)
			doc.SetPosition(ast.Position{Filename: rootFile})
			tree.Directives = append(tree.Directives, doc)
			return nil
		})
	}

	return diagnostics
}

// MustLoad loads a beancount file, panicking on error.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	loader := loader.New(loader.WithFollowIncludes())
//	result := loader.MustLoad(context.Background(), "main.beancount")
func (l *Loader) MustLoad(ctx context.Context, filename string) *LoadResult {
	result, err := l.Load(ctx, filename)
	if err != nil {
		panic(err)
	}
	return result
}

// LoadBytes parses beancount content from bytes with optional include resolution.
// The filename parameter is used only for error reporting and position tracking in
// parse errors. It does not need to be a real file path.
//
// When FollowIncludes is enabled and the content contains include directives:
//   - If filename is "<stdin>", an error is returned (includes not supported for stdin)
//   - If filename is a real path, includes are resolved relative to that path's directory
//
// When FollowIncludes is disabled, include directives are preserved in the AST and
// no resolution occurs.
//
// Example usage:
//
//	// Parse from stdin
//	ldr := loader.New(loader.WithFollowIncludes())
//	ast, err := ldr.LoadBytes(ctx, "<stdin>", stdinBytes)
//
//	// Parse from bytes with file context for includes
//	ast, err := ldr.LoadBytes(ctx, "/path/to/main.beancount", mainBytes)
func (l *Loader) LoadBytes(ctx context.Context, filename string, data []byte) (*ast.AST, error) {
	collector := telemetry.FromContext(ctx)

	// For display in telemetry, use basename
	displayName := filepath.Base(filename)
	parseTimer := collector.Start(fmt.Sprintf("loader.parse %s", displayName))
	defer parseTimer.End()

	result, err := parser.ParseBytesWithFilename(ctx, filename, data)
	if err != nil {
		return nil, parser.NewParseErrorWithSource(filename, err, data)
	}

	// If following includes is requested but we're parsing from stdin,
	// we can't resolve includes (no base directory context)
	if l.FollowIncludes && filename == "<stdin>" && len(result.Includes) > 0 {
		return nil, fmt.Errorf("include directives are not supported when reading from stdin")
	}

	// If following includes is requested and filename is a real path,
	// we could support it by falling back to Load() for the include resolution.
	// However, this is not the primary use case for LoadBytes, so we keep it simple.
	if l.FollowIncludes && filename != "<stdin>" && len(result.Includes) > 0 {
		// Could implement include resolution here, but simpler to just error
		// Users should use Load() if they need includes
		return nil, fmt.Errorf("include directives found; use Load() instead of LoadBytes() to resolve includes")
	}

	return result, nil
}

// MustLoadBytes parses beancount content from bytes, panicking on error.
// Intended for use in tests and examples where error handling is not needed.
//
// Example:
//
//	loader := loader.New()
//	ast := loader.MustLoadBytes(context.Background(), "test.beancount", data)
func (l *Loader) MustLoadBytes(ctx context.Context, filename string, data []byte) *ast.AST {
	result, err := l.LoadBytes(ctx, filename, data)
	if err != nil {
		panic(err)
	}
	return result
}

// loaderState tracks state during recursive loading.
type loaderState struct {
	visited     map[string]bool     // Absolute paths of files already loaded
	collector   telemetry.Collector // Telemetry collector for tracking load operations
	rootTimer   telemetry.Timer     // Root check timer from context
	root        string
	diagnostics []error
}

// loadRecursive recursively loads a file and all its includes.
// When l.rootTimer is set, creates hierarchical child timers.
// When l.rootTimer is nil, creates flat root-level timers.
func (l *loaderState) loadRecursive(ctx context.Context, filename string) (*ast.AST, error) {
	// Get absolute path for deduplication
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", filename, err)
	}

	// Check if already visited (deduplication - same file included multiple times)
	if l.visited[absPath] {
		// Return empty AST - this file was already processed
		return &ast.AST{}, nil
	}
	l.visited[absPath] = true

	// Create load timer - hierarchical or flat depending on rootTimer presence
	var loadTimer telemetry.Timer
	if l.rootTimer != nil {
		// Hierarchical: create child timer under root
		loadTimer = l.rootTimer.Child(fmt.Sprintf("loader.load %s", filepath.Base(filename)))
	} else {
		// Flat: create root-level timer
		loadTimer = l.collector.Start(fmt.Sprintf("loader.load %s", filepath.Base(filename)))
	}

	// Create parse timer - always as child of load timer
	var parseTimer telemetry.Timer
	if l.rootTimer != nil {
		// Hierarchical: simple child name
		parseTimer = loadTimer.Child("loader.parse")
	} else {
		// Flat: include filename in child name
		parseTimer = loadTimer.Child(fmt.Sprintf("loader.parse %s", filepath.Base(filename)))
	}

	// Read and parse the file
	data, err := os.ReadFile(filename)
	if err != nil {
		parseTimer.End()
		loadTimer.End()
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	result, err := parser.ParseBytesWithFilename(ctx, filename, data)
	parseTimer.End()

	if err != nil {
		loadTimer.End()
		// Wrap parser errors for consistent formatting
		return nil, parser.NewParseErrorWithSource(filename, err, data)
	}

	if err := prepareLoadedAST(result); err != nil {
		loadTimer.End()
		return nil, err
	}

	if absPath != l.root {
		for _, option := range result.Options {
			l.diagnostics = append(l.diagnostics, &IncludedOptionWarning{Option: option})
		}
	}

	// If no includes, end and return
	if len(result.Includes) == 0 {
		loadTimer.End()
		result.Includes = nil // Clear includes since we're in follow mode
		return result, nil
	}

	// Create merge timer as child of load timer
	mergeTimer := loadTimer.Child("ast.merging")

	// For flat mode, end load timer before recursive calls to reset current to nil,
	// allowing included files to create root-level timers
	if l.rootTimer == nil {
		loadTimer.End()
	} else {
		// For hierarchical mode, keep load timer active
		// (it will be ended when we return)
		defer loadTimer.End()
	}

	// Recursively load all includes and merge
	baseDir := filepath.Dir(absPath)
	var includedASTs []*ast.AST

	for _, inc := range result.Includes {
		// Check for cancellation
		select {
		case <-ctx.Done():
			mergeTimer.End()
			return nil, ctx.Err()
		default:
		}

		// Resolve path relative to the including file's directory
		includePath := inc.Filename.Value
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}

		// Recursively load the included file
		includedAST, err := l.loadRecursive(ctx, includePath)
		if err != nil {
			mergeTimer.End()
			// Don't wrap ParseError - it already contains full path information
			// Just propagate the error up the include chain
			return nil, err
		}

		includedASTs = append(includedASTs, includedAST)
	}

	// Merge ASTs
	merged := mergeASTs(result, includedASTs...)
	mergeTimer.End()

	return merged, nil
}

// mergeASTs combines a main AST with multiple included ASTs.
// The main AST's options take precedence over included files' options.
// All directives are combined and sorted for ledger processing.
func mergeASTs(main *ast.AST, included ...*ast.AST) *ast.AST {
	result := &ast.AST{
		Directives: make(ast.Directives, 0, len(main.Directives)),
		Options:    main.Options,   // Main file options take precedence
		Includes:   nil,            // All includes resolved, so clear this
		Plugins:    main.Plugins,   // Start with main file plugins
		Pushtags:   main.Pushtags,  // Start with main file pushtags
		Poptags:    main.Poptags,   // Start with main file poptags
		Pushmetas:  main.Pushmetas, // Start with main file pushmetas
		Popmetas:   main.Popmetas,  // Start with main file popmetas
	}

	// Add main file directives
	result.Directives = append(result.Directives, main.Directives...)

	// Add directives from all included files
	for _, inc := range included {
		result.Directives = append(result.Directives, inc.Directives...)

		// Merge plugins (append, don't override)
		result.Plugins = append(result.Plugins, inc.Plugins...)
	}

	_ = ast.SortDirectives(result)
	ast.MarkPushPopDirectivesApplied(result)

	return result
}

func prepareLoadedAST(tree *ast.AST) error {
	if err := ast.ApplyPushPopDirectives(tree); err != nil {
		return err
	}
	return ast.SortDirectives(tree)
}
