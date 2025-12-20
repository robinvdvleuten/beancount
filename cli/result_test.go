package cli

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestCommandError(t *testing.T) {
	t.Run("implements error interface", func(t *testing.T) {
		err := NewCommandError(1)
		assert.Error(t, err)
	})

	t.Run("returns exit code", func(t *testing.T) {
		err := NewCommandError(42)
		assert.Equal(t, err.ExitCode(), 42)
	})

	t.Run("supports type assertion", func(t *testing.T) {
		var err error = NewCommandError(1)
		cmdErr, ok := err.(*CommandError)
		assert.True(t, ok)
		assert.Equal(t, cmdErr.ExitCode(), 1)
	})
}

func TestCommandResult(t *testing.T) {
	t.Run("Success returns zero exit code", func(t *testing.T) {
		result := Success()
		assert.Equal(t, result.ExitCode, 0)
		assert.True(t, result.Err == nil)
	})

	t.Run("Failure returns non-zero exit code", func(t *testing.T) {
		testErr := NewCommandError(1)
		result := Failure(testErr)
		assert.Equal(t, result.ExitCode, 1)
		assert.True(t, result.Err != nil)
	})
}
