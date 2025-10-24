package telemetry

import (
	"io"
	"sync"
	"time"
)

// TimingCollector collects hierarchical timing data.
// It builds a tree structure of timers that can be reported as a nested view.
//
// TimingCollector is safe for concurrent calls to Start() and StartStructured(),
// allowing multiple goroutines to create independent timer trees. The mutex
// protects the collector's internal state (roots, current) during timer creation
// and completion.
//
// Individual Timer instances are not safe for concurrent use. See Timer interface
// documentation for usage constraints.
type TimingCollector struct {
	roots   []*timerNode
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
	config   *TimerConfig // Optional structured config for metrics calculation
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

	// If no current timer, this becomes a new root
	if c.current == nil {
		c.roots = append(c.roots, node)
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

// StartStructured begins timing an operation with structured configuration.
func (c *TimingCollector) StartStructured(config TimerConfig) StructuredTimer {
	c.mu.Lock()
	defer c.mu.Unlock()

	node := &timerNode{
		name:   config.Name,
		start:  time.Now(),
		config: &config, // Store config for metrics calculation
	}

	// If no current timer, this becomes a new root
	if c.current == nil {
		c.roots = append(c.roots, node)
		c.current = node
	} else {
		// Add as child of current node
		node.parent = c.current
		c.current.children = append(c.current.children, node)
		c.current = node
	}

	return &timingStructuredTimer{
		collector: c,
		node:      node,
		config:    config,
	}
}

// Report outputs the timing tree to a writer.
// Implemented in format.go
func (c *TimingCollector) Report(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.roots) == 0 {
		return
	}

	for _, root := range c.roots {
		formatTimingTree(w, root)
	}
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

	// Move current back to parent (or nil if this was a root)
	t.collector.current = t.node.parent
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
	t.collector.current = node

	return &timingTimer{
		collector: t.collector,
		node:      node,
	}
}

// timingStructuredTimer is a StructuredTimer implementation that records to a TimingCollector.
type timingStructuredTimer struct {
	collector *TimingCollector
	node      *timerNode
	config    TimerConfig
}

// End stops the timer.
func (t *timingStructuredTimer) End() {
	t.collector.mu.Lock()
	defer t.collector.mu.Unlock()

	t.node.end = time.Now()

	// Move current back to parent (or nil if this was a root)
	t.collector.current = t.node.parent
}

// Child creates a nested timer.
func (t *timingStructuredTimer) Child(name string) Timer {
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
	t.collector.current = node

	return &timingTimer{
		collector: t.collector,
		node:      node,
	}
}

// Config returns the timer configuration.
func (t *timingStructuredTimer) Config() TimerConfig {
	return t.config
}
