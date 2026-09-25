package cli

import (
	"context"
	stdErrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/importer"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
)

type ImportCmd struct {
	With      string `help:"Importer binary to run." required:"" placeholder:"IMPORTER"`
	Ledger    string `help:"Beancount ledger the Extracted directives are checked against." arg:"" type:"existingfile"`
	Statement string `help:"Statement for the Importer to read." arg:"" type:"existingfile"`
}

func (cmd *ImportCmd) Run(ctx *kong.Context) error {
	runCtx := context.Background()
	name := filepath.Base(cmd.With)

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
		renderer := NewErrorRenderer(text)
		for _, syntaxErr := range syntaxErrs {
			_, _ = fmt.Fprintln(ctx.Stderr, renderer.Render(syntaxErr))
		}
		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")
		return NewCommandError(1)
	}

	source, err := os.ReadFile(cmd.Ledger)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", cmd.Ledger, err)
	}
	loadResult, err := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery()).Load(runCtx, cmd.Ledger)
	if err != nil {
		_, _ = fmt.Fprintln(ctx.Stderr, NewErrorRenderer(source).Render(err))
		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")
		return NewCommandError(1)
	}
	loadResult.AST.Directives = append(loadResult.AST.Directives, extracted.Directives...)
	if err := ast.SortDirectives(loadResult.AST); err != nil {
		return err
	}

	errorCount, err := checkLedger(runCtx, ctx.Stderr, loadResult, loadResult.Root, source)
	if err != nil {
		return err
	}
	if errorCount > 0 {
		return NewCommandError(1)
	}

	_, err = ctx.Stdout.Write(text)
	return err
}

// formatExtracted formats the Extracted directives in the Importer's order,
// one blank line apart.
func formatExtracted(ctx context.Context, directives []ast.Directive) ([]byte, error) {
	var formatted strings.Builder
	if err := formatter.New().Format(ctx, &ast.AST{Directives: directives}, nil, &formatted); err != nil {
		return nil, err
	}

	// Every line that is not indented starts a directive.
	var out strings.Builder
	for line := range strings.Lines(formatted.String()) {
		if out.Len() > 0 && line[0] != ' ' {
			out.WriteByte('\n')
		}
		out.WriteString(line)
	}
	return []byte(out.String()), nil
}
