package ledger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/parser"
)

func TestHandlerRegistry_GetHandler(t *testing.T) {
	tests := []struct {
		kind    ast.DirectiveKind
		expects bool
	}{
		{ast.KindOpen, true},
		{ast.KindClose, true},
		{ast.KindTransaction, true},
		{ast.KindBalance, true},
		{ast.KindPad, true},
		{ast.KindNote, true},
		{ast.KindDocument, true},
		{ast.KindPrice, true},
		{ast.KindCommodity, true},
		{ast.KindEvent, true},
		{ast.KindQuery, true},
		{ast.KindCustom, true},
		{ast.DirectiveKind("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			handler := getHandler(tt.kind)
			if tt.expects {
				assert.NotZero(t, handler, "handler should be registered")
			} else {
				assert.Zero(t, handler, "handler should not be registered")
			}
		})
	}
}

func TestOpenHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	handler := &openHandler{}
	directive := tree.Directives[0]

	// Validate
	errs, delta := handler.validate(ctx, ledger, directive)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	// Apply
	handler.apply(ctx, ledger, directive, delta)

	// Verify
	acc, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok, "account should exist")
	assert.Equal(t, "Assets:Checking", string(acc.name))
}

func TestCloseHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
2020-12-31 close Assets:Checking
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open first
	openHandler := &openHandler{}
	_, openDelta := openHandler.validate(ctx, ledger, tree.Directives[0])
	openHandler.apply(ctx, ledger, tree.Directives[0], openDelta)

	// Close
	closeHandler := &closeHandler{}
	directive := tree.Directives[1]
	errs, delta := closeHandler.validate(ctx, ledger, directive)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	closeHandler.apply(ctx, ledger, directive, delta)

	// Verify
	acc, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok, "account should exist")
	assert.True(t, acc.isClosed(), "account should be closed")
}

func TestTransactionHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
2020-01-01 open Income:Salary

2020-01-15 * "Salary"
  Assets:Checking  1000.00 USD
  Income:Salary   -1000.00 USD
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open accounts
	for i := 0; i < 2; i++ {
		handler := getHandler(tree.Directives[i].Kind())
		_, delta := handler.validate(ctx, ledger, tree.Directives[i])
		handler.apply(ctx, ledger, tree.Directives[i], delta)
	}

	// Book, as Process does before validating, then process the transaction
	txnHandler := &transactionHandler{}
	txnDirective := tree.Directives[2]
	assert.NoError(t, ledger.book(ctx, tree))
	errs, delta := txnHandler.validate(ctx, ledger, txnDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	txnHandler.apply(ctx, ledger, txnDirective, delta)

	// Verify
	checking, _ := ledger.GetAccount("Assets:Checking")
	assert.Equal(t, "1000", checking.inventory.get("USD").String())
}

func TestBalanceHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
2020-01-01 pad Assets:Checking Equity:Opening-Balances
2020-01-15 balance Assets:Checking 1000.00 USD
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open account
	openHandler := &openHandler{}
	_, delta := openHandler.validate(ctx, ledger, tree.Directives[0])
	openHandler.apply(ctx, ledger, tree.Directives[0], delta)

	// Also need to open equity account for padding
	tree2 := parser.MustParseString(ctx, "2020-01-01 open Equity:Opening-Balances")
	ledger.opens = newOpenIndex(append(tree.Directives, tree2.Directives...))
	_, delta = openHandler.validate(ctx, ledger, tree2.Directives[0])
	openHandler.apply(ctx, ledger, tree2.Directives[0], delta)

	// Process pad: the padding planned for it is applied with it.
	ledger.pads = planPads(tree.Directives, ledger.booked, ledger.tolerances.balance)
	padHandler := &padHandler{}
	_, delta = padHandler.validate(ctx, ledger, tree.Directives[1])
	padHandler.apply(ctx, ledger, tree.Directives[1], delta)
	checking, _ := ledger.GetAccount("Assets:Checking")
	assert.Equal(t, "1000", checking.inventory.get("USD").String())

	// Process balance: the assertion holds, and changes nothing.
	balanceHandler := &balanceHandler{}
	errs, delta := balanceHandler.validate(ctx, ledger, tree.Directives[2])
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.Zero(t, delta)
}

func TestPadHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
2020-01-01 open Equity:Opening-Balances
2020-01-01 pad Assets:Checking Equity:Opening-Balances
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open accounts
	openHandler := &openHandler{}
	for i := 0; i < 2; i++ {
		_, delta := openHandler.validate(ctx, ledger, tree.Directives[i])
		openHandler.apply(ctx, ledger, tree.Directives[i], delta)
	}

	// Process pad
	padHandler := &padHandler{}
	padDirective := tree.Directives[2]
	errs, delta := padHandler.validate(ctx, ledger, padDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	// Without a plan the pad has nothing to apply.
	padHandler.apply(ctx, ledger, padDirective, delta)
	assert.Equal(t, 0, len(ledger.pads.padding))
}

func TestNoteHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
2020-07-09 note Assets:Checking "Called bank about pending deposit"
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open account
	openHandler := &openHandler{}
	_, delta := openHandler.validate(ctx, ledger, tree.Directives[0])
	openHandler.apply(ctx, ledger, tree.Directives[0], delta)

	// Process note
	noteHandler := &noteHandler{}
	noteDirective := tree.Directives[1]
	errs, _ := noteHandler.validate(ctx, ledger, noteDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	noteHandler.apply(ctx, ledger, noteDirective, nil)
	// Note handler doesn't mutate state
}

