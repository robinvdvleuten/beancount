package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

// TestStringEscapeMetadata verifies that string escape information is preserved
// during parsing for round-trip formatting with EscapeStyleOriginal.
func TestStringEscapeMetadata(t *testing.T) {
	tests := []struct {
		name              string
		source            string
		expectEscapeType  ast.EscapeType
		expectOriginalVal string
	}{
		{
			name:              "C-style escape sequences",
			source:            `option "title" "hello\nworld"`,
			expectEscapeType:  ast.EscapeTypeCStyle,
			expectOriginalVal: `"hello\nworld"`,
		},
		{
			name:              "Escaped quote",
			source:            `option "title" "say \"hi\""`,
			expectEscapeType:  ast.EscapeTypeCStyle,
			expectOriginalVal: `"say \"hi\""`,
		},
		{
			name:              "No escape sequences",
			source:            `option "title" "plain string"`,
			expectEscapeType:  ast.EscapeTypeNone,
			expectOriginalVal: `"plain string"`,
		},
		{
			name:              "Tab escape",
			source:            `option "title" "col1\tcol2"`,
			expectEscapeType:  ast.EscapeTypeCStyle,
			expectOriginalVal: `"col1\tcol2"`,
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
			assert.Equal(t, tt.expectEscapeType, opt.ValueEscapes.EscapeType)
			assert.Equal(t, tt.expectOriginalVal, opt.ValueEscapes.OriginalValue)
		})
	}
}

// TestStringRoundTripWithEscapeStyles verifies that strings can be round-tripped
// with different escape styles without losing fidelity.
func TestStringRoundTripWithEscapeStyles(t *testing.T) {
	source := `option "title" "hello\nworld"`

	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	opt := tree.Options[0]

	// Check that original quoted content is preserved
	assert.True(t, opt.ValueEscapes.HasOriginal())
	assert.Equal(t, `"hello\nworld"`, opt.ValueEscapes.QuotedContent())

	// Check that unquoted logical value was extracted
	assert.Equal(t, "hello\nworld", opt.Value)
}
