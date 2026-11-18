package report

import (
	"fmt"
	"time"
)

// RangeKind selects a preset or custom time range.
type RangeKind int

const (
	Today RangeKind = iota
	Last7Days
	Last30Days
	Last90Days
	Custom
)

// Presets lists the preset kinds in UI order.
var Presets = []RangeKind{Today, Last7Days, Last30Days, Last90Days}

func (k RangeKind) String() string {
	switch k {
	case Today:
		return "Today"
	case Last7Days:
		return "Last 7 days"
	case Last30Days:
		return "Last 30 days"
	case Last90Days:
		return "Last 90 days"
	case Custom:
		return "Custom"
	}
	return "?"
}

// Range is a time range. Presets are relative to "now" and are resolved
// each time a report runs, so "Today" never goes stale. Custom ranges cover
// whole days, FromDay 00:00:00 to ToDay 23:59:59, in the display time zone.
type Range struct {
	Kind           RangeKind
	FromDay, ToDay Date // Custom only
}

// Date is a calendar day without a time zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar day of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// In returns midnight at the start of the day in loc.
func (d Date) In(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// AddDays returns the date n days later (or earlier for negative n).
func (d Date) AddDays(n int) Date {
	return DateOf(time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC))
}

// Before reports whether d is before o.
func (d Date) Before(o Date) bool {
	return d.In(time.UTC).Before(o.In(time.UTC))
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// Preset returns a preset range.
func Preset(k RangeKind) Range { return Range{Kind: k} }

// CustomRange returns a range of whole days; from must not be after to.
func CustomRange(from, to Date) (Range, error) {
	if to.Before(from) {
		return Range{}, fmt.Errorf("start date %s is after end date %s", from, to)
	}
	return Range{Kind: Custom, FromDay: from, ToDay: to}, nil
}

// Bounds resolves the range to instants, given the current time and the
// display time zone.
//
//	Today:        local midnight .. now
//	Last N days:  now - N*24h .. now (as in the original viewer)
//	Custom:       FromDay 00:00:00 .. ToDay 23:59:59
func (r Range) Bounds(now time.Time, loc *time.Location) (from, to time.Time) {
	now = now.In(loc)
	switch r.Kind {
	case Today:
		return DateOf(now).In(loc), now
	case Last7Days:
		return now.AddDate(0, 0, -7), now
	case Last30Days:
		return now.AddDate(0, 0, -30), now
	case Last90Days:
		return now.AddDate(0, 0, -90), now
	default:
		return r.FromDay.In(loc), r.ToDay.AddDays(1).In(loc).Add(-time.Second)
	}
}
