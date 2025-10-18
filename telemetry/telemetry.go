// Package telemetry provides hierarchical timing collection for operations.
// It allows tracking operation durations in a tree structure for detailed
// performance analysis.
//
// The telemetry system uses the context pattern for non-intrusive instrumentation.
// Collectors are passed through context and can be enabled/disabled without
// changing function signatures.
//
// Example usage:
//
//	// Create a timing collector
//	collector := telemetry.NewTimingCollector()
//	ctx := telemetry.WithCollector(context.Background(), collector)
//
//	// Instrument operations
//	timer := collector.Start("Load file")
//	defer timer.End()
//
//	// Nested operations
//	childTimer := timer.Child("Parse")
//	// ... work ...
//	childTimer.End()
//
//	// Print report
//	collector.Report(os.Stderr)
package telemetry

import (
	"context"
	"io"
)

// contextKey is a private type for context keys to avoid collisions
type contextKey struct{}

var collectorKey = contextKey{}

// Collector is the main interface for collecting telemetry data.
// Implementations can collect timings, metrics, or other telemetry data.
type Collector interface {
	// Start begins timing an operation and returns a Timer.
	// The timer should be ended with End() when the operation completes.
	Start(name string) Timer

	// Report outputs the collected telemetry to a writer.
	// The format is implementation-specific.
	// The styles parameter can be used to add terminal styling (optional, can be nil).
	Report(w io.Writer, styles interface{})
}

// Timer tracks a single operation's timing.
// Timers support hierarchical nesting via Child().
type Timer interface {
	// End stops the timer and records the duration.
	End()

	// Child creates a nested timer under this timer.
	// The child timer will appear nested in the output.
	Child(name string) Timer
}

// WithCollector adds a collector to a context.
// The collector can be retrieved later with FromContext.
func WithCollector(ctx context.Context, collector Collector) context.Context {
	return context.WithValue(ctx, collectorKey, collector)
}

// FromContext extracts the collector from context.
// If no collector is present, returns a NoOpCollector that does nothing.
func FromContext(ctx context.Context) Collector {
	if collector, ok := ctx.Value(collectorKey).(Collector); ok {
		return collector
	}
	return noOpCollector{}
}
