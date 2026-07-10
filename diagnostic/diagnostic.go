// Package diagnostic defines shared severity classification for errors emitted
// while loading, configuring, and validating Beancount files.
package diagnostic

import "errors"

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

// SeverityOf returns an error's declared severity, unwrapping as needed so a
// wrapped warning keeps its classification. Ordinary errors are fatal by
// default so existing error types remain safe while being migrated.
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
