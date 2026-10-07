// Package loader provides functionality for loading Beancount files with support for
// include directives. It can recursively resolve and merge multiple files into a
// single AST, handling relative paths and deduplication.
//
// The loader supports two modes of operation:
//   - Simple mode: parses a single raw source AST with include directives preserved
//   - Follow mode: recursively loads all included files and merges them into one AST
//
// When following includes, the loader resolves relative paths from the directory of
// the file containing the include directive, and deduplicates files that are included
// multiple times.
//
// Example usage:
//
//	// Load a single file without following includes
//	ldr := loader.New()
//	result, err := ldr.Load(ctx, loader.Source{Path: "main.beancount"})
//
//	// Load with recursive include resolution
//	ldr := loader.New(loader.WithFollowIncludes())
//	result, err := ldr.Load(ctx, loader.Source{Path: "main.beancount"})
//
//	// Load a ledger read from stdin
//	result, err := ldr.Load(ctx, loader.Stdin(data))
package loader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
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
	// Includes contains the absolute paths of all included files (not
	// including the root), in the order they were loaded. This is only
	// populated when FollowIncludes is enabled.
	Includes []string
	// Diagnostics contains non-fatal warnings produced while loading.
	Diagnostics []error
	// Sources holds the text of every file read, keyed by the filename its
	// positions carry, so an error can be shown in its own file's context.
	Sources map[string][]byte
}

// errorAt returns a load error's text: its Error line, then the message.
func errorAt(pos ast.Position, message string) string {
	return fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, message)
}

// IncludedOptionWarning reports an option ignored because it came from an included file.
type IncludedOptionWarning struct {
	Option *ast.Option
}

func (w *IncludedOptionWarning) Error() string { return errorAt(w.Option.Position(), w.Message()) }

// Kind names the kind of warning.
func (w *IncludedOptionWarning) Kind() string { return "IncludedOptionWarning" }

// Message is the warning's text without its Error line.
func (w *IncludedOptionWarning) Message() string {
	return fmt.Sprintf("option %q from included file is ignored", w.Option.Name.Value)
}

func (w *IncludedOptionWarning) Severity() diagnostic.Severity {
	return diagnostic.SeverityWarning
}

func (w *IncludedOptionWarning) GetPosition() ast.Position { return w.Option.Position() }

// IncludeGlobNoMatchError reports an include that matched no files: an
// unmatched glob or, since beancount expands every include as a glob, a
// missing file.
type IncludeGlobNoMatchError struct {
	Include *ast.Include
}

func (e *IncludeGlobNoMatchError) Error() string { return errorAt(e.Include.Position(), e.Message()) }

// Kind names the kind of error.
func (e *IncludeGlobNoMatchError) Kind() string { return "IncludeGlobNoMatchError" }

// Message is the error's text without its Error line.
func (e *IncludeGlobNoMatchError) Message() string {
	return fmt.Sprintf("File glob %q does not match any files", e.Include.Filename.Value)
}

// Severity is fatal: official beancount reports an unmatched include glob as an error.
func (e *IncludeGlobNoMatchError) Severity() diagnostic.Severity { return diagnostic.SeverityError }

func (e *IncludeGlobNoMatchError) GetPosition() ast.Position { return e.Include.Position() }

// DuplicateIncludeError reports an include of a file that is already loaded,
// such as an include cycle or two files including the same one. The file is
// loaded only once.
type DuplicateIncludeError struct {
	Include *ast.Include
	Path    string // The included path, relative to the including file
}

func (e *DuplicateIncludeError) Error() string { return errorAt(e.Include.Position(), e.Message()) }

// Kind names the kind of error.
func (e *DuplicateIncludeError) Kind() string { return "DuplicateIncludeError" }

// Message is the error's text without its Error line.
func (e *DuplicateIncludeError) Message() string {
	return fmt.Sprintf("Duplicate filename parsed: %q", e.Path)
}

// Severity is fatal: official beancount reports a duplicate include as an error.
func (e *DuplicateIncludeError) Severity() diagnostic.Severity { return diagnostic.SeverityError }

func (e *DuplicateIncludeError) GetPosition() ast.Position { return e.Include.Position() }

// DocumentRootError reports a documents option pointing at a missing directory.
type DocumentRootError struct {
	Option *ast.Option
	Dir    string
}

func (e *DocumentRootError) Error() string { return errorAt(e.Option.Position(), e.Message()) }

// Kind names the kind of error.
func (e *DocumentRootError) Kind() string { return "DocumentRootError" }

