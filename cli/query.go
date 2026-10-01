package cli

import (
	"bufio"
	"context"
	stdErrors "errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/config"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/printer"
	"github.com/robinvdvleuten/beancount/query"
)

type QueryCmd struct {
	Format    string      `short:"f" default:"text" enum:"text,csv" help:"Output format: text or csv."`
	Output    string      `short:"o" placeholder:"FILE" help:"Write output to FILE instead of stdout."`
	Numberify bool        `short:"m" help:"Split amounts into per-currency number columns."`
	File      FileOrStdin `help:"Beancount input filename (use '-' for stdin)." arg:""`
	Query     []string    `help:"BQL query to run." arg:"" optional:""`
}

func (cmd *QueryCmd) Run(ctx *kong.Context, globals *Globals) error {
	if err := cmd.File.EnsureContents(); err != nil {
		return err
	}
	queryText := strings.Join(cmd.Query, " ")

	runCtx := context.Background()

	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery())
	loadResult, err := cmd.File.LoadResult(runCtx, ldr)
	if err != nil {
		sourceContent, readErr := cmd.File.GetSourceContent()
		if readErr != nil {
			return fmt.Errorf("failed to read file for error context: %w", readErr)
		}
		_, _ = fmt.Fprintln(ctx.Stderr, cmd.File.errorRenderer(sourceContent).Render(err))
		return NewCommandError(1)
	}
	tree := loadResult.AST

	// Like bean-query, load errors (syntax errors included) are reported and
	// the rest of the ledger is queried.
	for _, loadErr := range diagnostic.Errors(loadResult.Diagnostics) {
		_, _ = fmt.Fprintln(ctx.Stderr, loadErr.Error())
	}

	// Like bean-query, validation problems are reported but do not prevent
	// querying the loadable portion of the ledger.
	var validationErrors *ledger.ValidationErrors
	l := ledger.New()
	if err := l.Process(runCtx, tree); err != nil {
		if stdErrors.As(err, &validationErrors) {
			renderer := NewErrorRenderer(loadResult.Sources, printer.WithBookedPositions(l.BookedPositions))
			_, _ = fmt.Fprintln(ctx.Stderr, renderer.RenderAll(validationErrors.Errors))
		} else {
			return err
		}
	}

	// Invalid options were reported by the ledger above.
	cfg, _ := config.ParseOptions(tree)

	qctx := &query.Context{Ledger: l, Config: cfg, AST: tree}
	format := query.Format(cmd.Format)

	// Without a query argument, a terminal gets the interactive shell and
	// piped stdin is read as a single query, like bean-query. An empty
	// query, given or piped, prints nothing (query.Run).
	if len(cmd.Query) == 0 {
		if cmd.File.Filename != "<stdin>" && term.IsTerminal(int(os.Stdin.Fd())) {
			return runShell(runCtx, qctx, format, cmd.Numberify, os.Stdin, ctx.Stdout, ctx.Stderr, validationErrors, loadResult.Sources)
		}
		piped, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read query from stdin: %w", err)
		}
		queryText = string(piped)
	}

	if cmd.Output != "" {
		file := &lazyFile{path: cmd.Output}
		runErr := query.Run(runCtx, qctx, queryText, format, cmd.Numberify, file)
		if closeErr := file.Close(); runErr == nil {
			runErr = closeErr
		}
		return reportQueryError(ctx.Stderr, runErr)
	}

	return reportQueryError(ctx.Stderr, query.Run(runCtx, qctx, queryText, format, cmd.Numberify, ctx.Stdout))
}

// lazyFile creates its file on the first write, like the lazy file behind
// bean-query's -o, so a query that fails or prints nothing leaves none.
type lazyFile struct {
	path string
	file *os.File
}

func (f *lazyFile) Write(p []byte) (int, error) {
	if f.file == nil {
		file, err := os.Create(f.path)
		if err != nil {
			return 0, fmt.Errorf("failed to create output file %s: %w", f.path, err)
		}
		f.file = file
	}
	return f.file.Write(p)
}

// Close closes the file, if the first write created it.
func (f *lazyFile) Close() error {
	if f.file == nil {
		return nil
	}
	return f.file.Close()
}

// reportQueryError prints a statement that does not parse or compile like
// beanquery's shell, on stderr, and fails with exit status 1, as bean-query
// does. Any other error is returned as it is.
func reportQueryError(stderr io.Writer, err error) error {
	var queryErr *query.Error
	if !stdErrors.As(err, &queryErr) {
		return err
	}
	_, _ = fmt.Fprintln(stderr, queryErr.Report())
	return NewCommandError(1)
}

// runShell is the interactive query REPL: one query per line, with help,
// errors, and exit commands.
func runShell(ctx context.Context, qctx *query.Context, format query.Format, numberify bool, in io.Reader, out, errOut io.Writer, validationErrors *ledger.ValidationErrors, sources map[string][]byte) error {
	printShellBanner(out, qctx.AST)

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		_, _ = fmt.Fprint(out, "beancount> ")
		if !scanner.Scan() {
			_, _ = fmt.Fprintln(out)
			return scanner.Err()
		}
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), ";"))
		switch strings.ToLower(line) {
		case "":
			continue
		case "exit", "quit":
			return nil
		case "help":
			_, _ = fmt.Fprintln(out, "Enter a BQL query (SELECT, BALANCES, JOURNAL, PRINT).")
			_, _ = fmt.Fprintln(out, "Commands: errors (show ledger errors), exit or quit (leave the shell).")
			continue
		case "errors":
			if validationErrors == nil || len(validationErrors.Errors) == 0 {
				_, _ = fmt.Fprintln(out, "(no errors)")
				continue
			}
			renderer := NewErrorRenderer(sources, printer.WithBookedPositions(qctx.Ledger.BookedPositions))
			_, _ = fmt.Fprintln(out, renderer.RenderAll(validationErrors.Errors))
			continue
		}
		// Like beanquery's shell, report a failed statement on stderr and
		// go on.
		if err := query.Run(ctx, qctx, line, format, numberify, out); err != nil {
			var queryErr *query.Error
			if !stdErrors.As(err, &queryErr) {
				return err
			}
			_, _ = fmt.Fprintln(errOut, queryErr.Report())
		}
	}
}

// printShellBanner reports the ledger title and directive counts, following
// the official shell greeting.
func printShellBanner(out io.Writer, tree *ast.AST) {
	title := ""
	for _, option := range tree.Options {
		if option.Name.String() == "title" {
			title = option.Value.String()
		}
	}
	if title != "" {
		_, _ = fmt.Fprintf(out, "Input file: %q\n", title)
	}
	transactions, postings := 0, 0
	for _, entry := range tree.Directives {
		if txn, ok := entry.(*ast.Transaction); ok {
			transactions++
			postings += len(txn.Postings)
		}
	}
	_, _ = fmt.Fprintf(out, "Ready with %d directives (%d postings in %d transactions).\n",
		len(tree.Directives), postings, transactions)
}
