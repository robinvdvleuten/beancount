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
	name     string
	start    time.Time
	end      time.Time
	children []*timerNode
	parent   *timerNode
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
		name:  name,
		start: time.Now(),
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
		name:   name,
		start:  time.Now(),
		parent: t.node,
	}

	t.node.children = append(t.node.children, node)

	return &timingTimer{
		collector: t.collector,
		node:      node,
	}
}
