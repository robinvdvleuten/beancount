package ledger

import (
	"context"
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
		{ast.KindCustom, true},
		{ast.DirectiveKind("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			handler := GetHandler(tt.kind)
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

	handler := &OpenHandler{}
	directive := tree.Directives[0]

	// Validate
	errs, delta := handler.Validate(ctx, ledger, directive)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	// Apply
	handler.Apply(ctx, ledger, directive, delta)

	// Verify
	acc, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok, "account should exist")
	assert.Equal(t, "Assets:Checking", string(acc.Name))
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
	openHandler := &OpenHandler{}
	_, openDelta := openHandler.Validate(ctx, ledger, tree.Directives[0])
	openHandler.Apply(ctx, ledger, tree.Directives[0], openDelta)

	// Close
	closeHandler := &CloseHandler{}
	directive := tree.Directives[1]
	errs, delta := closeHandler.Validate(ctx, ledger, directive)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	closeHandler.Apply(ctx, ledger, directive, delta)

	// Verify
	acc, ok := ledger.GetAccount("Assets:Checking")
	assert.True(t, ok, "account should exist")
	assert.True(t, acc.IsClosed(), "account should be closed")
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
		handler := GetHandler(tree.Directives[i].Kind())
		_, delta := handler.Validate(ctx, ledger, tree.Directives[i])
		handler.Apply(ctx, ledger, tree.Directives[i], delta)
	}

	// Process transaction
	txnHandler := &TransactionHandler{}
	txnDirective := tree.Directives[2]
	errs, delta := txnHandler.Validate(ctx, ledger, txnDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	txnHandler.Apply(ctx, ledger, txnDirective, delta)

	// Verify
	checking, _ := ledger.GetAccount("Assets:Checking")
	assert.Equal(t, "1000", checking.Inventory.Get("USD").String())
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
	openHandler := &OpenHandler{}
	_, delta := openHandler.Validate(ctx, ledger, tree.Directives[0])
	openHandler.Apply(ctx, ledger, tree.Directives[0], delta)

	// Also need to open equity account for padding
	tree2 := parser.MustParseString(ctx, "2020-01-01 open Equity:Opening-Balances")
	_, delta = openHandler.Validate(ctx, ledger, tree2.Directives[0])
	openHandler.Apply(ctx, ledger, tree2.Directives[0], delta)

	// Process pad
	padHandler := &PadHandler{}
	_, delta = padHandler.Validate(ctx, ledger, tree.Directives[1])
	padHandler.Apply(ctx, ledger, tree.Directives[1], delta)

	// Process balance
	balanceHandler := &BalanceHandler{}
	balanceDirective := tree.Directives[2]
	errs, delta := balanceHandler.Validate(ctx, ledger, balanceDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	balanceHandler.Apply(ctx, ledger, balanceDirective, delta)
	// Balance handler stores synthetic transaction
}

func TestBalanceHandler_PadNotMarkedUsedOnFailure(t *testing.T) {
	ctx := context.Background()
	openHandler := &OpenHandler{}
	padHandler := &PadHandler{}
	balanceHandler := &BalanceHandler{}

	ledger := New()

	// Open accounts
	tree := parser.MustParseString(ctx, "2020-01-01 open Assets:Checking")
	_, delta := openHandler.Validate(ctx, ledger, tree.Directives[0])
	openHandler.Apply(ctx, ledger, tree.Directives[0], delta)

	tree2 := parser.MustParseString(ctx, "2020-01-01 open Equity:Opening-Balances")
	_, delta = openHandler.Validate(ctx, ledger, tree2.Directives[0])
	openHandler.Apply(ctx, ledger, tree2.Directives[0], delta)

	// Add pad
	tree3 := parser.MustParseString(ctx, "2020-01-01 pad Assets:Checking Equity:Opening-Balances")
	_, delta = padHandler.Validate(ctx, ledger, tree3.Directives[0])
	padHandler.Apply(ctx, ledger, tree3.Directives[0], delta)

	// Balance assertion that will fail: pad is dated 2020-01-01 but balance
	// is also 2020-01-01 — pad must be strictly before balance
	tree4 := parser.MustParseString(ctx, "2020-01-01 balance Assets:Checking 1000.00 USD")
	errs, _ := balanceHandler.Validate(ctx, ledger, tree4.Directives[0])

	// Validation should fail (pad date not before balance date)
	assert.True(t, len(errs) > 0, "validation should fail")
	// Pad should NOT be marked as used since validation failed
	assert.False(t, ledger.usedPads["Assets:Checking"], "pad should not be marked used on validation failure")
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
	openHandler := &OpenHandler{}
	for i := 0; i < 2; i++ {
		_, delta := openHandler.Validate(ctx, ledger, tree.Directives[i])
		openHandler.Apply(ctx, ledger, tree.Directives[i], delta)
	}

	// Process pad
	padHandler := &PadHandler{}
	padDirective := tree.Directives[2]
	errs, delta := padHandler.Validate(ctx, ledger, padDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	padHandler.Apply(ctx, ledger, padDirective, delta)

	// Verify pad was stored
	accountName := string(padDirective.(*ast.Pad).Account)
	storedPad := ledger.padEntries[accountName]
	assert.NotZero(t, storedPad)
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
	openHandler := &OpenHandler{}
	_, delta := openHandler.Validate(ctx, ledger, tree.Directives[0])
	openHandler.Apply(ctx, ledger, tree.Directives[0], delta)

	// Process note
	noteHandler := &NoteHandler{}
	noteDirective := tree.Directives[1]
	errs, _ := noteHandler.Validate(ctx, ledger, noteDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	noteHandler.Apply(ctx, ledger, noteDirective, nil)
	// Note handler doesn't mutate state
}

func TestDocumentHandler(t *testing.T) {
	ctx := context.Background()
	source := `
		2020-01-01 open Assets:Checking
		2020-07-09 document Assets:Checking "/documents/bank-statements/2020-07.pdf"
	`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Open account
	openHandler := &OpenHandler{}
	_, delta := openHandler.Validate(ctx, ledger, tree.Directives[0])
	openHandler.Apply(ctx, ledger, tree.Directives[0], delta)

	// Process document
	docHandler := &DocumentHandler{}
	docDirective := tree.Directives[1]
	errs, _ := docHandler.Validate(ctx, ledger, docDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	docHandler.Apply(ctx, ledger, docDirective, nil)
	// Document handler doesn't mutate state
}

func TestPriceHandler(t *testing.T) {
	ctx := context.Background()
	source := `
		2024-01-15 price USD 1.08 CAD
	`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Process price
	priceHandler := &PriceHandler{}
	priceDirective := tree.Directives[0]
	errs, delta := priceHandler.Validate(ctx, ledger, priceDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")
	assert.NotZero(t, delta, "delta should not be nil")

	priceHandler.Apply(ctx, ledger, priceDirective, delta)

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
	commodityHandler := &CommodityHandler{}
	commodityDirective := tree.Directives[0]
	errs, delta := commodityHandler.Validate(ctx, ledger, commodityDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	commodityHandler.Apply(ctx, ledger, commodityDirective, delta)

	// Verify commodity node was created in the graph
	commodity := commodityDirective.(*ast.Commodity)
	commodityNode := ledger.graph.GetNode(commodity.Currency)
	assert.NotZero(t, commodityNode, "commodity node should be created")
	assert.Equal(t, commodityNode.Kind, "commodity", "node should be of commodity kind")

	// Verify metadata is stored
	if commodityNodeMeta, ok := commodityNode.Meta.(*CommodityNode); ok {
		assert.Equal(t, commodityNodeMeta.ID, commodity.Currency, "commodity ID should match")
		assert.NotZero(t, commodityNodeMeta.Date, "commodity date should be stored")
	}
}

func TestEventHandler(t *testing.T) {
	ctx := context.Background()
	source := `
		2024-01-01 event "location" "New York, USA"
	`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	// Process event
	eventHandler := &EventHandler{}
	eventDirective := tree.Directives[0]
	errs, _ := eventHandler.Validate(ctx, ledger, eventDirective)
	assert.Equal(t, len(errs), 0, "should have no errors")

	eventHandler.Apply(ctx, ledger, eventDirective, nil)
	// Event handler doesn't mutate state currently
}

func TestCustomHandler(t *testing.T) {
	// Custom directives are not commonly used in standard Beancount
	// but the handler should accept them without error
	ledger := New()
	ctx := context.Background()

	customHandler := &CustomHandler{}
	date := newTestDate("2024-01-01")
	custom := ast.NewCustom(date, "test", nil)

	errs, _ := customHandler.Validate(ctx, ledger, custom)
	assert.Equal(t, len(errs), 0, "should have no errors")

	customHandler.Apply(ctx, ledger, custom, nil)
	// Custom handler doesn't mutate state currently
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
		handler := &CommodityHandler{}
		errs, delta := handler.Validate(ctx, ledger, directive)
		assert.Equal(t, len(errs), 0, "valid commodity codes should have no errors")
		assert.NotZero(t, delta, "delta should be returned")

		handler.Apply(ctx, ledger, directive, delta)
	}

	// Verify all commodities were added to the graph
	assert.NotZero(t, ledger.graph.GetNode("USD"), "USD node should exist")
	assert.NotZero(t, ledger.graph.GetNode("EUR"), "EUR node should exist")
	assert.NotZero(t, ledger.graph.GetNode("BTC"), "BTC node should exist")
	assert.NotZero(t, ledger.graph.GetNode("HOOL"), "HOOL node should exist")
	assert.NotZero(t, ledger.graph.GetNode("VTSAX"), "VTSAX node should exist")
	assert.NotZero(t, ledger.graph.GetNode("VACHR"), "VACHR (vacation hours) node should exist")
}

// TestCommodityWithMetadata tests that commodity metadata is preserved
func TestCommodityWithMetadata(t *testing.T) {
	ctx := context.Background()
	source := `
		2024-01-01 commodity USD
		  name: "US Dollar"
		  asset-class: "cash"
	`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	handler := &CommodityHandler{}
	directive := tree.Directives[0]
	errs, delta := handler.Validate(ctx, ledger, directive)
	assert.Equal(t, len(errs), 0, "commodity with metadata should validate")

	handler.Apply(ctx, ledger, directive, delta)

	// Verify metadata was stored
	node := ledger.graph.GetNode("USD")
	assert.NotZero(t, node, "node should exist")
	assert.Equal(t, node.Kind, "commodity", "node should be commodity kind")

	if commodityNodeMeta, ok := node.Meta.(*CommodityNode); ok {
		assert.Equal(t, commodityNodeMeta.ID, "USD", "ID should match")
		assert.True(t, len(commodityNodeMeta.Metadata) > 0, "metadata should be stored")
	}
}

// TestCommodityNodeKind tests that declared commodities have distinct node kind
func TestCommodityNodeKind(t *testing.T) {
	ctx := context.Background()
	source := `
		2024-01-01 commodity BTC
		  precision: 8
	`
	tree := parser.MustParseString(ctx, source)
	ledger := New()

	handler := &CommodityHandler{}
	directive := tree.Directives[0]
	_, delta := handler.Validate(ctx, ledger, directive)
	handler.Apply(ctx, ledger, directive, delta)

	// Verify the node type is distinct from implicit currency nodes
	btcNode := ledger.graph.GetNode("BTC")
	assert.Equal(t, btcNode.Kind, "commodity", "declared commodities should have 'commodity' kind")
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
	err := ledger.Process(ctx, tree)
	assert.NoError(t, err, "should process without errors")

	// Verify commodity node exists alongside account nodes
	commodityNode := ledger.graph.GetNode("USD")
	assert.NotZero(t, commodityNode, "commodity node should exist")
	assert.Equal(t, commodityNode.Kind, "commodity", "commodity node kind should be correct")

	// Verify account node also exists
	accountNode := ledger.graph.GetNode("Assets:Checking")
	assert.NotZero(t, accountNode, "account node should exist")
	assert.Equal(t, accountNode.Kind, "account", "account node kind should be correct")
}
