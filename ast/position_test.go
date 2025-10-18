package ast

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestSpan_Text(t *testing.T) {
	source := []byte("hello world")

	t.Run("Valid span", func(t *testing.T) {
		span := Span{Start: 0, End: 5}
		result := span.Text(source)
		assert.Equal(t, "hello", result)
	})

	t.Run("Valid span in middle", func(t *testing.T) {
		span := Span{Start: 6, End: 11}
		result := span.Text(source)
		assert.Equal(t, "world", result)
	})

	t.Run("Zero span", func(t *testing.T) {
		span := Span{Start: 0, End: 0}
		result := span.Text(source)
		assert.Equal(t, "", result, "zero span should return empty string")
	})

	t.Run("Negative start", func(t *testing.T) {
		span := Span{Start: -5, End: 3}
		result := span.Text(source)
		assert.Equal(t, "", result, "negative start should return empty string")
	})

	t.Run("Start greater than End", func(t *testing.T) {
		span := Span{Start: 10, End: 5}
		result := span.Text(source)
		assert.Equal(t, "", result, "start > end should return empty string")
	})

	t.Run("End beyond source length", func(t *testing.T) {
		span := Span{Start: 0, End: 100}
		result := span.Text(source)
		assert.Equal(t, "", result, "end > len(source) should return empty string")
	})

	t.Run("Start beyond source length", func(t *testing.T) {
		span := Span{Start: 100, End: 105}
		result := span.Text(source)
		assert.Equal(t, "", result, "start > len(source) should return empty string")
	})

	t.Run("Empty source", func(t *testing.T) {
		emptySource := []byte("")
		span := Span{Start: 0, End: 5}
		result := span.Text(emptySource)
		assert.Equal(t, "", result, "should handle empty source gracefully")
	})
}

func TestSpan_IsZero(t *testing.T) {
	t.Run("Zero span", func(t *testing.T) {
		span := Span{Start: 0, End: 0}
		assert.True(t, span.IsZero(), "zero span should be zero")
	})

	t.Run("Non-zero span", func(t *testing.T) {
		span := Span{Start: 0, End: 5}
		assert.True(t, !span.IsZero(), "non-zero span should not be zero")
	})

	t.Run("Negative values are not zero", func(t *testing.T) {
		span := Span{Start: -1, End: -1}
		assert.True(t, !span.IsZero(), "negative values should not be considered zero")
	})
}
