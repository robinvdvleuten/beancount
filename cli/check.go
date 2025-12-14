package cli

import (
	"context"
	stdErrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

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
		renderer := NewErrorRenderer(sourceContent)
		formatted := renderer.Render(err)
		_, _ = fmt.Fprintln(ctx.Stderr, formatted)

		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")

		reportTelemetry()
		os.Exit(1)
	}

	l := ledger.New()
	if err := l.Process(runCtx, ast); err != nil {
		var validationErrors *ledger.ValidationErrors
		if stdErrors.As(err, &validationErrors) {
			renderer := NewErrorRenderer(sourceContent)
			formatted := renderer.RenderAll(validationErrors.Errors)
			_, _ = fmt.Fprintln(ctx.Stderr, formatted)

			_, _ = fmt.Fprintln(ctx.Stderr)
			printError(ctx.Stderr, fmt.Sprintf("%d validation error(s) found", len(validationErrors.Errors)))

			reportTelemetry()
			os.Exit(1)
		}
		return err
	}

	printSuccess(ctx.Stdout, "Check passed")

	return nil
}
