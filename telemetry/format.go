package telemetry

import (
	"fmt"
	"io"
	"strconv"
	"strings"
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

	// Special handling for validation.transactions and ledger.processing timers
	timerName := node.name
	if strings.HasPrefix(node.name, "validation.transactions (") && strings.HasSuffix(node.name, " total)") {
		// Parse transaction count from name like "validation.transactions (123 total)"
		if countStr := strings.TrimPrefix(strings.TrimSuffix(node.name, " total)"), "validation.transactions ("); countStr != "" {
			if count, err := strconv.Atoi(countStr); err == nil && count > 0 {
				// Calculate metrics
				durationMs := float64(duration.Nanoseconds()) / 1e6
				if durationMs > 0 {
					txnsPerMs := float64(count) / durationMs
					avgTimePerTxn := duration / time.Duration(count)
					timerName = fmt.Sprintf("validation.transactions (%d total, %.1f/ms, %v avg)",
						count, txnsPerMs, avgTimePerTxn.Round(time.Microsecond))
				}
			}
		}
	} else if strings.HasPrefix(node.name, "ledger.processing (") && strings.HasSuffix(node.name, " directives)") {
		// Parse directive count from name like "ledger.processing (123 directives)"
		if countStr := strings.TrimPrefix(strings.TrimSuffix(node.name, " directives)"), "ledger.processing ("); countStr != "" {
			if count, err := strconv.Atoi(countStr); err == nil && count > 0 {
				// Calculate directives per ms
				durationMs := float64(duration.Nanoseconds()) / 1e6
				if durationMs > 0 {
					dirsPerMs := float64(count) / durationMs
					timerName = fmt.Sprintf("ledger.processing (%d directives, %.1f/ms)",
						count, dirsPerMs)
				}
			}
		}
	}

	// Format this node
	_, _ = fmt.Fprintf(w, "%s%s%s: %s\n", prefix, branch, timerName, formatDuration(duration))

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