func TestDocumentHandler(t *testing.T) {
	ctx := context.Background()

	// Beancount verifies the referenced file exists; use a real one.
	docFile := filepath.Join(t.TempDir(), "2020-07.pdf")
	assert.NoError(t, os.WriteFile(docFile, nil, 0o644))

	source := fmt.Sprintf(`
2020-01-01 open Assets:Checking
2020-07-09 document Assets:Checking %q
`, docFile)
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open account
	openHandler := &openHandler{}
	_, delta := openHandler.validate(ctx, ledger, tree.Directives[0])
	openHandler.apply(ctx, ledger, tree.Directives[0], delta)

	// Process document
	docHandler := &documentHandler{}
	docDirective := tree.Directives[1]
	errs, _ := docHandler.validate(ctx, ledger, docDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	docHandler.apply(ctx, ledger, docDirective, nil)
	// Document handler doesn't mutate state
}

func TestDocumentHandlerMissingFile(t *testing.T) {
	ctx := context.Background()
	source := `
2020-01-01 open Assets:Checking
2020-07-09 document Assets:Checking "/documents/does-not-exist.pdf"
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	openHandler := &openHandler{}
	_, delta := openHandler.validate(ctx, ledger, tree.Directives[0])
	openHandler.apply(ctx, ledger, tree.Directives[0], delta)

	docHandler := &documentHandler{}
	errs, _ := docHandler.validate(ctx, ledger, tree.Directives[1])
	assert.Equal(t, 1, len(errs), "missing file should be an error like bean-check")
	assert.Equal(t, "DocumentFileError", kindOf(errs[0]))
}

func TestPriceHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2024-01-15 price USD 1.08 CAD
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Process price
	priceHandler := &priceHandler{}
	priceDirective := tree.Directives[0]
	errs, delta := priceHandler.validate(ctx, ledger, priceDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	priceHandler.apply(ctx, ledger, priceDirective, delta)

	// Verify price was stored
	date := newTestDate("2024-01-15")
	rate, found := ledger.GetPrice(date, "USD", "CAD")
	assert.True(t, found)
	assert.True(t, rate.Equal(mustParseDec("1.08")))
}

func TestCommodityHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2024-01-01 commodity USD
  name: "US Dollar"
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Process commodity
	commodityHandler := &commodityHandler{}
	commodityDirective := tree.Directives[0]
	errs, delta := commodityHandler.validate(ctx, ledger, commodityDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	commodityHandler.apply(ctx, ledger, commodityDirective, delta)

	// Verify the commodity was recorded as declared, so a second
	// declaration is reported
	assert.True(t, ledger.commodities["USD"], "commodity should be declared")
	errs, delta = commodityHandler.validate(ctx, ledger, commodityDirective)
	assert.Equal(t, 1, len(errs))
	assert.Equal(t, "DuplicateCommodityError", kindOf(errs[0]))
	assert.Zero(t, delta)
}

func TestEventHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2024-01-01 event "location" "New York, USA"
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Process event
	eventHandler := &eventHandler{}
	eventDirective := tree.Directives[0]
	errs, _ := eventHandler.validate(ctx, ledger, eventDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	eventHandler.apply(ctx, ledger, eventDirective, nil)
	// Event handler doesn't mutate state currently
}

func TestCustomHandler(t *testing.T) {
	// Custom directives are not commonly used in standard Beancount
	// but the handler should accept them without error
	ledger := New()
	ctx := context.Background()

	customHandler := &customHandler{}
	date := newTestDate("2024-01-01")
	custom := ast.NewCustom(date, "test", nil)

	errs, _ := customHandler.validate(ctx, ledger, custom)
	assert.Equal(t, len(errs), 0, "should have no errors")

	customHandler.apply(ctx, ledger, custom, nil)
	// Custom handler doesn't mutate state currently
}

func TestQueryHandler(t *testing.T) {
	ctx := context.Background()
	source := `
2024-01-01 query "cash" "SELECT * FROM accounts"
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	queryHandler := &queryHandler{}
	queryDirective := tree.Directives[0]
	errs, _ := queryHandler.validate(ctx, ledger, queryDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	queryHandler.apply(ctx, ledger, queryDirective, nil)
	// Query handler doesn't mutate state
}

// TestCommodityValidation_ValidCodes tests that multiple valid commodity codes are accepted
func TestCommodityValidation_ValidCodes(t *testing.T) {
	ctx := context.Background()
	// Use commodity codes that the parser recognizes
	source := `
2024-01-01 commodity USD
2024-01-01 commodity EUR
2024-01-01 commodity BTC
2024-01-01 commodity HOOL
2024-01-01 commodity VTSAX
2024-01-01 commodity VACHR
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	for _, directive := range tree.Directives {
		handler := &commodityHandler{}
		errs, delta := handler.validate(ctx, ledger, directive)
		assert.Equal(t, len(errs), 0, "valid commodity codes should have no errors")
		assert.NotZero(t, delta, "delta should be returned")

		handler.apply(ctx, ledger, directive, delta)
	}

	// Verify all commodities were declared
	for _, currency := range []string{"USD", "EUR", "BTC", "HOOL", "VTSAX", "VACHR"} {
		assert.True(t, ledger.commodities[currency], "%s should be declared", currency)
	}
}

// TestCommodityIntegrationWithOtherDirectives tests that commodities work with other directives
func TestCommodityIntegrationWithOtherDirectives(t *testing.T) {
	ctx := context.Background()
	source := `
2024-01-01 commodity USD
  name: "US Dollar"

2024-01-01 open Assets:Checking USD
2024-01-01 open Equity:Opening USD

2024-01-02 * "Initial deposit"
  Assets:Checking  100 USD
  Equity:Opening  -100 USD
`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Process all directives
	_, err := processErr(ctx, ledger, tree)
	assert.NoError(t, err, "should process without errors")

	// Verify the commodity is declared alongside the accounts
	assert.True(t, ledger.commodities["USD"], "commodity should be declared")
	_, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok, "account should exist")
}
