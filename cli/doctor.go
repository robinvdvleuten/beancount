package cli

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/parser"
	"github.com/robinvdvleuten/beancount/printer"
)

// DoctorCmd provides doctor utilities for debugging beancount files.
type DoctorCmd struct {
	Lex         LexCmd         `cmd:"" help:"Show lexical tokens from a beancount file."`
	MissingOpen MissingOpenCmd `cmd:"" name:"missing_open" aliases:"missing-open" help:"Print the open directives a beancount file is missing."`
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
	// Like bean-doctor, the ledger's load and validation errors are not
	// reported: a directive that fails validation is still applied, and so
	// still uses its accounts.
	result, err := ledgerload.Load(runCtx, cmd.File.Source())
	if err != nil {
		return cmd.File.reportLoadFailure(ctx.Stderr, loadFailureLabelled, err)
	}

	opens := ledger.MissingOpens(result.AST)
	directives := make(ast.Directives, len(opens))
	for i, open := range opens {
		directives[i] = open
	}
	return printer.Print(runCtx, ctx.Stdout, directives)
}
