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
	"sync"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/telemetry"
	"golang.org/x/sync/errgroup"
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
	if !l.FollowIncludes {
		// Simple case: just parse the single file
		parseTimer := telemetry.StartTimer(ctx, fmt.Sprintf("loader.parse %s", filepath.Base(filename)))
		defer parseTimer.End()
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", filename, err)
		}
		result, err := parser.ParseBytesWithFilename(ctx, filename, data)
		if err != nil {
			// Wrap parser errors for consistent formatting
			return nil, parser.NewParseError(filename, err)
		}
		return result, nil
	}

	// Recursive loading with include resolution
	loadTimer := telemetry.StartTimer(ctx, fmt.Sprintf("loader.load %s", filepath.Base(filename)))
	defer loadTimer.End()
	state := &loaderState{
		visited: make(map[string]bool),
	}

	return state.loadRecursive(ctx, filename, nil)
}

// LoadBytes parses beancount content from a byte slice with optional recursive include resolution.
// The filename parameter is used for error reporting and as the base path for resolving includes.
// When FollowIncludes is enabled, relative include paths are resolved from the directory of filename.
func (l *Loader) LoadBytes(ctx context.Context, filename string, data []byte) (*ast.AST, error) {
	if !l.FollowIncludes {
		// Simple case: just parse the provided data
		parseTimer := telemetry.StartTimer(ctx, fmt.Sprintf("loader.parse %s", filepath.Base(filename)))
		defer parseTimer.End()
		result, err := parser.ParseBytesWithFilename(ctx, filename, data)
		if err != nil {
			// Wrap parser errors for consistent formatting
			return nil, parser.NewParseError(filename, err)
		}
		return result, nil
	}

	// For recursive loading, parse the initial data then follow includes from disk
	parseTimer := telemetry.StartTimer(ctx, fmt.Sprintf("loader.parse %s", filepath.Base(filename)))
	result, err := parser.ParseBytesWithFilename(ctx, filename, data)
	parseTimer.End()
	if err != nil {
		return nil, parser.NewParseError(filename, err)
	}

	// If no includes, return as-is
	if len(result.Includes) == 0 {
		return result, nil
	}

	// Recursively load all includes from disk
	loadTimer := telemetry.StartTimer(ctx, fmt.Sprintf("loader.load includes for %s", filepath.Base(filename)))
	defer loadTimer.End()
	state := &loaderState{
		visited: make(map[string]bool),
	}

	// Get absolute path for include resolution
	// Special handling for STDIN ("-"): use current working directory as base
	var absPath, baseDir string
	if filename == "-" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory for STDIN: %w", err)
		}
		absPath = filepath.Join(baseDir, "-") // Use a pseudo-path for visited tracking
	} else {
		var err error
		absPath, err = filepath.Abs(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", filename, err)
		}
		baseDir = filepath.Dir(absPath)
	}
	state.visited[absPath] = true // Mark the main file as visited
	var includedASTs []*ast.AST

	for _, inc := range result.Includes {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Resolve path relative to the main file's directory
		includePath := inc.Filename
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}

		// Recursively load the included file from disk
		includedAST, err := state.loadRecursive(ctx, includePath, nil)
		if err != nil {
			return nil, fmt.Errorf("in file %s: %w", filename, err)
		}

		includedASTs = append(includedASTs, includedAST)
	}

	// Merge all ASTs
	mergeTimer := loadTimer.Child("ast.merging")
	merged := mergeASTs(result, includedASTs...)
	mergeTimer.End()
	return merged, nil
}

// loaderState tracks state during recursive loading.
type loaderState struct {
	visited map[string]bool // Absolute paths of files already loaded
	mu      sync.Mutex      // Protects visited map during concurrent loading
}

