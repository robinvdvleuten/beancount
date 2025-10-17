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

	"github.com/robinvdvleuten/beancount/parser"
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
func (l *Loader) Load(ctx context.Context, filename string) (*parser.AST, error) {
	if !l.FollowIncludes {
		// Simple case: just parse the single file
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", filename, err)
		}
		ast, err := parser.ParseBytesWithFilename(ctx, filename, data)
		if err != nil {
			// Wrap parser errors for consistent formatting
			return nil, parser.NewParseError(filename, err)
		}
		return ast, nil
	}

	// Recursive loading with include resolution
	state := &loaderState{
		visited: make(map[string]bool),
	}

	return state.loadRecursive(ctx, filename)
}

// loaderState tracks state during recursive loading.
type loaderState struct {
	visited map[string]bool // Absolute paths of files already loaded
}

// loadRecursive recursively loads a file and all its includes.
func (l *loaderState) loadRecursive(ctx context.Context, filename string) (*parser.AST, error) {
	// Get absolute path for deduplication
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", filename, err)
	}

	// Check if already visited (deduplication - same file included multiple times)
	if l.visited[absPath] {
		// Return empty AST - this file was already processed
		return &parser.AST{}, nil
	}
	l.visited[absPath] = true

	// Read and parse the file
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	ast, err := parser.ParseBytesWithFilename(ctx, filename, data)
	if err != nil {
		// Wrap parser errors for consistent formatting
		return nil, parser.NewParseError(filename, err)
	}

	// If no includes, return as-is
	if len(ast.Includes) == 0 {
		ast.Includes = nil // Clear includes since we're in follow mode
		return ast, nil
	}

	// Recursively load all includes and merge
	baseDir := filepath.Dir(absPath)
	var includedASTs []*parser.AST

	for _, inc := range ast.Includes {
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
			return nil, fmt.Errorf("in file %s: %w", filename, err)
		}

		includedASTs = append(includedASTs, includedAST)
	}

	// Merge all ASTs
	merged := mergeASTs(ast, includedASTs...)
	return merged, nil
}

// mergeASTs combines a main AST with multiple included ASTs.
// The main AST's options take precedence over included files' options.
// All directives are combined and will be sorted by date by the parser.
func mergeASTs(main *parser.AST, included ...*parser.AST) *parser.AST {
	result := &parser.AST{
		Directives: make(parser.Directives, 0, len(main.Directives)),
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
	_ = parser.SortDirectives(result)

	return result
}
