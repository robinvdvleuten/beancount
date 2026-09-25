package importer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// fixturePath is the fixture Importer, built once for the package's tests.
var fixturePath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "importer-fixture")
	if err != nil {
		panic(err)
	}
	fixturePath = filepath.Join(dir, "fixture")
	if runtime.GOOS == "windows" {
		fixturePath += ".exe"
	}
	build := exec.Command("go", "build", "-o", fixturePath, "./testdata/fixture")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// syncBuffer collects the Importer's output, which go-plugin writes from
// its own goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func open(t *testing.T, mode string) (*Client, *syncBuffer) {
	t.Helper()
	t.Setenv("FIXTURE_MODE", mode)
	stderr := &syncBuffer{}
	client, err := Open(context.Background(), fixturePath, stderr)
	assert.NoError(t, err)
	t.Cleanup(client.Close)
	return client, stderr
}

func TestIdentify(t *testing.T) {
	client, _ := open(t, "")
	ctx := context.Background()

	matches, err := client.Identify(ctx, "statement.csv")
	assert.NoError(t, err)
	assert.True(t, matches)

	matches, err = client.Identify(ctx, "statement.ofx")
	assert.NoError(t, err)
	assert.False(t, matches)
}

func TestExtract(t *testing.T) {
	client, stderr := open(t, "")

	directives, err := client.Extract(context.Background(), "statement.csv")
	assert.NoError(t, err)

	assert.Equal(t, `2024-01-15 * "Coffee Shop" "Latte"
  import-id: "TX-001"
  Expenses:Food                       4.50 USD
  Assets:Checking
2024-01-15 balance Assets:Checking  100.00 USD
`, format(t, directives))

	client.Close()
	assert.Contains(t, stderr.String(), "log line from statement.csv")
	assert.Contains(t, stderr.String(), "stderr line")
}

func TestExtractErrors(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{mode: "extract-error", want: "statement is truncated"},
		{mode: "wrong-kind", want: "directive 1: protocol v1 carries only transactions and balance assertions, not price"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			client, _ := open(t, tt.mode)
			_, err := client.Extract(context.Background(), "statement.csv")
			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestOpenErrors(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{mode: "protocol-2", want: "incompatible API version with plugin. Plugin version: 2, Client versions: [1]"},
		{mode: "not-importer", want: "Unrecognized remote plugin message: hello"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Setenv("FIXTURE_MODE", tt.mode)
			_, err := Open(context.Background(), fixturePath, &syncBuffer{})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "failed to start "+fixturePath+" as a beancount Importer (protocol version 1)")
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
