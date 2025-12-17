package ast

import (
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestDate_String(t *testing.T) {
	t.Run("ValidDate", func(t *testing.T) {
		date, err := NewDate("2024-12-17")
		assert.NoError(t, err)
		assert.Equal(t, "2024-12-17", date.String())
	})

	t.Run("NilDate", func(t *testing.T) {
		var date *Date
		assert.Equal(t, "", date.String())
	})

	t.Run("ZeroDate", func(t *testing.T) {
		date := &Date{}
		assert.Equal(t, "", date.String())
	})

	t.Run("RoundTrip", func(t *testing.T) {
		original := "2024-12-17"
		date, err := NewDate(original)
		assert.NoError(t, err)
		assert.Equal(t, original, date.String())

		// Parse the string back and verify it matches
		date2, err := NewDate(date.String())
		assert.NoError(t, err)
		assert.Equal(t, date.Year(), date2.Year())
		assert.Equal(t, date.Month(), date2.Month())
		assert.Equal(t, date.Day(), date2.Day())
	})

	t.Run("LeapYearDate", func(t *testing.T) {
		date, err := NewDate("2024-02-29")
		assert.NoError(t, err)
		assert.Equal(t, "2024-02-29", date.String())
	})

	t.Run("YearBoundary", func(t *testing.T) {
		date, err := NewDate("2023-12-31")
		assert.NoError(t, err)
		assert.Equal(t, "2023-12-31", date.String())

		date, err = NewDate("2024-01-01")
		assert.NoError(t, err)
		assert.Equal(t, "2024-01-01", date.String())
	})
}
