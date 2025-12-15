package parser

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/formatter"
)

// getDirectiveLine extracts the line number from a directive.
func getDirectiveLine(directive ast.Directive) int {
	switch v := directive.(type) {
	case *ast.Commodity:
		return v.Pos.Line
	case *ast.Open:
		return v.Pos.Line
	case *ast.Close:
		return v.Pos.Line
	case *ast.Balance:
		return v.Pos.Line
	case *ast.Pad:
		return v.Pos.Line
	case *ast.Note:
		return v.Pos.Line
	case *ast.Document:
		return v.Pos.Line
	case *ast.Price:
		return v.Pos.Line
	case *ast.Event:
		return v.Pos.Line
	case *ast.Custom:
		return v.Pos.Line
	case *ast.Transaction:
		return v.Pos.Line
	default:
		return 0
	}
}

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

// TestDirectivePositioning tests that all directive types have correct position tracking.
func TestDirectivePositioning(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		expectedTypes []string // Expected directive types in order
	}{
		{
			name: "basic directives",
			source: `2023-01-01 open Assets:Checking USD
2023-01-02 close Assets:Checking
2023-01-03 balance Assets:Checking 100.00 USD
2023-01-04 commodity USD
2023-01-05 pad Assets:Checking Equity:Opening-Balances
2023-01-06 note Assets:Checking "Test note"
2023-01-07 document Assets:Checking "/path/to/file.pdf"
2023-01-08 price HOOL 100.00 USD
2023-01-09 event "location" "NYC"
2023-01-10 custom "budget" Expenses:Food 500.00 USD`,
			expectedTypes: []string{"open", "close", "balance", "commodity", "pad", "note", "document", "price", "event", "custom"},
		},
		{
			name: "transaction directives",
			source: `2023-01-01 * "Payee" "Narration"
  Assets:Checking  100.00 USD
  Expenses:Food
2023-01-02 ! "Pending" "Test"
  Assets:Savings   50.00 USD
  Expenses:Shopping`,
			expectedTypes: []string{"transaction", "transaction"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), tt.source)
			assert.NoError(t, err)

			// Check that we have the expected number of directives
			assert.Equal(t, len(tt.expectedTypes), len(tree.Directives),
				"expected %d directives, got %d", len(tt.expectedTypes), len(tree.Directives))

			// Verify position and type of each directive
			for i, directive := range tree.Directives {
				expectedType := tt.expectedTypes[i]
				actualType := directive.Directive()
				actualLine := getDirectiveLine(directive)

				assert.Equal(t, expectedType, actualType,
					"directive %d should be type %s, got %s",
					i, expectedType, actualType)

				// Check expected line numbers based on test case
				var expectedLine int
				switch tt.name {
				case "basic directives":
					// Basic directives are on sequential lines 1-10
					expectedLine = i + 1
				case "transaction directives":
					// Transaction directives: first on line 1, second on line 4 (after 3-line transaction)
					if i == 0 {
						expectedLine = 1
					} else {
						expectedLine = 4
					}
				}
				assert.Equal(t, expectedLine, actualLine,
					"directive %d should be on line %d, got line %d",
					i, expectedLine, actualLine)
			}
		})
	}
}

// TestMultiLineDirectivePositioning tests position tracking when dates and directives are on separate lines.
func TestMultiLineDirectivePositioning(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		expectedLines []int // Expected line numbers for directives in order
	}{
		{
			name: "date and directive on separate lines",
			source: `2023-01-01
open Assets:Checking USD

2023-01-02
close Assets:Checking

2023-01-03
balance Assets:Checking 100.00 USD`,
			expectedLines: []int{2, 5, 8},
		},
		{
			name: "multiple blank lines between date and directive",
			source: `2023-01-01


open Assets:Checking USD

2023-01-02

close Assets:Checking`,
			expectedLines: []int{4, 8},
		},
		{
			name: "mixed single-line and multi-line directives",
			source: `2023-01-01 open Assets:Checking USD
2023-01-02
close Assets:Checking
2023-01-03 balance Assets:Checking 100.00 USD
2023-01-04
commodity USD`,
			expectedLines: []int{1, 3, 4, 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := ParseString(context.Background(), tt.source)
			assert.NoError(t, err)

			assert.Equal(t, len(tt.expectedLines), len(tree.Directives),
				"expected %d directives, got %d", len(tt.expectedLines), len(tree.Directives))

			for i, directive := range tree.Directives {
				expectedLine := tt.expectedLines[i]
				actualLine := getDirectiveLine(directive)
				assert.Equal(t, expectedLine, actualLine,
					"directive %d should be on line %d, got line %d",
					i, expectedLine, actualLine)
			}
		})
	}
}

