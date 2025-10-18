package telemetry

import "io"

// noOpCollector is a collector that does nothing.
// It provides zero overhead when telemetry is disabled.
type noOpCollector struct{}

// Start returns a no-op timer.
func (noOpCollector) Start(name string) Timer {
	return noOpTimer{}
}

// Report does nothing.
func (noOpCollector) Report(w io.Writer, styles interface{}) {}

// noOpTimer is a timer that does nothing.
type noOpTimer struct{}

// End does nothing.
func (noOpTimer) End() {}

// Child returns a no-op timer.
func (noOpTimer) Child(name string) Timer {
	return noOpTimer{}
}
