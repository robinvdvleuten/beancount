package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alecthomas/kong"

	"github.com/robinvdvleuten/beancount/telemetry"
	"github.com/robinvdvleuten/beancount/web"
)

type WebCmd struct {
	File     string `help:"Beancount ledger file to serve." arg:""`
	Port     int    `help:"Port to listen on." default:"8080"`
	Create   bool   `help:"Automatically create file if it doesn't exist (no confirmation prompt)." short:"c"`
	ReadOnly bool   `help:"Enable read-only mode (no write operations allowed)." short:"r"`
}

func (cmd *WebCmd) Run(ctx *kong.Context, globals *Globals) error {
	runCtx := context.Background()

	if globals.Telemetry {
		collector := telemetry.NewTimingCollector()
		runCtx = telemetry.WithCollector(runCtx, collector)

		defer func() {
			_, _ = fmt.Fprintln(ctx.Stderr)
			collector.Report(ctx.Stderr)
		}()
	}

	ledgerFile, err := filepath.Abs(cmd.File)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	if _, err := os.Stat(ledgerFile); err != nil {
		if os.IsNotExist(err) {
			shouldCreate := cmd.Create

			if !shouldCreate {
				confirmed, err := promptYesNo(ctx, fmt.Sprintf("File %q does not exist. Create it?", ledgerFile))
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}
				shouldCreate = confirmed
			}

			if !shouldCreate {
				return fmt.Errorf("file does not exist: %s", ledgerFile)
			}

			parentDir := filepath.Dir(ledgerFile)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			if err := os.WriteFile(ledgerFile, []byte(""), 0600); err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			printInfof(ctx.Stdout, "Created empty ledger file: %s", pathStyle.Render(ledgerFile))
		} else {
			return fmt.Errorf("failed to access file: %w", err)
		}
	}

	version := Version
	if version == "" {
		version = "dev"
	}
	commitSHA := CommitSHA
	if commitSHA == "" {
		commitSHA = "local"
	}

	server := web.NewWithVersion(cmd.Port, ledgerFile, version, commitSHA)
	server.ReadOnly = cmd.ReadOnly

	printInfof(ctx.Stdout, "Starting server on %s:%d", server.Host, cmd.Port)
	printInfof(ctx.Stdout, "Serving ledger: %s", pathStyle.Render(ledgerFile))

	if cmd.ReadOnly {
		printInfof(ctx.Stdout, "Server running in READ-ONLY mode")
	}

	return server.Start(runCtx)
}
