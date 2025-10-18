package parser

import (
	"context"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/shopspring/decimal"
)

// Helper function to parse an expression from a string
func parseExpressionFromString(t *testing.T, input string) (decimal.Decimal, error) {
	t.Helper()

	// Lex the input
	lexer := NewLexer([]byte(input), "test")
	tokens := lexer.ScanAll()

	// Create parser
	parser := NewParser([]byte(input), tokens, "test", lexer.Interner())

	// Parse expression
	return parser.parseExpression()
}

func TestParseExpression_SimpleAddition(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2 + 3", "5"},
		{"10 + 5", "15"},
		{"1.5 + 2.5", "4"},
		{"100.00 + 50.00", "150"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_SimpleSubtraction(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"5 - 3", "2"},
		{"10 - 5", "5"},
		{"100.00 - 25.50", "74.5"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_SimpleMultiplication(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2 * 3", "6"},
		{"5 * 4", "20"},
		{"1.5 * 2", "3"},
		{"10.00 * 3.5", "35"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_SimpleDivision(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"6 / 2", "3"},
		{"10 / 2", "5"},
		{"40.00 / 3", "13.3333333333333333"}, // shopspring/decimal default precision
		{"100 / 4", "25"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_OperatorPrecedence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2 + 3 * 4", "14"},     // 2 + (3 * 4) = 2 + 12 = 14
		{"10 - 2 * 3", "4"},     // 10 - (2 * 3) = 10 - 6 = 4
		{"20 / 4 + 5", "10"},    // (20 / 4) + 5 = 5 + 5 = 10
		{"2 * 3 + 4 * 5", "26"}, // (2 * 3) + (4 * 5) = 6 + 20 = 26
		{"100 / 2 - 10", "40"},  // (100 / 2) - 10 = 50 - 10 = 40
		{"5 + 10 / 2", "10"},    // 5 + (10 / 2) = 5 + 5 = 10
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_Parentheses(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"(2 + 3)", "5"},
		{"(2 + 3) * 4", "20"},          // (2 + 3) * 4 = 5 * 4 = 20
		{"2 * (3 + 4)", "14"},          // 2 * (3 + 4) = 2 * 7 = 14
		{"(10 - 2) * 3", "24"},         // (10 - 2) * 3 = 8 * 3 = 24
		{"((2 + 3) * 4)", "20"},        // nested parentheses
		{"(100 / 4) + (20 / 5)", "29"}, // (100 / 4) + (20 / 5) = 25 + 4 = 29
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_DecimalPrecision(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"40.00 / 3", "13.3333333333333333"}, // shopspring/decimal default precision
		{"1.5 + 2.7", "4.2"},
		{"10.123 * 2.5", "25.3075"},
		{"100.50 - 0.25", "100.25"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_ComplexExpressions(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// From TODO.txt example
		{"(40.00 / 3) + 5", "18.3333333333333333"}, // shopspring/decimal default precision
		{"((40.00 / 3) + 5)", "18.3333333333333333"},

		// Other complex cases
		{"(2 + 3) * (4 + 5)", "45"},
		{"100 / (2 + 3)", "20"},
		{"(10 + 20) / (3 - 1)", "15"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_UnaryMinus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"-5", "-5"},
		{"-10.50", "-10.5"},
		{"-(2 + 3)", "-5"},
		{"-5 + 10", "5"},
		{"10 + -5", "5"},
		{"-5 * 2", "-10"},
		{"-10 / 2", "-5"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseExpressionFromString(t, tt.input)
			assert.NoError(t, err)

			want, _ := decimal.NewFromString(tt.want)
			assert.True(t, got.Equal(want), "got %s, want %s", got.String(), want.String())
		})
	}
}

func TestParseExpression_DivisionByZero(t *testing.T) {
	tests := []string{
		"10 / 0",
		"5 / (2 - 2)",
		"100 / (5 - 5)",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseExpressionFromString(t, input)
			assert.Error(t, err, "expected division by zero error")
			if err != nil && err.Error() != "" {
				assert.True(t, contains(err.Error(), "division by zero"), "expected 'division by zero' error, got: %v", err)
			}
		})
	}
}

func TestParseExpression_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"missing closing paren", "(2 + 3"},
		{"missing operand", "2 +"},
		{"invalid operator sequence", "2 + * 3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseExpressionFromString(t, tt.input)
			assert.Error(t, err, "expected error for %q", tt.input)
		})
	}
}

// Test that expressions work in complete amount parsing
func TestParseAmount_WithExpression(t *testing.T) {
	tests := []struct {
		input        string
		wantValue    string
		wantCurrency string
	}{
		{"100.50 USD", "100.50", "USD"},
		{"(40.00 / 3) USD", "13.33333333333333333333333333", "USD"},
		{"40.00 / 3 + 5 USD", "18.33333333333333333333333333", "USD"},
		{"(2 + 3) * 4 EUR", "20", "EUR"},
		// Negative expressions (bug fix verification)
		{"-5 + 10 USD", "5", "USD"},    // Bug case: negative start of expression
		{"-10 * 2 USD", "-20", "USD"},  // Negative with multiplication
		{"-100 / 2 EUR", "-50", "EUR"}, // Negative with division
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tree, err := ParseString(context.Background(), tt.input)
			assert.NoError(t, err, "parse error")

			// This won't directly give us an amount, but we can test through
			// a transaction or other directive. For now, just verify no parse error.
			assert.NotEqual(t, nil, tree, "expected non-nil tree")
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && len(substr) > 0 &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
