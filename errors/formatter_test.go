package errors

import (
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

type positionalError struct {
	pos ast.Position
	msg string
}

func (e positionalError) Error() string               { return e.msg }
func (e positionalError) GetPosition() ast.Position   { return e.pos }
func (e positionalError) GetDirective() ast.Directive { return nil }

type directiveError struct {
	pos       ast.Position
	directive ast.Directive
	msg       string
}

func (e directiveError) Error() string             { return e.msg }
func (e directiveError) GetPosition() ast.Position { return e.pos }
func (e directiveError) GetDirective() ast.Directive {
	return e.directive
}

func TestTextFormatter_Format_WithPosition(t *testing.T) {
	tf := NewTextFormatter(nil, nil)

	err := positionalError{
		pos: ast.Position{
			Filename: "file.bean",
			Line:     42,
		},
		msg: "something went wrong",
	}

	output := tf.Format(err)
	assert.Equal(t, "file.bean:42: something went wrong", output)
}

func TestTextFormatter_Format_WithDirectiveContext(t *testing.T) {
	tf := NewTextFormatter(nil, nil)

	date := &ast.Date{Time: time.Date(2024, time.January, 10, 0, 0, 0, 0, time.UTC)}
	directive := &ast.Balance{
		Pos: ast.Position{
			Filename: "ledger.bean",
			Line:     12,
		},
		Date:    date,
		Account: ast.Account("Assets:Cash"),
	}

	err := directiveError{
		pos:       directive.Pos,
		directive: directive,
		msg:       "balance assertion failed",
	}

	output := tf.Format(err)
	expected := "ledger.bean:12: balance assertion failed\n\n" +
		"   1 │ 2024-01-10 balance Assets:Cash\n"

	assert.Equal(t, expected, output)
}

func TestTextFormatter_Format_WithMetadata(t *testing.T) {
	tf := NewTextFormatter(nil, nil)

	date := &ast.Date{Time: time.Date(2024, time.January, 10, 0, 0, 0, 0, time.UTC)}
	directive := &ast.Balance{
		Pos: ast.Position{
			Filename: "ledger.bean",
			Line:     12,
		},
		Date:    date,
		Account: ast.Account("Assets:Cash"),
	}
	directive.AddMetadata(
		&ast.Metadata{Key: "source", Value: "\"statement\""},
		&ast.Metadata{Key: "confidence", Value: "0.95"},
	)

	err := directiveError{
		pos:       directive.Pos,
		directive: directive,
		msg:       "balance assertion failed",
	}

	output := tf.Format(err)
	expected := "ledger.bean:12: balance assertion failed\n\n" +
		"   1 │ 2024-01-10 balance Assets:Cash\n" +
		"   2 │   source: \"statement\"\n" +
		"   3 │   confidence: 0.95\n"

	assert.Equal(t, expected, output)
}

func TestTextFormatter_Format_CustomDirective(t *testing.T) {
	tf := NewTextFormatter(nil, nil)

	date := &ast.Date{Time: time.Date(2024, time.January, 10, 0, 0, 0, 0, time.UTC)}
	directive := &ast.Custom{
		Pos: ast.Position{
			Filename: "ledger.bean",
			Line:     15,
		},
		Date: date,
		Type: "budget",
		Values: []*ast.CustomValue{
			{String: strPtr("marketing")},
			{Number: strPtr("12.5")},
			{Amount: &ast.Amount{Value: "45.30", Currency: "USD"}},
			{BooleanValue: strPtr("TRUE")},
		},
	}

	err := directiveError{
		pos:       directive.Pos,
		directive: directive,
		msg:       "custom directive invalid",
	}

	output := tf.Format(err)
	expected := "ledger.bean:15: custom directive invalid\n\n" +
		"   1 │ 2024-01-10 custom \"budget\" \"marketing\" 12.5 45.30 USD TRUE\n"

	assert.Equal(t, expected, output)
}

func strPtr(value string) *string {
	return &value
}
