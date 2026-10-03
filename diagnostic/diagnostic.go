// Package diagnostic defines the shape and the severity classification
// shared by the errors emitted while loading, configuring, and validating
// Beancount files.
package diagnostic

import (
	"errors"

	"github.com/robinvdvleuten/beancount/ast"
)

// Severity describes whether a diagnostic prevents successful processing.
type Severity uint8

const (
	SeverityError Severity = iota
	SeverityWarning
)

// Diagnostic is an error that explicitly declares its severity.
type Diagnostic interface {
	error
	Severity() Severity
}

// Positioned is the shape of every error a loaded ledger reports on a line:
// syntax, load, option and ledger errors alike. Renderers read it instead of
// matching on types or trimming Error's text. Its severity is SeverityOf: an
// error unless the type declares another, which package ast, unable to
// import this one, cannot do.
type Positioned interface {
	error
	// Kind names the kind of error, e.g. "DuplicateIncludeError": the type
	// the web API sends.
	Kind() string
	// Message is the error's text without its Error line.
	Message() string
	// GetPosition returns where the error is reported.
	GetPosition() ast.Position
}

// SeverityOf returns an error's declared severity, unwrapping as needed so a
// wrapped warning keeps its classification. An error that declares no
// severity is fatal.
func SeverityOf(err error) Severity {
	var d Diagnostic
	if errors.As(err, &d) {
		return d.Severity()
	}
	return SeverityError
}

// Errors returns only fatal diagnostics.
func Errors(errs []error) []error {
	return filter(errs, SeverityError)
}

// Warnings returns only non-fatal diagnostics.
func Warnings(errs []error) []error {
	return filter(errs, SeverityWarning)
}

func filter(errs []error, severity Severity) []error {
	filtered := make([]error, 0, len(errs))
	for _, err := range errs {
		if SeverityOf(err) == severity {
			filtered = append(filtered, err)
		}
	}
	return filtered
}
