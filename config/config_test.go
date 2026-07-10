package config

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestFromASTOptionValidation(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name:   "supported option",
			source: `option "booking_method" "FIFO"`,
		},
		{
			name:   "known but unconsumed options are ignored",
			source: "option \"operating_currency\" \"USD\"\noption \"title\" \"My Ledger\"",
		},
		{
			name:    "unknown option is rejected",
			source:  `option "nomatch" "x"`,
			wantErr: `invalid option: "nomatch"`,
		},
		{
			name:    "reserved option may not be set",
			source:  `option "filename" "x"`,
			wantErr: `option "filename" may not be set`,
		},
		{
			name:    "deprecated plugin option may not be set",
			source:  `option "plugin" "beancount.plugins.auto"`,
			wantErr: `option "plugin" may not be set`,
		},
		{
			name:    "all invalid options are reported",
			source:  "option \"nomatch\" \"x\"\noption \"filename\" \"y\"",
			wantErr: `invalid option: "nomatch"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parser.MustParseString(context.Background(), tt.source)
			cfg, err := FromAST(tree)

			if tt.wantErr == "" {
				assert.NoError(t, err)
				assert.True(t, cfg != nil)
				return
			}
			assert.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tt.wantErr),
				"error %q should contain %q", err.Error(), tt.wantErr)
		})
	}
}

func TestOperatingCurrenciesAccumulate(t *testing.T) {
	tree := parser.MustParseString(context.Background(),
		"option \"operating_currency\" \"USD\"\noption \"operating_currency\" \"EUR\"\noption \"operating_currency\" \"USD\"")
	cfg, err := FromAST(tree)
	assert.NoError(t, err)
	// Declaration order, duplicates preserved, matching beancount's
	// list-typed option semantics.
	assert.Equal(t, []string{"USD", "EUR", "USD"}, cfg.OperatingCurrencies)

	cfg, err = FromAST(parser.MustParseString(context.Background(), `option "title" "No currencies"`))
	assert.NoError(t, err)
	assert.Equal(t, 0, len(cfg.OperatingCurrencies))
}
