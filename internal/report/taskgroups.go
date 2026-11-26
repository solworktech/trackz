package report

import (
	"context"

	"github.com/solworktech/trackz/internal/store"
)

// UngroupedLabel is the Task Groups entry for the windows in no group.
const UngroupedLabel = "Ungrouped"

// groupFunc returns a function giving a record's task group ("" for none)
// and whether the record counts in the Task Groups view at all: screen
// lock (sleep) does not, nor does a status that is in no group (a break,
// not ungrouped work). title normalises window titles.
func (s *Service) groupFunc(title func(string) string) func(store.Record) (string, bool) {
	s.mu.RLock()
	m := s.groups
	s.mu.RUnlock()
	return func(rec store.Record) (string, bool) {
		if rec.AppName == store.SleepAppName {
			return "", false
		}
		g := m.Group(rec.ProcessName, title(rec.WindowName))
		if g == "" && rec.ProcessName == store.StatusProcessName {
			return "", false
		}
		return g, true
	}
}

// timePerGroup is the Task Groups view: the time of each task group, most
// first, then Ungrouped.
func (s *Service) timePerGroup(ctx context.Context, r Range, f Filter) (Series, error) {
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return Series{}, err
	}
	group := s.groupFunc(s.titleFunc())
	byGroup := map[string]*Point{}
	var order []string
	for _, rec := range recs {
		g, counted := group(rec)
		if !counted {
			continue
		}
		p := byGroup[g]
		if p == nil {
			p = &Point{Label: g, Item: Item{Group: g}}
			if g == "" {
				p.Label, p.Item = UngroupedLabel, Item{Ungrouped: true}
			}
			byGroup[g] = p
			order = append(order, g)
		}
		p.Value += float64(rec.Duration())
		p.span(rec)
	}
	var pts []Point
	for _, g := range order {
		if g != "" {
			pts = append(pts, *byGroup[g])
		}
	}
	pts = topN(pts, 0)
	if p := byGroup[""]; p != nil {
		pts = append(pts, *p) // last, whatever its size
	}
	s.setSpans(r, pts)
	return Series{Title: "Time per task group", Unit: Seconds, Points: pts}, nil
}

// GroupWindows returns the windows (process and title, as on the
// Dashboard) of task group it (Item.Group, or Item.Ungrouped for the
// windows in none) in r, the top Rows by time as on the Dashboard.
func (s *Service) GroupWindows(ctx context.Context, it Item, r Range, f Filter) (*Dashboard, error) {
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return nil, err
	}
	group := s.groupFunc(s.titleFunc())
	kept := recs[:0]
	for _, rec := range recs {
		if g, counted := group(rec); counted && g == it.Group {
			kept = append(kept, rec)
		}
	}
	d := s.dashboard(r, kept, s.Config().RowLimit)
	d.Series.Title = "Windows in " + it.Group
	if it.Ungrouped {
		d.Series.Title = "Windows in no task group"
	}
	return d, nil
}
