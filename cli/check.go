package cli

import (
	"context"
	stdErrors "errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
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

	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery())
	loadResult, err := cmd.File.LoadResult(runCtx, ldr)
	if err != nil {
		renderer := NewErrorRenderer(sourceContent)
		formatted := renderer.Render(err)
		_, _ = fmt.Fprintln(ctx.Stderr, formatted)

		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")

		reportTelemetry()
		return NewCommandError(1)
	}
	errorCount, err := checkLedger(runCtx, ctx.Stderr, loadResult, cmd.File.GetAbsoluteFilename(), sourceContent)
	if err != nil {
		return err
	}
	if errorCount > 0 {
		reportTelemetry()
		return NewCommandError(1)
	}

	printSuccess(ctx.Stdout, "Check passed")

	return nil
}

// checkLedger prints the load diagnostics, processes the loaded AST and
// prints its validation errors. sourceContent is mainFile's. It returns how
// many errors it printed.
func checkLedger(ctx context.Context, stderr io.Writer, loadResult *loader.LoadResult, mainFile string, sourceContent []byte) (int, error) {
	for _, warning := range diagnostic.Warnings(loadResult.Diagnostics) {
		printInfof(stderr, "%s", warning)
	}
	loadErrors := diagnostic.Errors(loadResult.Diagnostics)
	for _, loadErr := range loadErrors {
		// A syntax error in the main file is shown in its source context,
		// like a failed load.
		if syntaxErr, ok := loadErr.(*parser.ParseError); ok && syntaxErr.Pos.Filename == mainFile {
			_, _ = fmt.Fprintln(stderr, NewErrorRenderer(sourceContent).Render(syntaxErr))
			continue
		}
		// A positioned error already starts with "path:line:", which editors
		// jump to only when it starts the line.
		if _, ok := loadErr.(interface{ GetPosition() ast.Position }); ok {
			_, _ = fmt.Fprintln(stderr, errorStyle.Render(loadErr.Error()))
			continue
		}
		printError(stderr, loadErr.Error())
	}

	if err := ledger.New().Process(ctx, loadResult.AST); err != nil {
		var validationErrors *ledger.ValidationErrors
		if !stdErrors.As(err, &validationErrors) {
			return 0, err
		}
		renderer := NewErrorRenderer(sourceContent)
		_, _ = fmt.Fprintln(stderr, renderer.RenderAll(validationErrors.Errors))

		_, _ = fmt.Fprintln(stderr)
		total := len(validationErrors.Errors) + len(loadErrors)
		printError(stderr, fmt.Sprintf("%d validation error(s) found", total))
		return total, nil
	}
	return len(loadErrors), nil
}
