package parser

import (
	"context"
	"io"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/robinvdvleuten/beancount/ast"
)

var (
	lex = lexer.MustSimple([]lexer.SimpleRule{
		{"Date", `\d{4}-\d{2}-\d{2}`},
		{"Account", `[A-Z][A-Za-z]*:[A-Za-z0-9][A-Za-z0-9:-]*`},
		{"String", `"[^"]*"`},
		{"Number", `[-+]?(\d*\.)?\d+`},
		{"Link", `\^[A-Za-z0-9_-]+`},
		{"Tag", `#[A-Za-z0-9_-]+`},
		{"Ident", `[A-Za-z][0-9A-Za-z_-]*`},
		{"Punct", `[!*:,@{}]`},
		{"Comment", `;[^\n]*\n`},
		{"Whitespace", `[[:space:]]`},
		{"ignore", `.`},
	})

	parser = participle.MustBuild[ast.AST](
		participle.Lexer(lex),
		participle.Unquote("String"),
		participle.Elide("Comment", "Whitespace"),
		participle.Union[ast.Directive](
			&ast.Commodity{},
			&ast.Open{},
			&ast.Close{},
			&ast.Balance{},
			&ast.Pad{},
			&ast.Note{},
			&ast.Document{},
			&ast.Price{},
			&ast.Event{},
			&ast.Custom{},
			&ast.Transaction{},
		),
		participle.UseLookahead(2),
	)
)

// Parse AST from an io.Reader.
func Parse(ctx context.Context, r io.Reader) (*ast.AST, error) {
	tree, err := parser.Parse("", r)
	if err != nil {
		return nil, err
	}

	if err := ast.ApplyPushPopDirectives(tree); err != nil {
		return nil, err
	}

	return tree, ast.SortDirectives(tree)
}

// ParseString parses AST from a string.
func ParseString(ctx context.Context, str string) (*ast.AST, error) {
	tree, err := parser.ParseString("", str)
	if err != nil {
		return nil, err
	}

	if err := ast.ApplyPushPopDirectives(tree); err != nil {
		return nil, err
	}

	return tree, ast.SortDirectives(tree)
}

// ParseBytes parses AST from bytes.
func ParseBytes(ctx context.Context, data []byte) (*ast.AST, error) {
	return ParseBytesWithFilename(ctx, "", data)
}

// ParseBytesWithFilename parses AST from bytes with a filename for position tracking.
// The filename will be included in position information in the AST for better error reporting.
func ParseBytesWithFilename(ctx context.Context, filename string, data []byte) (*ast.AST, error) {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tree, err := parser.ParseBytes(filename, data)
	if err != nil {
		return nil, err
	}

	if err := ast.ApplyPushPopDirectives(tree); err != nil {
		return nil, err
	}

	return tree, ast.SortDirectives(tree)
}
