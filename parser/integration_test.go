package parser

import (
	"context"
	"testing"
)

func TestParseSimpleTransaction(t *testing.T) {
	input := `2014-05-05 * "Cafe" "Coffee"
  Expenses:Food  4.50 USD
  Assets:Cash
`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 1 {
		t.Fatalf("got %d directives, want 1", len(ast.Directives))
	}

	t.Logf("Successfully parsed transaction!")
}

func TestParseBalance(t *testing.T) {
	input := `2014-08-09 balance Assets:Checking 100.00 USD`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 1 {
		t.Fatalf("got %d directives, want 1", len(ast.Directives))
	}

	t.Logf("Successfully parsed balance!")
}

func TestParseOpen(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 1 {
		t.Fatalf("got %d directives, want 1", len(ast.Directives))
	}

	t.Logf("Successfully parsed open!")
}

func TestParseMultipleDirectives(t *testing.T) {
	input := `2014-01-01 open Assets:Checking USD
2014-01-02 open Expenses:Food

2014-05-05 * "Cafe" "Coffee"
  Expenses:Food  4.50 USD
  Assets:Checking

2014-08-09 balance Assets:Checking 100.00 USD
`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 4 {
		t.Fatalf("got %d directives, want 4", len(ast.Directives))
	}

	t.Logf("Successfully parsed %d directives!", len(ast.Directives))
}

func TestParseWithComments(t *testing.T) {
	input := `; This is a comment
2014-01-01 open Assets:Checking
; Another comment
2014-05-05 * "Test"
  Expenses:Food  10 USD
  Assets:Checking
`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 2 {
		t.Fatalf("got %d directives, want 2", len(ast.Directives))
	}

	t.Logf("Successfully parsed with comments!")
}

func TestParseWithMetadata(t *testing.T) {
	input := `2014-01-01 open Assets:Checking
  account-number: "123456"
  bank: "Chase"
`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 1 {
		t.Fatalf("got %d directives, want 1", len(ast.Directives))
	}

	t.Logf("Successfully parsed with metadata!")
}

func TestParseAmountWithoutCurrency(t *testing.T) {
	input := `2023-06-02 * "buy stocks"
  Assets:Investments:Stock  100 STOCK {}
  Assets:Cash -1600.00
`

	ast, err := ParseString(context.Background(), input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(ast.Directives) != 1 {
		t.Fatalf("got %d directives, want 1", len(ast.Directives))
	}

	t.Logf("Successfully parsed transaction with amount without currency!")
}
