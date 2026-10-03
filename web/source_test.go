package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/diagnostic"
	"github.com/robinvdvleuten/beancount/ledgerload"
	"github.com/robinvdvleuten/beancount/loader"
)

// TestSourceErrorJSON pins the bytes the web API sends for each kind of
// error, with the ledger's directory as DIR.
func TestSourceErrorJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.beancount")
	assert.NoError(t, os.WriteFile(path, []byte(`option "nosuch" "1"
option "booking_method" "NOPE"
option "inferred_tolerance_multiplier" "0.5"
option "allow_pipe_separator" "TRUE"
2024-01-01 open Assets:Cash
2024-01-02 garbage
2024-01-03 * "spent"
  Assets:Cash  -5 USD
  Expenses:Food  5 USD
2024-01-04 balance Assets:Cash 1 USD
`), 0o600))

	result, err := ledgerload.Load(context.Background(), loader.Source{Path: path})
	assert.NoError(t, err)

	var got []string
	for _, err := range diagnostic.Errors(result.Diagnostics()) {
		data, err := json.Marshal(jsonSafeSourceError(err))
		assert.NoError(t, err)
		got = append(got, strings.ReplaceAll(string(data), dir, "DIR"))
	}
	assert.Equal(t, []string{
		`{"message":"DIR/main.beancount:6:12: invalid token \"garbage\"","position":{"filename":"DIR/main.beancount","offset":172,"line":6,"column":12},"type":"ParseError"}`,
		`{"message":"DIR/main.beancount:1: Invalid option: 'nosuch'","position":{"filename":"DIR/main.beancount","offset":0,"line":1,"column":1},"type":"InvalidOptionError"}`,
		`{"message":"DIR/main.beancount:2: Error for option 'booking_method': 'NOPE'","position":{"filename":"DIR/main.beancount","offset":20,"line":2,"column":1},"type":"OptionValueError"}`,
		`{"message":"DIR/main.beancount:3: Renamed to 'tolerance_multiplier'.","position":{"filename":"DIR/main.beancount","offset":51,"line":3,"column":1},"type":"RenamedOptionError"}`,
		`{"message":"DIR/main.beancount:4: Allowing pipe separator temporarily; this will go away eventually.","position":{"filename":"DIR/main.beancount","offset":96,"line":4,"column":1},"type":"DeprecatedOptionError"}`,
		`{"account":"Expenses:Food","date":"2024-01-03","message":"DIR/main.beancount:7: Invalid reference to unknown account 'Expenses:Food'","position":{"filename":"DIR/main.beancount","offset":191,"line":7,"column":12},"type":"AccountNotOpenError"}`,
		`{"account":"Assets:Cash","date":"2024-01-04","message":"DIR/main.beancount:10: Balance mismatch for Assets:Cash:\n  Expected: 1 USD\n  Actual:   -5 USD","position":{"filename":"DIR/main.beancount","offset":257,"line":10,"column":12},"type":"BalanceMismatchError"}`,
	}, got)
}
