package main

import (
	"context"
	stdErrors "errors"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount/errors"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/output"
	"github.com/robinvdvleuten/beancount/telemetry"
)

// FileContentFlag is a custom flag that loads file contents from a path or STDIN.
// It supports "-" as a special filename that reads from standard input.
type FileContentFlag struct {
	Filename string
	Contents []byte
}

// Decode implements kong.MapperValue to customize file loading with STDIN support.
func (f *FileContentFlag) Decode(ctx *kong.DecodeContext) error {
	var filename string
	err := ctx.Scan.PopValueInto("filename", &filename)
	if err != nil {
		return err
	}

	// Allow unsetting
	if filename == "" {
		*f = FileContentFlag{}
		return nil
	}

	// Handle STDIN
	if filename == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		f.Contents = data
		f.Filename = filename
		return nil
	}

	// Handle regular files
	filename = kong.ExpandPath(filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to open %q: %w", filename, err)
	}
	f.Contents = data
	f.Filename = filename
	return nil
}

// Globals defines global flags and context available to all commands.
type Globals struct {
	Telemetry bool `help:"Show timing telemetry for operations."`

	// OutStyles provides styling for stdout output (injected by main)
	OutStyles *output.Styles `kong:"-"`
	// ErrStyles provides styling for stderr output (injected by main)
	ErrStyles *output.Styles `kong:"-"`
}

type CheckCmd struct {
	File FileContentFlag `help:"Beancount input filename." arg:""`
}

func (cmd *CheckCmd) Run(ctx *kong.Context, globals *Globals) error {
	// Create context for cancellation support
	runCtx := context.Background()

	// Create telemetry collector if flag is set
	var collector telemetry.Collector
	if globals.Telemetry {
		collector = telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)
	}

	// Load the input file and recursively resolve all includes
	// Use LoadBytes to support STDIN and avoid re-reading from disk
	ldr := loader.New(loader.WithFollowIncludes())
	ast, err := ldr.LoadBytes(runCtx, cmd.File.Filename, cmd.File.Contents)
	if err != nil {
		// Format parser errors consistently with ledger errors
		errFormatter := errors.NewTextFormatter(nil, globals.ErrStyles)
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprint(ctx.Stderr, formatted)
		_, _ = fmt.Fprintln(ctx.Stderr)
		return fmt.Errorf("parse error")
	}

	// Create a new ledger and process the AST
	l := ledger.New()
	if err := l.Process(runCtx, ast); err != nil {
		// Print all validation errors
		var validationErrors *ledger.ValidationErrors
		if stdErrors.As(err, &validationErrors) {
			// Create a formatter for rendering errors
			f := formatter.New()
			errFormatter := errors.NewTextFormatter(f, globals.ErrStyles)

			// Format all errors
			formatted := errFormatter.FormatAll(validationErrors.Errors)
			_, _ = fmt.Fprint(ctx.Stderr, formatted)
			_, _ = fmt.Fprintln(ctx.Stderr) // Add final newline

			errCount := globals.ErrStyles.Error(fmt.Sprintf("%d validation error(s) found", len(validationErrors.Errors)))
			return fmt.Errorf("%s", errCount)
		}
		return err
	}

	// Success
	_, _ = fmt.Fprintln(ctx.Stdout, globals.OutStyles.Success("✓ Check passed"))

	// Output telemetry report if enabled
	if globals.Telemetry {
		_, _ = fmt.Fprintln(ctx.Stderr)
		collector.Report(ctx.Stderr, globals.ErrStyles)
	}

	return nil
}

type FormatCmd struct {
	File           FileContentFlag `help:"Beancount input filename." arg:""`
	CurrencyColumn int             `help:"Column for currency alignment (overrides prefix-width and num-width if set, auto if 0)." default:"0"`
	PrefixWidth    int             `help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int             `help:"Width for numbers (auto if 0)." default:"0"`
}

func (cmd *FormatCmd) Run(ctx *kong.Context, globals *Globals) error {
	// Create context for cancellation support
	runCtx := context.Background()

	// Create telemetry collector if flag is set
	var collector telemetry.Collector
	if globals.Telemetry {
		collector = telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)
	}

	// Load only the single file (don't follow includes)
	// Use LoadBytes to support STDIN and avoid re-reading from disk
	ldr := loader.New()
	ast, err := ldr.LoadBytes(runCtx, cmd.File.Filename, cmd.File.Contents)
	if err != nil {
		// Format parser errors consistently
		errFormatter := errors.NewTextFormatter(nil, globals.ErrStyles)
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprint(ctx.Stderr, formatted)
		_, _ = fmt.Fprintln(ctx.Stderr)
		return fmt.Errorf("parse error")
	}

	// Use the already-loaded file contents (supports STDIN and avoids re-reading)
	contents := cmd.File.Contents

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
	if err := f.Format(runCtx, ast, contents, ctx.Stdout); err != nil {
		return err
	}

	// Output telemetry report if enabled
	if globals.Telemetry {
		_, _ = fmt.Fprintln(ctx.Stderr)
		collector.Report(ctx.Stderr, globals.ErrStyles)
	}

	return nil
}

type Commands struct {
	Globals // Embed globals to make --telemetry available at root level

	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
}
