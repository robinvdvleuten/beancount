package cli

import (
	"fmt"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/parser"
)

// DoctorCmd provides doctor utilities for debugging beancount files.
type DoctorCmd struct {
	Lex LexCmd `cmd:"" help:"Show lexical tokens from a beancount file."`
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
