package ledger

import (
	"fmt"
	"sort"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/shopspring/decimal"
)

// Graph is a minimal directed graph abstraction for the ledger.
// It represents accounts, currencies, and their relationships through edges.
//
// Nodes are identified by string IDs and represent either:
//   - Account names (e.g., "Assets:Cash", "Liabilities:Loan")
//   - Currency codes (e.g., "USD", "EUR", "BTC")
//
// Edges represent relationships:
//   - Price edges: currency conversions with temporal semantics (e.g., USD→EUR on 2024-01-15)
//   - Account edges: opening/closing relationships (metadata-only, no weight)
//   - Transaction edges: hyperedges connecting multiple postings (not directly stored; handled by inventory)
//
// The graph uses an adjacency structure optimized for path finding and currency conversion queries.
// Prices automatically create bidirectional edges (USD→EUR creates EUR→USD inverse).
type Graph struct {
	// nodes maps node ID to node metadata
	nodes map[string]*Node

	// edges maps from node ID to list of outgoing edges
	// Structure: edges[fromNodeID] = []*Edge
	edges map[string][]*Edge

	// priceEdgesByDate maps (date string) to list of price edges for efficient temporal lookup
	// Used for forward-fill price queries
	priceEdgesByDate map[string][]*Edge

	// sortedDates maintains price dates in chronological order for forward-fill lookups
	sortedDates []*ast.Date
}

// Node represents a vertex in the ledger graph.
// Nodes are typed (Account, Currency, Commodity) for semantic clarity.
type Node struct {
	ID   string      // Unique identifier (e.g., "Assets:Cash", "USD")
	Kind string      // "account", "currency", "commodity"
	Meta interface{} // Optional metadata (e.g., Account pointer, commodity info)
}

// Edge represents a directed relationship between two nodes.
// Edges carry weights (for prices), dates (for temporal semantics), and original directives.
type Edge struct {
	From       string          // Source node ID
	To         string          // Target node ID
	Kind       string          // "price", "opening", "closing", "transaction_posting"
	Date       *ast.Date       // Date the edge is valid from (required for all edges)
	Weight     decimal.Decimal // Rate/amount for price edges; zero for non-price edges
	Meta       interface{}     // Original directive (ast.Price, ast.Transaction, etc.)
	Inferred   bool            // True if edge was inferred (e.g., inverse price edge)
	ValidUntil *ast.Date       // Optional: edge validity end date (for closings)
}

// NewGraph creates a new empty ledger graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:            make(map[string]*Node),
		edges:            make(map[string][]*Edge),
		priceEdgesByDate: make(map[string][]*Edge),
		sortedDates:      make([]*ast.Date, 0),
	}
}

// AddNode adds a node to the graph or returns existing node if already present.
func (g *Graph) AddNode(id, kind string, meta interface{}) *Node {
	if node, exists := g.nodes[id]; exists {
		return node
	}

	node := &Node{
		ID:   id,
		Kind: kind,
		Meta: meta,
	}
	g.nodes[id] = node
	return node
}

// GetNode retrieves a node by ID, or nil if not found.
func (g *Graph) GetNode(id string) *Node {
	return g.nodes[id]
}

// AddEdge adds a directed edge to the graph.
// Automatically ensures both source and target nodes exist.
// Returns the added edge for chaining or inspection.
func (g *Graph) AddEdge(edge *Edge) *Edge {
	// Ensure nodes exist
	g.AddNode(edge.From, "unknown", nil)
	g.AddNode(edge.To, "unknown", nil)

	// Add edge to adjacency list
	g.edges[edge.From] = append(g.edges[edge.From], edge)

	// Index price edges by date for forward-fill queries
	if edge.Kind == "price" && edge.Date != nil {
		dateKey := edge.Date.String()
		g.priceEdgesByDate[dateKey] = append(g.priceEdgesByDate[dateKey], edge)

		// Keep sortedDates sorted (only add if new date)
		dateExists := false
		for _, d := range g.sortedDates {
			if d.String() == dateKey {
				dateExists = true
				break
			}
		}
		if !dateExists {
			g.sortedDates = append(g.sortedDates, edge.Date)
			sort.Slice(g.sortedDates, func(i, j int) bool {
				return g.sortedDates[i].Before(g.sortedDates[j].Time)
			})
		}
	}

	return edge
}

// GetOutgoingEdges returns all edges leaving a node.
func (g *Graph) GetOutgoingEdges(fromID string) []*Edge {
	edges := g.edges[fromID]
	if edges == nil {
		return []*Edge{}
	}
	return edges
}

