package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/robinvdvleuten/beancount/output"
)

var (
	// Version contains the application version number. It's set via ldflags
	// when building.
	Version = ""

	// CommitSHA contains the SHA of the commit that this application was built
	// against. It's set via ldflags when building.
	CommitSHA = ""

	cli struct {
		Version kong.VersionFlag `help:"Show version information"`
		Commands
	}
)

func main() {
	// Initialize globals with output styles before parsing
	// This way they're available to all commands via the Globals instance
	cli.OutStyles = output.NewStyles(os.Stdout)
	cli.ErrStyles = output.NewStyles(os.Stderr)

	ctx := kong.Parse(&cli,
		kong.Vars{
			"version": buildVersion(),
		},
		kong.Name("beancount"),
		kong.Description("A beancount file parser and formatter."),
		kong.UsageOnError(),
		kong.Bind(&cli.Globals),
	)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}

func buildVersion() string {
	if Version == "" {
		Version = "dev"
	}
	if CommitSHA == "" {
		return Version
	}
	return fmt.Sprintf("%s (%s)", Version, CommitSHA)
}
