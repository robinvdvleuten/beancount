package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
	"github.com/robinvdvleuten/beancount/query/bql"
)

// intervalValue is a relative time interval, dateutil's relativedelta as
// beanquery's interval() builds it: years, months and days, with months
// beyond a year carried into years.
type intervalValue struct {
	Years, Months, Days int64
}

// newInterval builds an interval like relativedelta's constructor, which
// carries months beyond eleven, either way, into years: Go's division
// truncates toward zero and the remainder takes the months' sign, as
// relativedelta's does.
func newInterval(years, months, days int64) *intervalValue {
	return &intervalValue{Years: years + months/12, Months: months % 12, Days: days}
}

// String renders the interval like relativedelta's repr:
// relativedelta(years=+1, months=+2), the fields that are not zero, each
// with its sign.
func (i *intervalValue) String() string {
	var fields []string
	for _, field := range []struct {
		name  string
		value int64
	}{{"years", i.Years}, {"months", i.Months}, {"days", i.Days}} {
		if field.value != 0 {
			fields = append(fields, fmt.Sprintf("%s=%+d", field.name, field.value))
		}
	}
	return "relativedelta(" + strings.Join(fields, ", ") + ")"
}

func (i *intervalValue) neg() *intervalValue {
	return &intervalValue{Years: -i.Years, Months: -i.Months, Days: -i.Days}
}

func (i *intervalValue) add(other *intervalValue) *intervalValue {
	return newInterval(addInt(i.Years, other.Years), addInt(i.Months, other.Months), addInt(i.Days, other.Days))
}

// addTo moves d by the interval like relativedelta's addition to a date:
// years and months first, the day clipped to the end of the month they
// reach, then the days.
func (i *intervalValue) addTo(d *ast.Date) *ast.Date {
	year := int64(d.Year()) + i.Years
	month := int64(d.Month()) + i.Months
	if month > 12 {
		year++
		month -= 12
	} else if month < 1 {
		year--
		month += 12
	}
	if year < 1 || year > 9999 {
		fail("year %d is out of range", year)
	}
	day := min(d.Day(), daysIn(int(year), time.Month(month)))
	moved := time.Date(int(year), time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return addDays(&ast.Date{Time: moved}, i.Days)
}

// daysIn is the number of days in a month.
func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// intervalPattern is what beanquery's interval() reads, matched whole: a
// signed number, white space and a unit.
var intervalPattern = regexp.MustCompile(`^([-+]?[0-9]+)[\s\x1c-\x1f\x85\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]+(day|month|year)s?$`)

// parseInterval is beanquery's interval(): "3 days", "-1 month", "2 years",
// NULL for anything else.
func parseInterval(s string) any {
	match := intervalPattern.FindStringSubmatch(s)
	if match == nil {
		return nil
	}
	number, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		failIntegerOverflow()
	}
	switch match[2] {
	case "day":
		return newInterval(0, 0, number)
	case "month":
		return newInterval(0, number, 0)
	default:
		return newInterval(number, 0, 0)
	}
}

// dateBin is beanquery's date_bin(): the start of the bin of stride, laid
// out from origin, that source falls in. Like beanquery, a stride of months
// or years steps from origin one stride at a time, the day clipped at each
// step, and a stride that does not move forward gives NULL.
func dateBin(stride *intervalValue, source, origin *ast.Date) any {
	if stride.Months != 0 || stride.Years != 0 {
		if !stride.addTo(origin).After(origin.Time) {
			return nil
		}
		if !source.Before(origin.Time) {
			d := origin
			for {
				n := stride.addTo(d)
				if !n.Before(source.Time) {
					return d
				}
				d = n
			}
		}
		back := stride.neg()
		n := origin
		for {
			n = back.addTo(n)
			if !n.After(source.Time) {
				return n
			}
		}
	}
	if stride.Days < 0 {
		return nil
	}
	if stride.Days == 0 {
		// Python's float modulo by zero.
		fail("float modulo")
	}
	diff := daysBetween(origin, source)
	modulo := diff % stride.Days
	if modulo < 0 {
		modulo += stride.Days
	}
	return addDays(origin, diff-modulo)
}

