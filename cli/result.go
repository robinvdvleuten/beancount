package cli

// CommandError signals a command failure with a specific exit code.
// Commands return this after handling all output (printing errors/warnings to stderr).
// Main centralizes exit handling instead of commands calling os.Exit directly.
type CommandError struct {
	exitCode int
}

// NewCommandError creates a new CommandError with the given exit code.
func NewCommandError(exitCode int) *CommandError {
	return &CommandError{exitCode: exitCode}
}

// Error implements the error interface.
func (e *CommandError) Error() string {
	return "command failed"
}

// ExitCode returns the exit code associated with this error.
func (e *CommandError) ExitCode() int {
	return e.exitCode
}
