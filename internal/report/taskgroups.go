package report

import (
	"context"
	"fmt"
	"slices"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/groups"
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

// GroupPreview is what a task group being edited takes in a range.
type GroupPreview struct {
	// Windows are the windows (process and title) the group takes, most
	// time first.
	Windows []Point
	// Missed are windows one of its include patterns or processes matches
	// but that are not in it; Detail says why ("excluded", "in <group>").
	Missed []Point
}

// previewLimit is the most windows a GroupPreview lists of each kind.
const previewLimit = 50

// PreviewGroup returns what the group named name takes in r (records that
// pass f) with the task groups draft, which need not be saved: for editing
// groups.
func (s *Service) PreviewGroup(ctx context.Context, draft []groups.Group, name string, r Range, f Filter) (GroupPreview, error) {
	m, err := groups.New(draft)
	if err != nil {
		return GroupPreview{}, err
	}
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return GroupPreview{}, err
	}
	title := s.titleFunc()
	type key struct{ proc, title string }
	var in []store.Record
	missed := map[key]*Point{}
	var order []key
	for _, rec := range recs {
		if rec.AppName == store.SleepAppName {
			continue
		}
		t := title(rec.WindowName)
		g := m.Group(rec.ProcessName, t)
		switch {
		case g == name:
			in = append(in, rec)
			continue
		case !m.Candidate(name, rec.ProcessName, t):
			continue
		}
		k := key{rec.ProcessName, t}
		p := missed[k]
		if p == nil {
			p = &Point{Label: t, Process: rec.ProcessName, App: rec.AppName, Detail: "excluded"}
			if g != "" {
				p.Detail = "in " + g
			}
			missed[k] = p
			order = append(order, k)
		}
		p.Value += float64(rec.Duration())
	}
	var prev GroupPreview
	// All of them, short ones too: this is for seeing what the group takes.
	prev.Windows = topN(s.windowTotals(in), previewLimit)
	s.setSpans(r, prev.Windows)
	for _, k := range order {
		prev.Missed = append(prev.Missed, *missed[k])
	}
	prev.Missed = topN(prev.Missed, previewLimit)
	return prev, nil
}

// Processes returns the processes with focus time in r (ignoring any
// filter), most used first: Label and Process are the process_name, App
// its application, Value the seconds.
func (s *Service) Processes(ctx context.Context, r Range) ([]Point, error) {
	recs, err := s.records(ctx, r, Filter{})
	if err != nil {
		return nil, err
	}
	byProc := map[string]*Point{}
	var order []string
	for _, rec := range recs {
		if rec.AppName == store.SleepAppName || rec.ProcessName == store.StatusProcessName {
			continue
		}
		p := byProc[rec.ProcessName]
		if p == nil {
			p = &Point{Label: rec.ProcessName, Process: rec.ProcessName, App: rec.AppName}
			byProc[rec.ProcessName] = p
			order = append(order, rec.ProcessName)
		}
		p.Value += float64(rec.Duration())
	}
	pts := make([]Point, 0, len(order))
	for _, k := range order {
		pts = append(pts, *byProc[k])
	}
	return topN(pts, 0), nil
}

// WindowGroup returns the task group of a window (process and title as
// reports show it, as in Item) and why it is in it.
func (s *Service) WindowGroup(process, title string) (string, groups.How) {
	s.mu.RLock()
	m := s.groups
	s.mu.RUnlock()
	return m.Explain(process, title)
}

// GroupNames returns the names of the configured task groups, in order.
func (s *Service) GroupNames() []string {
	gs := s.Config().TaskGroups
	names := make([]string, len(gs))
	for i, g := range gs {
		names[i] = g.Name
	}
	return names
}

// AssignWindow assigns a window (process and title as reports show it) by
// hand to the task group named group, or with "" to none, and saves the
// task groups (to the config file as it is now).
func (s *Service) AssignWindow(process, title, group string) error {
	_, err := s.ModifyConfig(func(c config.Config) (config.Config, error) {
		if group != "" && !slices.ContainsFunc(c.TaskGroups, func(g groups.Group) bool { return g.Name == group }) {
			return c, fmt.Errorf("no task group %q", group)
		}
		c.TaskGroups = groups.Assign(c.TaskGroups, groups.Window{Process: process, Title: title}, group)
		return c, nil
	})
	return err
}
