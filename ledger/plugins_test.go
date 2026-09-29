package ledger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestPluginsBeancountV3Removed(t *testing.T) {
	// beancount v3 removed these modules, so naming one fails to import.
	removed := []string{
		"book_conversions", "divert_expenses", "exclude_tag", "fill_account", "fix_payees", "forecast",
		"ira_contribs", "mark_unverified", "merge_meta", "split_expenses", "tag_pending", "unrealized",
	}
	var source strings.Builder
	for _, name := range removed {
		source.WriteString(`plugin "beancount.plugins.` + name + "\"\n")
	}

	err := New().Process(context.Background(), parser.MustParseString(context.Background(), source.String()))
	var validation *ValidationErrors
	assert.True(t, errors.As(err, &validation), "%v", err)
	var lines []int
	for _, e := range validation.Errors {
		diagnostic := e.(*Diagnostic)
		assert.Equal(t, "PluginImportError", diagnostic.Kind())
		lines = append(lines, diagnostic.GetPosition().Line)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, lines)
}
