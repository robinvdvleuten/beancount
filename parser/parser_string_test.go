package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestStringRawAndValue(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		expectRaw   string
		expectValue string
	}{
		{
			name:        "C-style escape sequences",
			source:      `option "title" "hello\nworld"`,
			expectRaw:   `"hello\nworld"`,
			expectValue: "hello\nworld",
		},
		{
			name:        "Escaped quote",
			source:      `option "title" "say \"hi\""`,
			expectRaw:   `"say \"hi\""`,
			expectValue: `say "hi"`,
		},
		{
			name:        "No escape sequences",
			source:      `option "title" "plain string"`,
			expectRaw:   `"plain string"`,
			expectValue: "plain string",
		},
		{
			name:        "Tab escape",
			source:      `option "title" "col1\tcol2"`,
			expectRaw:   `"col1\tcol2"`,
			expectValue: "col1\tcol2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), tt.source)
			assert.NoError(t, err)
			assert.True(t, tree != nil)

			if len(tree.Options) == 0 {
				t.Fatal("expected at least one option")
			}

			opt := tree.Options[0]
			assert.Equal(t, tt.expectRaw, opt.Value.Raw)
			assert.Equal(t, tt.expectValue, opt.Value.Value)
		})
	}
}

func TestStringRoundTrip(t *testing.T) {
	source := `option "title" "hello\nworld"`

	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	opt := tree.Options[0]

	assert.True(t, opt.Value.HasRaw())
	assert.Equal(t, `"hello\nworld"`, opt.Value.Raw)
	assert.Equal(t, "hello\nworld", opt.Value.Value)
}
