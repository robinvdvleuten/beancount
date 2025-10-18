package formatter

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestMetadataEscaping(t *testing.T) {
	t.Run("Programmatic metadata with quote", func(t *testing.T) {
		date, _ := ast.NewDate("2023-01-01")
		acct, _ := ast.NewAccount("Assets:Cash")

		txn := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPostings(
				ast.NewPosting(acct, ast.WithAmount("100.00", "USD")),
			),
		)

		// Add metadata with a quote - should be escaped
		txn.Metadata = []*ast.Metadata{
			{Key: "note", Value: `He said "hello"`},
		}

		f := New()
		var buf bytes.Buffer
		err := f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, `note: "He said \"hello\"`), "quote should be escaped")
		assert.True(t, !strings.Contains(output, `"He said "hello""`), "unescaped quote would break syntax")
	})

	t.Run("Programmatic metadata with backslash", func(t *testing.T) {
		date, _ := ast.NewDate("2023-01-01")
		acct, _ := ast.NewAccount("Assets:Cash")

		txn := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPostings(
				ast.NewPosting(acct, ast.WithAmount("100.00", "USD")),
			),
		)

		// Add metadata with a backslash (e.g., Windows path)
		txn.Metadata = []*ast.Metadata{
			{Key: "file", Value: `C:\Users\Documents\receipt.pdf`},
		}

		f := New()
		var buf bytes.Buffer
		err := f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, `file: "C:\\Users\\Documents\\receipt.pdf"`), "backslashes should be escaped")
	})

	t.Run("Programmatic metadata with both quote and backslash", func(t *testing.T) {
		date, _ := ast.NewDate("2023-01-01")
		acct, _ := ast.NewAccount("Assets:Cash")

		txn := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPostings(
				ast.NewPosting(acct, ast.WithAmount("100.00", "USD")),
			),
		)

		// Add metadata with both special characters
		txn.Metadata = []*ast.Metadata{
			{Key: "note", Value: `Path "C:\temp" exists`},
		}

		f := New()
		var buf bytes.Buffer
		err := f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, `note: "Path \"C:\\temp\" exists"`), "both quote and backslash should be escaped")
	})

	t.Run("Programmatic metadata with plain text", func(t *testing.T) {
		date, _ := ast.NewDate("2023-01-01")
		acct, _ := ast.NewAccount("Assets:Cash")

		txn := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPostings(
				ast.NewPosting(acct, ast.WithAmount("100.00", "USD")),
			),
		)

		// Add metadata with no special characters
		txn.Metadata = []*ast.Metadata{
			{Key: "note", Value: "Plain text no escapes needed"},
		}

		f := New()
		var buf bytes.Buffer
		err := f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, `note: "Plain text no escapes needed"`), "plain text should remain unchanged")
	})

	t.Run("Parsed metadata with escapes", func(t *testing.T) {
		// Verify that parsing unescapes string content so formatting remains idempotent.
		source := []byte(`2023-01-01 * "Test"
  note: "This has a \" quote"
  Assets:Cash  100.00 USD
`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		txn, ok := tree.Directives[0].(*ast.Transaction)
		assert.True(t, ok, "expected Transaction directive")
		assert.Equal(t, 1, len(txn.Metadata))
		assert.Equal(t, `This has a " quote`, txn.Metadata[0].Value)

		f := New(WithSource(source))
		var buf bytes.Buffer
		err = f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()

		assert.True(t, strings.Contains(output, `note: "This has a \" quote"`), "escapes should be preserved once")
		assert.False(t, strings.Contains(output, `\\\"`), "escapes must not be doubled")
	})

	t.Run("Parsed metadata with negative number", func(t *testing.T) {
		source := []byte(`2023-01-01 * "Test"
  price: -45.00 USD
  Assets:Cash  100.00 USD
`)

		tree, err := parser.ParseBytes(context.Background(), source)
		assert.NoError(t, err)

		txn, ok := tree.Directives[0].(*ast.Transaction)
		assert.True(t, ok, "expected Transaction directive")
		assert.Equal(t, 1, len(txn.Metadata))
		assert.Equal(t, `-45.00 USD`, txn.Metadata[0].Value)

		f := New(WithSource(source))
		var buf bytes.Buffer
		err = f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		assert.True(t, strings.Contains(output, `price: "-45.00 USD"`), "value should preserve minus without extra spaces")
	})

	t.Run("Multiple metadata entries", func(t *testing.T) {
		date, _ := ast.NewDate("2023-01-01")
		acct, _ := ast.NewAccount("Assets:Cash")

		txn := ast.NewTransaction(date, "Test",
			ast.WithFlag("*"),
			ast.WithPostings(
				ast.NewPosting(acct, ast.WithAmount("100.00", "USD")),
			),
		)

		txn.Metadata = []*ast.Metadata{
			{Key: "note1", Value: "Plain"},
			{Key: "note2", Value: `Has "quote"`},
			{Key: "note3", Value: `Has \backslash`},
			{Key: "note4", Value: `Has "both" \things`},
		}

		f := New()
		var buf bytes.Buffer
		err := f.FormatTransaction(txn, &buf)
		assert.NoError(t, err)

		output := buf.String()
		lines := strings.Split(output, "\n")

		// Verify each metadata line exists with proper escaping
		var metadataLines []string
		for _, line := range lines {
			if strings.Contains(line, "note") {
				metadataLines = append(metadataLines, strings.TrimSpace(line))
			}
		}

		assert.Equal(t, 4, len(metadataLines))
		assert.Equal(t, `note1: "Plain"`, metadataLines[0])
		assert.Equal(t, `note2: "Has \"quote\""`, metadataLines[1])
		assert.Equal(t, `note3: "Has \\backslash"`, metadataLines[2])
		assert.Equal(t, `note4: "Has \"both\" \\things"`, metadataLines[3])
	})
}
