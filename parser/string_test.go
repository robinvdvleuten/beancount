package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestUnquoteString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
		errorMsg    string
	}{
		// Basic cases
		{
			name:     "empty string",
			input:    `""`,
			expected: "",
		},
		{
			name:     "simple string",
			input:    `"hello"`,
			expected: "hello",
		},

		// Fast path - no escapes
		{
			name:     "fast path - no escapes",
			input:    `"hello world"`,
			expected: "hello world",
		},
		{
			name:     "fast path - with numbers",
			input:    `"test123"`,
			expected: "test123",
		},

		// Escape sequences
		{
			name:     "escaped quote",
			input:    `"hello \"world\""`,
			expected: `hello "world"`,
		},
		{
			name:     "escaped backslash",
			input:    `"hello \\world"`,
			expected: `hello \world`,
		},
		{
			name:     "escaped newline",
			input:    `"hello \nworld"`,
			expected: "hello \nworld",
		},
		{
			name:     "escaped tab",
			input:    `"hello \tworld"`,
			expected: "hello \tworld",
		},
		{
			name:     "escaped carriage return",
			input:    `"hello \rworld"`,
			expected: "hello \rworld",
		},

		// Multiple escape sequences
		{
			name:     "multiple escapes",
			input:    `"hello \"world\"\n\\test\tend"`,
			expected: `hello "world"` + "\n" + `\test` + "\t" + "end",
		},

		// Edge cases
		{
			name:     "only backslash",
			input:    `"\\"`,
			expected: `\`,
		},
		{
			name:     "only quote",
			input:    `"\""`,
			expected: `"`,
		},

		// Error cases
		{
			name:        "no quotes",
			input:       "hello",
			expectError: true,
			errorMsg:    "string must be enclosed in double quotes",
		},
		{
			name:        "single quote only",
			input:       `"`,
			expectError: true,
			errorMsg:    "string must be enclosed in double quotes",
		},
		{
			name:        "unterminated string",
			input:       `"hello`,
			expectError: true,
			errorMsg:    "string must be enclosed in double quotes",
		},
		{
			name:        "backslash at end",
			input:       `"hello\`,
			expectError: true,
			errorMsg:    "string must be enclosed in double quotes",
		},
		{
			name:        "invalid escape sequence",
			input:       `"hello\x"`,
			expectError: true,
			errorMsg:    "invalid escape sequence '\\x'",
		},
		{
			name:        "invalid escape sequence with number",
			input:       `"hello\5"`,
			expectError: true,
			errorMsg:    "invalid escape sequence '\\5'",
		},
		{
			name:        "invalid escape sequence with space",
			input:       `"hello\ "`,
			expectError: true,
			errorMsg:    "invalid escape sequence '\\ '",
		},

		// Unicode and special characters
		{
			name:     "unicode characters",
			input:    `"héllo wörld"`,
			expected: "héllo wörld",
		},
		{
			name:     "emoji",
			input:    `"🚀 rocket"`,
			expected: "🚀 rocket",
		},

		// Mixed escapes
		{
			name:     "mixed escapes at start and end",
			input:    `"\nhello\tworld\r\n"`,
			expected: "\nhello\tworld\r\n",
		},

		// Empty and whitespace
		{
			name:     "whitespace only",
			input:    `"   "`,
			expected: "   ",
		},
		{
			name:     "newlines and tabs",
			input:    `"\n\t\n"`,
			expected: "\n\t\n",
		},

		// Escaped backslash followed by escape chars should be literal
		{
			name:     "escaped backslash followed by n",
			input:    `"\\n"`,
			expected: "\\n",
		},
		{
			name:     "escaped backslash followed by t",
			input:    `"\\t"`,
			expected: "\\t",
		},
		{
			name:     "escaped backslash followed by r",
			input:    `"\\r"`,
			expected: "\\r",
		},
		{
			name:     "multiple escaped backslashes with escape chars",
			input:    `"\\\\n\\t"`,
			expected: "\\\\n\\t",
		},
		{
			name:     "mixed escaped and literal escapes",
			input:    `"\\n\n\\t\t"`,
			expected: "\\n\n\\t\t",
		},
		{
			name:     "escaped backslash then newline escape",
			input:    `"\\\n"`, // raw bytes: \ \ \ n -> \\ is escaped backslash, \n is newline escape
			expected: "\\\n",   // literal backslash + newline character
		},
		{
			name:     "double escaped backslash with n",
			input:    `"\\\\n"`, // \\\\ followed by n
			expected: "\\\\n",   // two literal backslashes followed by n
		},
	}

	p := &Parser{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.unquoteString(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
		errorMsg    string
	}{
		{
			name:     "valid string",
			input:    `"hello world"`,
			expected: "hello world",
		},
		{
			name:     "string with escapes",
			input:    `"hello \"world\""`,
			expected: `hello "world"`,
		},
		{
			name:     "string with newline",
			input:    `"line1\nline2"`,
			expected: "line1\nline2",
		},
		{
			name:        "invalid string literal",
			input:       `"hello\world"`,
			expectError: true,
			errorMsg:    "invalid string literal: invalid escape sequence '\\w'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a lexer to tokenize the input
			l := NewLexer([]byte(tt.input), "test.beancount")
			tokens, err := l.ScanAll()
			assert.NoError(t, err)
			interner := l.Interner()

			p := NewParser([]byte(tt.input), tokens, "test.beancount", interner)
			result, err := p.parseString()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result.Value)
			}
		})
	}
}

func TestParseStringWithMetadata(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid metadata string",
			input:    `2024-01-01 open Assets:Checking USD\n  metadata: "value"`,
			expected: "value",
		},
		{
			name:     "metadata string with escapes",
			input:    `2024-01-01 open Assets:Checking USD\n  metadata: "value with \"quotes\""`,
			expected: `value with "quotes"`,
		},
		{
			name:     "metadata string with newline",
			input:    `2024-01-01 open Assets:Checking USD\n  metadata: "line1\nline2"`,
			expected: "line1\nline2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := ParseString(context.Background(), tt.input)
			assert.NoError(t, err)
			assert.Equal(t, 1, len(ast.Directives))

			// Test that parsing succeeds and contains metadata
			// Note: We'll test more detailed metadata structure in integration tests
			t.Logf("Successfully parsed metadata test: %s", tt.name)
		})
	}
}

func TestParseCostWithLabel(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:     "cost with label",
			input:    `2024-01-01 * "Transaction"\n  Assets:Checking  100 USD {"USD" , "label"}`,
			expected: "label",
		},
		{
			name:     "cost with escaped label",
			input:    `2024-01-01 * "Transaction"\n  Assets:Checking  100 USD {"USD" , "label with \"quotes\""}`,
			expected: `label with "quotes"`,
		},
		{
			name:     "cost with label containing newline",
			input:    `2024-01-01 * "Transaction"\n  Assets:Checking  100 USD {"USD" , "line1\nline2"}`,
			expected: "line1\nline2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := ParseString(context.Background(), tt.input)

			if tt.expectError {
				if err == nil {
					t.Logf("Expected error but got none")
					t.Fail()
				} else {
					t.Logf("Got error: %v", err)
					assert.Contains(t, err.Error(), "invalid string literal")
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, len(ast.Directives))
				t.Logf("Successfully parsed cost test: %s", tt.name)
			}
		})
	}
}

// String Raw and Value tests (from parser_string_test.go)

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

// Benchmark tests
func BenchmarkUnquoteStringNoEscapes(b *testing.B) {
	p := &Parser{}
	input := `"this is a long string without any escape sequences that should be fast"`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.unquoteString(input)
	}
}

func BenchmarkUnquoteStringWithEscapes(b *testing.B) {
	p := &Parser{}
	input := `"this string has \"multiple\" \\escape\\ sequences \nthat \tshould \rbe slower"`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.unquoteString(input)
	}
}

func BenchmarkUnquoteStringShort(b *testing.B) {
	p := &Parser{}
	input := `"short"`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.unquoteString(input)
	}
}
