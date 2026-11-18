// Package report is the controller layer shared by all frontends. It turns
// raw trackz records into frontend-neutral data (series for charts, tables,
// labels) and handles time ranges, time zones, custom queries and CSV export.
//
// Nothing here depends on a UI toolkit: the Gio frontend draws Series as
// pie/bar/line charts, a TUI can render the same Series as text bars.
package report

import (
	"strconv"
	"time"

	"github.com/solworktech/trackz/internal/store"
	"github.com/solworktech/trackz/internal/timefmt"
)

// Unit says how to interpret Point values.
type Unit int

const (
	// Seconds values are durations in seconds.
	Seconds Unit = iota
	// Number values are plain numbers.
	Number
)

// Point is one bar, slice or line vertex.
type Point struct {
	Label string  // display label (window title, app name, date, ...)
	Value float64 // in Series.Unit
	// Process is the process name the point mostly relates to, for icon
	// lookup. Empty when not applicable.
	Process string
	// App is the application name, also used for icon lookup.
	App string
	// Detail is extra tooltip text, e.g. "12 window switches".
	Detail string
}

// Series is an ordered list of points.
type Series struct {
	Title  string
	Unit   Unit
	Points []Point
}

// Total sums all values.
func (s Series) Total() float64 {
	var t float64
	for _, p := range s.Points {
		t += p.Value
	}
	return t
}

// Max returns the largest value (0 for an empty series).
func (s Series) Max() float64 {
	var m float64
	for _, p := range s.Points {
		if p.Value > m {
			m = p.Value
		}
	}
	return m
}

// FormatValue renders v according to the series unit: durations as
// HH:MM:SS, numbers compactly.
func (s Series) FormatValue(v float64) string { return FormatValue(s.Unit, v) }

// FormatShort renders v briefly, for labels on bars ("1.5h").
func (s Series) FormatShort(v float64) string {
	if s.Unit == Seconds {
		if s.Max() < 3600 {
			return strconv.FormatInt(int64(v)/60, 10) + "m"
		}
		return timefmt.Hours(int64(v))
	}
	return formatNumber(v)
}

// FormatValue renders v according to unit.
func FormatValue(unit Unit, v float64) string {
	if unit == Seconds {
		return timefmt.Duration(int64(v))
	}
	return formatNumber(v)
}

// Dashboard is the data for the main view: the top activities as a series
// and the same data as a table (also used for CSV export).
type Dashboard struct {
	Series Series
	Table  *store.Table
	Hidden Hidden
}

// ChartKind is the visual form a frontend should use.
type ChartKind int

const (
	BarChart ChartKind = iota
	LineChart
	PieChart
	TimeSeriesChart
	TableView
)

// ChartKinds lists all kinds, in UI order.
var ChartKinds = []ChartKind{BarChart, LineChart, PieChart, TimeSeriesChart, TableView}

func (k ChartKind) String() string {
	switch k {
	case BarChart:
		return "Bar"
	case LineChart:
		return "Line"
	case PieChart:
		return "Pie"
	case TimeSeriesChart:
		return "Time Series"
	case TableView:
		return "Table"
	}
	return "?"
}

// View identifies a predefined report.
type View int

const (
	ViewDashboard View = iota
	ViewTopApps
	ViewTimeSpent
	ViewWindowActivity
	ViewDailyStats
	ViewCustomQuery
	ViewSettings
)

// Views lists the views in UI order.
var Views = []View{ViewDashboard, ViewTopApps, ViewTimeSpent, ViewWindowActivity, ViewDailyStats, ViewCustomQuery, ViewSettings}

func (v View) String() string {
	switch v {
	case ViewDashboard:
		return "Dashboard"
	case ViewTopApps:
		return "Top Apps"
	case ViewTimeSpent:
		return "Time Spent"
	case ViewWindowActivity:
		return "Window Activity"
	case ViewDailyStats:
		return "Daily Stats"
	case ViewCustomQuery:
		return "Custom Query"
	case ViewSettings:
		return "Settings"
	}
	return "?"
}

// Result is what a view produces: a series, a table, or both, plus the
// chart kind the view is meant to be drawn as.
type Result struct {
	View   View
	Kind   ChartKind
	Series Series
	Table  *store.Table
	Range  Range
	// Generated is when the data was computed.
	Generated time.Time
	// Hidden describes entries (windows or applications) left out because
	// their total is below the minimum (config.MinDurationSeconds).
	Hidden Hidden
}

// Hidden summarises entries excluded by the minimum-total filter.
type Hidden struct {
	Count       int64
	Seconds     int64
	MinDuration int
}
