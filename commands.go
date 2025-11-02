package main

import (
	"context"
	stdErrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/errors"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
	"github.com/robinvdvleuten/beancount/web"
)

// FileOrStdin is a flag value that accepts either a file path or "-" for stdin.
// When "-" is provided, it reads from os.Stdin and sets Contents.
// When a file path is provided, it just validates the filename exists.
// For stdin input, Filename is set to "<stdin>" and Contents is populated.
// For file input, Filename is set and Contents is nil (will be read by loader).
//
// Example usage:
//
//	type MyCmd struct {
//	    Input FileOrStdin `arg:"" help:"Input file or - for stdin"`
//	}
//
//	// User runs: myapp -
//	// Result: cmd.Input.Filename = "<stdin>", cmd.Input.Contents = [stdin data]
//
//	// User runs: myapp file.txt
//	// Result: cmd.Input.Filename = "file.txt", cmd.Input.Contents = nil
type FileOrStdin struct {
	Filename string
	Contents []byte
}

// Decode implements kong.MapperValue to customize how the flag value is decoded.
// For stdin ("-" or empty), reads from os.Stdin.
// For files, just validates the filename exists but doesn't read contents.
func (f *FileOrStdin) Decode(ctx *kong.DecodeContext) error {
	var filename string
	if err := ctx.Scan.PopValueInto("filename", &filename); err != nil {
		return err
	}

	if filename == "-" || filename == "" {
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		f.Filename = "<stdin>"
		f.Contents = contents
		return nil
	}

	if _, err := os.Stat(filename); err != nil {
		return err
	}
	f.Filename = filename
	f.Contents = nil

	return nil
}

// EnsureContents ensures that Contents is populated.
// If Filename is empty (unset), reads from stdin.
// This handles the case where the argument is optional and not provided.
func (f *FileOrStdin) EnsureContents() error {
	if f.Filename == "" {
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		f.Filename = "<stdin>"
		f.Contents = contents
	}
	return nil
}

// GetSourceContent returns the source content for error formatting.
// For stdin, returns the already-read Contents.
// For files, reads and returns the file contents.
func (f *FileOrStdin) GetSourceContent() ([]byte, error) {
	if f.Filename == "<stdin>" {
		return f.Contents, nil
	}
	return os.ReadFile(f.Filename)
}

// GetAbsoluteFilename returns the absolute path for the file.
// For stdin, returns "<stdin>".
// For files, resolves to absolute path (falls back to original on error).
func (f *FileOrStdin) GetAbsoluteFilename() string {
	if f.Filename == "<stdin>" {
		return f.Filename
	}
	absPath, err := filepath.Abs(f.Filename)
	if err != nil {
		return f.Filename
	}
	return absPath
}

// LoadAST loads the AST using the appropriate loader method.
// For stdin, uses LoadBytes with the already-read Contents.
// For files, uses Load which handles includes properly.
func (f *FileOrStdin) LoadAST(ctx context.Context, ldr *loader.Loader) (*ast.AST, error) {
	absFilename := f.GetAbsoluteFilename()

	if f.Filename == "<stdin>" {
		return ldr.LoadBytes(ctx, absFilename, f.Contents)
	}
	return ldr.Load(ctx, absFilename)
}

// Globals defines global flags available to all commands.
type Globals struct {
	Telemetry bool `help:"Show timing telemetry for operations."`
}

type CheckCmd struct {
	File FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
}

