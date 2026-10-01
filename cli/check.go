package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sync"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/printer"
	"github.com/robinvdvleuten/beancount/telemetry"
)

type CheckCmd struct {
	File FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
	JSON bool        `name:"json" help:"Output errors as JSON."`
}

func (cmd *CheckCmd) Run(ctx *kong.Context, globals *Globals) error {
	if err := cmd.File.EnsureContents(); err != nil {
		if cmd.JSON && errors.Is(err, fs.ErrNotExist) {
			return writeJSONErrors(ctx.Stdout, []error{newMissingFileError(cmd.File.GetAbsoluteFilename())})
		}
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

	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery())
	loadResult, err := cmd.File.LoadResult(runCtx, ldr)
	if cmd.JSON {
		return cmd.writeJSON(runCtx, ctx.Stdout, loadResult, err)
	}
	if err != nil {
		sourceContent, readErr := cmd.File.GetSourceContent()
		if readErr != nil {
			return fmt.Errorf("failed to read file for error context: %w", readErr)
		}
		formatted := cmd.File.errorRenderer(sourceContent).Render(err)
		_, _ = fmt.Fprintln(ctx.Stderr, formatted)

		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")

		reportTelemetry()
		return NewCommandError(1)
	}
	errorCount, err := checkLedger(runCtx, ctx.Stderr, loadResult, cmd.File.GetAbsoluteFilename())
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
// prints its validation errors, each in the context of the loaded file its
// position names. It returns how many errors it printed.
func checkLedger(ctx context.Context, stderr io.Writer, loadResult *loader.LoadResult, mainFile string) (int, error) {
	for _, warning := range diagnostic.Warnings(loadResult.Diagnostics) {
		printInfof(stderr, "%s", warning)
	}
	l := ledger.New()
	loadErrors, validationErrors, err := ledgerErrors(ctx, l, loadResult)
	if err != nil {
		return 0, err
	}
	// Like bean-check, an error's transaction shows its postings as booked.
	renderer := NewErrorRenderer(loadResult.Sources, printer.WithBookedPositions(l.BookedPositions))
	for _, loadErr := range loadErrors {
		// A syntax error in the main file is shown in its source context,
		// like a failed load.
		if syntaxErr, ok := loadErr.(*parser.ParseError); ok && syntaxErr.Pos.Filename == mainFile {
			_, _ = fmt.Fprintln(stderr, renderer.Render(syntaxErr))
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

	if len(validationErrors) > 0 {
		_, _ = fmt.Fprintln(stderr, renderer.RenderAll(validationErrors))

		_, _ = fmt.Fprintln(stderr)
		total := len(validationErrors) + len(loadErrors)
		printError(stderr, fmt.Sprintf("%d validation error(s) found", total))
		return total, nil
	}
	return len(loadErrors), nil
}

// ledgerErrors processes the loaded AST into l and returns the errors
// check reports, in its order: the fatal load diagnostics, then the
// validation errors.
func ledgerErrors(ctx context.Context, l *ledger.Ledger, loadResult *loader.LoadResult) (loadErrors, validationErrors []error, err error) {
	loadErrors = diagnostic.Errors(loadResult.Diagnostics)
	if err := l.Process(ctx, loadResult.AST); err != nil {
		var validation *ledger.ValidationErrors
		if !errors.As(err, &validation) {
			return nil, nil, err
		}
		validationErrors = validation.Errors
	}
	return loadErrors, validationErrors, nil
}
