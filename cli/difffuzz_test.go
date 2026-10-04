//go:build difffuzz

package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	stdErrors "errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/query"
)

var (
	fuzzTime = flag.Duration("difffuzz.time", 30*time.Second, "how long TestDiffFuzz mutates ledgers, once it has run every seed")
	fuzzSeed = flag.Uint64("difffuzz.seed", 0, "seed of TestDiffFuzz's mutations (0 picks one)")
	fuzzOut  = flag.String("difffuzz.out", "../.difffuzz", "directory TestDiffFuzz writes minimized divergences to")
)

// fuzzQueries are the statements TestDiffFuzz runs on every ledger. They
// are fixed: the fuzzer varies the ledger, and the query fixtures the
// statements.
var fuzzQueries = []string{
	"SELECT date, flag, account, position, weight, balance",
	"SELECT account, sum(position), sum(cost(position)) GROUP BY account ORDER BY account",
	"BALANCES AT cost",
	"JOURNAL",
	"PRINT",
}

// TestDiffFuzz is a differential fuzzer: it runs the compliance fixtures and
// the fuzz corpora, then mutations of them until -difffuzz.time is up,
// through our check, format and query and through the official tools, and
// compares what the parity suites compare: the lines check reports errors
// on and whether it fails, format's bytes on a ledger ours parses, and each
// of fuzzQueries' stdout and whether it fails. It minimizes a ledger the
// two differ on by lines, confirms it with the tool itself, and writes it to
// -difffuzz.out with both outputs. A mutation is reported only for what its
// seed agrees on, so a divergence is found once and not again in every
// ledger made from its seed. A ledger bean-check or bean-query raises on is
// skipped, as is one matching knownDivergences.
//
//	go test -tags=difffuzz ./cli -run DiffFuzz -difffuzz.time=5m -timeout=0
func TestDiffFuzz(t *testing.T) {
	requireOfficialTool(t, "bean-check", 3)
	requireOfficialTool(t, "bean-format", 3)
	requireBeanquery(t)

	seed := *fuzzSeed
	if seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NoError(t, err)
	f := &fuzzer{
		t:      t,
		path:   filepath.Join(dir, "fuzz.beancount"),
		python: beanqueryPython(t),
		seen:   map[string]bool{},
		known:  map[string]int{},
	}
	defer func() { f.oracle.stop() }()

	seeds := loadFuzzSeeds(t)
	diverging := make([][]string, len(seeds))
	for i, src := range seeds {
		diverging[i] = f.run(src, nil)
	}
	rng := rand.New(rand.NewPCG(seed, 0))
	for deadline := time.Now().Add(*fuzzTime); time.Now().Before(deadline); {
		i := rng.IntN(len(seeds))
		f.run(mutate(rng, seeds[i], seeds), diverging[i])
	}

	t.Logf("seed %d: %d ledgers, %d divergences written, %d more like them, %d the tools do not confirm",
		seed, f.ledgers, f.written, f.repeats, f.unconfirmed)
	for _, name := range slices.Sorted(maps.Keys(f.known)) {
		t.Logf("known: %d × %s", f.known[name], name)
	}
	if f.written > 0 {
		t.Errorf("%d divergences from the official tools written to %s", f.written, *fuzzOut)
	}
}

// knownDivergences are the divergences TestDiffFuzz counts instead of
// writing: a minimized ledger of the kind (a prefix of the divergence's)
// that matches. Each names the issue or the KNOWN_GAPS.md entry that
// decided it. A panic is never known. Signed zero (#408) is not among them: divergences reads a
// zero's sign out of a query's output.
var knownDivergences = []struct {
	kind    string
	reason  string
	matches func(src string) bool
}{
	{"format", "#663, deliberate: bean-format turns a lone \\r into a line break; we keep it", loneCR.MatchString},
	{"check-lines", "KNOWN_GAPS.md, deliberate: a tag or link after a posting is a syntax error on its own line", bodyTagAfterPosting.MatchString},
	{"", "KNOWN_GAPS.md, a non-goal: a Built-in Plugin other than auto_accounts and implicit_prices does not run", hasIgnoredPlugin},
	{"query-4", "KNOWN_GAPS.md: PRINT quotes a custom directive's account value", customAccount.MatchString},
	{"query-4", "KNOWN_GAPS.md: PRINT ignores render_commas", func(src string) bool { return strings.Contains(src, `"render_commas"`) }},
}

