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
	Contents []byte // Only populated for stdin, nil for files
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
		// Read from stdin
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		f.Filename = "<stdin>"
		f.Contents = contents
		return nil
	}

	// For files, just validate the file exists but don't read contents yet
	if _, err := os.Stat(filename); err != nil {
		return err
	}
	f.Filename = filename
	f.Contents = nil // Will be read by loader

	return nil
}

// Globals defines global flags available to all commands.
type Globals struct {
	Telemetry bool `help:"Show timing telemetry for operations."`
}

type CheckCmd struct {
	File FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
}

func (cmd *CheckCmd) Run(ctx *kong.Context, globals *Globals) error {
	// If no filename provided, read from stdin
	if cmd.File.Filename == "" {
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		cmd.File.Filename = "<stdin>"
		cmd.File.Contents = contents
	}

	// Create context for cancellation support
	runCtx := context.Background()

	// Create telemetry collector if flag is set
	var collector telemetry.Collector
	var checkTimer telemetry.Timer
	var once sync.Once

	// reportTelemetry prints telemetry if enabled (idempotent via sync.Once)
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

		// Create root check timer
		checkTimer = collector.Start(fmt.Sprintf("check %s", filepath.Base(cmd.File.Filename)))
		runCtx = telemetry.WithRootTimer(runCtx, checkTimer)

		// Defer telemetry report for success path (error paths call manually before exit)
		defer reportTelemetry()
	}

	// Load input: use LoadBytes for stdin, Load for files
	var ast *ast.AST
	var err error
	if cmd.File.Filename == "<stdin>" {
		// Stdin: contents already read by FileOrStdin.Decode
		ldr := loader.New(loader.WithFollowIncludes())
		ast, err = ldr.LoadBytes(runCtx, cmd.File.Filename, cmd.File.Contents)
	} else {
		// File: use Load which handles includes properly
		ldr := loader.New(loader.WithFollowIncludes())
		ast, err = ldr.Load(runCtx, cmd.File.Filename)
	}
	if err != nil {
		// Format parser errors consistently with ledger errors
		errFormatter := errors.NewTextFormatter(nil)
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprintln(ctx.Stderr, formatted)

		// Print blank line and error summary before telemetry
		_, _ = fmt.Fprintln(ctx.Stderr)
		_, _ = fmt.Fprintln(ctx.Stderr, "parse error")

		// Print telemetry before exit
		reportTelemetry()
		os.Exit(1)
	}

	// Create a new ledger and process the AST
	l := ledger.New()
	if err := l.Process(runCtx, ast); err != nil {
		// Print all validation errors
		var validationErrors *ledger.ValidationErrors
		if stdErrors.As(err, &validationErrors) {
			// Create a formatter for rendering errors
			f := formatter.New()
			errFormatter := errors.NewTextFormatter(f)

			// Format all errors
			formatted := errFormatter.FormatAll(validationErrors.Errors)
			_, _ = fmt.Fprintln(ctx.Stderr, formatted)

			// Print blank line and error summary before telemetry
			_, _ = fmt.Fprintln(ctx.Stderr)
			_, _ = fmt.Fprintf(ctx.Stderr, "%d validation error(s) found\n", len(validationErrors.Errors))

			// Print telemetry before exit
			reportTelemetry()
			os.Exit(1)
		}
		return err
	}

	// Success
	_, _ = fmt.Fprintln(ctx.Stdout, "✓ Check passed")

	return nil
}

type FormatCmd struct {
	File           FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
	CurrencyColumn int         `help:"Column for currency alignment (overrides prefix-width and num-width if set, auto if 0)." default:"0"`
	PrefixWidth    int         `help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int         `help:"Width for numbers (auto if 0)." default:"0"`
}

func (cmd *FormatCmd) Run(ctx *kong.Context, globals *Globals) error {
	// If no filename provided, read from stdin
	if cmd.File.Filename == "" {
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		cmd.File.Filename = "<stdin>"
		cmd.File.Contents = contents
	}

	// Create context for cancellation support
	runCtx := context.Background()

	// Create telemetry collector if flag is set
	var collector telemetry.Collector
	if globals.Telemetry {
		collector = telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)

		// Defer telemetry report - runs regardless of early returns
		defer func() {
			_, _ = fmt.Fprintln(ctx.Stderr)
			collector.Report(ctx.Stderr)
		}()
	}

	// Load input: use LoadBytes for stdin, Load for files
	var ast *ast.AST
	var err error
	if cmd.File.Filename == "<stdin>" {
		// Stdin: contents already read by FileOrStdin.Decode
		ldr := loader.New()
		ast, err = ldr.LoadBytes(runCtx, cmd.File.Filename, cmd.File.Contents)
	} else {
		// File: use Load (though format doesn't follow includes, this is consistent)
		ldr := loader.New()
		ast, err = ldr.Load(runCtx, cmd.File.Filename)
	}
	if err != nil {
		// Format parser errors consistently
		errFormatter := errors.NewTextFormatter(nil)
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprint(ctx.Stderr, formatted)
		_, _ = fmt.Fprintln(ctx.Stderr)
		return fmt.Errorf("parse error")
	}

	// Create formatter with options
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

	// Format and output to stdout
	var contents []byte
	if cmd.File.Filename == "<stdin>" {
		// Contents already read for stdin
		contents = cmd.File.Contents
	} else {
		// Read file contents for formatter (needs original source for comment preservation)
		var err error
		contents, err = os.ReadFile(cmd.File.Filename)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
	}
	if err := f.Format(runCtx, ast, contents, os.Stdout); err != nil {
		return err
	}

	return nil
}

type Commands struct {
	Globals // Embed globals to make --telemetry available at root level

	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
}
