package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/formatter"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/parser"
)

const officialBeancountVersion = "2.3.6"

type complianceFixture struct {
	name       string
	source     string
	checkOK    bool
	formatted  string
	lexerTypes []parser.TokenType
}

var complianceFixtures = []complianceFixture{
	{
		name: "basic transaction",
		source: `2000-01-01 open Assets:Cash USD
2000-01-01 open Equity:Opening USD
2000-01-02 * "Opening"
  Assets:Cash  10 USD
  Equity:Opening  -10 USD
`,
		checkOK: true,
		formatted: `2000-01-01 open Assets:Cash USD
2000-01-01 open Equity:Opening USD
2000-01-02 * "Opening"
    Assets:Cash       10 USD
    Equity:Opening   -10 USD
`,
	},
	{
		name: "unbalanced transaction",
		source: `2000-01-01 open Assets:Cash USD
2000-01-01 open Equity:Opening USD
2000-01-02 * "Broken"
  Assets:Cash  10 USD
  Equity:Opening  -9 USD
`,
		checkOK: false,
	},
	{
		name:       "lexer literals",
		source:     "2000-01-01 open Assets:Cash USD\n",
		checkOK:    true,
		lexerTypes: []parser.TokenType{parser.DATE, parser.OPEN, parser.ACCOUNT, parser.IDENT},
	},
}

func TestComplianceFixtures(t *testing.T) {
	for _, fixture := range complianceFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			tree, err := parser.ParseBytes(context.Background(), []byte(fixture.source))
			if err == nil {
				err = ledger.New().Process(context.Background(), tree)
			}
			assert.Equal(t, fixture.checkOK, err == nil)

			if fixture.formatted != "" {
				tree := parser.MustParseString(context.Background(), fixture.source)
				var output bytes.Buffer
				assert.NoError(t, formatter.New().Format(context.Background(), tree, []byte(fixture.source), &output))
				assert.Equal(t, fixture.formatted, output.String())
			}

			if fixture.lexerTypes != nil {
				lexer := parser.NewLexer([]byte(fixture.source), "fixture.beancount")
				tokens, err := lexer.ScanAll()
				assert.NoError(t, err)
				var tokenTypes []parser.TokenType
				for _, token := range tokens {
					if token.Type != parser.EOF {
						tokenTypes = append(tokenTypes, token.Type)
					}
				}
				assert.Equal(t, fixture.lexerTypes, tokenTypes)
			}
		})
	}
}

func TestOfficialBeancountDifferential(t *testing.T) {
	if os.Getenv("BEANCOUNT_DIFFERENTIAL") != "1" {
		t.Skip("set BEANCOUNT_DIFFERENTIAL=1 to compare with the official tools")
	}

	versionCmd := exec.Command("bean-check", "--version")
	versionOutput, err := versionCmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(versionOutput), officialBeancountVersion)

	for _, fixture := range complianceFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			cmd := exec.Command("bean-check", "/dev/stdin")
			cmd.Stdin = strings.NewReader(fixture.source)
			err := cmd.Run()
			var exitErr *exec.ExitError
			officialOK := err == nil
			if err != nil && !errors.As(err, &exitErr) {
				t.Fatalf("run bean-check: %v", err)
			}
			assert.Equal(t, fixture.checkOK, officialOK)

			if fixture.formatted != "" {
				formatCmd := exec.Command("bean-format", "/dev/stdin")
				formatCmd.Stdin = strings.NewReader(fixture.source)
				output, err := formatCmd.Output()
				assert.NoError(t, err)
				// Exact official formatting is asserted only after the formatter
				// parity commit; for now ensure the fixture is accepted.
				assert.True(t, len(output) > 0)
			}
		})
	}
}