var (
	loneCR              = regexp.MustCompile(`\r(?:[^\n]|$)`)
	pluginLine          = regexp.MustCompile(`(?m)^plugin\s+"([^"]*)"`)
	bodyTagAfterPosting = regexp.MustCompile(`(?m)^[ \t]+[A-Z].*\n[ \t]+[#^]`)
	customAccount       = regexp.MustCompile(`(?m)^\d{4}-\d{2}-\d{2}\s+custom\s.*\s[A-Z][A-Za-z0-9-]*:`)
)

func hasIgnoredPlugin(src string) bool {
	for _, plugin := range pluginLine.FindAllStringSubmatch(src, -1) {
		if plugin[1] != "beancount.plugins.auto_accounts" && plugin[1] != "beancount.plugins.implicit_prices" {
			return true
		}
	}
	return false
}

// verdict is what one implementation makes of a ledger. A part it has
// nothing to say on, such as the check of a ledger bean-check raises on, is
// left out of the comparison.
type verdict struct {
	checked     bool
	checkLines  []int
	checkFailed bool
	// unblamed is set when bean-check reports an error on no line of the
	// ledger (line 0, or <load>), where we blame the line that causes it
	// (lineGaps).
	unblamed bool
	// checkOutput lists the errors, a line and a message each.
	checkOutput string
	format      *string
	// queries holds fuzzQueries' outputs, nil for a statement that was not
	// run or that beanquery raises on: the statements are valid, so a ledger
	// it fails one on is one it cannot handle.
	queries  []*queryOutput
	panicked string
}

// negativeZero matches a zero beanquery prints with a sign, which booking
// gives none here (#408).
var negativeZero = regexp.MustCompile(`-(0(?:\.0+)?(?:[^.\d]|$))`)

// zeroCost matches the space unsigned leaves before a zero cost, which a
// table pads and PRINT does not.
var zeroCost = regexp.MustCompile(`\{ (0(?:\.0+)? )`)

// unsigned reads the sign out of every zero in a query's output: a space
// in its place keeps a table's columns.
func unsigned(output string) string {
	return zeroCost.ReplaceAllString(negativeZero.ReplaceAllString(output, " $1"), "{$1")
}

// divergences returns the kinds of divergence between our verdict and the
// official one: "panic", "check-exit" or else "check-lines", "format", and
// "query-N" for fuzzQueries[N].
func divergences(ours, official verdict) []string {
	var kinds []string
	if ours.panicked != "" {
		kinds = append(kinds, "panic")
	}
	if ours.checked && official.checked {
		switch {
		case ours.checkFailed != official.checkFailed:
			kinds = append(kinds, "check-exit")
		case !official.unblamed && !slices.Equal(ours.checkLines, official.checkLines):
			kinds = append(kinds, "check-lines")
		}
	}
	if ours.format != nil && official.format != nil && *ours.format != *official.format {
		kinds = append(kinds, "format")
	}
	for i := range min(len(ours.queries), len(official.queries)) {
		if ours.queries[i] == nil || official.queries[i] == nil {
			continue
		}
		got, want := *ours.queries[i], *official.queries[i]
		got.stdout, want.stdout = unsigned(got.stdout), unsigned(want.stdout)
		if got != want {
			kinds = append(kinds, fmt.Sprintf("query-%d", i))
		}
	}
	return kinds
}

// minimize returns lines without those the divergence does not need: delta
// debugging by lines, which removes ever smaller chunks while diverges
// holds, until no single line can go.
func minimize(lines []string, diverges func([]string) bool) []string {
	chunks := 2
	for len(lines) >= 2 {
		size := (len(lines) + chunks - 1) / chunks
		reduced := false
		for start := 0; start < len(lines); start += size {
			candidate := slices.Concat(lines[:start], lines[min(start+size, len(lines)):])
			if diverges(candidate) {
				lines, chunks, reduced = candidate, max(chunks-1, 2), true
				break
			}
		}
		if reduced {
			continue
		}
		if size == 1 {
			break
		}
		chunks = min(chunks*2, len(lines))
	}
	return lines
}

type fuzzer struct {
	t      *testing.T
	path   string
	python string
	oracle *oracle
	// silent counts the ledgers in a row the oracle did not answer for.
	silent int

	seen        map[string]bool
	known       map[string]int
	ledgers     int
	written     int
	repeats     int
	unconfirmed int
}

// run compares the implementations on src and returns the kinds of
// divergence between them. It reports the first of each command, unless
// the ledger src was made from diverges on that command too.
func (f *fuzzer) run(src []byte, seedKinds []string) []string {
	f.ledgers++
	kinds := divergences(f.verdicts(src))
	reported := map[string]bool{}
	for _, kind := range seedKinds {
		reported[command(kind)] = true
	}
	for _, kind := range kinds {
		if of := command(kind); !reported[of] {
			reported[of] = true
			f.report(src, kind)
		}
	}
	return kinds
}

