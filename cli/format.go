package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

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
		renderer := NewErrorRenderer(sourceContent)
		formatted := renderer.Render(err)
		_, _ = fmt.Fprint(ctx.Stderr, formatted)
		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")
		return NewCommandError(1)
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
