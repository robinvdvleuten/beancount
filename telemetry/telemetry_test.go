package telemetry

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestNoOpCollector(t *testing.T) {
	// NoOp collector should do nothing and have zero overhead
	collector := noOpCollector{}

	timer := collector.Start("test")
	timer.End()

	child := timer.Child("child")
	child.End()

	var buf bytes.Buffer
	collector.Report(&buf)

	// Should produce no output
	if buf.Len() != 0 {
		t.Errorf("NoOp collector should produce no output, got: %s", buf.String())
	}
}

func TestFromContextReturnsNoOpWhenMissing(t *testing.T) {
	ctx := context.Background()
	collector := FromContext(ctx)

	// Should return NoOp collector, not nil
	if collector == nil {
		t.Fatal("FromContext should never return nil")
	}

	// Should be a NoOp collector
	if _, ok := collector.(noOpCollector); !ok {
		t.Errorf("FromContext should return noOpCollector when none present, got: %T", collector)
	}
}

func TestWithCollector(t *testing.T) {
	ctx := context.Background()
	collector := NewTimingCollector()

	ctx = WithCollector(ctx, collector)

	retrieved := FromContext(ctx)
	// Compare as Collector interface
	retrievedTiming, ok := retrieved.(*TimingCollector)
	if !ok || retrievedTiming != collector {
		t.Error("FromContext should return the same collector that was added")
	}
}

func TestTimingCollectorBasic(t *testing.T) {
	collector := NewTimingCollector()

	timer := collector.Start("Operation")
	time.Sleep(10 * time.Millisecond)
	timer.End()

	var buf bytes.Buffer
	collector.Report(&buf)

	output := buf.String()

	// Should contain operation name
	if !strings.Contains(output, "Operation") {
		t.Errorf("Output should contain operation name, got: %s", output)
	}

	// Should contain duration
	if !strings.Contains(output, "ms") {
		t.Errorf("Output should contain duration, got: %s", output)
	}
}

func TestTimingCollectorHierarchical(t *testing.T) {
	collector := NewTimingCollector()

	// Root operation
	root := collector.Start("Total")
	time.Sleep(5 * time.Millisecond)

	// Child operation
	child := root.Child("Child")
	time.Sleep(5 * time.Millisecond)
	child.End()

	// Another child
	child2 := root.Child("Child 2")
	time.Sleep(5 * time.Millisecond)
	child2.End()

	root.End()

	var buf bytes.Buffer
	collector.Report(&buf)

	output := buf.String()

	// Should contain all operation names
	if !strings.Contains(output, "Total") {
		t.Errorf("Output should contain 'Total', got: %s", output)
	}
	if !strings.Contains(output, "Child") {
		t.Errorf("Output should contain 'Child', got: %s", output)
	}
	if !strings.Contains(output, "Child 2") {
		t.Errorf("Output should contain 'Child 2', got: %s", output)
	}

	// Should have tree structure (contains tree characters)
	if !strings.Contains(output, "├─") && !strings.Contains(output, "└─") {
		t.Errorf("Output should contain tree structure, got: %s", output)
	}
}

func TestTimingCollectorDeepNesting(t *testing.T) {
	collector := NewTimingCollector()

	// Create deeply nested timers
	t1 := collector.Start("Level 1")
	t2 := t1.Child("Level 2")
	t3 := t2.Child("Level 3")
	time.Sleep(5 * time.Millisecond)
	t3.End()
	t2.End()
	t1.End()

	var buf bytes.Buffer
	collector.Report(&buf)

	output := buf.String()

	// Should contain all levels
	if !strings.Contains(output, "Level 1") || !strings.Contains(output, "Level 2") || !strings.Contains(output, "Level 3") {
		t.Errorf("Output should contain all levels, got: %s", output)
	}

	// Count indentation levels (each level adds 3 chars: "│  " or "   ")
	lines := strings.Split(output, "\n")
	foundLevel3 := false
	for _, line := range lines {
		if strings.Contains(line, "Level 3") {
			foundLevel3 = true
			// Level 3 should be indented (has prefix before "└─" or "├─")
			if !strings.Contains(line, "   ") && !strings.Contains(line, "│  ") {
				t.Errorf("Level 3 should be indented, got: %s", line)
			}
		}
	}
	if !foundLevel3 {
		t.Error("Should find Level 3 in output")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{1 * time.Millisecond, "1ms"},
		{10 * time.Millisecond, "10ms"},
		{100 * time.Millisecond, "100ms"},
		{999 * time.Millisecond, "999ms"},
		{1 * time.Second, "1.00s"},
		{1500 * time.Millisecond, "1.50s"},
		{2 * time.Second, "2.00s"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.duration)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
		}
	}
}

func TestTimingCollectorEmptyReport(t *testing.T) {
	collector := NewTimingCollector()

	var buf bytes.Buffer
	collector.Report(&buf)

	// Should produce no output when no timers have been started
	if buf.Len() != 0 {
		t.Errorf("Empty collector should produce no output, got: %s", buf.String())
	}
}