// statement returns the index in fuzzQueries of a "query-N" kind.
func statement(kind string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(kind, "query-"))
	return n
}

// command returns the command a kind of divergence is of: "check" for
// "check-lines".
func command(kind string) string {
	name, _, _ := strings.Cut(kind, "-")
	return name
}

// verdicts writes src to the fuzzer's ledger and returns our verdict on it
// and the oracle's.
func (f *fuzzer) verdicts(src []byte) (ours, official verdict) {
	assert.NoError(f.t, os.WriteFile(f.path, src, 0o600))
	return f.ours(), f.askOracle()
}

// report minimizes src, which diverges in kind, and writes it out once the
// official tool confirms it, unless it is a known divergence or has the
// signature of one written before.
func (f *fuzzer) report(src []byte, kind string) {
	lines := minimize(strings.SplitAfter(string(src), "\n"), func(lines []string) bool {
		return slices.Contains(divergences(f.verdicts([]byte(strings.Join(lines, "")))), kind)
	})
	minimized := strings.Join(lines, "")

	for _, known := range knownDivergences {
		if kind != "panic" && strings.HasPrefix(kind, known.kind) && known.matches(minimized) {
			f.known[known.reason]++
			return
		}
	}

	ours, official := f.verdicts([]byte(minimized))
	if !slices.Contains(divergences(ours, official), kind) {
		return // The oracle did not answer this time.
	}
	name := fmt.Sprintf("%s-%x", kind, sha256.Sum256([]byte(signature(kind, ours, official))))[:len(kind)+9]
	if f.seen[name] {
		f.repeats++
		return
	}
	f.seen[name] = true

	official = f.runTool(kind)
	if !slices.Contains(divergences(ours, official), kind) {
		f.unconfirmed++
		f.t.Logf("%s: the official tool does not confirm the oracle's divergence on\n%s", kind, minimized)
		return
	}

	assert.NoError(f.t, os.MkdirAll(*fuzzOut, 0o755))
	base := filepath.Join(*fuzzOut, name)
	assert.NoError(f.t, os.WriteFile(base+".beancount", []byte(minimized), 0o644))
	assert.NoError(f.t, os.WriteFile(base+".txt", []byte(describe(kind, ours, official)), 0o644))
	f.written++
	f.t.Logf("%s: wrote %s.beancount", kind, base)
}

// outputs returns what the two implementations print for the command a
// kind of divergence is of.
func outputs(kind string, ours, official verdict) (string, string) {
	switch command(kind) {
	case "check":
		return ours.checkOutput, official.checkOutput
	case "format":
		return *ours.format, *official.format
	case "query":
		n := statement(kind)
		render := func(o *queryOutput) string {
			return fmt.Sprintf("%s%s(exit %d)\n", o.stdout, o.stderr, o.exitCode)
		}
		return render(ours.queries[n]), render(official.queries[n])
	default:
		return ours.panicked, ""
	}
}

// describe renders what the two implementations make of a ledger that
// diverges in kind.
func describe(kind string, ours, official verdict) string {
	title := kind
	if command(kind) == "query" {
		title += ": " + fuzzQueries[statement(kind)]
	}
	got, want := outputs(kind, ours, official)
	return fmt.Sprintf("%s\n\n== ours\n%s\n== official\n%s", title, got, want)
}

// varying is what differs between two ledgers that diverge for one reason:
// strings, parenthesized amounts, accounts, numbers and column widths, and
// outside check's messages, which name tokens in capitals, currencies.
var varying = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile(`"[^"]*"|'[^']*'`), `""`},
	{regexp.MustCompile(`\([^)]*\)`), "()"},
	{regexp.MustCompile(`\b[A-Z][A-Za-z0-9-]*(?::[A-Za-z0-9-]+)+`), "A"},
	{regexp.MustCompile(`-?\d+(?:\.\d+)?`), "0"},
	{regexp.MustCompile(`\s+`), " "},
	{regexp.MustCompile(`-{2,}`), "-"},
}

var currency = regexp.MustCompile(`\b[A-Z][A-Z0-9]+\b`)

