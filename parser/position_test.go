package parser

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
)

// TestErrorPositioning verifies that parse errors are reported at the correct
// line, not at the next token's line when a required token is missing.
func TestErrorPositioning(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		expectedLine int
		expectedMsg  string
	}{
		{
			name: "missing currency after number",
			source: `2023-01-01 * "test"
    Assets:Checking    100.00
    Expenses:Food`,
			expectedLine: 2,
			expectedMsg:  "expected currency",
		},
		{
			name: "missing account in pad",
			source: `2023-01-01 pad Assets:Checking
`,
			expectedLine: 1,
			expectedMsg:  "expected account",
		},
		{
			name: "missing string in note",
			source: `2023-01-01 note Assets:Checking
`,
			expectedLine: 1,
			expectedMsg:  "expected string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseString(context.Background(), tt.source)
			assert.Error(t, err)

			// Check error is a ParseError with correct line
			parseErr, ok := err.(*ParseError)
			assert.True(t, ok, "expected *ParseError, got %T: %v", err, err)

			assert.Equal(t, tt.expectedLine, parseErr.Pos.Line,
				"error should be on line %d, got line %d: %s",
				tt.expectedLine, parseErr.Pos.Line, parseErr.Error())
			assert.True(t, strings.Contains(parseErr.Message, tt.expectedMsg),
				"error message should contain %q, got %q",
				tt.expectedMsg, parseErr.Message)
		})
	}
}
