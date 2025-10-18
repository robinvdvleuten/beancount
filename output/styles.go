// Package output provides styling helpers for terminal output.
package output

import (
	"io"

	"github.com/muesli/termenv"
)

// Styles provides styled output helpers for the CLI.
type Styles struct {
	output *termenv.Output
}

// NewStyles creates a new Styles instance for the given writer.
func NewStyles(w io.Writer) *Styles {
	return &Styles{
		output: termenv.NewOutput(w),
	}
}

// Success returns a styled success string (green + bold).
func (s *Styles) Success(text string) string {
	return s.output.String(text).
		Foreground(s.output.Color("2")).
		Bold().
		String()
}

// Error returns a styled error string (red + bold).
func (s *Styles) Error(text string) string {
	return s.output.String(text).
		Foreground(s.output.Color("1")).
		Bold().
		String()
}

// FilePath returns a styled file path (cyan).
func (s *Styles) FilePath(text string) string {
	return s.output.String(text).
		Foreground(s.output.Color("6")).
		String()
}

// Account returns a styled account name (yellow).
func (s *Styles) Account(text string) string {
	return s.output.String(text).
		Foreground(s.output.Color("3")).
		String()
}

// Amount returns a styled amount/currency (magenta).
func (s *Styles) Amount(text string) string {
	return s.output.String(text).
		Foreground(s.output.Color("5")).
		String()
}

// Keyword returns a styled keyword (bold).
func (s *Styles) Keyword(text string) string {
	return s.output.String(text).
		Bold().
		String()
}

// Dim returns dimmed text (for secondary information).
func (s *Styles) Dim(text string) string {
	return s.output.String(text).
		Faint().
		String()
}

// Warning returns a styled warning (yellow + bold).
func (s *Styles) Warning(text string) string {
	return s.output.String(text).
		Foreground(s.output.Color("3")).
		Bold().
		String()
}

// Timing returns a styled timing string, colored based on duration.
// Fast operations (< 10ms) are dimmed, medium (< 100ms) are normal,
// slow (< 1s) are yellow, very slow (>= 1s) are red.
func (s *Styles) Timing(text string, isSlowOperation bool) string {
	if isSlowOperation {
		return s.output.String(text).
			Foreground(s.output.Color("1")).
			String()
	}
	return s.Dim(text)
}

// Output returns the underlying termenv Output for advanced usage.
func (s *Styles) Output() *termenv.Output {
	return s.output
}
