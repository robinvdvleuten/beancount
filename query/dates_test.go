package query

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/robinvdvleuten/beancount/ast"
)

func mustDate(t *testing.T, s string) *ast.Date {
	t.Helper()
	date := &ast.Date{}
	assert.NoError(t, date.Capture([]string{s}))
	return date
}

// TestNewInterval pins relativedelta's carry of months into years.
func TestNewInterval(t *testing.T) {
	assert.Equal(t, "relativedelta(years=+1, months=+2)", newInterval(0, 14, 0).String())
	assert.Equal(t, "relativedelta(years=-1, months=-1)", newInterval(0, -13, 0).String())
	assert.Equal(t, "relativedelta(months=+11, days=+400)", newInterval(0, 11, 400).String())
	assert.Equal(t, "relativedelta()", newInterval(0, 0, 0).String())
}

// TestDateBinMonths pins beanquery's stepping one stride at a time, which
// clips the day at each step: from January 31, two months on is March 28.
func TestDateBinMonths(t *testing.T) {
	month := newInterval(0, 1, 0)
	origin := mustDate(t, "2023-01-31")
	assert.Equal(t, "2023-03-28", dateBin(month, mustDate(t, "2023-04-01"), origin).(*ast.Date).String())
	assert.Equal(t, "2022-11-30", dateBin(month, mustDate(t, "2022-12-15"), origin).(*ast.Date).String())
	assert.Equal(t, nil, dateBin(newInterval(0, -1, 0), origin, origin))
}

// TestStrptime pins Python's strptime for what a date takes, probed with
// Python 3.11.
func TestStrptime(t *testing.T) {
	for _, tt := range []struct{ s, format, want string }{
		{"2024-060", "%Y-%j", "2024-02-29"},
		{"2023-366", "%Y-%j", "2024-01-01"},
		{"05 17", "%m %d", "1900-05-17"},
		{"17 sept 2023", "%d %b %Y", ""},
		{"17 Sep 2023", "%d %b %Y", "2023-09-17"},
		{"68-01-01", "%y-%m-%d", "2068-01-01"},
		{"69-01-01", "%y-%m-%d", "1969-01-01"},
		{"10%", "%d%%", "1900-01-10"},
		{"100%", "%d%%", ""},
	} {
		date, ok := tryStrptime(tt.s, tt.format)
		if tt.want == "" {
			assert.False(t, ok, tt.s)
			continue
		}
		assert.True(t, ok, tt.s)
		assert.Equal(t, tt.want, date.String(), tt.s)
	}
}

func TestRoundInt(t *testing.T) {
	assert.Equal(t, int64(0), roundInt(5, -1))
	assert.Equal(t, int64(20), roundInt(15, -1))
	assert.Equal(t, int64(-20), roundInt(-25, -1))
	assert.Equal(t, int64(0), roundInt(4000000000000000000, -19))
	assert.Equal(t, int64(0), roundInt(-9223372036854775807, -30))
}
