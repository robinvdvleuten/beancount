package diagnostic

import (
	"errors"
	"testing"

	"github.com/alecthomas/assert/v2"
)

type warning string

func (w warning) Error() string      { return string(w) }
func (w warning) Severity() Severity { return SeverityWarning }

func TestClassify(t *testing.T) {
	err := errors.New("fatal")
	warn := warning("notice")

	assert.Equal(t, SeverityError, SeverityOf(err))
	assert.Equal(t, SeverityWarning, SeverityOf(warn))
	assert.Equal(t, []error{err}, Errors([]error{err, warn}))
	assert.Equal(t, []error{warn}, Warnings([]error{err, warn}))
}