// daysBetween counts the days from a to b.
func daysBetween(a, b *ast.Date) int64 {
	return (b.Unix() - a.Unix()) / 86400
}

// dateTrunc is beanquery's date_trunc(): d truncated to the start of its
// week (a Monday), month, quarter, year, decade, century or millennium, and
// NULL for any other field.
func dateTrunc(field string, d *ast.Date) any {
	year, month := d.Year(), d.Month()
	first := func(year int, month time.Month) any {
		return &ast.Date{Time: time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)}
	}
	switch field {
	case "week":
		return addDays(d, -int64((d.Weekday()+6)%7))
	case "month":
		return first(year, month)
	case "quarter":
		return first(year, month-(month-1)%3)
	case "year":
		return first(year, time.January)
	case "decade":
		return first(year-year%10, time.January)
	case "century":
		return first(year-(year-1)%100, time.January)
	case "millennium":
		return first(year-(year-1)%1000, time.January)
	}
	return nil
}

// datePart is beanquery's date_part(): one field of d, and NULL for a field
// it does not know.
func datePart(field string, d *ast.Date) any {
	year := int64(d.Year())
	isoYear, isoWeek := d.ISOWeek()
	switch field {
	case "weekday", "dow":
		return int64((d.Weekday() + 6) % 7)
	case "isoweekday", "isodow":
		return int64((d.Weekday()+6)%7 + 1)
	case "week":
		return int64(isoWeek)
	case "month":
		return int64(d.Month())
	case "quarter":
		return int64((d.Month()-1)/3 + 1)
	case "year":
		return year
	case "isoyear":
		return int64(isoYear)
	case "decade":
		return year / 10
	case "century":
		return (year-1)/100 + 1
	case "millennium":
		return (year-1)/1000 + 1
	case "epoch":
		return d.Unix()
	}
	return nil
}

// strptimeDirectives are the regular expressions Python's _strptime gives
// the directives a date takes its fields from, and the ones it reads and
// drops.
var strptimeDirectives = map[byte]string{
	'd': `(?P<d>3[01]|[12]\d|0[1-9]|[1-9]| [1-9])`,
	'm': `(?P<m>1[0-2]|0[1-9]|[1-9])`,
	'Y': `(?P<Y>\d\d\d\d)`,
	'y': `(?P<y>\d\d)`,
	'j': `(?P<j>36[0-6]|3[0-5]\d|[12]\d\d|0[1-9]\d|00[1-9]|[1-9]\d|0[1-9]|[1-9])`,
	'b': `(?P<b>jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)`,
	'B': `(?P<B>january|february|march|april|may|june|july|august|september|october|november|december)`,
	'a': `(?:mon|tue|wed|thu|fri|sat|sun)`,
	'A': `(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday)`,
	'H': `(?:2[0-3]|[0-1]\d|\d)`,
	'I': `(?:1[0-2]|0[1-9]|[1-9])`,
	'M': `(?:[0-5]\d|\d)`,
	'S': `(?:6[0-1]|[0-5]\d|\d)`,
	'f': `(?:[0-9]{1,6})`,
	'p': `(?:am|pm)`,
	'%': `%`,
}

var monthNames = []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}

