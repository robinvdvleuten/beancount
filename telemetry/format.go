package telemetry

import (
	"fmt"
	"io"
	"time"

	"github.com/robinvdvleuten/beancount/output"
)

// formatTimingTree outputs the timing tree in a hierarchical format.
// Example output:
//
//	Total: 125ms
//	├─ Load: 85ms
//	│  ├─ Parse main.beancount: 45ms
//	│  └─ Merge ASTs: 5ms
//	└─ Process Ledger: 40ms
func formatTimingTree(w io.Writer, root *timerNode, stylesInterface interface{}) {
	// Type assert to get the styles
	var styles *output.Styles
	if s, ok := stylesInterface.(*output.Styles); ok {
		styles = s
	}

	// Calculate duration
	duration := root.end.Sub(root.start)

	// Format root node
	if styles != nil {
		name := styles.Keyword(root.name)
		timing := formatDuration(duration, false)
		_, _ = fmt.Fprintf(w, "%s: %s\n", name, timing)
	} else {
		_, _ = fmt.Fprintf(w, "%s: %s\n", root.name, formatDuration(duration, false))
	}

	// Format children recursively
	for i, child := range root.children {
		isLast := i == len(root.children)-1
		formatNode(w, child, "", isLast, styles)
	}
}

// formatNode recursively formats a node and its children.
func formatNode(w io.Writer, node *timerNode, prefix string, isLast bool, styles *output.Styles) {
	// Calculate duration
	duration := node.end.Sub(node.start)

	// Determine if this is a slow operation (>= 100ms)
	isSlowOperation := duration >= 100*time.Millisecond

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
	if styles != nil {
		treeChars := styles.Dim(prefix + branch)
		timing := formatDuration(duration, isSlowOperation)
		if isSlowOperation {
			timing = styles.Warning(timing)
		} else {
			timing = styles.Dim(timing)
		}
		_, _ = fmt.Fprintf(w, "%s%s: %s\n", treeChars, node.name, timing)
	} else {
		_, _ = fmt.Fprintf(w, "%s%s%s: %s\n", prefix, branch, node.name, formatDuration(duration, false))
	}

	// Format children
	childPrefix := prefix + extension
	for i, child := range node.children {
		childIsLast := i == len(node.children)-1
		formatNode(w, child, childPrefix, childIsLast, styles)
	}
}

// formatDuration formats a duration for display.
// Shows milliseconds for < 1s, seconds for >= 1s.
// The isSlowOperation parameter is for future use (currently unused but kept for API consistency).
func formatDuration(d time.Duration, isSlowOperation bool) string {
	if d < time.Second {
		ms := float64(d) / float64(time.Millisecond)
		return fmt.Sprintf("%.0fms", ms)
	}
	s := float64(d) / float64(time.Second)
	return fmt.Sprintf("%.2fs", s)
}