// signature is what a divergence looks like without what varies from one
// ledger to the next: the lines only one of the outputs has, less varying.
// Ledgers that diverge for one reason mostly share it, so the fuzzer writes
// the first of them.
func signature(kind string, ours, official verdict) string {
	var lines []string
	only := func(side, text, other string) {
		for line := range strings.Lines(text) {
			if strings.Contains(other, line) {
				continue
			}
			for _, v := range varying {
				line = v.pattern.ReplaceAllString(line, v.with)
			}
			if command(kind) != "check" {
				line = currency.ReplaceAllString(line, "C")
			}
			lines = append(lines, side+strings.TrimSpace(line))
		}
	}
	got, want := outputs(kind, ours, official)
	only("< ", got, want)
	only("> ", want, got)
	slices.Sort(lines)
	return kind + "\n" + strings.Join(slices.Compact(lines), "\n")
}

// ours runs our check, format and fuzzQueries on the fuzzer's ledger.
func (f *fuzzer) ours() (v verdict) {
	defer func() {
		if r := recover(); r != nil {
			v.panicked = fmt.Sprint(r)
		}
	}()

	_, stderr, err := runCommand(f.t, "check", f.path)
	v.checked, v.checkFailed = true, err != nil
	v.checkLines = errorLines(f.path, stderr)
	for line := range strings.Lines(stderr) {
		if e, ok := strings.CutPrefix(line, f.path+":"); ok {
			v.checkOutput += e
		}
	}

	// format fails on a syntax error, where bean-format, which does not
	// parse, has nothing to compare with.
	if stdout, _, err := runCommand(f.t, "format", f.path); err == nil {
		v.format = &stdout
	}

	ctx := context.Background()
	qctx, _, err := loadQueryContext(ctx, io.Discard, &FileOrStdin{Filename: f.path})
	if err != nil {
		return v
	}
	for _, statement := range fuzzQueries {
		var stdout, stderr strings.Builder
		err := reportQueryError(&stderr, query.Run(ctx, qctx, statement, query.Format("text"), false, &stdout))
		output := queryOutput{stdout: stdout.String(), stderr: stderr.String()}
		var cmdErr *CommandError
		if stdErrors.As(err, &cmdErr) {
			output.exitCode = cmdErr.ExitCode()
		} else if err != nil {
			panic(err)
		}
		v.queries = append(v.queries, &output)
	}
	return v
}

// officialCheck reads bean-check --json's output for the fuzzer's ledger
// into v.
func (f *fuzzer) officialCheck(v *verdict, output []byte) {
	var result struct {
		Errors []struct {
			Message  string  `json:"message"`
			Filename *string `json:"filename"`
			Lineno   *int    `json:"lineno"`
		} `json:"errors"`
	}
	if json.Unmarshal(output, &result) != nil {
		return
	}
	v.checked, v.checkFailed = true, len(result.Errors) > 0
	for _, e := range result.Errors {
		message, _, _ := strings.Cut(e.Message, "\n")
		if e.Filename == nil || *e.Filename != f.path || e.Lineno == nil || *e.Lineno == 0 {
			v.unblamed = true
			v.checkOutput += fmt.Sprintf("<load>: %s\n", message)
			continue
		}
		v.checkOutput += fmt.Sprintf("%d: %s\n", *e.Lineno, message)
	}
	v.checkLines = jsonErrorLines(f.t, f.path, output)
}

// askOracle returns the official verdict on the fuzzer's ledger as the
// oracle process gives it, or an empty one when the oracle does not answer.
func (f *fuzzer) askOracle() (v verdict) {
	if f.oracle == nil {
		f.oracle = startOracle(f.t, f.python)
	}
	answer, ok := f.oracle.ask(f.path)
	if !ok {
		// Python may die or hang on a ledger, which is then skipped, but
		// not on one after the other.
		stderr := f.oracle.stop()
		f.oracle = nil
		if f.silent++; f.silent == 3 {
			f.t.Fatalf("the oracle does not answer:\n%s", stderr)
		}
		return v
	}
	f.silent = 0
	if string(answer.Check) != "null" {
		f.officialCheck(&v, answer.Check)
	}
	v.format = answer.Format
	for _, stdout := range answer.Queries {
		var output *queryOutput
		if stdout != nil {
			output = &queryOutput{stdout: *stdout}
		}
		v.queries = append(v.queries, output)
	}
	return v
}

