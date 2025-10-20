package telemetry

import (
	"fmt"
	"io"
	"time"
)

// formatTimingTree outputs the timing tree in a hierarchical format.
// Example output:
//
//	Total: 125ms
//	├─ Load: 85ms
//	│  ├─ Parse main.beancount: 45ms
//	│  └─ Merge ASTs: 5ms
//	└─ Process Ledger: 40ms
func formatTimingTree(w io.Writer, root *timerNode) {
	// Calculate duration
	duration := root.end.Sub(root.start)

	// Format root node
	_, _ = fmt.Fprintf(w, "%s: %s\n", root.name, formatDuration(duration))

	// Format children recursively
	for i, child := range root.children {
		isLast := i == len(root.children)-1
		formatNode(w, child, "", isLast)
	}
}

// formatNode recursively formats a node and its children.
func formatNode(w io.Writer, node *timerNode, prefix string, isLast bool) {
	// Calculate duration
	duration := node.end.Sub(node.start)

	// Choose tree characters
	var branch, extension string
	if isLast {
		branch = "└─ "
		extension = "   "
	} else {
		branch = "├─ "
		extension = "│  "
	}

	// Format this node
	_, _ = fmt.Fprintf(w, "%s%s%s: %s\n", prefix, branch, node.name, formatDuration(duration))

	// Format children
	childPrefix := prefix + extension
	for i, child := range node.children {
		childIsLast := i == len(node.children)-1
		formatNode(w, child, childPrefix, childIsLast)
	}
}

// formatDuration formats a duration for display.
// Shows microseconds for < 1ms, milliseconds for < 1s, seconds for >= 1s.
// Prefixes with ~ when rounding loses significant precision.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		// Show microseconds for very fast operations (< 1ms)
		us := float64(d) / float64(time.Microsecond)
		return fmt.Sprintf("%.0fµs", us)
	}
	if d < time.Second {
		ms := float64(d) / float64(time.Millisecond)
		// Check if rounding to integer ms loses significant precision
		truncatedMs := int(ms)
		truncated := time.Duration(truncatedMs) * time.Millisecond
		// Add ~ if the fractional part is >= 50µs
		if d > truncated && d-truncated >= 50*time.Microsecond {
			return fmt.Sprintf("~%.0fms", ms)
		}
		return fmt.Sprintf("%.0fms", ms)
	}
	s := float64(d) / float64(time.Second)
	return fmt.Sprintf("%.2fs", s)
}
