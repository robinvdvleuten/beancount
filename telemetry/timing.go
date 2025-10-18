package telemetry

import (
	"io"
	"sync"
	"time"
)

// TimingCollector collects hierarchical timing data.
// It builds a tree structure of timers that can be reported as a nested view.
type TimingCollector struct {
	root    *timerNode
	current *timerNode
	mu      sync.Mutex
}

// timerNode represents a single timed operation in the tree.
type timerNode struct {
	name      string
	start     time.Time
	end       time.Time
	duration  time.Duration // Aggregated duration (if aggregated)
	callCount int           // Number of aggregated calls (1 if not aggregated)
	children  []*timerNode
	parent    *timerNode
}

// NewTimingCollector creates a new timing collector.
func NewTimingCollector() *TimingCollector {
	return &TimingCollector{}
}

// Start begins timing an operation.
func (c *TimingCollector) Start(name string) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	node := &timerNode{
		name:      name,
		start:     time.Now(),
		callCount: 1,
	}

	// If this is the first timer, it becomes the root
	if c.root == nil {
		c.root = node
		c.current = node
	} else {
		// Add as child of current node
		node.parent = c.current
		c.current.children = append(c.current.children, node)
		c.current = node
	}

	return &timingTimer{
		collector: c,
		node:      node,
	}
}

// Report outputs the timing tree to a writer.
// Implemented in format.go
func (c *TimingCollector) Report(w io.Writer, styles interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.root == nil {
		return
	}

	// Aggregate duplicate sibling nodes before formatting
	// This reduces thousands of transaction timers into aggregated entries
	aggregateNode(c.root)

	formatTimingTree(w, c.root, styles)
}

// timingTimer is a Timer implementation that records to a TimingCollector.
type timingTimer struct {
	collector *TimingCollector
	node      *timerNode
}

// End stops the timer.
func (t *timingTimer) End() {
	t.collector.mu.Lock()
	defer t.collector.mu.Unlock()

	t.node.end = time.Now()

	// Move current back to parent
	if t.node.parent != nil {
		t.collector.current = t.node.parent
	}
}

// Child creates a nested timer.
func (t *timingTimer) Child(name string) Timer {
	// Create a new timer that will be nested under this one
	// We need to temporarily set current to this node
	t.collector.mu.Lock()
	defer t.collector.mu.Unlock()

	node := &timerNode{
		name:      name,
		start:     time.Now(),
		callCount: 1,
		parent:    t.node,
	}

	t.node.children = append(t.node.children, node)

	return &timingTimer{
		collector: t.collector,
		node:      node,
	}
}

// aggregateNode recursively aggregates sibling nodes with the same name.
// This reduces thousands of individual transaction timers into a single
// aggregated entry showing total time and call count.
// Preserves the execution order based on first occurrence.
func aggregateNode(node *timerNode) {
	if node == nil || len(node.children) == 0 {
		return
	}

	// Group children by name, tracking first occurrence index for ordering
	groups := make(map[string][]*timerNode)
	firstIndex := make(map[string]int)
	var uniqueNames []string

	for i, child := range node.children {
		if _, exists := groups[child.name]; !exists {
			firstIndex[child.name] = i
			uniqueNames = append(uniqueNames, child.name)
		}
		groups[child.name] = append(groups[child.name], child)
	}

	// Build new aggregated children list in order of first occurrence
	var newChildren []*timerNode

	for _, name := range uniqueNames {
		siblings := groups[name]
		if len(siblings) == 1 {
			// Single node - no aggregation needed
			// But still recursively aggregate its children
			aggregateNode(siblings[0])
			newChildren = append(newChildren, siblings[0])
		} else {
			// Multiple siblings with same name - aggregate them
			aggregated := &timerNode{
				name:      name,
				parent:    node,
				callCount: len(siblings),
				children:  make([]*timerNode, 0),
			}

			// Sum durations and aggregate children
			var totalDuration time.Duration
			childGroups := make(map[string][]*timerNode)
			childFirstIndex := make(map[string]int)
			var childUniqueNames []string
			childIndexCounter := 0

			for _, sibling := range siblings {
				// Calculate this sibling's duration
				duration := sibling.end.Sub(sibling.start)
				totalDuration += duration

				// Collect all grandchildren for aggregation, preserving order
				for _, grandchild := range sibling.children {
					if _, exists := childGroups[grandchild.name]; !exists {
						childFirstIndex[grandchild.name] = childIndexCounter
						childUniqueNames = append(childUniqueNames, grandchild.name)
					}
					childGroups[grandchild.name] = append(childGroups[grandchild.name], grandchild)
					childIndexCounter++
				}
			}

			aggregated.duration = totalDuration

			// Recursively aggregate grandchildren in order of first occurrence
			for _, childName := range childUniqueNames {
				grandchildren := childGroups[childName]
				if len(grandchildren) == 1 {
					// Single grandchild
					aggregateNode(grandchildren[0])
					grandchildren[0].parent = aggregated
					aggregated.children = append(aggregated.children, grandchildren[0])
				} else {
					// Multiple grandchildren with same name - aggregate them
					aggregatedChild := &timerNode{
						name:      childName,
						parent:    aggregated,
						callCount: len(grandchildren),
					}

					var childTotalDuration time.Duration
					for _, gc := range grandchildren {
						childTotalDuration += gc.end.Sub(gc.start)
					}
					aggregatedChild.duration = childTotalDuration

					// For deeply nested children, we could recursively aggregate further
					// but for now, we'll aggregate at 2 levels which covers validation.transaction and its children
					aggregated.children = append(aggregated.children, aggregatedChild)
				}
			}

			newChildren = append(newChildren, aggregated)
		}
	}

	// Replace children with aggregated list
	node.children = newChildren
}
