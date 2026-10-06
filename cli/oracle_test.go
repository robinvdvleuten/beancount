package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// The frozen oracle: what the official tools print for an input, recorded
// the first time a suite runs them and read back from then on, so the
// parity suites and the differential fuzzer compare against beancount
// without starting Python for every ledger. An entry is keyed by the
// official toolchain's versions, the call and the content of every file
// the call reads, so an edited fixture or another beancount is a miss,
// never a stale answer. A miss runs the tool when it is on PATH; without
// it, the comparison is skipped, as it was before the cache.

// oracleCacheEnv names the environment variable that moves the cache, by
// default in the user's cache directory, which every checkout of the
// repository shares.
const oracleCacheEnv = "BEANCOUNT_ORACLE_CACHE"

// oracleSchema is the version of the entries' layout. Changing it starts a
// new cache.
const oracleSchema = "1"

// officialRun is what an official tool printed and how it exited.
type officialRun struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

// oracleCall is one question to an official tool.
type oracleCall struct {
	// tool is the program on PATH that answers the call.
	tool string
	// args is what the answer depends on besides the inputs: the command
	// line, a script's text.
	args []string
	// inputs are the ledgers the call reads; what they include and the
	// documents directories they name count too.
	inputs []string
}

// frozenOracle answers oracleCalls from the cache at dir.
type frozenOracle struct {
	dir string
	// live holds the official tools on PATH, which answer a miss.
	live map[string]bool
	// roots are absolute directories an answer may print, written as a
	// token in the cache, so a fixture's answer holds in every checkout.
	roots []string
	// python is the interpreter bean-query runs on, when it is on PATH.
	python string
}

var (
	oracleOnce sync.Once
	frozen     *frozenOracle
)

// requireOracle returns the frozen oracle, and skips the test when neither
// the official tools are on PATH nor a run with them has filled the cache.
func requireOracle(t *testing.T) *frozenOracle {
	t.Helper()

	o := findOracle(t)
	if o == nil {
		t.Skip("bean-check not found in PATH and no recorded official output; install beancount 3.x and beanquery 0.2 to run this suite")
	}
	return o
}

// findOracle returns the frozen oracle, or nil when there is none.
func findOracle(t *testing.T) *frozenOracle {
	t.Helper()

	oracleOnce.Do(func() { frozen = newOracle(t) })
	return frozen
}

// newOracle opens the cache of the official toolchain on PATH, which it
// checks the versions of, or else of the last one that filled it.
func newOracle(t *testing.T) *frozenOracle {
	t.Helper()

	cache := os.Getenv(oracleCacheEnv)
	if cache == "" {
		dir, err := os.UserCacheDir()
		assert.NoError(t, err)
		cache = filepath.Join(dir, "beancount", "oracle")
	}
	root, err := filepath.Abs("..")
	assert.NoError(t, err)
	root, err = filepath.EvalSymlinks(root)
	assert.NoError(t, err)
	o := &frozenOracle{live: map[string]bool{}, roots: []string{root}}

	current := filepath.Join(cache, "current")
	toolchain := officialToolchain(t, o)
	if toolchain == "" {
		name, err := os.ReadFile(current)
		if err != nil {
			return nil
		}
		o.dir = filepath.Join(cache, strings.TrimSpace(string(name)))
		return o
	}
	sum := sha256.Sum256([]byte(oracleSchema + "\n" + toolchain))
	name := hex.EncodeToString(sum[:8])
	o.dir = filepath.Join(cache, name)
	assert.NoError(t, os.MkdirAll(o.dir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(o.dir, "toolchain"), []byte(toolchain), 0o644))
	assert.NoError(t, os.WriteFile(current, []byte(name+"\n"), 0o644))
	return o
}

