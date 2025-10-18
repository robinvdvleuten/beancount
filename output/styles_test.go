package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewStyles(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	if styles == nil {
		t.Fatal("NewStyles should return non-nil Styles")
	}

	if styles.output == nil {
		t.Error("Styles should have non-nil output")
	}
}

func TestStylesSuccess(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Success("test message")

	// Should contain the message
	if !strings.Contains(result, "test") {
		t.Errorf("Success() result should contain message, got: %s", result)
	}
}

func TestStylesError(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Error("error message")

	// Should contain the message
	if !strings.Contains(result, "error") {
		t.Errorf("Error() result should contain message, got: %s", result)
	}
}

func TestStylesFilePath(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.FilePath("/path/to/file.txt")

	// Should contain the path
	if !strings.Contains(result, "/path/to/file.txt") {
		t.Errorf("FilePath() result should contain path, got: %s", result)
	}
}

func TestStylesAccount(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Account("Assets:Checking")

	// Should contain the account name
	if !strings.Contains(result, "Assets:Checking") {
		t.Errorf("Account() result should contain account, got: %s", result)
	}
}

func TestStylesAmount(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Amount("100.50 USD")

	// Should contain the amount
	if !strings.Contains(result, "100.50") {
		t.Errorf("Amount() result should contain amount, got: %s", result)
	}
}

func TestStylesKeyword(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Keyword("balance")

	// Should contain the keyword
	if !strings.Contains(result, "balance") {
		t.Errorf("Keyword() result should contain keyword, got: %s", result)
	}
}

func TestStylesDim(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Dim("dimmed text")

	// Should contain the text
	if !strings.Contains(result, "dimmed text") {
		t.Errorf("Dim() result should contain text, got: %s", result)
	}
}

func TestStylesWarning(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	result := styles.Warning("warning message")

	// Should contain the message
	if !strings.Contains(result, "warning") {
		t.Errorf("Warning() result should contain message, got: %s", result)
	}
}

func TestStylesTiming(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	t.Run("FastOperation", func(t *testing.T) {
		result := styles.Timing("5ms", false)

		// Should contain the timing
		if !strings.Contains(result, "5ms") {
			t.Errorf("Timing() result should contain timing, got: %s", result)
		}
	})

	t.Run("SlowOperation", func(t *testing.T) {
		result := styles.Timing("500ms", true)

		// Should contain the timing
		if !strings.Contains(result, "500ms") {
			t.Errorf("Timing() result should contain timing, got: %s", result)
		}
	})
}

func TestStylesOutput(t *testing.T) {
	var buf bytes.Buffer
	styles := NewStyles(&buf)

	output := styles.Output()

	if output == nil {
		t.Error("Output() should return non-nil termenv.Output")
	}
}
