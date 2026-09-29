package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/alecthomas/kong"
)

const (
	unformatted = "2020-01-01 open Assets:A\n2020-01-02 * \"x\"\n  Assets:A 1.00 USD\n  Assets:LongerName -1 USD\n"
	formatted   = "2020-01-01 open Assets:A\n2020-01-02 * \"x\"\n  Assets:A           1.00 USD\n  Assets:LongerName    -1 USD\n"
)

// runCommand parses and runs args in process. A parse error is returned
// before anything runs.
func runCommand(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var cmds Commands
	var out, errOut bytes.Buffer
	parser, err := kong.New(&cmds, kong.Writers(&out, &errOut), kong.Bind(&cmds.Globals))
	assert.NoError(t, err)
	ctx, err := parser.Parse(args)
	if err != nil {
		return "", "", err
	}
	err = ctx.Run()
	return out.String(), errOut.String(), err
}

func writeLedger(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	assert.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readLedger(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	assert.NoError(t, err)
	return string(content)
}

func TestFormatOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeLedger(t, dir, "main.beancount", unformatted)
	output := filepath.Join(dir, "out.beancount")

	stdout, _, err := runCommand(t, "format", path, "-o", output)
	assert.NoError(t, err)
	assert.Equal(t, "", stdout)
	assert.Equal(t, formatted, readLedger(t, output))
	assert.Equal(t, unformatted, readLedger(t, path))
}

func TestFormatInPlace(t *testing.T) {
	dir := t.TempDir()
	first := writeLedger(t, dir, "first.beancount", unformatted)
	second := writeLedger(t, dir, "second.beancount", unformatted)

	stdout, _, err := runCommand(t, "format", "-i", first, second)
	assert.NoError(t, err)
	assert.Equal(t, "", stdout)
	assert.Equal(t, formatted, readLedger(t, first))
	assert.Equal(t, formatted, readLedger(t, second))
}

func TestFormatSeveralFilesNeedInPlace(t *testing.T) {
	dir := t.TempDir()
	first := writeLedger(t, dir, "first.beancount", unformatted)
	second := writeLedger(t, dir, "second.beancount", unformatted)

	// Like bean-format, a usage error, with or without --output.
	for _, args := range [][]string{
		{"format", first, second},
		{"format", first, second, "-o", filepath.Join(dir, "out.beancount")},
	} {
		_, _, err := runCommand(t, args...)
		var parseErr *kong.ParseError
		assert.True(t, errors.As(err, &parseErr), "%v: %v", args, err)
	}
	assert.Equal(t, unformatted, readLedger(t, first))
	assert.Equal(t, unformatted, readLedger(t, second))
}

func TestFormatInPlaceLeavesAFileThatFailsToParse(t *testing.T) {
	dir := t.TempDir()
	bad := writeLedger(t, dir, "bad.beancount", "this is bad\n")
	good := writeLedger(t, dir, "good.beancount", unformatted)

	_, stderr, err := runCommand(t, "format", "-i", bad, good)
	var cmdErr *CommandError
	assert.True(t, errors.As(err, &cmdErr), "%v", err)
	assert.Equal(t, 1, cmdErr.ExitCode())
	assert.Contains(t, stderr, "parse error")
	assert.Equal(t, "this is bad\n", readLedger(t, bad))
	assert.Equal(t, formatted, readLedger(t, good))
}

func TestCheckJSON(t *testing.T) {
	dir := t.TempDir()
	good := writeLedger(t, dir, "good.beancount", "2020-01-01 open Assets:A\n")
	bad := writeLedger(t, dir, "bad.beancount", "2020-01-01 open Assets:A\n2020-01-02 * \"x\"\n  Assets:B  1 USD\n  Assets:A\n")

	stdout, stderr, err := runCommand(t, "check", "--json", good)
	assert.NoError(t, err)
	assert.Equal(t, "{\"errors\": []}\n", stdout)
	assert.Equal(t, "", stderr)

	// bean-check --json's bytes: the message without its location, and the
	// file's absolute path and line.
	stdout, _, err = runCommand(t, "check", "--json", bad)
	var cmdErr *CommandError
	assert.True(t, errors.As(err, &cmdErr), "%v", err)
	assert.Equal(t, 1, cmdErr.ExitCode())
	assert.Equal(t, `{"errors": [{"message": "Invalid reference to unknown account 'Assets:B'", "filename": `+pythonJSONString(bad)+`, "lineno": 2}]}`+"\n", stdout)
}

func TestDoctorMissingOpenAlias(t *testing.T) {
	path := filepath.Join(missingOpenDir, "balance_and_note.beancount")
	alias, _, err := runCommand(t, "doctor", "missing-open", path)
	assert.NoError(t, err)
	assert.Equal(t, runOurMissingOpen(t, path), alias)
}

func TestFormatOutputDashIsStdout(t *testing.T) {
	dir := t.TempDir()
	path := writeLedger(t, dir, "main.beancount", unformatted)
	t.Chdir(dir)

	stdout, _, err := runCommand(t, "format", path, "-o", "-")
	assert.NoError(t, err)
	assert.Equal(t, formatted, stdout)
	_, err = os.Stat(filepath.Join(dir, "-"))
	assert.True(t, errors.Is(err, os.ErrNotExist), "%v", err)
}

func TestFormatInPlaceNeedsFiles(t *testing.T) {
	dir := t.TempDir()
	path := writeLedger(t, dir, "main.beancount", unformatted)

	// Rejected before anything is written.
	for _, args := range [][]string{{"format", "-i"}, {"format", "-i", path, "-"}} {
		_, _, err := runCommand(t, args...)
		var parseErr *kong.ParseError
		assert.True(t, errors.As(err, &parseErr), "%v: %v", args, err)
	}
	assert.Equal(t, unformatted, readLedger(t, path))
}

func TestFormatInPlaceChecksEveryFileFirst(t *testing.T) {
	dir := t.TempDir()
	path := writeLedger(t, dir, "main.beancount", unformatted)

	_, _, err := runCommand(t, "format", "-i", path, filepath.Join(dir, "missing.beancount"))
	assert.Error(t, err)
	assert.Equal(t, unformatted, readLedger(t, path))
}

func TestCheckJSONMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.beancount")

	// Like bean-check --json, which blames <load>:0.
	stdout, _, err := runCommand(t, "check", "--json", path)
	var cmdErr *CommandError
	assert.True(t, errors.As(err, &cmdErr), "%v", err)
	assert.Equal(t, `{"errors": [{"message": `+pythonJSONString(`File "`+path+`" does not exist`)+`, "filename": "<load>", "lineno": 0}]}`+"\n", stdout)
}
