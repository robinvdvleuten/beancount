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

// CommandResult encapsulates the outcome of a command execution.
// It allows commands to return structured results instead of calling os.Exit directly,
// enabling better testability and centralizing exit handling in main().
type CommandResult struct {
	// ExitCode is the exit code to return to the OS.
	// 0 indicates success, non-zero indicates failure.
	ExitCode int

	// Err is any error that occurred during command execution.
	// Commands should populate this for non-zero exit codes.
	Err error
}

// Success returns a CommandResult indicating successful execution.
func Success() CommandResult {
	return CommandResult{ExitCode: 0}
}

// Failure returns a CommandResult indicating failure with the given error.
func Failure(err error) CommandResult {
	return CommandResult{ExitCode: 1, Err: err}
}
