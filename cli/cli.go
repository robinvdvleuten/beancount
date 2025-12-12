// Package cli provides common utilities for building command-line interfaces.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/loader"
)

var (
	successSymbol = "✓"
	errorSymbol   = "✗"
	infoSymbol    = "→"

	successStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#00D787", Dark: "#00D787"})
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#FF5F87", Dark: "#FF5F87"})
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5FAFFF", Dark: "#5FAFFF"})
	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#00D7D7", Dark: "#00D7D7"})
)

func printSuccess(w io.Writer, message string) {
	_, _ = fmt.Fprintf(w, "%s %s\n",
		successStyle.Render(successSymbol),
		message,
	)
}

func printError(w io.Writer, message string) {
	_, _ = fmt.Fprintf(w, "%s %s\n",
		errorStyle.Render(errorSymbol),
		errorStyle.Render(message),
	)
}

func printInfof(w io.Writer, format string, args ...interface{}) {
	formatted := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(w, "%s %s\n",
		infoStyle.Render(infoSymbol),
		formatted,
	)
}

// promptYesNo prompts the user with a yes/no question.
// Returns false by default if stdin is not a terminal.
func promptYesNo(ctx *kong.Context, question string) (bool, error) {
	if !isTerminal() {
		return false, nil
	}

	var confirm bool

	form := huh.NewConfirm().
		Title(question).
		WithButtonAlignment(lipgloss.Left).
		Value(&confirm)

	err := form.Run()
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	return confirm, nil
}

func isTerminal() bool {
	fileInfo, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// FileOrStdin accepts either a file path or "-" for stdin.
// For stdin: Filename="<stdin>", Contents populated.
// For files: Filename set, Contents nil (read by loader).
type FileOrStdin struct {
	Filename string
	Contents []byte
}

// Decode implements kong.MapperValue.
func (f *FileOrStdin) Decode(ctx *kong.DecodeContext) error {
	var filename string
	if err := ctx.Scan.PopValueInto("filename", &filename); err != nil {
		return err
	}

	if filename == "-" || filename == "" {
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		f.Filename = "<stdin>"
		f.Contents = contents
		return nil
	}

	if _, err := os.Stat(filename); err != nil {
		return err
	}
	f.Filename = filename
	f.Contents = nil

	return nil
}

// EnsureContents populates Contents from stdin if Filename is empty.
func (f *FileOrStdin) EnsureContents() error {
	if f.Filename == "" {
		contents, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read from stdin: %w", err)
		}
		f.Filename = "<stdin>"
		f.Contents = contents
	}
	return nil
}

// GetSourceContent returns source content for error formatting.
func (f *FileOrStdin) GetSourceContent() ([]byte, error) {
	if f.Filename == "<stdin>" {
		return f.Contents, nil
	}
	return os.ReadFile(f.Filename)
}

// GetAbsoluteFilename returns the absolute path, or "<stdin>" for stdin.
func (f *FileOrStdin) GetAbsoluteFilename() string {
	if f.Filename == "<stdin>" {
		return f.Filename
	}
	absPath, err := filepath.Abs(f.Filename)
	if err != nil {
		return f.Filename
	}
	return absPath
}

// LoadAST loads the AST using LoadBytes for stdin or Load for files.
func (f *FileOrStdin) LoadAST(ctx context.Context, ldr *loader.Loader) (*ast.AST, error) {
	absFilename := f.GetAbsoluteFilename()

	if f.Filename == "<stdin>" {
		return ldr.LoadBytes(ctx, absFilename, f.Contents)
	}
	return ldr.Load(ctx, absFilename)
}
