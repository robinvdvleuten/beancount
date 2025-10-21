// Package loader provides functionality for loading Beancount files with support for
// include directives. It can recursively resolve and merge multiple files into a
// single AST, handling relative paths and deduplication.
//
// The loader supports two modes of operation:
//   - Simple mode: Parses a single file with include directives preserved in the AST
//   - Follow mode: Recursively loads all included files and merges them into one AST
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
	"os"
	"path/filepath"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/telemetry"
)

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
func (l *Loader) Load(ctx context.Context, filename string) (*ast.AST, error) {
	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

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
		return result, nil
	}

	// Recursive loading with include resolution
	// Only use root timer-based hierarchy if telemetry is enabled
	rootTimer := telemetry.RootTimerFromContext(ctx)
	state := &loaderState{
		visited:   make(map[string]bool),
		collector: collector,
		rootTimer: rootTimer,
	}

	// If no root timer, fall back to flat hierarchy (for non-check commands or disabled telemetry)
	if rootTimer == nil {
		return state.loadRecursiveFlat(ctx, filename)
	}

	return state.loadRecursive(ctx, filename)
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

// loaderState tracks state during recursive loading.
type loaderState struct {
	visited   map[string]bool     // Absolute paths of files already loaded
	collector telemetry.Collector // Telemetry collector for tracking load operations
	rootTimer telemetry.Timer     // Root check timer from context
}

// loadRecursive recursively loads a file and all its includes.
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

	// ALL files get the same treatment: loader.load -> loader.parse -> parser timers
	loadTimer := l.rootTimer.Child(fmt.Sprintf("loader.load %s", filepath.Base(filename)))
	parseTimer := loadTimer.Child("loader.parse")

	// Read and parse the file
	data, err := os.ReadFile(filename)
	if err != nil {
		parseTimer.End()
		loadTimer.End()
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	result, err := parser.ParseBytesWithFilename(ctx, filename, data)
	if err != nil {
		parseTimer.End()
		loadTimer.End()
		// Wrap parser errors for consistent formatting
		return nil, parser.NewParseErrorWithSource(filename, err, data)
	}
	parseTimer.End()
	loadTimer.End()

	// If no includes, return
	if len(result.Includes) == 0 {
		result.Includes = nil // Clear includes since we're in follow mode
		return result, nil
	}

	// Recursively load all includes and merge
	baseDir := filepath.Dir(absPath)
	var includedASTs []*ast.AST

	for _, inc := range result.Includes {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Resolve path relative to the including file's directory
		includePath := inc.Filename
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}

		// Recursively load the included file
		includedAST, err := l.loadRecursive(ctx, includePath)
		if err != nil {
			// Don't wrap ParseError - it already contains full path information
			// Just propagate the error up the include chain
			return nil, err
		}

		includedASTs = append(includedASTs, includedAST)
	}

	// Merge ASTs
	mergeTimer := l.rootTimer.Child("ast.merging")
	merged := mergeASTs(result, includedASTs...)
	mergeTimer.End()

	return merged, nil
}

// loadRecursiveFlat recursively loads files without a root timer.
// Used when telemetry is disabled or for non-check commands.
// Creates root-level load timers for all files (old behavior).
func (l *loaderState) loadRecursiveFlat(ctx context.Context, filename string) (*ast.AST, error) {
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

	// Create load wrapper timer for this file
	loadTimer := l.collector.Start(fmt.Sprintf("loader.load %s", filepath.Base(filename)))

	// Create parse timer as child of load timer
	parseTimer := loadTimer.Child(fmt.Sprintf("loader.parse %s", filepath.Base(filename)))

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

	// If no includes, end and return
	if len(result.Includes) == 0 {
		loadTimer.End()
		result.Includes = nil // Clear includes since we're in follow mode
		return result, nil
	}

	// Create merge timer as child of load timer
	mergeTimer := loadTimer.Child("ast.merging")

	// End load timer before recursive calls - this resets current to nil,
	// allowing included files to create root-level timers
	loadTimer.End()

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
		includePath := inc.Filename
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}

		// Recursively load the included file
		includedAST, err := l.loadRecursiveFlat(ctx, includePath)
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
// All directives are combined and will be sorted by date by the parser.
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

		// Note: Pushtag/Poptag/Pushmeta/Popmeta are already applied during parsing,
		// so we don't need to merge them here (they've already modified their
		// respective file's directives)
	}

	// Re-sort all directives by date
	_ = ast.SortDirectives(result)

	return result
}
