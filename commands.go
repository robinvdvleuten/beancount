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
	File []byte `help:"Beancount input filename." arg:"" type:"filecontent"`
}

func (cmd *CheckCmd) Run(ctx *kong.Context) error {
	// Parse the input file
	ast, err := parser.ParseBytes(cmd.File)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Create a new ledger and process the AST
	l := ledger.New()
	if err := l.Process(ast); err != nil {
		// Print all validation errors
		var validationErrors *ledger.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, e := range validationErrors.Errors {
				_, _ = fmt.Fprintf(ctx.Stderr, "ERROR: %s\n", e)
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
	File           []byte `help:"Beancount input filename." arg:"" type:"filecontent"`
	CurrencyColumn int    `help:"Column for currency alignment (overrides prefix-width and num-width if set, auto if 0)." default:"0"`
	PrefixWidth    int    `help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int    `help:"Width for numbers (auto if 0)." default:"0"`
}

func (cmd *FormatCmd) Run(ctx *kong.Context) error {
	// Parse the input file
	ast, err := parser.ParseBytes(cmd.File)
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
	if err := f.Format(ast, cmd.File, os.Stdout); err != nil {
		return err
	}

	return nil
}

type Commands struct {
	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
}