// runTool returns the official verdict on the fuzzer's ledger as the tool a
// kind of divergence is of gives it.
func (f *fuzzer) runTool(kind string) (v verdict) {
	switch command(kind) {
	case "check":
		// bean-check exits 1 on a ledger with errors and when it raises,
		// which prints no JSON.
		output, _ := exec.Command("bean-check", "--json", f.path).Output()
		f.officialCheck(&v, output)
	case "format":
		if output, err := exec.Command("bean-format", f.path).Output(); err == nil {
			formatted := string(output)
			v.format = &formatted
		}
	case "query":
		n := statement(kind)
		v.queries = make([]*queryOutput, len(fuzzQueries))
		if output, err := exec.Command("bean-query", "-f", "text", f.path, fuzzQueries[n]).Output(); err == nil {
			v.queries[n] = &queryOutput{stdout: string(output)}
		}
	}
	return v
}

// oracle is the official tools as one process, testdata/difffuzz_oracle.py,
// which answers for a ledger without Python's startup.
type oracle struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  bytes.Buffer
	answers chan []byte
}

type oracleAnswer struct {
	Check   json.RawMessage `json:"check"`
	Format  *string         `json:"format"`
	Queries []*string       `json:"queries"`
}

func startOracle(t *testing.T, python string) *oracle {
	t.Helper()

	// One answer may come after ask gave up on it.
	o := &oracle{answers: make(chan []byte, 1)}
	o.cmd = exec.Command(python, filepath.Join("testdata", "difffuzz_oracle.py"))
	o.cmd.Stderr = &o.stderr
	stdin, err := o.cmd.StdinPipe()
	assert.NoError(t, err)
	stdout, err := o.cmd.StdoutPipe()
	assert.NoError(t, err)
	assert.NoError(t, o.cmd.Start())
	o.stdin = stdin

	go func() {
		defer close(o.answers)
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			o.answers <- line
		}
	}()
	return o
}

// ask returns the oracle's answer for the ledger at path, or false when it
// dies or takes too long over it.
func (o *oracle) ask(path string) (answer oracleAnswer, ok bool) {
	request, err := json.Marshal(map[string]any{"path": path, "queries": fuzzQueries})
	if err != nil {
		panic(err)
	}
	if _, err := o.stdin.Write(append(request, '\n')); err != nil {
		return answer, false
	}
	select {
	case line, open := <-o.answers:
		return answer, open && json.Unmarshal(line, &answer) == nil
	case <-time.After(30 * time.Second):
		return answer, false
	}
}

// stop ends the process and returns what it wrote on stderr.
func (o *oracle) stop() string {
	if o == nil {
		return ""
	}
	_ = o.stdin.Close()
	_ = o.cmd.Process.Kill()
	_ = o.cmd.Wait()
	return o.stderr.String()
}

// loadFuzzSeeds returns the ledgers the fuzzer starts from: every
// compliance fixture, but those lineGaps and formatGaps list (queryGaps'
// are about their statements), and the parser's and the formatter's fuzz
// corpora.
func loadFuzzSeeds(t *testing.T) [][]byte {
	t.Helper()

	var seeds [][]byte
	err := filepath.WalkDir(complianceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".beancount" {
			return err
		}
		name := strings.TrimSuffix(filepath.Base(path), ".beancount")
		name = strings.TrimSuffix(strings.TrimSuffix(name, ".pass"), ".fail")
		_, lineGap := lineGaps[name]
		_, formatGap := formatGaps[name]
		if dir := filepath.Base(filepath.Dir(path)); (lineGap && dir == "compliance") || (formatGap && dir == "format") {
			return nil
		}
		src, err := os.ReadFile(path)
		seeds = append(seeds, src)
		return err
	})
	assert.NoError(t, err)

	for _, corpus := range []string{"../parser/testdata/fuzz/FuzzParser", "../formatter/testdata/fuzz/FuzzFormatter"} {
		paths, err := filepath.Glob(filepath.Join(corpus, "*"))
		assert.NoError(t, err)
		for _, path := range paths {
			entry, err := os.ReadFile(path)
			assert.NoError(t, err)
			seeds = append(seeds, decodeFuzzCorpus(string(entry))...)
		}
	}
	return seeds
}

// decodeFuzzCorpus returns the []byte and string values of a go test fuzz
// v1 corpus entry.
func decodeFuzzCorpus(entry string) [][]byte {
	var values [][]byte
	for line := range strings.Lines(entry) {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"[]byte(", "string("} {
			quoted, ok := strings.CutPrefix(line, prefix)
			if !ok {
				continue
			}
			if value, err := strconv.Unquote(strings.TrimSuffix(quoted, ")")); err == nil {
				values = append(values, []byte(value))
			}
		}
	}
	return values
}

