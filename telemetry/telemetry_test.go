package telemetry

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
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
	assert.Equal(t, 0, buf.Len(), "NoOp collector should produce no output")
}

func TestFromContextReturnsNoOpWhenMissing(t *testing.T) {
	ctx := context.Background()
	collector := FromContext(ctx)

	// Should return NoOp collector, not nil
	assert.True(t, collector != nil, "FromContext should never return nil")

	// Should be a NoOp collector
	assert.True(t, func() bool { _, ok := collector.(noOpCollector); return ok }(), "FromContext should return noOpCollector when none present")
}

func TestWithCollector(t *testing.T) {
	ctx := context.Background()
	collector := NewTimingCollector()

	ctx = WithCollector(ctx, collector)

	retrieved := FromContext(ctx)
	// Compare as Collector interface
	retrievedTiming, ok := retrieved.(*TimingCollector)
	assert.True(t, ok && retrievedTiming == collector, "FromContext should return the same collector that was added")
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
	assert.True(t, strings.Contains(output, "Operation"), "Output should contain operation name")

	// Should contain duration
	assert.True(t, strings.Contains(output, "ms"), "Output should contain duration")
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
	assert.True(t, strings.Contains(output, "Total"), "Output should contain 'Total'")
	assert.True(t, strings.Contains(output, "Child"), "Output should contain 'Child'")
	assert.True(t, strings.Contains(output, "Child 2"), "Output should contain 'Child 2'")

	// Should have tree structure (contains tree characters)
	assert.True(t, strings.Contains(output, "├─") || strings.Contains(output, "└─"), "Output should contain tree structure")
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
	assert.True(t, strings.Contains(output, "Level 1") && strings.Contains(output, "Level 2") && strings.Contains(output, "Level 3"), "Output should contain all levels")

	// Count indentation levels (each level adds 3 chars: "│  " or "   ")
	lines := strings.Split(output, "\n")
	foundLevel3 := false
	for _, line := range lines {
		if strings.Contains(line, "Level 3") {
			foundLevel3 = true
			// Level 3 should be indented (has prefix before "└─" or "├─")
			assert.True(t, strings.Contains(line, "   ") || strings.Contains(line, "│  "), "Level 3 should be indented")
		}
	}
	assert.True(t, foundLevel3, "Should find Level 3 in output")
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		// Microsecond precision for < 1ms
		{100 * time.Microsecond, "100µs"},
		{500 * time.Microsecond, "500µs"},
		{999 * time.Microsecond, "999µs"},
		// Exact milliseconds (no rounding indicator)
		{1 * time.Millisecond, "1ms"},
		{10 * time.Millisecond, "10ms"},
		{100 * time.Millisecond, "100ms"},
		{999 * time.Millisecond, "999ms"},
		// Rounded milliseconds (with ~ indicator when precision lost >= 50µs)
		{1*time.Millisecond + 50*time.Microsecond, "~1ms"},
		{1*time.Millisecond + 100*time.Microsecond, "~1ms"},
		{1*time.Millisecond + 142*time.Microsecond, "~1ms"},
		{5*time.Millisecond + 500*time.Microsecond, "~6ms"}, // 5.5ms rounds up to 6ms
		// Below rounding threshold (no ~ indicator)
		{1*time.Millisecond + 49*time.Microsecond, "1ms"},
		{1*time.Millisecond + 25*time.Microsecond, "1ms"},
		// Seconds
		{1 * time.Second, "1.00s"},
		{1500 * time.Millisecond, "1.50s"},
		{2 * time.Second, "2.00s"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.duration)
		assert.Equal(t, tt.want, got, "formatDuration mismatch")
	}
}

func TestTimingCollectorEmptyReport(t *testing.T) {
	collector := NewTimingCollector()

	var buf bytes.Buffer
	collector.Report(&buf)

	// Should produce no output when no timers have been started
	assert.Equal(t, 0, buf.Len(), "Empty collector should produce no output")
}

func TestWithRootTimerDoesNotOverwriteCollector(t *testing.T) {
	// Regression test for bug where WithRootTimer would overwrite the collector
	// in context because both context keys were equal (empty struct instances).
	collector := NewTimingCollector()
	ctx := WithCollector(context.Background(), collector)

	// Verify collector is retrievable
	retrieved := FromContext(ctx)
	retrievedTiming, ok := retrieved.(*TimingCollector)
	assert.True(t, ok && retrievedTiming == collector, "Collector should be retrievable after WithCollector")

	// Add a root timer to the context
	rootTimer := collector.Start("root")
	ctx = WithRootTimer(ctx, rootTimer)

	// Verify collector is STILL retrievable after WithRootTimer
	retrieved = FromContext(ctx)
	retrievedTiming, ok = retrieved.(*TimingCollector)
	assert.True(t, ok && retrievedTiming == collector, "Collector should still be retrievable after WithRootTimer")

	// Verify root timer is also retrievable
	retrievedTimer := RootTimerFromContext(ctx)
	assert.True(t, retrievedTimer != nil, "Root timer should be retrievable")

	rootTimer.End()
}

func TestCollectorStartWithRootTimer(t *testing.T) {
	// Test that demonstrates the bug fix: parser timers should nest under loader.parse
	// when using both Child() and Start() with the same collector.
	collector := NewTimingCollector()
	ctx := WithCollector(context.Background(), collector)

	// Simulate check command creating root timer
	checkTimer := collector.Start("check")
	ctx = WithRootTimer(ctx, checkTimer)

	// Simulate loader creating timers via Child()
	rootTimer := RootTimerFromContext(ctx)
	loadTimer := rootTimer.Child("loader.load")
	parseTimer := loadTimer.Child("loader.parse")

	// Simulate parser creating timers via Start() (using collector from context)
	parserCollector := FromContext(ctx)
	lexTimer := parserCollector.Start("parser.lexing")
	lexTimer.End()

	parsingTimer := parserCollector.Start("parser.parsing")
	parsingTimer.End()

	parseTimer.End()
	loadTimer.End()
	checkTimer.End()

	// Verify the hierarchy is correct
	var buf bytes.Buffer
	collector.Report(&buf)
	output := buf.String()

	// Parser timers should be nested under loader.parse
	assert.True(t, strings.Contains(output, "check"), "Output should contain 'check'")
	assert.True(t, strings.Contains(output, "loader.load"), "Output should contain 'loader.load'")
	assert.True(t, strings.Contains(output, "loader.parse"), "Output should contain 'loader.parse'")
	assert.True(t, strings.Contains(output, "parser.lexing"), "Output should contain 'parser.lexing'")
	assert.True(t, strings.Contains(output, "parser.parsing"), "Output should contain 'parser.parsing'")

	// Verify parser timers appear after loader.parse (indicating they're nested)
	lines := strings.Split(output, "\n")
	foundParse := false
	foundLexing := false
	for _, line := range lines {
		if strings.Contains(line, "loader.parse") {
			foundParse = true
		}
		if foundParse && strings.Contains(line, "parser.lexing") {
			foundLexing = true
			// parser.lexing should be indented more than loader.parse
			// (it has more leading spaces before the tree character)
			assert.True(t, strings.Contains(line, "   ") || strings.Contains(line, "│  "), "parser.lexing should be indented under loader.parse")
		}
	}
	assert.True(t, foundParse, "Should find loader.parse in output")
	assert.True(t, foundLexing, "Should find parser.lexing after loader.parse in output")
}
