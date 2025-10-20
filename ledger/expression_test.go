package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func TestEvaluateExpression(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		// Basic operations
		{
			name: "addition",
			expr: "(5 + 3)",
			want: "8",
		},
		{
			name: "subtraction",
			expr: "(10 - 3)",
			want: "7",
		},
		{
			name: "multiplication",
			expr: "(5 * 2)",
			want: "10",
		},
		{
			name: "division",
			expr: "(40 / 4)",
			want: "10",
		},
		// Decimals
		{
			name: "decimal division",
			expr: "(40.00 / 3)",
			want: "13.3333333333333333",
		},
		{
			name: "decimal multiplication",
			expr: "(100 * 1.5)",
			want: "150",
		},
		// Precedence
		{
			name: "precedence multiply first",
			expr: "(10 + 5 * 2)",
			want: "20",
		},
		{
			name: "precedence divide first",
			expr: "(20 - 10 / 2)",
			want: "15",
		},
		// Parentheses
		{
			name: "nested parentheses",
			expr: "((5 + 3) * 2)",
			want: "16",
		},
		{
			name: "complex nested",
			expr: "(((40 / 3) + 5) * 2)",
			want: "36.6666666666666666",
		},
		// Negative numbers
		{
			name: "negative operand",
			expr: "(-50 + 10)",
			want: "-40",
		},
		{
			name: "unary minus",
			expr: "(-(5 + 3))",
			want: "-8",
		},
		// Spaces
		{
			name: "spaces around operators",
			expr: "( 5 + 3 )",
			want: "8",
		},
		{
			name: "no spaces",
			expr: "(5+3)",
			want: "8",
		},
		// Error cases
		{
			name:    "division by zero",
			expr:    "(10 / 0)",
			wantErr: true,
		},
		{
			name:    "missing closing paren",
			expr:    "(5 + 3",
			wantErr: true,
		},
		{
			name:    "invalid syntax",
			expr:    "(5 + )",
			wantErr: true,
		},
		{
			name:    "not wrapped in parens",
			expr:    "5 + 3",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateExpression(tt.expr)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestParseAmount_Expressions(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		// Basic operations
		{
			name:  "addition",
			value: "(5 + 3)",
			want:  "8",
		},
		{
			name:  "subtraction",
			value: "(10 - 3)",
			want:  "7",
		},
		{
			name:  "multiplication",
			value: "(5 * 2)",
			want:  "10",
		},
		{
			name:  "division",
			value: "(40 / 4)",
			want:  "10",
		},
		// Decimals
		{
			name:  "decimal division",
			value: "(40.00 / 3)",
			want:  "13.3333333333333333",
		},
		{
			name:  "decimal multiplication",
			value: "(100 * 1.5)",
			want:  "150",
		},
		// Precedence
		{
			name:  "precedence multiply first",
			value: "(10 + 5 * 2)",
			want:  "20",
		},
		{
			name:  "precedence divide first",
			value: "(20 - 10 / 2)",
			want:  "15",
		},
		// Parentheses
		{
			name:  "nested parentheses",
			value: "((5 + 3) * 2)",
			want:  "16",
		},
		{
			name:  "complex nested",
			value: "(((40 / 3) + 5) * 2)",
			want:  "36.6666666666666666",
		},
		// Negative numbers
		{
			name:  "negative operand",
			value: "(-50 + 10)",
			want:  "-40",
		},
		{
			name:  "unary minus",
			value: "(-(5 + 3))",
			want:  "-8",
		},
		// Spaces
		{
			name:  "spaces around operators",
			value: "( 5 + 3 )",
			want:  "8",
		},
		{
			name:  "no spaces",
			value: "(5+3)",
			want:  "8",
		},
		// Error cases
		{
			name:    "division by zero",
			value:   "(10 / 0)",
			wantErr: true,
		},
		{
			name:    "missing closing paren",
			value:   "(5 + 3",
			wantErr: true,
		},
		{
			name:    "invalid syntax",
			value:   "(5 + )",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amt := &ast.Amount{
				Value:    tt.value,
				Currency: "USD",
			}

			got, err := ParseAmount(amt)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

func TestParseAmount_PlainNumbers(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "integer",
			value: "100",
			want:  "100",
		},
		{
			name:  "decimal",
			value: "100.50",
			want:  "100.5",
		},
		{
			name:  "negative",
			value: "-50.25",
			want:  "-50.25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amt := &ast.Amount{
				Value:    tt.value,
				Currency: "USD",
			}

			got, err := ParseAmount(amt)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}
