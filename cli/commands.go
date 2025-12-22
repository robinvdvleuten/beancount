package cli

var (
	Version   = ""
	CommitSHA = ""
)

// Globals defines global flags available to all commands.
type Globals struct {
	Telemetry bool `help:"Show timing telemetry for operations."`
}

type Commands struct {
	Globals

	Check  CheckCmd  `cmd:"" help:"Parse, check and realize a beancount input file."`
	Doctor DoctorCmd `cmd:"" help:"Doctor utilities for debugging beancount files."`
	Format FormatCmd `cmd:"" help:"Format a beancount file to align numbers and currencies."`
	Web    WebCmd    `cmd:"" help:"Start a web server."`
}
