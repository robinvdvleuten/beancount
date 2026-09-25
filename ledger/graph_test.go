package ledger

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Helper to parse decimal - consistent with existing tests
func mustParseDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// Helper to create a Date from string (consistent with existing tests)
func newTestDate(dateStr string) *ast.Date {
	date := &ast.Date{}
	err := date.Capture([]string{dateStr})
	if err != nil {
		panic(err)
	}
	return date
}

func TestNewGraph(t *testing.T) {
	g := NewGraph()
	assert.NotZero(t, g)
	assert.Equal(t, len(g.nodes), 0)
	assert.Equal(t, len(g.edges), 0)
}

func TestGraph_AddNode(t *testing.T) {
	g := NewGraph()

	node := g.AddNode("USD", "currency", nil)
	assert.NotZero(t, node)
	assert.Equal(t, node.ID, "USD")
	assert.Equal(t, node.Kind, "currency")

	// Adding same node again returns existing
	node2 := g.AddNode("USD", "currency", nil)
	assert.Equal(t, node, node2)
	assert.Equal(t, len(g.nodes), 1)
}

func TestGraph_GetNode(t *testing.T) {
	g := NewGraph()
	g.AddNode("Assets:Cash", "account", nil)

	node := g.GetNode("Assets:Cash")
	assert.NotZero(t, node)
	assert.Equal(t, node.ID, "Assets:Cash")

	// Non-existent node returns nil
	missing := g.GetNode("Assets:Missing")
	assert.Zero(t, missing)
}

func TestGraph_AddEdge_Basic(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	edge := &Edge{
		From:   "USD",
		To:     "EUR",
		Kind:   "price",
		Date:   date,
		Weight: mustParseDec("0.92"),
	}

	result := g.AddEdge(edge)
	assert.Equal(t, result, edge)

	// Nodes should be auto-created
	assert.NotZero(t, g.GetNode("USD"))
	assert.NotZero(t, g.GetNode("EUR"))

	// Edge should be retrievable
	outgoing := g.GetOutgoingEdges("USD")
	assert.Equal(t, len(outgoing), 1)
	assert.Equal(t, outgoing[0].To, "EUR")
}

func TestGraph_AddEdge_CreatesNodes(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	edge := &Edge{
		From:   "GBP",
		To:     "JPY",
		Kind:   "price",
		Date:   date,
		Weight: mustParseDec("150.5"),
	}

	g.AddEdge(edge)

	// Both nodes should exist
	assert.NotZero(t, g.GetNode("GBP"))
	assert.NotZero(t, g.GetNode("JPY"))
}

func TestGraph_GetOutgoingEdges(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	// Add multiple edges from USD
	edge1 := &Edge{From: "USD", To: "EUR", Kind: "price", Date: date, Weight: mustParseDec("0.92")}
	edge2 := &Edge{From: "USD", To: "GBP", Kind: "price", Date: date, Weight: mustParseDec("0.79")}
	edge3 := &Edge{From: "EUR", To: "GBP", Kind: "price", Date: date, Weight: mustParseDec("0.86")}

	g.AddEdge(edge1)
	g.AddEdge(edge2)
	g.AddEdge(edge3)

	// Check outgoing edges from USD
	usdOutgoing := g.GetOutgoingEdges("USD")
	assert.Equal(t, len(usdOutgoing), 2)

	// Check outgoing edges from EUR
	eurOutgoing := g.GetOutgoingEdges("EUR")
	assert.Equal(t, len(eurOutgoing), 1)

	// Non-existent node returns empty slice
	missing := g.GetOutgoingEdges("CAD")
	assert.Equal(t, len(missing), 0)
}

func TestGraph_PriceEdgesIndexing(t *testing.T) {
	g := NewGraph()
	date1 := newTestDate("2024-01-15")
	date2 := newTestDate("2024-01-20")

	// Add price edges out of order and repeat one date.
	g.AddEdge(&Edge{From: "USD", To: "EUR", Kind: "price", Date: date2, Weight: mustParseDec("0.94")})
	g.AddEdge(&Edge{From: "USD", To: "EUR", Kind: "price", Date: date1, Weight: mustParseDec("0.92")})
	g.AddEdge(&Edge{From: "EUR", To: "GBP", Kind: "price", Date: date1, Weight: mustParseDec("0.86")})

	// Both dates should be indexed
	assert.Equal(t, len(g.sortedDates), 2)

	// Dates should be sorted
	assert.True(t, g.sortedDates[0].String() == date1.String())
	assert.True(t, g.sortedDates[1].String() == date2.String())
}

func TestGraph_GetPriceEdgesOnDate(t *testing.T) {
	g := NewGraph()
	date1 := newTestDate("2024-01-10")
	date2 := newTestDate("2024-01-15")
	date3 := newTestDate("2024-01-20")

	// Add edges on different dates
	g.AddEdge(&Edge{From: "USD", To: "EUR", Kind: "price", Date: date1, Weight: mustParseDec("0.90")})
	g.AddEdge(&Edge{From: "EUR", To: "GBP", Kind: "price", Date: date2, Weight: mustParseDec("0.86")})
	g.AddEdge(&Edge{From: "GBP", To: "JPY", Kind: "price", Date: date3, Weight: mustParseDec("150")})

	// Query on date2 - should get edges from date1 and date2
	edges := g.GetPriceEdgesOnDate(date2)
	assert.True(t, len(edges) >= 2)

	// Should be in reverse chronological order (most recent first)
	if len(edges) >= 2 {
		assert.True(t, edges[0].Date.String() == date2.String())
	}
}