// Message is the error's text without its Error line.
func (e *DocumentRootError) Message() string {
	return fmt.Sprintf("Document root '%s' does not exist", e.Dir)
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

	// SyntaxRecovery keeps loading past syntax errors, like beancount: each
	// one becomes a diagnostic and drops only the directive it is in.
	// Without it, the first syntax error fails the load.
	SyntaxRecovery bool
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

// WithSyntaxRecovery keeps loading past syntax errors, reporting them as
// diagnostics (see Loader.SyntaxRecovery).
func WithSyntaxRecovery() Option {
	return func(l *Loader) {
		l.SyntaxRecovery = true
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

// Source is what Load reads: a file, or text that stands for one.
type Source struct {
	// Path locates the source: Load reads the file there when Data is
	// nil, and relative include and documents paths resolve against its
	// directory.
	Path string
	// Data is the source text, read instead of the file at Path. Its
	// positions carry Path all the same.
	Data []byte
}

// Stdin returns the source of a ledger read from standard input. Like
// bean-check /dev/stdin, it is loaded as the file /dev/stdin: relative
// include and documents paths resolve against /dev, and its positions carry
// /dev/stdin.
func Stdin(data []byte) Source {
	return Source{Path: "/dev/stdin", Data: data}
}

// read returns the source text: Data, or else the file at Path.
func (s Source) read() ([]byte, error) {
	if s.Data != nil {
		return s.Data, nil
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", s.Path, err)
	}
	return data, nil
}

// Load parses a source with optional recursive include resolution. Every
// option applies alike to a file and to text given as Data.
func (l *Loader) Load(ctx context.Context, src Source) (*LoadResult, error) {
	// Extract telemetry collector from context
	collector := telemetry.FromContext(ctx)

	// Get absolute path for the root file
	absPath, err := filepath.Abs(src.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", src.Path, err)
	}

	if !l.FollowIncludes {
		// Simple case: just parse the single file
		parseTimer := collector.Start(fmt.Sprintf("loader.parse %s", filepath.Base(src.Path)))
		defer parseTimer.End()

		data, err := src.read()
		if err != nil {
			return nil, err
		}
		result, diagnostics, err := parseFile(ctx, src.Path, data, l.SyntaxRecovery)
		if err != nil {
			return nil, err
		}
		if l.DiscoverDocuments {
			diagnostics = append(diagnostics, discoverDocuments(result, absPath)...)
		}
		return &LoadResult{
			AST:         result,
			Root:        absPath,
			Includes:    nil,
			Diagnostics: diagnostics,
			Sources:     map[string][]byte{src.Path: data},
		}, nil
	}

	// Recursive loading with include resolution
	// Use root timer for hierarchy if available, otherwise create flat timers
	rootTimer := telemetry.RootTimerFromContext(ctx)
	state := &loaderState{
		visited:        make(map[string]bool),
		sources:        make(map[string][]byte),
		collector:      collector,
		rootTimer:      rootTimer,
		root:           absPath,
		syntaxRecovery: l.SyntaxRecovery,
	}

	ast, err := state.loadRecursive(ctx, src)
	if err != nil {
		return nil, err
	}

	// The included files, in the order they were loaded: the root is first.
	includes := state.order[1:]

	if l.DiscoverDocuments {
		state.diagnostics = append(state.diagnostics, discoverDocuments(ast, absPath)...)
	}

	return &LoadResult{
		AST:         ast,
		Root:        absPath,
		Includes:    includes,
		Diagnostics: state.diagnostics,
		Sources:     state.sources,
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
			accounts = mentionedAccounts(tree)
		}

		// Like os.walk, the walk follows a root that is a symlink (the
		// trailing separator makes WalkDir resolve it) and keeps the root
		// as written in each path, but does not descend into a symlinked
		// directory inside the tree, nor take one for a file.
		_ = filepath.WalkDir(dir+string(filepath.Separator), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				if target, err := os.Stat(path); err == nil && target.IsDir() {
					return nil
				}
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

// loaderState tracks state during recursive loading.
type loaderState struct {
	visited        map[string]bool     // Absolute paths of files already loaded
	order          []string            // The same paths, in the order they were loaded
	sources        map[string][]byte   // Text of each file read, by the filename its positions carry
	collector      telemetry.Collector // Telemetry collector for tracking load operations
	rootTimer      telemetry.Timer     // Root check timer from context
	root           string
	diagnostics    []error
	syntaxRecovery bool
}

// parseFile parses one file. With recovery, its syntax errors come back as
// diagnostics alongside the AST of the directives that parsed; without, the
// first one that drops a directive fails the load, and those that keep it
// are left out, since the AST holds the whole source.
func parseFile(ctx context.Context, filename string, data []byte, recovery bool) (*ast.AST, []error, error) {
	tree, err := parser.ParseBytesWithFilename(ctx, filename, data)
	var syntaxErrs parser.ParseErrors
	if errors.As(err, &syntaxErrs) {
		if !recovery {
			if first := syntaxErrs.FirstDropping(); first != nil {
				return nil, nil, first
			}
			return tree, nil, nil
		}
		diagnostics := make([]error, len(syntaxErrs))
		for i, syntaxErr := range syntaxErrs {
			diagnostics[i] = syntaxErr
		}
		return tree, diagnostics, nil
	}
	if err != nil {
		return nil, nil, parser.NewParseErrorWithSource(filename, err, data)
	}
	return tree, nil, nil
}

// loadRecursive recursively loads a file and all its includes.
// When l.rootTimer is set, creates hierarchical child timers.
// When l.rootTimer is nil, creates flat root-level timers.
func (l *loaderState) loadRecursive(ctx context.Context, src Source) (*ast.AST, error) {
	filename := src.Path
	// Get absolute path for deduplication
	absPath, err := filepath.Abs(src.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", src.Path, err)
	}

	// Check if already visited (deduplication - same file included multiple times)
	if l.visited[absPath] {
		// Return empty AST - this file was already processed
		return &ast.AST{}, nil
	}
	l.visited[absPath] = true
	l.order = append(l.order, absPath)

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
	data, err := src.read()
	if err != nil {
		parseTimer.End()
		loadTimer.End()
		return nil, err
	}
	l.sources[filename] = data

	result, syntaxErrs, err := parseFile(ctx, filename, data, l.syntaxRecovery)
	parseTimer.End()

	if err != nil {
		loadTimer.End()
		return nil, err
	}
	l.diagnostics = append(l.diagnostics, syntaxErrs...)

	// Unbalanced pushes and pops are non-fatal load errors, per file.
	pushPopErrors, err := prepareLoadedAST(result)
	if err != nil {
		loadTimer.End()
		return nil, err
	}
	l.diagnostics = append(l.diagnostics, pushPopErrors...)

	// Like beancount, an included file's options are checked and then
	// ignored: an invalid one is an error, a valid one a warning.
	if absPath != l.root {
		for _, option := range result.Options {
			if errs := config.CheckOption(option); len(errs) > 0 {
				l.diagnostics = append(l.diagnostics, errs...)
				continue
			}
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

		// Like beancount, expand every include as a glob relative to the
		// including file's directory, so a missing plain path is a no-match
		// load error too.
		includePath := inc.Filename.Value
		resolvedPaths := globInclude(baseDir, includePath)
		if len(resolvedPaths) == 0 {
			l.diagnostics = append(l.diagnostics, &IncludeGlobNoMatchError{Include: inc})
		}

		for _, path := range resolvedPaths {
			if absInclude, err := filepath.Abs(path); err == nil && l.visited[absInclude] {
				shown := path
				if rel, err := filepath.Rel(baseDir, path); err == nil {
					shown = rel
				}
				l.diagnostics = append(l.diagnostics, &DuplicateIncludeError{Include: inc, Path: shown})
				continue
			}

			// Recursively load the included file
			includedAST, err := l.loadRecursive(ctx, Source{Path: path})
			if err != nil {
				mergeTimer.End()
				// Don't wrap ParseError - it already contains full path information
				// Just propagate the error up the include chain
				return nil, err
			}

			includedASTs = append(includedASTs, includedAST)
		}
	}

	// Merge ASTs
	merged := mergeASTs(result, includedASTs...)
	mergeTimer.End()

	return merged, nil
}

// mergeASTs combines a main AST with multiple included ASTs.
// Like beancount v2, only the main AST's options and plugins count: an
// included file's are dropped. All directives are combined and sorted for
// ledger processing.
func mergeASTs(main *ast.AST, included ...*ast.AST) *ast.AST {
	result := &ast.AST{
		Directives: make(ast.Directives, 0, len(main.Directives)),
		Options:    main.Options,   // Main file options take precedence
		Includes:   nil,            // All includes resolved, so clear this
		Plugins:    main.Plugins,   // Included files' plugins do not run
		Pushtags:   main.Pushtags,  // Start with main file pushtags
		Poptags:    main.Poptags,   // Start with main file poptags
		Pushmetas:  main.Pushmetas, // Start with main file pushmetas
		Popmetas:   main.Popmetas,  // Start with main file popmetas
	}

	// Add main file directives
	result.Directives = append(result.Directives, main.Directives...)

	// Add directives, and the amounts of dropped ones, from all included files
	result.DroppedAmounts = slices.Clone(main.DroppedAmounts)
	for _, inc := range included {
		result.Directives = append(result.Directives, inc.Directives...)
		result.DroppedAmounts = append(result.DroppedAmounts, inc.DroppedAmounts...)
	}

	_ = ast.SortDirectives(result)
	ast.MarkPushPopDirectivesApplied(result)

	return result
}

func prepareLoadedAST(tree *ast.AST) ([]error, error) {
	pushPopErrors := ast.ApplyPushPopDirectives(tree)
	return pushPopErrors, ast.SortDirectives(tree)
}

// mentionedAccounts returns every account a directive of tree names.
func mentionedAccounts(tree *ast.AST) map[string]bool {
	accounts := make(map[string]bool)
	for _, directive := range tree.Directives {
		if d, ok := directive.(ast.WithAccounts); ok {
			for _, account := range d.Accounts() {
				accounts[string(account)] = true
			}
		}
	}
	return accounts
}
