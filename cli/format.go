package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

type FormatCmd struct {
	Files          []FileOrStdin `help:"Beancount input filenames (use '-' for stdin, or omit for stdin); several only with --in-place." arg:"" optional:"" name:"file"`
	Output         string        `short:"o" help:"Output file ('-' or omit for stdout)." type:"path"`
	InPlace        bool          `short:"i" help:"Edit files in place."`
	CurrencyColumn int           `short:"c" help:"Column for currency alignment (auto-calculated from content if 0, overrides prefix-width and num-width if set)." default:"0"`
	PrefixWidth    int           `short:"w" help:"Width in characters for account names (auto if 0)." default:"0"`
	NumWidth       int           `short:"W" help:"Width for numbers (auto if 0)." default:"0"`
}

// Validate rejects, like bean-format, several files written anywhere but
// in place, and --in-place without files to write.
func (cmd *FormatCmd) Validate() error {
	if len(cmd.Files) > 1 && !cmd.InPlace {
		return errors.New("--output can only be used with a single file; try --in-place instead")
	}
	if cmd.InPlace {
		if len(cmd.Files) == 0 {
			return errors.New("--in-place needs at least one file")
		}
		for _, file := range cmd.Files {
			if file.Filename == "<stdin>" {
				return errors.New("--in-place needs a file, not stdin")
			}
		}
	}
	return nil
}

func (cmd *FormatCmd) Run(ctx *kong.Context, globals *Globals) error {
	files := cmd.Files
	if len(files) == 0 {
		files = []FileOrStdin{{}}
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

	for i := range files {
		if err := files[i].EnsureContents(); err != nil {
			return err
		}
	}

	failed := false
	for i := range files {
		file := &files[i]
		var formatted bytes.Buffer
		ok, err := cmd.format(runCtx, ctx.Stderr, file, &formatted)
		if err != nil {
			return err
		}
		if !ok {
			failed = true
			continue
		}

		// Like bean-format, a file is written only once it is formatted, so
		// one that fails to load is left as it is.
		// A file keeps its mode, and a new one gets the umask's, like
		// Python's open.
		target := file.Filename
		switch {
		case cmd.InPlace:
			err = os.WriteFile(target, formatted.Bytes(), 0o666)
		case cmd.Output != "" && cmd.Output != "-":
			target = cmd.Output
			err = os.WriteFile(target, formatted.Bytes(), 0o666)
		default:
			target = "stdout"
			_, err = ctx.Stdout.Write(formatted.Bytes())
		}
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", target, err)
		}
	}
	if failed {
		return NewCommandError(1)
	}
	return nil
}

// format formats one file into w. It reports a file that fails to load on
// stderr and returns false.
func (cmd *FormatCmd) format(ctx context.Context, stderr io.Writer, file *FileOrStdin, w io.Writer) (bool, error) {
	sourceContent, err := file.GetSourceContent()
	if err != nil {
		return false, fmt.Errorf("failed to read file: %w", err)
	}

	ldr := loader.New()
	loadResult, err := file.LoadResult(ctx, ldr)
	if err != nil {
		formatted := file.errorRenderer(sourceContent).Render(err)
		_, _ = fmt.Fprint(stderr, formatted)
		_, _ = fmt.Fprintln(stderr)
		printError(stderr, "parse error")
		return false, nil
	}
	for _, warning := range loadResult.Diagnostics {
		printInfof(stderr, "%s", warning)
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
	if err := formatter.New(opts...).Format(ctx, loadResult.AST, sourceContent, w); err != nil {
		return false, err
	}
	return true, nil
}
