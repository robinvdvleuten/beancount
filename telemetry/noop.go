package telemetry

import "io"

// noOpCollector is a collector that does nothing.
// It provides zero overhead when telemetry is disabled.
type noOpCollector struct{}

// Start returns a no-op timer.
func (noOpCollector) Start(name string) Timer {
	return noOpTimer{}
}

// StartStructured returns a no-op structured timer.
func (noOpCollector) StartStructured(config TimerConfig) StructuredTimer {
	return noOpStructuredTimer{}
}

// Report does nothing.
func (noOpCollector) Report(w io.Writer) {}

// noOpTimer is a timer that does nothing.
type noOpTimer struct{}

// End does nothing.
func (noOpTimer) End() {}

// Child returns a no-op timer.
func (noOpTimer) Child(name string) Timer {
	return noOpTimer{}
}

// noOpStructuredTimer is a structured timer that does nothing.
type noOpStructuredTimer struct{}

// End does nothing.
func (noOpStructuredTimer) End() {}

// Child returns a no-op timer.
func (noOpStructuredTimer) Child(name string) Timer {
	return noOpTimer{}
}

// Config returns an empty config.
func (noOpStructuredTimer) Config() TimerConfig {
	return TimerConfig{}
}