func TestGraph_MultipleEdgesSameSource(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	// USD has multiple outgoing edges
	g.AddEdge(&Edge{From: "USD", To: "EUR", Kind: "price", Date: date, Weight: mustParseDec("0.92")})
	g.AddEdge(&Edge{From: "USD", To: "GBP", Kind: "price", Date: date, Weight: mustParseDec("0.79")})
	g.AddEdge(&Edge{From: "USD", To: "JPY", Kind: "price", Date: date, Weight: mustParseDec("150")})

	outgoing := g.GetOutgoingEdges("USD")
	assert.Equal(t, len(outgoing), 3)

	// Verify all targets
	targets := make(map[string]bool)
	for _, e := range outgoing {
		targets[e.To] = true
	}
	assert.True(t, targets["EUR"])
	assert.True(t, targets["GBP"])
	assert.True(t, targets["JPY"])
}

func TestGraph_GetStats(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	// Add some structure
	g.AddEdge(&Edge{From: "USD", To: "EUR", Kind: "price", Date: date, Weight: mustParseDec("0.92")})
	g.AddEdge(&Edge{From: "EUR", To: "GBP", Kind: "price", Date: date, Weight: mustParseDec("0.86")})
	g.AddEdge(&Edge{From: "Assets:Cash", To: "Assets:Savings", Kind: "transfer", Date: date, Weight: decimal.Zero})

	stats := g.GetStats()
	assert.Equal(t, stats.NodeCount, 5)  // USD, EUR, GBP, Assets:Cash, Assets:Savings
	assert.Equal(t, stats.EdgeCount, 3)  // All edges
	assert.Equal(t, stats.PriceCount, 2) // Only price edges
}

func TestGraph_EdgeMetadata(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	price := ast.NewPrice(date, "USD", nil)

	edge := &Edge{
		From:   "USD",
		To:     "EUR",
		Kind:   "price",
		Date:   date,
		Weight: mustParseDec("0.92"),
		Meta:   price,
	}

	g.AddEdge(edge)

	outgoing := g.GetOutgoingEdges("USD")
	assert.Equal(t, len(outgoing), 1)

	// Metadata should be preserved
	priceMeta := outgoing[0].Meta.(*ast.Price)
	assert.Equal(t, priceMeta.Commodity, "USD")
}

func TestGraph_InferredEdgeFlag(t *testing.T) {
	g := NewGraph()
	date := newTestDate("2024-01-15")

	// Add explicit edge
	explicit := &Edge{
		From:     "USD",
		To:       "EUR",
		Kind:     "price",
		Date:     date,
		Weight:   mustParseDec("0.92"),
		Inferred: false,
	}

	// Add inferred edge (e.g., inverse)
	inferred := &Edge{
		From:     "EUR",
		To:       "USD",
		Kind:     "price",
		Date:     date,
		Weight:   mustParseDec("1.0869"), // 1/0.92
		Inferred: true,
	}

	g.AddEdge(explicit)
	g.AddEdge(inferred)

	// Both should be retrievable
	usdOutgoing := g.GetOutgoingEdges("USD")
	assert.Equal(t, usdOutgoing[0].Inferred, false)

	eurOutgoing := g.GetOutgoingEdges("EUR")
	assert.Equal(t, eurOutgoing[0].Inferred, true)
}

func TestGraph_IsEdgeValidOnDate(t *testing.T) {
	tests := []struct {
		name      string
		edge      *Edge
		queryDate *ast.Date
		expected  bool
	}{
		{
			name: "edge on exact date",
			edge: &Edge{
				Date:       newTestDate("2024-01-15"),
				ValidUntil: nil,
			},
			queryDate: newTestDate("2024-01-15"),
			expected:  true,
		},
		{
			name: "edge before query date",
			edge: &Edge{
				Date:       newTestDate("2024-01-10"),
				ValidUntil: nil,
			},
			queryDate: newTestDate("2024-01-15"),
			expected:  true,
		},
		{
			name: "edge after query date",
			edge: &Edge{
				Date:       newTestDate("2024-01-20"),
				ValidUntil: nil,
			},
			queryDate: newTestDate("2024-01-15"),
			expected:  false,
		},
		{
			name: "edge expired",
			edge: &Edge{
				Date:       newTestDate("2024-01-10"),
				ValidUntil: newTestDate("2024-01-12"),
			},
			queryDate: newTestDate("2024-01-15"),
			expected:  false,
		},
		{
			name: "edge valid within range",
			edge: &Edge{
				Date:       newTestDate("2024-01-10"),
				ValidUntil: newTestDate("2024-01-20"),
			},
			queryDate: newTestDate("2024-01-15"),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEdgeValidOnDate(tt.edge, tt.queryDate)
			assert.Equal(t, result, tt.expected)
		})
	}
}
