package beancount

import (
	"errors"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
)

type CheckCmd struct {
	File kong.NamedFileContentFlag `help:"Beancount input filename." arg:""`
}

func (cmd *CheckCmd) Run(ctx *kong.Context) error {
	// Parse the input file with filename for better error reporting
	ast, err := parser.ParseBytesWithFilename(cmd.File.Filename, cmd.File.Contents)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Create a new ledger and process the AST
	l := ledger.New()
	if err := l.Process(ast); err != nil {
		// Print all validation errors
		var validationErrors *ledger.ValidationErrors
		if errors.As(err, &validationErrors) {
			// Create a formatter for rendering transaction context
			f := formatter.New()

			for i, e := range validationErrors.Errors {
				// Check if this is an error with context formatting support
				if balErr, ok := e.(*ledger.TransactionNotBalancedError); ok && balErr.Transaction != nil {
					// Use the enhanced formatting with transaction context
					_, _ = fmt.Fprint(ctx.Stderr, balErr.FormatWithContext(f))
				} else if accErr, ok := e.(*ledger.AccountNotOpenError); ok && accErr.Directive != nil {
					// Use the enhanced formatting with directive context
					_, _ = fmt.Fprint(ctx.Stderr, accErr.FormatWithContext(f))
				} else {
					// For other errors, use standard formatting
					_, _ = fmt.Fprintf(ctx.Stderr, "%s\n", e)
				}

				// Add blank line between errors (but not after the last one)
				if i < len(validationErrors.Errors)-1 {
					_, _ = fmt.Fprintln(ctx.Stderr)
				}
			}
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
	// Parse the input file with filename for better error reporting
	ast, err := parser.ParseBytesWithFilename(cmd.File.Filename, cmd.File.Contents)
	if err != nil {
		return err
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
	if err := f.Format(ast, cmd.File.Contents, os.Stdout); err != nil {
		return err
	}

	return nil
}

type Commands struct {
	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
}