// GetPriceEdgesOnDate returns all price edges valid on or before the given date.
// Returns edges in reverse chronological order (most recent first).
func (g *Graph) GetPriceEdgesOnDate(date *ast.Date) []*Edge {
	var result []*Edge

	// Iterate dates in reverse chronological order
	for i := len(g.sortedDates) - 1; i >= 0; i-- {
		sortedDate := g.sortedDates[i]

		// Stop if we've gone before the lookup date
		if sortedDate.After(date.Time) {
			continue
		}

		dateKey := sortedDate.String()
		if edges, ok := g.priceEdgesByDate[dateKey]; ok {
			result = append(result, edges...)
		}
	}

	return result
}

// FindPath performs breadth-first search to find a path from source to target node.
// Used for currency conversion pathfinding (e.g., USD→EUR→GBP).
//
// The date parameter enables temporal edge filtering: only edges valid on or before the date are used.
// Returns the path as a slice of edges in order, or an error if no path exists.
//
// Time complexity: O(V + E) where V is nodes and E is edges in the search space.
// Space complexity: O(V) for queue and visited set.
func (g *Graph) FindPath(fromID, toID string, date *ast.Date) ([]*Edge, error) {
	// Same node - identity path
	if fromID == toID {
		return []*Edge{}, nil
	}

	// BFS to find path
	type queueItem struct {
		nodeID string
		edges  []*Edge
	}

	queue := []queueItem{{nodeID: fromID, edges: []*Edge{}}}
	visited := make(map[string]bool)
	visited[fromID] = true

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		// Explore outgoing edges
		for _, edge := range g.GetOutgoingEdges(item.nodeID) {
			// Skip edges invalid for this date
			if edge.Kind == "price" && !isEdgeValidOnDate(edge, date) {
				continue
			}

			targetID := edge.To

			// Found target
			if targetID == toID {
				return append(item.edges, edge), nil
			}

			// Skip visited nodes to avoid cycles
			if visited[targetID] {
				continue
			}

			visited[targetID] = true
			queue = append(queue, queueItem{
				nodeID: targetID,
				edges:  append(item.edges, edge),
			})
		}
	}

	return nil, fmt.Errorf("no path found from %s to %s on %s", fromID, toID, date.String())
}

// isEdgeValidOnDate checks if an edge is valid on or before the given date.
// For price edges, this means the edge's date is on or before the lookup date,
// and the edge is not expired (ValidUntil is after the lookup date).
func isEdgeValidOnDate(edge *Edge, date *ast.Date) bool {
	if edge.Date == nil {
		return true
	}

	// Edge date must be on or before lookup date
	if edge.Date.After(date.Time) {
		return false
	}

	// If there's a validity end date, it must be after the lookup date
	if edge.ValidUntil != nil && !edge.ValidUntil.After(date.Time) {
		return false
	}

	return true
}

// ConvertAmount converts an amount from one currency to another using price edges.
// Uses pathfinding to find a conversion path if a direct edge doesn't exist.
// Returns the converted amount using the most recent prices on or before the date.
//
// Same-currency conversions always return 1.0.
// Returns an error if no conversion path exists or if intermediate conversions fail.
func (g *Graph) ConvertAmount(amount decimal.Decimal, fromCur, toCur string, date *ast.Date) (decimal.Decimal, error) {
	// Same currency - identity conversion
	if fromCur == toCur {
		return decimal.NewFromInt(1), nil
	}

	// Find path from source to target currency
	path, err := g.FindPath(fromCur, toCur, date)
	if err != nil {
		return decimal.Zero, err
	}

	// Calculate conversion by multiplying all edge weights
	result := decimal.NewFromInt(1)
	for _, edge := range path {
		if edge.Kind != "price" || edge.Weight.IsZero() {
			return decimal.Zero, fmt.Errorf("invalid price edge in conversion path: %s→%s", edge.From, edge.To)
		}
		result = result.Mul(edge.Weight)
	}

	return result, nil
}

// Stats returns information about the graph structure.
// Useful for debugging and understanding graph growth.
type Stats struct {
	NodeCount  int
	EdgeCount  int
	PriceCount int
}

// GetStats returns graph statistics.
func (g *Graph) GetStats() Stats {
	edgeCount := 0
	priceCount := 0

	for _, edges := range g.edges {
		for _, edge := range edges {
			edgeCount++
			if edge.Kind == "price" {
				priceCount++
			}
		}
	}

	return Stats{
		NodeCount:  len(g.nodes),
		EdgeCount:  edgeCount,
		PriceCount: priceCount,
	}
}
