package cli

import (
	"bytes"
	"context"
	stdErrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/importer"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/printer"
)

type ImportCmd struct {
	With           string `help:"Importer binary to run." required:"" placeholder:"IMPORTER"`
	UnknownAccount string `help:"Account that balances an Extracted transaction with a single posting." default:"Expenses:Unknown" placeholder:"ACCOUNT"`
	Ledger         string `help:"Beancount ledger the Extracted directives are checked against." arg:"" type:"existingfile"`
	Statement      string `help:"Statement for the Importer to read." arg:"" type:"existingfile"`
}

func (cmd *ImportCmd) Run(ctx *kong.Context) error {
	runCtx := context.Background()
	name := filepath.Base(cmd.With)

	unknown, err := ast.NewAccount(cmd.UnknownAccount)
	if err != nil {
		printError(ctx.Stderr, fmt.Sprintf("invalid --unknown-account %q: %s", cmd.UnknownAccount, err))
		return NewCommandError(1)
	}

	client, err := importer.Open(runCtx, cmd.With, ctx.Stderr)
	if err != nil {
		printError(ctx.Stderr, err.Error())
		return NewCommandError(1)
	}
	defer client.Close()

	matches, err := client.Identify(runCtx, cmd.Statement)
	if err != nil {
		printError(ctx.Stderr, fmt.Sprintf("importer %s: %s", name, err))
		return NewCommandError(1)
	}
	if !matches {
		printError(ctx.Stderr, fmt.Sprintf("importer %s does not recognize %s", name, cmd.Statement))
		return NewCommandError(1)
	}

	directives, err := client.Extract(runCtx, cmd.Statement)
	if err != nil {
		printError(ctx.Stderr, fmt.Sprintf("importer %s: %s", name, err))
		return NewCommandError(1)
	}

	source, err := os.ReadFile(cmd.Ledger)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", cmd.Ledger, err)
	}
	loadResult, ok := cmd.loadLedger(runCtx, ctx.Stderr, source)
	if !ok {
		return NewCommandError(1)
	}

	// Duplicates are looked up in the ledger alone. Its errors are reported
	// by the check below, which loads the ledger again, because processing
	// rewrites the tree it processes.
	existing := ledger.New()
	if err := existing.Process(runCtx, loadResult.AST); err != nil {
		var validationErrors *ledger.ValidationErrors
		if !stdErrors.As(err, &validationErrors) {
			return err
		}
	}
	directives, err = dropDuplicates(runCtx, ctx.Stderr, existing, directives)
	if err != nil {
		return err
	}
	addUnknownPostings(directives, unknown)

	text, err := formatExtracted(runCtx, directives)
	if err != nil {
		return err
	}

	// Check the printed text rather than the Importer's values, so that
	// Error lines point at what goes to stdout.
	extractedName := filepath.Base(cmd.Statement) + " (extracted)"
	extracted, err := parser.ParseBytesWithFilename(runCtx, extractedName, text)
	if err != nil {
		var syntaxErrs parser.ParseErrors
		if !stdErrors.As(err, &syntaxErrs) {
			syntaxErrs = parser.ParseErrors{parser.NewParseErrorWithSource(extractedName, err, text)}
		}
		renderer := NewErrorRenderer(map[string][]byte{extractedName: text})
		for _, syntaxErr := range syntaxErrs {
			_, _ = fmt.Fprintln(ctx.Stderr, renderer.Render(syntaxErr))
		}
		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")
		return NewCommandError(1)
	}

	loadResult, ok = cmd.loadLedger(runCtx, ctx.Stderr, source)
	if !ok {
		return NewCommandError(1)
	}
	loadResult.AST.Directives = append(loadResult.AST.Directives, extracted.Directives...)
	if err := ast.SortDirectives(loadResult.AST); err != nil {
		return err
	}

	errorCount, err := checkLedger(runCtx, ctx.Stderr, loadResult, loadResult.Root)
	if err != nil {
		return err
	}
	if errorCount > 0 {
		return NewCommandError(1)
	}

	_, err = ctx.Stdout.Write(text)
	return err
}

// loadLedger loads the ledger with its includes, reporting a failed load
// on stderr.
func (cmd *ImportCmd) loadLedger(ctx context.Context, stderr io.Writer, source []byte) (*loader.LoadResult, bool) {
	loadResult, err := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery()).Load(ctx, cmd.Ledger)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, NewErrorRenderer(map[string][]byte{cmd.Ledger: source}).Render(err))
		_, _ = fmt.Fprintln(stderr)
		printError(stderr, "parse error")
		return nil, false
	}
	return loadResult, true
}

// dropDuplicates returns the Extracted directives the ledger does not
// record yet, in their order. Each Duplicate is reported on stderr at the
// ledger directive it matched. Extracted directives are not compared with
// each other.
func dropDuplicates(ctx context.Context, stderr io.Writer, l *ledger.Ledger, directives []ast.Directive) ([]ast.Directive, error) {
	kept := make([]ast.Directive, 0, len(directives))
	for _, directive := range directives {
		match, ok := l.Duplicate(directive)
		if !ok {
			kept = append(kept, directive)
			continue
		}
		text, err := formatExtracted(ctx, []ast.Directive{directive})
		if err != nil {
			return nil, err
		}
		header, _, _ := strings.Cut(string(text), "\n")
		pos := match.Position()
		_, _ = fmt.Fprintf(stderr, "%s:%d: Duplicate left out: %s\n", pos.Filename, pos.Line, header)
	}
	return kept, nil
}

// addUnknownPostings balances each transaction with a single posting that
// has units with an amount-less posting to the Unknown account, whose
// amount Booking completes, weight at cost or price included.
func addUnknownPostings(directives []ast.Directive, unknown ast.Account) {
	for _, directive := range directives {
		txn, ok := directive.(*ast.Transaction)
		if !ok || len(txn.Postings) != 1 {
			continue
		}
		units, err := ledger.ParseAmount(txn.Postings[0].Amount)
		if err != nil || units.IsZero() {
			continue
		}
		txn.Postings = append(txn.Postings, ast.NewPosting(unknown))
	}
}

// formatExtracted prints the Extracted directives in the Importer's order,
// as bean-query prints directives, but starting at the first directive, so
// Error lines count from its header.
func formatExtracted(ctx context.Context, directives []ast.Directive) ([]byte, error) {
	var printed bytes.Buffer
	if err := printer.Print(ctx, &printed, directives); err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(printed.Bytes(), []byte("\n")), nil
}
