package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// withStdin makes input what the command under test reads from stdin.
func withStdin(t *testing.T, input string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")
	assert.NoError(t, os.WriteFile(path, []byte(input), 0o600))
	file, err := os.Open(path)
	assert.NoError(t, err)
	stdin := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = stdin
		_ = file.Close()
	})
}

// runOfficialCheckStdin runs bean-check on /dev/stdin with input and
// returns its output, when bean-check 3.x is installed.
func runOfficialCheckStdin(t *testing.T, input string) (string, bool) {
	t.Helper()

	if !hasOfficialTool(t, "bean-check", 3) {
		return "", false
	}
	cmd := exec.Command("bean-check", "/dev/stdin")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run bean-check: %v", err)
	}
	return string(out), true
}

// TestCheckStdin pins how check loads a ledger from stdin, which is how
// bean-check /dev/stdin loads it: like a file at /dev/stdin, so it recovers
// from syntax errors, follows includes and discovers documents, with
// relative paths resolved against /dev. Our errors name the source /dev/stdin too.
func TestCheckStdin(t *testing.T) {
	t.Run("SyntaxErrorThenValidDirectives", func(t *testing.T) {
		input := "2024-01-01 open Assets:Cash\n" +
			"2024-01-02 garbage here\n" +
			"2024-01-03 open Assets:Bank\n" +
			"2024-01-04 * \"x\"\n" +
			"  Assets:Cash  1 USD\n" +
			"  Assets:Bank  2 USD\n"
		withStdin(t, input)
		_, stderr, err := runCommand(t, "check", "-")
		var cmdErr *CommandError
		assert.True(t, errors.As(err, &cmdErr), "want a CommandError, got %v", err)
		// The directives after the syntax error are checked.
		assert.Equal(t, []int{2, 4}, errorLines("/dev/stdin", stderr), stderr)
		assert.Contains(t, stderr, "/dev/stdin:4: Transaction does not balance: (3 USD)")

		if official, ok := runOfficialCheckStdin(t, input); ok {
			assert.Equal(t, errorLines("/dev/stdin", official), errorLines("/dev/stdin", stderr), "bean-check:\n%s\nours:\n%s", official, stderr)
		}
	})

	t.Run("RelativeIncludeMatchesNothing", func(t *testing.T) {
		input := "include \"inc.beancount\"\n2024-01-01 open Assets:Cash\n"
		withStdin(t, input)
		_, stderr, err := runCommand(t, "check", "-")
		var cmdErr *CommandError
		assert.True(t, errors.As(err, &cmdErr), "want a CommandError, got %v", err)
		const message = `File glob "inc.beancount" does not match any files`
		// Like a file's, the missing include is blamed on its line, and
		// bean-check's on <load>:0 (KNOWN_GAPS.md).
		assert.Contains(t, stderr, "/dev/stdin:1: "+message)

		if official, ok := runOfficialCheckStdin(t, input); ok {
			assert.Contains(t, official, "<load>:0: "+message)
		}
	})

	t.Run("AbsoluteIncludeIsFollowed", func(t *testing.T) {
		included := filepath.Join(t.TempDir(), "inc.beancount")
		assert.NoError(t, os.WriteFile(included, []byte("2024-01-01 open Assets:Bank\n"+
			"2024-01-02 * \"x\"\n"+
			"  Assets:Bank  1 USD\n"+
			"  Assets:Cash  2 USD\n"), 0o600))
		input := "include \"" + filepath.ToSlash(included) + "\"\n2024-01-01 open Assets:Cash\n"
		withStdin(t, input)
		_, stderr, err := runCommand(t, "check", "-")
		var cmdErr *CommandError
		assert.True(t, errors.As(err, &cmdErr), "want a CommandError, got %v", err)
		assert.Equal(t, []int{2}, errorLines(included, stderr), stderr)

		if official, ok := runOfficialCheckStdin(t, input); ok {
			assert.Equal(t, errorLines(included, official), errorLines(included, stderr), "bean-check:\n%s\nours:\n%s", official, stderr)
		}
	})

	t.Run("RelativeDocumentsRootResolvesAgainstDev", func(t *testing.T) {
		input := "option \"documents\" \"docs\"\n2024-01-01 open Assets:Cash\n"
		withStdin(t, input)
		_, stderr, err := runCommand(t, "check", "-")
		var cmdErr *CommandError
		assert.True(t, errors.As(err, &cmdErr), "want a CommandError, got %v", err)
		root, err := filepath.Abs("/dev/docs")
		assert.NoError(t, err)
		message := "Document root '" + root + "' does not exist"
		assert.Contains(t, stderr, "/dev/stdin:1: "+message)

		if official, ok := runOfficialCheckStdin(t, input); ok {
			assert.Contains(t, official, message)
		}
	})
}