// officialToolchain marks the official tools on PATH live in o and returns
// their versions and the Python version bean-query runs on, or "" when
// bean-check is not on PATH. It fails the test for a tool of another
// version than the suites target.
func officialToolchain(t *testing.T, o *frozenOracle) string {
	t.Helper()

	if _, err := exec.LookPath("bean-check"); err != nil {
		return ""
	}
	var versions []string
	for _, tool := range []string{"bean-check", "bean-format", "bean-doctor"} {
		if hasOfficialTool(t, tool, 3) {
			o.live[tool] = true
			versions = append(versions, tool+": "+toolVersion(t, tool, "--version"))
		}
	}
	if _, err := exec.LookPath("bean-query"); err == nil {
		requireBeanquery(t)
		o.live["bean-query"] = true
		o.python = beanqueryPython(t)
		versions = append(versions,
			"bean-query: "+toolVersion(t, "bean-query", "--version"),
			// #709: what beanquery prints depends on its Python.
			"python: "+toolVersion(t, o.python, "-c", "import sys; print(sys.version)"))
	}
	return strings.Join(versions, "\n") + "\n"
}

func toolVersion(t *testing.T, tool string, args ...string) string {
	t.Helper()

	out, err := exec.Command(tool, args...).Output()
	assert.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// run returns the official answer to call, and skips the test when there
// is none: the cache misses and the tool is not on PATH.
func (o *frozenOracle) run(t *testing.T, call oracleCall, answer func() officialRun) officialRun {
	t.Helper()

	run, ok := o.lookup(t, call, func() (officialRun, bool) { return answer(), true })
	if !ok {
		t.Skipf("no recorded output of %s %s; install it to record one", call.tool, strings.Join(call.args, " "))
	}
	return run
}

// lookup returns the official answer to call from the cache or, when the
// tool is on PATH, from answer, which it records unless answer fails. It
// returns false when there is no answer.
func (o *frozenOracle) lookup(t *testing.T, call oracleCall, answer func() (officialRun, bool)) (officialRun, bool) {
	t.Helper()

	path := filepath.Join(o.dir, o.key(t, call)+".json")
	var run officialRun
	if data, err := os.ReadFile(path); err == nil {
		assert.NoError(t, json.Unmarshal(data, &run))
		return o.restore(run), true
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read the oracle cache: %v", err)
	}
	if !o.live[call.tool] {
		return run, false
	}
	run, ok := answer()
	if !ok {
		return run, false
	}
	data, err := json.Marshal(o.tokenize(run))
	assert.NoError(t, err)
	// Write and rename, so a test binary running beside this one reads a
	// whole entry or none.
	tmp, err := os.CreateTemp(o.dir, "entry-*")
	assert.NoError(t, err)
	_, err = tmp.Write(data)
	assert.NoError(t, errors.Join(err, tmp.Close()))
	assert.NoError(t, os.Rename(tmp.Name(), path))
	return run, true
}

// isLive reports whether tool is on PATH.
func (o *frozenOracle) isLive(tool string) bool {
	return o.live[tool]
}

// with returns the oracle with root, an absolute directory, written as a
// token too.
func (o *frozenOracle) with(root string) *frozenOracle {
	c := *o
	c.roots = append(slices.Clip(o.roots), root)
	return &c
}

func (o *frozenOracle) token(i int) string {
	return fmt.Sprintf("\x00root%d\x00", i)
}

// tokenizePath writes path's root as a token.
func (o *frozenOracle) tokenizePath(s string) string {
	for i, root := range o.roots {
		s = strings.ReplaceAll(s, root, o.token(i))
	}
	return s
}

func (o *frozenOracle) tokenize(run officialRun) officialRun {
	run.Stdout, run.Stderr = o.tokenizePath(run.Stdout), o.tokenizePath(run.Stderr)
	return run
}

func (o *frozenOracle) restore(run officialRun) officialRun {
	for i, root := range o.roots {
		run.Stdout = strings.ReplaceAll(run.Stdout, o.token(i), root)
		run.Stderr = strings.ReplaceAll(run.Stderr, o.token(i), root)
	}
	return run
}

// key digests call: its tool, its arguments and every file its inputs
// read, their roots written as tokens.
func (o *frozenOracle) key(t *testing.T, call oracleCall) string {
	t.Helper()

	h := sha256.New()
	digestField(h, call.tool)
	for _, arg := range call.args {
		digestField(h, o.tokenizePath(o.absolute(t, arg)))
	}
	seen := map[string]bool{}
	for _, input := range call.inputs {
		o.digestLedger(t, h, o.absolute(t, input), seen)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// absolute returns a path argument, which may be relative to the package
// directory, as an absolute path, and any other argument as it is.
func (o *frozenOracle) absolute(t *testing.T, arg string) string {
	t.Helper()

	if !strings.HasSuffix(arg, ".beancount") || filepath.IsAbs(arg) {
		return arg
	}
	abs, err := filepath.Abs(arg)
	assert.NoError(t, err)
	return abs
}

var (
	includeLine   = regexp.MustCompile(`(?m)^include[ \t]+"([^"]*)"`)
	documentsLine = regexp.MustCompile(`(?m)^option[ \t]+"documents"[ \t]+"([^"]*)"`)
)

// digestLedger writes the content of the ledger at path to h, then that of
// every file it may include and the names of the files under its documents
// directories. It reads the include lines by pattern, not with the parser,
// so a key never depends on the code the suites test. A glob counts every
// file under the directory its pattern starts in.
func (o *frozenOracle) digestLedger(t *testing.T, h hash.Hash, path string, seen map[string]bool) {
	t.Helper()

	if seen[path] {
		return
	}
	seen[path] = true
	digestField(h, "file "+o.tokenizePath(path))
	src, err := os.ReadFile(path)
	if err != nil {
		digestField(h, "unreadable")
		return
	}
	digestField(h, string(src))

	resolve := func(name string) string {
		if filepath.IsAbs(name) {
			return name
		}
		return filepath.Join(filepath.Dir(path), name)
	}
	for _, include := range includeLine.FindAllSubmatch(src, -1) {
		pattern := resolve(string(include[1]))
		dir := pattern
		if i := strings.IndexAny(pattern, "*?["); i >= 0 {
			dir = filepath.Dir(pattern[:i+1])
		}
		o.walk(t, h, dir, func(file string) { o.digestLedger(t, h, file, seen) })
	}
	for _, documents := range documentsLine.FindAllSubmatch(src, -1) {
		o.walk(t, h, resolve(string(documents[1])), func(file string) {
			digestField(h, "document "+o.tokenizePath(file))
		})
	}
}

// walk calls visit for root, when it is a file, or else for every file
// under it, and writes to h that it has none when it does not exist.
func (o *frozenOracle) walk(t *testing.T, h hash.Hash, root string, visit func(string)) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			visit(path)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		digestField(h, "absent "+o.tokenizePath(root))
		return
	}
	assert.NoError(t, err)
}

// digestField writes s to h, its length first, so no two sequences of
// fields write the same bytes.
func digestField(h hash.Hash, s string) {
	_, _ = fmt.Fprintf(h, "%d:%s", len(s), s) // A hash never fails a write.
}

// execOfficial runs an official tool and returns what it printed and how
// it exited. It fails the test when the tool cannot run at all.
func execOfficial(t *testing.T, name string, args ...string) officialRun {
	t.Helper()

	cmd := exec.Command(name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	run := officialRun{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		run.Exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s: %v", name, err)
	}
	return run
}

func TestFrozenOracle(t *testing.T) {
	newTestOracle := func(t *testing.T, live bool, root string) *frozenOracle {
		return &frozenOracle{dir: t.TempDir(), live: map[string]bool{"tool": live}, roots: []string{root}}
	}
	write := func(t *testing.T, path, content string) {
		assert.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		assert.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	t.Run("RecordsALiveAnswerAndReadsItBack", func(t *testing.T) {
		root := t.TempDir()
		ledger := filepath.Join(root, "main.beancount")
		write(t, ledger, "2020-01-01 open Assets:Cash\n")
		o := newTestOracle(t, true, root)
		call := oracleCall{tool: "tool", args: []string{"--json", ledger}, inputs: []string{ledger}}
		calls := 0
		answer := func() officialRun { calls++; return officialRun{Stdout: "out", Stderr: "err", Exit: 1} }

		assert.Equal(t, officialRun{Stdout: "out", Stderr: "err", Exit: 1}, o.run(t, call, answer))
		assert.Equal(t, officialRun{Stdout: "out", Stderr: "err", Exit: 1}, o.run(t, call, answer))
		assert.Equal(t, 1, calls)

		offline := *o
		offline.live = map[string]bool{}
		assert.Equal(t, officialRun{Stdout: "out", Stderr: "err", Exit: 1}, offline.run(t, call, answer))
	})

	t.Run("SkipsAMissWithoutTheTool", func(t *testing.T) {
		root := t.TempDir()
		ledger := filepath.Join(root, "main.beancount")
		write(t, ledger, "")
		o := newTestOracle(t, false, root)
		var skipped bool
		t.Run("miss", func(t *testing.T) {
			defer func() { skipped = t.Skipped() }()
			o.run(t, oracleCall{tool: "tool", inputs: []string{ledger}}, func() officialRun {
				t.Fatal("ran a tool that is not on PATH")
				return officialRun{}
			})
		})
		assert.True(t, skipped)
	})

	t.Run("RecordsNoFailedAnswer", func(t *testing.T) {
		o := newTestOracle(t, true, t.TempDir())
		call := oracleCall{tool: "tool", args: []string{"x"}}
		_, ok := o.lookup(t, call, func() (officialRun, bool) { return officialRun{}, false })
		assert.False(t, ok)
		run, ok := o.lookup(t, call, func() (officialRun, bool) { return officialRun{Stdout: "late"}, true })
		assert.True(t, ok)
		assert.Equal(t, "late", run.Stdout)
	})

	t.Run("KeysWhatTheInputsRead", func(t *testing.T) {
		root := t.TempDir()
		ledger := filepath.Join(root, "main.beancount")
		write(t, ledger, "include \"sub/a.beancount\"\ninclude \"glob/**/*.beancount\"\noption \"documents\" \"docs\"\n")
		write(t, filepath.Join(root, "sub", "a.beancount"), "include \"../b.beancount\"\n")
		write(t, filepath.Join(root, "b.beancount"), "")
		o := newTestOracle(t, true, root)
		call := oracleCall{tool: "tool", inputs: []string{ledger}}
		key := o.key(t, call)
		assert.Equal(t, key, o.key(t, call))
		assert.NotEqual(t, key, o.key(t, oracleCall{tool: "other", inputs: []string{ledger}}))
		assert.NotEqual(t, key, o.key(t, oracleCall{tool: "tool", args: []string{"-m"}, inputs: []string{ledger}}))

		for _, change := range []string{
			"b.beancount",                     // included by an included file
			"glob/2020/x.beancount",           // a new match of a glob
			"docs/Assets/Cash/2020-01-01.pdf", // a document
			"sub/a.beancount",
		} {
			write(t, filepath.Join(root, change), "2020-01-01 open Assets:Cash\n")
			changed := o.key(t, call)
			assert.NotEqual(t, key, changed, "%s", change)
			key = changed
		}
		write(t, filepath.Join(root, "unrelated.beancount"), "")
		assert.Equal(t, key, o.key(t, call))
	})

	t.Run("HoldsInAnotherCheckout", func(t *testing.T) {
		dir := t.TempDir()
		a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
		for _, root := range []string{a, b} {
			write(t, filepath.Join(root, "main.beancount"), "include \"x.beancount\"\n")
		}
		o := newTestOracle(t, true, a)
		ledger := filepath.Join(a, "main.beancount")
		o.run(t, oracleCall{tool: "tool", args: []string{ledger}, inputs: []string{ledger}}, func() officialRun {
			return officialRun{Stdout: ledger + ":1: error\n"}
		})

		other := &frozenOracle{dir: o.dir, roots: []string{b}}
		ledger = filepath.Join(b, "main.beancount")
		run := other.run(t, oracleCall{tool: "tool", args: []string{ledger}, inputs: []string{ledger}}, nil)
		assert.Equal(t, ledger+":1: error\n", run.Stdout)
	})
}