// mutate returns src after one to three of the mutations.
func mutate(rng *rand.Rand, src []byte, seeds [][]byte) []byte {
	lines := strings.SplitAfter(string(src), "\n")
	for range 1 + rng.IntN(3) {
		if len(lines) == 0 {
			break
		}
		lines = mutations[rng.IntN(len(mutations))](rng, lines, seeds)
	}
	return []byte(strings.Join(lines, ""))
}

var (
	numberPattern   = regexp.MustCompile(`\s(-?\d+(?:\.\d+)?)(?:\s|$)`)
	currencyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]{2,}\b`)
	accountPattern  = regexp.MustCompile(`\b(?:Assets|Liabilities|Equity|Income|Expenses)(?::[A-Za-z0-9-]+)+`)
	datePattern     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	postingPattern  = regexp.MustCompile(`^(\s+(?:[!*] )?[A-Z][A-Za-z0-9-]*(?::[A-Za-z0-9-]+)+)(.*?)(\r?\n?)$`)
	indentPattern   = regexp.MustCompile(`^[ \t]*`)
	flagPattern     = regexp.MustCompile(` ([*!]|txn) `)
)

// mutations change a ledger's lines the way ledgers differ from one
// another: they keep most of it valid, since a ledger that no longer parses
// tests little past the parser.
var mutations = []func(rng *rand.Rand, lines []string, seeds [][]byte) []string{
	// Drop a line.
	func(rng *rand.Rand, lines []string, _ [][]byte) []string {
		i := rng.IntN(len(lines))
		return slices.Delete(slices.Clone(lines), i, i+1)
	},
	// Repeat a line.
	func(rng *rand.Rand, lines []string, _ [][]byte) []string {
		i := rng.IntN(len(lines))
		return slices.Insert(slices.Clone(lines), i, lines[i])
	},
	// Swap two lines.
	func(rng *rand.Rand, lines []string, _ [][]byte) []string {
		lines = slices.Clone(lines)
		i, j := rng.IntN(len(lines)), rng.IntN(len(lines))
		lines[i], lines[j] = lines[j], lines[i]
		return lines
	},
	// Insert a line of another ledger.
	func(rng *rand.Rand, lines []string, seeds [][]byte) []string {
		other := strings.SplitAfter(string(seeds[rng.IntN(len(seeds))]), "\n")
		return slices.Insert(slices.Clone(lines), rng.IntN(len(lines)+1), other[rng.IntN(len(other))])
	},
	// Append a directive of another ledger.
	func(rng *rand.Rand, lines []string, seeds [][]byte) []string {
		blocks := strings.Split(string(seeds[rng.IntN(len(seeds))]), "\n\n")
		return append(slices.Clone(lines), "\n", blocks[rng.IntN(len(blocks))]+"\n")
	},
	// Insert an option or a plugin that changes what the ledger means.
	func(rng *rand.Rand, lines []string, _ [][]byte) []string {
		return slices.Insert(slices.Clone(lines), 0, pick(rng, fuzzOptions)+"\n")
	},
	// Change a number.
	rewrite(numberPattern, func(rng *rand.Rand, number string, _ []string) string {
		return pick(rng, []string{"-" + number, "0", number + "0", number + ".005", number + "1", "1", strings.TrimLeft(number, "-0123456789")})
	}),
	// Use another of the ledger's currencies, or a common one.
	rewrite(currencyPattern, func(rng *rand.Rand, _ string, lines []string) string {
		return pick(rng, append(currencyPattern.FindAllString(strings.Join(lines, ""), -1), "USD", "EUR", "HOOL"))
	}),
	// Use another of the ledger's accounts.
	rewrite(accountPattern, func(rng *rand.Rand, account string, lines []string) string {
		return pick(rng, append(accountPattern.FindAllString(strings.Join(lines, ""), -1), account+":Sub"))
	}),
	// Use another of the ledger's dates, or one around it.
	rewrite(datePattern, func(rng *rand.Rand, date string, lines []string) string {
		choices := datePattern.FindAllString(strings.Join(lines, ""), -1)
		if parsed, err := time.Parse(time.DateOnly, date); err == nil {
			for _, days := range []int{-366, -1, 1, 366} {
				choices = append(choices, parsed.AddDate(0, 0, days).Format(time.DateOnly))
			}
		}
		return pick(rng, choices)
	}),
	// Flag a transaction otherwise.
	rewrite(flagPattern, func(rng *rand.Rand, _ string, _ []string) string {
		return pick(rng, []string{"*", "!", "txn", "P"})
	}),
	// Give a posting a cost or a price, or take its amount away.
	rewrite(postingPattern, func(rng *rand.Rand, posting string, _ []string) string {
		parts := postingPattern.FindStringSubmatch(posting)
		return parts[1] + pick(rng, []string{"", parts[2] + pick(rng, fuzzCostsAndPrices)}) + parts[3]
	}),
	// Indent a line otherwise.
	rewrite(indentPattern, func(rng *rand.Rand, _ string, _ []string) string {
		return pick(rng, []string{"", " ", "  ", "    ", "\t", " \t"})
	}),
	// End a line otherwise.
	func(rng *rand.Rand, lines []string, _ [][]byte) []string {
		lines = slices.Clone(lines)
		i := rng.IntN(len(lines))
		lines[i] = strings.TrimRight(lines[i], "\r\n") + pick(rng, []string{"", "\n", "\r\n", "\r", "  \n", " ; ✨\n", " #tag\n", " ^link\n"})
		return lines
	},
}

var fuzzOptions = []string{
	`option "booking_method" "STRICT"`,
	`option "booking_method" "STRICT_WITH_SIZE"`,
	`option "booking_method" "NONE"`,
	`option "booking_method" "FIFO"`,
	`option "booking_method" "LIFO"`,
	`option "booking_method" "HIFO"`,
	`option "booking_method" "AVERAGE"`,
	`option "inferred_tolerance_default" "*:0.01"`,
	`option "inferred_tolerance_default" "USD:0.5"`,
	`option "inferred_tolerance_multiplier" "1.1"`,
	`option "infer_tolerance_from_cost" "TRUE"`,
	`option "operating_currency" "USD"`,
	`option "render_commas" "TRUE"`,
	`option "display_precision" "USD:0.001"`,
	`option "name_assets" "Activa"`,
	`plugin "beancount.plugins.auto_accounts"`,
	`plugin "beancount.plugins.implicit_prices"`,
}

var fuzzCostsAndPrices = []string{
	" {}",
	" {1.5 USD}",
	" {{10 USD}}",
	" {2 # 3 USD}",
	" {2020-01-01}",
	` {"lot"}`,
	" {*}",
	" {1.5 USD, 2020-01-01}",
	" @ 2 USD",
	" @@ 20 USD",
	" @ 0 USD",
	" @",
	" {1 USD} @ 2 USD",
}

func pick(rng *rand.Rand, choices []string) string {
	return choices[rng.IntN(len(choices))]
}

// rewrite returns the mutation that replaces one match of pattern, or of
// its group when it has just one, on a line that has one, with what replace
// makes of it.
func rewrite(pattern *regexp.Regexp, replace func(rng *rand.Rand, match string, lines []string) string) func(*rand.Rand, []string, [][]byte) []string {
	return func(rng *rand.Rand, lines []string, _ [][]byte) []string {
		var candidates []int
		for i, line := range lines {
			if pattern.MatchString(line) {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 0 {
			return lines
		}
		lines = slices.Clone(lines)
		i := candidates[rng.IntN(len(candidates))]
		matches := pattern.FindAllStringSubmatchIndex(lines[i], -1)
		match := matches[rng.IntN(len(matches))]
		start, end := match[0], match[1]
		if pattern.NumSubexp() == 1 {
			start, end = match[2], match[3]
		}
		lines[i] = lines[i][:start] + replace(rng, lines[i][start:end], lines) + lines[i][end:]
		return lines
	}
}

func TestMinimize(t *testing.T) {
	lines := strings.SplitAfter("a\nb\nc\nd\ne\nf\ng\nh\n", "\n")
	needsBoth := func(lines []string) bool {
		return slices.Contains(lines, "c\n") && slices.Contains(lines, "f\n")
	}
	assert.Equal(t, []string{"c\n", "f\n"}, minimize(lines, needsBoth))
	assert.Equal(t, []string{"c\n"}, minimize([]string{"c\n"}, needsBoth))
}

func TestDivergences(t *testing.T) {
	text := func(s string) *string { return &s }
	checked := func(failed bool, lines ...int) verdict {
		return verdict{checked: true, checkFailed: failed, checkLines: lines}
	}

	assert.Equal(t, nil, divergences(checked(true, 3), checked(true, 3)))
	assert.Equal(t, []string{"check-lines"}, divergences(checked(true, 3), checked(true, 4)))
	assert.Equal(t, []string{"check-exit"}, divergences(checked(false), checked(true, 4)))

	// bean-check blames no line where we blame one, or raises.
	unblamed := checked(true, 0)
	unblamed.unblamed = true
	assert.Equal(t, nil, divergences(checked(true, 3), unblamed))
	assert.Equal(t, []string{"check-exit"}, divergences(checked(false), unblamed))
	assert.Equal(t, nil, divergences(checked(true, 3), verdict{}))

	// format compares only where both have an output.
	assert.Equal(t, []string{"format"}, divergences(verdict{format: text("a")}, verdict{format: text("b")}))
	assert.Equal(t, nil, divergences(verdict{}, verdict{format: text("b")}))

	// A query compares only where beanquery runs the statement, and a zero
	// whatever its sign (#408).
	ours := verdict{queries: []*queryOutput{{stdout: "a"}, {stdout: "b"}, {stdout: " 0.00 USD  -0.001  -0 USD\n"}}}
	official := verdict{queries: []*queryOutput{{stdout: "a"}, {stdout: "c"}, {stdout: "-0.00 USD  -0.001  -0 USD\n"}}}
	assert.Equal(t, []string{"query-1"}, divergences(ours, official))
	official.queries[1] = nil
	assert.Equal(t, nil, divergences(ours, official))
	assert.Equal(t, "{0 EUR, 2020-01-03}   0 USD", unsigned("{-0 EUR, 2020-01-03}  -0 USD"))
	assert.Equal(t, unsigned("-1 HOOL { 0.00 USD}  -0.001"), unsigned("-1 HOOL {-0.00 USD}  -0.001"))
	assert.Equal(t, nil, divergences(ours, verdict{}))
	failed := verdict{queries: []*queryOutput{{stderr: "error: boom\n", exitCode: 1}}}
	assert.Equal(t, []string{"query-0"}, divergences(failed, verdict{queries: []*queryOutput{{}}}))

	assert.Equal(t, []string{"panic"}, divergences(verdict{panicked: "boom"}, verdict{}))
}

func TestSignature(t *testing.T) {
	sign := func(ours, official string) string {
		return signature("check-lines", verdict{checkOutput: ours}, verdict{checkOutput: official})
	}
	first := sign("2: Invalid reference to unknown account 'Assets:A'\n",
		"3: Invalid account name: Assets:A\n2: Invalid reference to unknown account 'Assets:A'\n")
	second := sign("7: Invalid reference to unknown account 'Assets:Csah'\n",
		"8: Invalid account name: Assets:Csah\n7: Invalid reference to unknown account 'Assets:Csah'\n")
	assert.Equal(t, "check-lines\n> 0: Invalid account name: A", first)
	assert.Equal(t, first, second)

	diff := func(ours, official string) string {
		return signature("query-0", verdict{queries: []*queryOutput{{stdout: ours}}}, verdict{queries: []*queryOutput{{stdout: official}}})
	}
	assert.Equal(t,
		diff("2020-02-01 balance Assets:A     5 EUR\n", "2020-02-01 balance Assets:A     5 EUR   ; Diff: -5 EUR\n"),
		diff("2020-03-01 balance Assets:Cash  -100.00 USD\n", "2020-03-01 balance Assets:Cash  -100.00 USD   ; Diff: 100.00 USD\n"))
}

// TestMutate checks that no mutation fails on a seed, whatever it holds.
func TestMutate(t *testing.T) {
	seeds := loadFuzzSeeds(t)
	rng := rand.New(rand.NewPCG(1, 0))
	for range 20000 {
		mutate(rng, seeds[rng.IntN(len(seeds))], seeds)
	}
}

func TestDecodeFuzzCorpus(t *testing.T) {
	entry := "go test fuzz v1\n[]byte(\"2020-01-01 open Assets:A\\n\")\nstring(\"x\")\nint(3)\n"
	assert.Equal(t, [][]byte{[]byte("2020-01-01 open Assets:A\n"), []byte("x")}, decodeFuzzCorpus(entry))
}

func TestKnownDivergences(t *testing.T) {
	assert.True(t, loneCR.MatchString("a\rb\n"))
	assert.True(t, loneCR.MatchString("a\r"))
	assert.False(t, loneCR.MatchString("a\r\nb\n"))

	assert.True(t, hasIgnoredPlugin("plugin \"beancount.plugins.auto_accounts\"\nplugin \"beancount.plugins.leafonly\"\n"))
	assert.False(t, hasIgnoredPlugin("plugin \"beancount.plugins.auto_accounts\"\n"))

	assert.True(t, customAccount.MatchString("2020-01-01 custom \"budget\" Assets:A 1 USD\n"))
	assert.False(t, customAccount.MatchString("2020-01-01 custom \"budget\" \"Assets:A\"\n  Assets:A 1 USD\n"))
}
