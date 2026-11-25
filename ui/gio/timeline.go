package gio

import (
	"context"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/timefmt"
)

// drill is a view's drill-down: the timeline of one of its points (focus
// time per hour, day or week of the range), shown instead of the view's
// chart until Back is clicked. It follows the range and the filter.
type drill struct {
	point report.Point // what was clicked (label, icon, Item)

	// The timeline, for the inputs in key (guarded by UI.mu).
	key       string
	series    report.Series
	generated time.Time
	rng       report.Range
	err       error
	loading   bool

	back      widget.Clickable
	hideEmpty widget.Bool
	chart     columnChart
}

// openTimeline shows the timeline of p in place of the current view's
// chart.
func (u *UI) openTimeline(p report.Point) {
	if !hasTimeline(p) {
		return
	}
	d := &drill{point: p}
	u.mu.Lock()
	if old := u.drills[u.view]; old != nil {
		d.hideEmpty.Value = old.hideEmpty.Value // keep the toggle's state
	}
	u.drills[u.view] = d
	u.mu.Unlock()
	u.ensureTimeline(true)
	u.invalidate()
}

// currentDrill returns the current view's drill-down, if one is open.
func (u *UI) currentDrill() *drill {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.drills[u.view]
}

// ensureTimeline starts loading the current drill-down's timeline when its
// inputs changed, when it is due for its automatic refresh, or if force.
func (u *UI) ensureTimeline(force bool) {
	d := u.currentDrill()
	if d == nil {
		return
	}
	key := u.resultKey()
	u.mu.Lock()
	fresh := d.key == key && (d.loading || d.err != nil || !u.liveSince(d.generated, d.rng))
	if fresh && !force {
		u.mu.Unlock()
		return
	}
	d.key, d.loading = key, true
	u.mu.Unlock()
	v, rng, filter, item := u.view, u.rng, u.filter, d.point.Item
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		s, err := u.svc.Timeline(ctx, item, rng, filter)
		u.mu.Lock()
		if u.drills[v] == d && d.key == key { // not superseded
			d.series, d.err, d.loading = s, err, false
			d.generated, d.rng = time.Now(), rng
		}
		u.mu.Unlock()
		u.invalidate()
	}()
}

// handleDrill processes the drill-down's Back button and the clicks on the
// view's points that open one.
func (u *UI) handleDrill(gtx C) {
	if d := u.currentDrill(); d != nil && d.back.Clicked(gtx) {
		u.mu.Lock()
		delete(u.drills, u.view)
		u.mu.Unlock()
	}
}

// takeClicks opens the timeline of a point clicked in the current view's
// charts or table in the last frame.
func (u *UI) takeClicks(res *report.Result) {
	var charts []interface {
		takeClick() (report.Point, bool)
	}
	switch u.view {
	case report.ViewDashboard:
		charts = append(charts, &u.dashPie)
		if r, ok := u.dashTable.takeClick(); ok && res != nil && r < len(res.Series.Points) {
			u.openTimeline(res.Series.Points[r]) // the table's rows are the points
		}
	case report.ViewTopApps:
		charts = append(charts, &u.topBars)
	case report.ViewTimeSpent:
		charts = append(charts, &u.spentPie)
	}
	for _, c := range charts {
		if p, ok := c.takeClick(); ok {
			u.openTimeline(p)
		}
	}
}

// layoutDrill draws the drill-down d: a heading with Back, the item and
// the toggle for empty segments, then the timeline.
func (u *UI) layoutDrill(gtx C, d *drill) D {
	u.mu.Lock()
	s, err, loading, generated := d.series, d.err, d.loading, d.generated
	u.mu.Unlock()
	seg := u.svc.SegmentFor(u.rng)
	title := d.point.Label
	if it := d.point.Item; it.Process != "" && it.Process != title {
		title += " (" + it.Process + ")"
	}
	info := s.Title
	if info == "" {
		info = "Focus time per " + seg.String()
	}
	info += " · " + u.svc.RangeLabel(u.rng)
	if loading {
		info += " · loading…"
	} else if !generated.IsZero() {
		info += " · updated " + timefmt.Format(generated.In(u.svc.Location()), u.svc.Config().TimeFormat)
	}
	if d.hideEmpty.Value {
		s = s.WithoutEmpty()
	}
	heading := func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &d.back, "← Back") }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 12, Right: 8}.Layout(gtx, func(gtx C) D {
					return layoutIcon(gtx, u.icons.get(d.point), 22)
				})
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return truncLabel(gtx, u.th, 18, cs.fg, title) }),
					layout.Rigid(func(gtx C) D { return truncLabel(gtx, u.th, 13, cs.muted, info) }),
				)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 12}.Layout(gtx,
					material.CheckBox(u.th, &d.hideEmpty, "Hide "+seg.String()+"s without focus").Layout)
			}),
		)
	}
	body := func(gtx C) D {
		switch {
		case err != nil:
			l := material.Body1(u.th, "Error: "+err.Error())
			l.Color = cs.err
			return l.Layout(gtx)
		case loading && len(s.Points) == 0:
			return D{Size: gtx.Constraints.Max}
		}
		return d.chart.layout(gtx, u.th, s)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(heading),
		layout.Rigid(func(gtx C) D { return u.filterBadge(gtx) }),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Flexed(1, body),
		layout.Rigid(u.layoutExport),
	)
}

// drillSeries returns the timeline shown by the current view's drill-down
// (for the CSV export), if one is open.
func (u *UI) drillSeries() (report.Series, bool) {
	d := u.currentDrill()
	if d == nil {
		return report.Series{}, false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if d.hideEmpty.Value {
		return d.series.WithoutEmpty(), true
	}
	return d.series, true
}