// TestPositionTrackingWithComments tests that comments don't affect position tracking.
func TestPositionTrackingWithComments(t *testing.T) {
	source := `; This is a comment
2023-01-01 open Assets:Checking USD
; Another comment

2023-01-02
close Assets:Checking
; Final comment`

	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(tree.Directives))

	// First directive should be on line 2 (after comment)
	assert.Equal(t, 2, getDirectiveLine(tree.Directives[0]))

	// Second directive should be on line 6 (after comment and blank line)
	assert.Equal(t, 6, getDirectiveLine(tree.Directives[1]))
}

// TestPositionTrackingRegression tests the specific failing fuzz case for regression.
func TestPositionTrackingRegression(t *testing.T) {
	// This was the failing fuzz case that exposed the position tracking bug
	source := "0000-01-01\nopen Assets:0"

	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(tree.Directives))

	// The open directive should be on line 2, not line 1
	directive := tree.Directives[0]
	actualLine := getDirectiveLine(directive)
	assert.Equal(t, 2, actualLine,
		"open directive should be on line 2, got line %d", actualLine)

	// Verify it's the correct directive type
	open, ok := directive.(*ast.Open)
	assert.True(t, ok, "expected *ast.Open, got %T", directive)
	assert.Equal(t, "Assets:0", string(open.Account))

	// Test round-trip formatting to ensure positions are preserved
	// This should not panic and should produce valid output
	f := formatter.New()
	// Create a simple AST with just this directive for formatting
	testAST := &ast.AST{
		Directives: []ast.Directive{directive},
	}

	var buf strings.Builder
	err = f.Format(context.Background(), testAST, []byte(source), &buf)
	assert.NoError(t, err)
	output := buf.String()
	assert.True(t, output != "")
	assert.Contains(t, output, "open Assets:0")
}

// TestPositionTrackingWithMetadata tests position tracking when directives have metadata.
func TestPositionTrackingWithMetadata(t *testing.T) {
	source := `2023-01-01 open Assets:Checking USD
  account-number: "12345"
  bank: "Test Bank"

2023-01-02
balance Assets:Checking 100.00 USD
  tolerance: "0.01"`

	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(tree.Directives))

	// First directive on line 1
	assert.Equal(t, 1, getDirectiveLine(tree.Directives[0]))

	// Second directive on line 6 (after metadata and blank line)
	assert.Equal(t, 6, getDirectiveLine(tree.Directives[1]))
}

// TestPositionTrackingComplexScenario tests complex scenarios with mixed formatting.
func TestPositionTrackingComplexScenario(t *testing.T) {
	source := `option "title" "Test Ledger"

; Account setup
2023-01-01
open Assets:Checking USD
  description: "Primary checking"

2023-01-01 open Expenses:Food
2023-01-01 open Expenses:Transport

2023-01-02
* "Grocery Store" "Weekly shopping"
  Expenses:Food      50.00 USD
  Assets:Checking

2023-01-03 balance Assets:Checking 950.00 USD

2023-01-04
note Assets:Checking "Account review"

2023-01-05 price USD 1.00 EUR

; End of period
2023-01-31 close Assets:Checking`

	tree, err := ParseString(context.Background(), source)
	assert.NoError(t, err)

	expectedLines := []int{5, 8, 9, 12, 16, 19, 21, 24}
	assert.Equal(t, len(expectedLines), len(tree.Directives),
		"expected %d directives, got %d", len(expectedLines), len(tree.Directives))

	for i, directive := range tree.Directives {
		expectedLine := expectedLines[i]
		actualLine := getDirectiveLine(directive)
		assert.Equal(t, expectedLine, actualLine,
			"directive %d should be on line %d, got line %d",
			i, expectedLine, actualLine)
	}
}