// strptime is Python's datetime.strptime(s, format).date() for the
// directives a date needs: %d, %m, %Y, %y, %j, %b and %B, and %a, %A, %H,
// %I, %M, %S, %f and %p, which it reads and drops. Like Python, it matches
// case-insensitively, white space in the format matches any white space,
// and the whole string must match. A string that does not match, or names
// no valid date, fails the statement with Python's message.
func strptime(s, format string) *ast.Date {
	var pattern strings.Builder
	pattern.WriteString(`(?i)^`)
	for i := 0; i < len(format); i++ {
		c := format[i]
		switch {
		case c == '%' && i+1 < len(format):
			i++
			directive, ok := strptimeDirectives[format[i]]
			if !ok {
				fail("'%c' is a bad directive in format %s", format[i], pyrepr.String(format))
			}
			pattern.WriteString(directive)
		case c == '%':
			fail("stray %% in format %s", pyrepr.String(format))
		case bql.IsSpace(rune(c)):
			pattern.WriteString(`\s+`)
			for i+1 < len(format) && bql.IsSpace(rune(format[i+1])) {
				i++
			}
		default:
			pattern.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re, err := regexp.Compile(pattern.String())
	if err != nil {
		fail("%s", err)
	}
	loc := re.FindStringSubmatchIndex(s)
	if loc == nil {
		fail("time data %s does not match format %s", pyrepr.String(s), pyrepr.String(format))
	}
	if loc[1] != len(s) {
		fail("unconverted data remains: %s", s[loc[1]:])
	}
	fields := map[string]string{}
	for i, name := range re.SubexpNames() {
		if name != "" && loc[2*i] >= 0 {
			fields[name] = s[loc[2*i]:loc[2*i+1]]
		}
	}
	number := func(name string) (int, bool) {
		value, ok := fields[name]
		if !ok {
			return 0, false
		}
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n, true
	}
	year, month, day := 1900, 1, 1
	if y, ok := number("Y"); ok {
		year = y
	} else if y, ok := number("y"); ok {
		year = 2000 + y
		if y >= 69 {
			year = 1900 + y
		}
	}
	if m, ok := number("m"); ok {
		month = m
	}
	for _, name := range []string{"b", "B"} {
		if value, ok := fields[name]; ok {
			for i, full := range monthNames {
				if strings.HasPrefix(full, strings.ToLower(value)) {
					month = i + 1
					break
				}
			}
		}
	}
	if d, ok := number("d"); ok {
		day = d
	}
	if year < 1 {
		fail("year %d is out of range", year)
	}
	// Like Python, a day of the year decides the date alone.
	if j, ok := number("j"); ok {
		return &ast.Date{Time: time.Date(year, time.January, j, 0, 0, 0, 0, time.UTC)}
	}
	if day > daysIn(year, time.Month(month)) {
		fail("day is out of range for month")
	}
	return &ast.Date{Time: time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)}
}

// parseDateFormats are the layouts parseDate reads without a format: the
// common spellings of a date dateutil's parser reads, which it is not a
// port of (KNOWN_GAPS.md).
var parseDateFormats = []string{
	"%Y-%m-%d", "%Y/%m/%d", "%Y.%m.%d", "%Y%m%d",
	"%m/%d/%Y", "%m-%d-%Y", "%d.%m.%Y",
	"%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M:%S", "%Y-%m-%dT%H:%M", "%Y-%m-%d %H:%M",
	"%B %d, %Y", "%b %d, %Y", "%B %d %Y", "%b %d %Y",
	"%d %B %Y", "%d %b %Y", "%Y %B %d", "%Y %b %d",
	"%a, %d %b %Y", "%A, %B %d, %Y",
}

// parseDate is beanquery's parse_date() without a format, which reads the
// string with dateutil's parser: here, the first of parseDateFormats that
// reads it whole. Anything else fails the statement with dateutil's
// message.
func parseDate(s string) *ast.Date {
	for _, format := range parseDateFormats {
		if date, ok := tryStrptime(strings.TrimSpace(s), format); ok {
			return date
		}
	}
	fail("Unknown string format: %s", s)
	return nil
}

// tryStrptime is strptime, reporting a string it cannot read instead of
// failing.
func tryStrptime(s, format string) (date *ast.Date, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, failed := r.(evalError); !failed {
				panic(r)
			}
			date, ok = nil, false
		}
	}()
	return strptime(s, format), true
}
