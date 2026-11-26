package report

import (
	"context"
	"fmt"
	"time"

	"github.com/solworktech/trackz/internal/store"
	"github.com/solworktech/trackz/internal/timefmt"
)

// Item identifies what a point of the Dashboard or of an application view
// stands for, so its timeline can be asked for (Service.Timeline). The
// zero Item stands for nothing: points that have no timeline.
type Item struct {
	// Dashboard: a process and a title (as reports show it, so all the
	// titles merged into it).
	Process, Title string
	// Top Apps, Time Spent: an application (Process and Title empty).
	App string
	// Task Groups: a group, or (Ungrouped) the windows in none.
	Group     string
	Ungrouped bool
}

// IsZero reports whether it stands for nothing.
func (it Item) IsZero() bool { return it == Item{} }

// match reports whether rec belongs to it; title normalises window titles,
// group returns a record's task group (see Service.groupOf).
func (it Item) match(rec store.Record, title func(string) string, group func(store.Record) (string, bool)) bool {
	switch {
	case it.Group != "" || it.Ungrouped:
		g, counted := group(rec)
		return counted && g == it.Group
	case it.Process != "" || it.Title != "":
		return rec.ProcessName == it.Process && title(rec.WindowName) == it.Title
	}
	return rec.AppName == it.App
}

// Segment is the length of a timeline's bars.
type Segment int

const (
	Hourly Segment = iota // ranges of one day
	Daily                 // up to a month
	Weekly                // longer: weeks starting on Monday
)

// String is the segment as in "per hour": hour, day or week.
func (g Segment) String() string {
	switch g {
	case Hourly:
		return "hour"
	case Daily:
		return "day"
	}
	return "week"
}

// maxDailyDays is the longest range shown per day; longer ones per week.
const maxDailyDays = 31

// SegmentFor returns the timeline segment for r: hours for a single day
// (Today, a custom range of one day), days up to a month, weeks beyond.
func (s *Service) SegmentFor(r Range) Segment {
	switch {
	case r.singleDay():
		return Hourly
	case r.days(s.now(), s.Location()) <= maxDailyDays:
		return Daily
	}
	return Weekly
}

// singleDay reports whether r is one calendar day.
func (r Range) singleDay() bool {
	return r.Kind == Today || (r.Kind == Custom && r.FromDay == r.ToDay)
}

// days is the length of r in days (rounded up).
func (r Range) days(now time.Time, loc *time.Location) int {
	from, to := r.Bounds(now, loc)
	return int((to.Sub(from) + 24*time.Hour - time.Second) / (24 * time.Hour))
}

// Span describes when the item of a point was first and last in focus in
// r ("First 08:12:03 · last 17:40:55"), with the dates when r is longer
// than a day. Empty when unknown.
func (s *Service) Span(r Range, first, last int64) string {
	if first == 0 || last == 0 {
		return ""
	}
	cfg := s.Config()
	loc := cfg.Location()
	f := func(t int64) string {
		tm := time.Unix(t, 0).In(loc)
		if r.singleDay() {
			return timefmt.Format(tm, cfg.TimeFormat)
		}
		return cfg.FormatDateTime(tm)
	}
	return "First " + f(first) + " · last " + f(last)
}

// setSpans fills in each point's Span from its First and Last.
func (s *Service) setSpans(r Range, pts []Point) {
	for i := range pts {
		pts[i].Span = s.Span(r, pts[i].First, pts[i].Last)
	}
}

// Timeline returns the focus time of it in r (records that pass f) per
// segment (SegmentFor): a point for every hour, day or week of the range,
// those without focus time included (with Value 0). A record that spans
// segments counts in each for the part that falls in it; the parts before
// or after r are left out. Detail holds the number of focus periods in the
// segment.
func (s *Service) Timeline(ctx context.Context, it Item, r Range, f Filter) (Series, error) {
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return Series{}, err
	}
	title := s.titleFunc()
	group := s.groupFunc(title)
	cfg := s.Config()
	loc := cfg.Location()
	from, to := r.Bounds(s.now(), loc)
	seg := s.SegmentFor(r)

	start := func(t time.Time) time.Time {
		t = t.In(loc)
		switch seg {
		case Hourly:
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		case Daily:
			return DateOf(t).In(loc)
		}
		d := DateOf(t)
		back := (int(t.Weekday()) + 6) % 7 // days since Monday
		return d.AddDays(-back).In(loc)
	}
	next := func(t time.Time) time.Time {
		switch seg {
		case Hourly:
			return t.Add(time.Hour) // an hour also across a DST change
		case Daily:
			return DateOf(t).AddDays(1).In(loc)
		}
		return DateOf(t).AddDays(7).In(loc)
	}
	label := func(t time.Time) string {
		if seg == Hourly {
			return timefmt.Format(t, "%H:00")
		}
		return cfg.FormatDate(t)
	}

	var bounds []int64 // segment i is [bounds[i], bounds[i+1])
	var pts []Point
	for b := start(from); b.Before(to); b = next(b) {
		bounds = append(bounds, b.Unix())
		pts = append(pts, Point{Label: label(b)})
	}
	if len(pts) == 0 {
		return Series{}, nil
	}
	bounds = append(bounds, next(time.Unix(bounds[len(bounds)-1], 0)).Unix())
	counts := make([]int, len(pts))
	lo, hi := from.Unix(), to.Unix()
	for _, rec := range recs {
		if rec.End == 0 || !it.match(rec, title, group) {
			continue
		}
		a, b := max(rec.Start, lo), min(rec.End, hi)
		for i := range pts {
			sa, sb := max(a, bounds[i]), min(b, bounds[i+1])
			if sa < sb {
				pts[i].Value += float64(sb - sa)
				counts[i]++
			}
		}
	}
	for i := range pts {
		switch counts[i] {
		case 0:
		case 1:
			pts[i].Detail = "1 focus period"
		default:
			pts[i].Detail = fmt.Sprintf("%d focus periods", counts[i])
		}
	}
	t := "Focus time per " + seg.String()
	if seg == Weekly {
		t += " (weeks from Monday)"
	}
	return Series{Title: t, Unit: Seconds, Points: pts}, nil
}

// WithoutEmpty returns s without its points of value 0.
func (s Series) WithoutEmpty() Series {
	out := s
	out.Points = nil
	for _, p := range s.Points {
		if p.Value > 0 {
			out.Points = append(out.Points, p)
		}
	}
	return out
}