func (cmd *CheckCmd) Run(ctx *kong.Context, globals *Globals) error {
	if err := cmd.File.EnsureContents(); err != nil {
		return err
	}

	runCtx := context.Background()

	var collector telemetry.Collector
	var checkTimer telemetry.Timer
	var once sync.Once

	reportTelemetry := func() {
		once.Do(func() {
			if collector != nil {
				checkTimer.End()
				_, _ = fmt.Fprintln(ctx.Stderr)
				collector.Report(ctx.Stderr)
			}
		})
	}

	if globals.Telemetry {
		collector = telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)

		checkTimer = collector.Start(fmt.Sprintf("check %s", filepath.Base(cmd.File.Filename)))
		runCtx = telemetry.WithRootTimer(runCtx, checkTimer)

		defer reportTelemetry()
	}

	sourceContent, err := cmd.File.GetSourceContent()
	if err != nil {
		return fmt.Errorf("failed to read file for error context: %w", err)
	}

	ldr := loader.New(loader.WithFollowIncludes())
	ast, err := cmd.File.LoadAST(runCtx, ldr)
	if err != nil {
		errFormatter := errors.NewTextFormatter(nil, errors.WithSource(sourceContent))
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprintln(ctx.Stderr, formatted)

		_, _ = fmt.Fprintln(ctx.Stderr)
		_, _ = fmt.Fprintln(ctx.Stderr, "parse error")

		reportTelemetry()
		os.Exit(1)
	}

	l := ledger.New()
	if err := l.Process(runCtx, ast); err != nil {
		var validationErrors *ledger.ValidationErrors
		if stdErrors.As(err, &validationErrors) {
			f := formatter.New()
			errFormatter := errors.NewTextFormatter(f, errors.WithSource(sourceContent))

			formatted := errFormatter.FormatAll(validationErrors.Errors)
			_, _ = fmt.Fprintln(ctx.Stderr, formatted)

			_, _ = fmt.Fprintln(ctx.Stderr)
			_, _ = fmt.Fprintf(ctx.Stderr, "%d validation error(s) found\n", len(validationErrors.Errors))

			reportTelemetry()
			os.Exit(1)
		}
		return err
	}

	_, _ = fmt.Fprintln(ctx.Stdout, "✓ Check passed")

	return nil
}

type FormatCmd struct {
	File           FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
	CurrencyColumn int         `help:"Column for currency alignment (auto-calculated from content if 0, overrides prefix-width and num-width if set)." default:"0"`
	PrefixWidth    int         `help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int         `help:"Width for numbers (auto if 0)." default:"0"`
}

func (cmd *FormatCmd) Run(ctx *kong.Context, globals *Globals) error {
	if err := cmd.File.EnsureContents(); err != nil {
		return err
	}

	runCtx := context.Background()

	var collector telemetry.Collector
	if globals.Telemetry {
		collector = telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)

		defer func() {
			_, _ = fmt.Fprintln(ctx.Stderr)
			collector.Report(ctx.Stderr)
		}()
	}

	sourceContent, err := cmd.File.GetSourceContent()
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	ldr := loader.New()
	ast, err := cmd.File.LoadAST(runCtx, ldr)
	if err != nil {
		errFormatter := errors.NewTextFormatter(nil, errors.WithSource(sourceContent))
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprint(ctx.Stderr, formatted)
		_, _ = fmt.Fprintln(ctx.Stderr)
		return fmt.Errorf("parse error")
	}

	var opts []formatter.Option
	if cmd.CurrencyColumn > 0 {
		opts = append(opts, formatter.WithCurrencyColumn(cmd.CurrencyColumn))
	}
	if cmd.PrefixWidth > 0 {
		opts = append(opts, formatter.WithPrefixWidth(cmd.PrefixWidth))
	}
	if cmd.NumWidth > 0 {
		opts = append(opts, formatter.WithNumWidth(cmd.NumWidth))
	}
	f := formatter.New(opts...)

	if err := f.Format(runCtx, ast, sourceContent, os.Stdout); err != nil {
		return err
	}

	return nil
}

type WebCmd struct {
	File FileOrStdin `help:"Beancount ledger file to serve." arg:"" optional:""`
	Port int         `help:"Port to listen on." default:"8080"`
}

func (cmd *WebCmd) Run(ctx *kong.Context, globals *Globals) error {
	runCtx := context.Background()

	if globals.Telemetry {
		collector := telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)

		defer func() {
			_, _ = fmt.Fprintln(ctx.Stderr)
			collector.Report(ctx.Stderr)
		}()
	}

	var ledgerFile string
	if cmd.File.Filename != "" {
		ledgerFile = cmd.File.GetAbsoluteFilename()
	}

	server := web.NewWithVersion(cmd.Port, ledgerFile, Version, CommitSHA)

	_, _ = fmt.Fprintf(ctx.Stdout, "Starting server on %s:%d\n", server.Host, cmd.Port)
	if ledgerFile != "" {
		_, _ = fmt.Fprintf(ctx.Stdout, "Serving ledger: %s\n", ledgerFile)
	}

	return server.Start(runCtx)
}

type Commands struct {
	Globals

	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
	Web    WebCmd    `cmd:"" help:"Start a web server."`
}
