package cli

import (
	"context"
	stdErrors "errors"
	"fmt"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/printer"
)

// DoctorCmd provides doctor utilities for debugging beancount files.
type DoctorCmd struct {
	Lex         LexCmd         `cmd:"" help:"Show lexical tokens from a beancount file."`
	MissingOpen MissingOpenCmd `cmd:"" name:"missing_open" help:"Print the open directives a beancount file is missing."`
}

// LexCmd shows lexical tokens from a beancount file.
type LexCmd struct {
	File FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
}

// Run executes the lex command.
func (cmd *LexCmd) Run(ctx *kong.Context, globals *Globals) error {
	if err := cmd.File.EnsureContents(); err != nil {
		return err
	}

	// Get source content for lexing
	content, err := cmd.File.GetSourceContent()
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Create lexer and scan all tokens
	lexer := parser.NewLexer(content, cmd.File.Filename)
	tokens, err := lexer.ScanAll()
	if err != nil {
		// Handle specific lexer errors like InvalidUTF8Error
		if _, ok := err.(*parser.InvalidUTF8Error); ok {
			return fmt.Errorf("lexer error: %w", err)
		}
		return fmt.Errorf("failed to lex file: %w", err)
	}

	// Display tokens in the format: TYPE line:col "content"
	for _, token := range tokens {
		// Skip EOF token for clean output
		if token.Type == parser.EOF {
			continue
		}

		// Get the token content
		content := token.String(content)

		// Format: TYPE line:col "content"
		_, _ = fmt.Fprintf(ctx.Stdout, "%-10s %d:%d    %q\n",
			token.Type.String(),
			token.Line,
			token.Column,
			content)
	}

	return nil
}

// MissingOpenCmd prints an open directive for every account the ledger uses
// without an open or close directive, like bean-doctor missing_open.
type MissingOpenCmd struct {
	File FileOrStdin `help:"Beancount input filename (use '-' for stdin, or omit for stdin)." arg:"" optional:""`
}

// Run executes the missing_open command.
func (cmd *MissingOpenCmd) Run(ctx *kong.Context) error {
	if err := cmd.File.EnsureContents(); err != nil {
		return err
	}

	runCtx := context.Background()
	ldr := loader.New(loader.WithFollowIncludes(), loader.WithDocumentsDiscovery(), loader.WithSyntaxRecovery())
	loadResult, err := cmd.File.LoadResult(runCtx, ldr)
	if err != nil {
		sourceContent, readErr := cmd.File.GetSourceContent()
		if readErr != nil {
			return fmt.Errorf("failed to read file for error context: %w", readErr)
		}
		_, _ = fmt.Fprintln(ctx.Stderr, NewErrorRenderer(sourceContent).Render(err))
		_, _ = fmt.Fprintln(ctx.Stderr)
		printError(ctx.Stderr, "parse error")
		return NewCommandError(1)
	}

	// Like bean-doctor, the ledger's load and validation errors are not
	// reported: a directive that fails validation is still applied, and so
	// still uses its accounts.
	if err := ledger.New().Process(runCtx, loadResult.AST); err != nil {
		var validationErrors *ledger.ValidationErrors
		if !stdErrors.As(err, &validationErrors) {
			return err
		}
	}

	opens := ledger.MissingOpens(loadResult.AST)
	directives := make(ast.Directives, len(opens))
	for i, open := range opens {
		directives[i] = open
	}
	return printer.Print(runCtx, ctx.Stdout, directives)
}
