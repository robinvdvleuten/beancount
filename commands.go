package beancount

import (
	"context"
	stdErrors "errors"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount/errors"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
)

type CheckCmd struct {
	File kong.NamedFileContentFlag `help:"Beancount input filename." arg:""`
}

func (cmd *CheckCmd) Run(ctx *kong.Context) error {
	// Create context for cancellation support
	runCtx := context.Background()

	// Load the input file and recursively resolve all includes
	ldr := loader.New(loader.WithFollowIncludes())
	ast, err := ldr.Load(runCtx, cmd.File.Filename)
	if err != nil {
		// Format parser errors consistently with ledger errors
		errFormatter := errors.NewTextFormatter(nil)
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
			errFormatter := errors.NewTextFormatter(f)

			// Format all errors
			formatted := errFormatter.FormatAll(validationErrors.Errors)
			_, _ = fmt.Fprint(ctx.Stderr, formatted)
			_, _ = fmt.Fprintln(ctx.Stderr) // Add final newline

			return fmt.Errorf("%d validation error(s) found", len(validationErrors.Errors))
		}
		return err
	}

	// Success
	_, _ = fmt.Fprintln(ctx.Stdout, "✓ Check passed")
	return nil
}

type FormatCmd struct {
	File           kong.NamedFileContentFlag `help:"Beancount input filename." arg:""`
	CurrencyColumn int                       `help:"Column for currency alignment (overrides prefix-width and num-width if set, auto if 0)." default:"0"`
	PrefixWidth    int                       `help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int                       `help:"Width for numbers (auto if 0)." default:"0"`
}

func (cmd *FormatCmd) Run(ctx *kong.Context) error {
	// Create context for cancellation support
	runCtx := context.Background()

	// Load only the single file (don't follow includes)
	ldr := loader.New()
	ast, err := ldr.Load(runCtx, cmd.File.Filename)
	if err != nil {
		// Format parser errors consistently
		errFormatter := errors.NewTextFormatter(nil)
		formatted := errFormatter.Format(err)
		_, _ = fmt.Fprint(ctx.Stderr, formatted)
		_, _ = fmt.Fprintln(ctx.Stderr)
		return fmt.Errorf("parse error")
	}

	// Read file contents for formatter (needs original source for comment preservation)
	contents, err := os.ReadFile(cmd.File.Filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
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
	if err := f.Format(runCtx, ast, contents, os.Stdout); err != nil {
		return err
	}

	return nil
}

type Commands struct {
	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
}