// loadRecursive recursively loads a file and all its includes.
// If timer is nil, a new timer will be created; otherwise the provided timer is used.
func (l *loaderState) loadRecursive(ctx context.Context, filename string, timer telemetry.Timer) (*ast.AST, error) {
	// Get absolute path for deduplication
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", filename, err)
	}

	// Read and parse the file
	// Use provided timer or create a new one
	var parseTimer telemetry.Timer
	if timer != nil {
		parseTimer = timer
	} else {
		parseTimer = telemetry.StartTimer(ctx, fmt.Sprintf("loader.parse %s", filepath.Base(filename)))
	}
	defer parseTimer.End()

	// Check if already visited (deduplication - same file included multiple times)
	// Lock to safely check and update the visited map during concurrent loading
	l.mu.Lock()
	if l.visited[absPath] {
		l.mu.Unlock()
		// Return empty AST - this file was already processed
		return &ast.AST{}, nil
	}
	l.visited[absPath] = true

	// Read file while holding lock to prevent TOCTOU race condition
	// This ensures atomic check-mark-read operation during concurrent loading
	// File I/O is relatively fast compared to parsing, which happens outside the lock
	data, err := os.ReadFile(filename)
	if err != nil {
		// Clean up visited map on read failure to allow retry
		delete(l.visited, absPath)
		l.mu.Unlock()
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}
	l.mu.Unlock()

	result, err := parser.ParseBytesWithFilename(ctx, filename, data)
	if err != nil {
		// Wrap parser errors for consistent formatting
		return nil, parser.NewParseError(filename, err)
	}

	// If no includes, return as-is
	if len(result.Includes) == 0 {
		result.Includes = nil // Clear includes since we're in follow mode
		return result, nil
	}

	// Recursively load all includes and merge
	baseDir := filepath.Dir(absPath)

	// Pre-allocate slice to preserve include order
	includedASTs := make([]*ast.AST, len(result.Includes))

	// Create child timers for all includes before spawning goroutines
	// This ensures they appear as siblings in the telemetry tree
	includeTimers := make([]telemetry.Timer, len(result.Includes))
	for i, inc := range result.Includes {
		includeTimers[i] = parseTimer.Child(fmt.Sprintf("loader.parse %s", filepath.Base(inc.Filename)))
	}

	// Use errgroup to load includes concurrently
	g, gctx := errgroup.WithContext(ctx)

	for i, inc := range result.Includes {
		// Capture loop variables for goroutine
		i := i
		inc := inc
		childTimer := includeTimers[i]

		g.Go(func() error {
			// Resolve path relative to the including file's directory
			includePath := inc.Filename
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(baseDir, includePath)
			}

			// Set the parent timer in context so parser creates nested timers
			childCtx := telemetry.WithParentTimer(gctx, childTimer)

			// Recursively load the included file with the pre-created timer
			includedAST, err := l.loadRecursive(childCtx, includePath, childTimer)
			if err != nil {
				return fmt.Errorf("in file %s: %w", filename, err)
			}

			includedASTs[i] = includedAST
			return nil
		})
	}

	// Wait for all includes to be loaded
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Merge all ASTs
	mergeTimer := parseTimer.Child("ast.merging")
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
		Includes:   nil,            // All includes resolved, so clear this
		Plugins:    main.Plugins,   // Start with main file plugins
		Pushtags:   main.Pushtags,  // Start with main file pushtags
		Poptags:    main.Poptags,   // Start with main file poptags
		Pushmetas:  main.Pushmetas, // Start with main file pushmetas
		Popmetas:   main.Popmetas,  // Start with main file popmetas
	}

	// Merge options: main file options override duplicates, but preserve unique options from includes
	// Build a map of main file option names for deduplication
	mainOptionsMap := make(map[string]bool)
	for _, opt := range main.Options {
		mainOptionsMap[opt.Name] = true
	}

	// Add options from included files (only if not overridden by main file)
	for _, inc := range included {
		for _, opt := range inc.Options {
			if !mainOptionsMap[opt.Name] {
				result.Options = append(result.Options, opt)
				mainOptionsMap[opt.Name] = true // Mark as added to avoid duplicates from multiple includes
			}
		}
	}

	// Add main file options last (these have precedence)
	result.Options = append(result.Options, main.Options...)

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
