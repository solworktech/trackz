package gio

import (
	"context"
	"fmt"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/timefmt"
)

// drill is a drill-down of a view, shown instead of its chart until Back
// is clicked: the timeline of one of its points (focus time per hour, day
// or week of the range), or, for a task group (or Ungrouped), its windows
// as bars, a pie or a table, or its timeline (UI.groupView); a click on a
// window shows that window's timeline. It follows the range and the
// filter. Each view keeps a stack of them.
type drill struct {
	point report.Point // what was clicked (label, icon, Item)
	group bool         // point is a task group (or Ungrouped)

	// The data, for the inputs in key (guarded by UI.mu).
	key       string
	series    report.Series     // timeline
	windows   *report.Dashboard // a group's windows
	generated time.Time
	rng       report.Range
	err       error
	loading   bool

	back      widget.Clickable
	hideEmpty widget.Bool
	chart     columnChart
	table     tableView
	pie       pieChart
	bars      barChart
}

// Views of a task group's drill-down (UI.groupView).
const (
	groupBars     = "Bar"
	groupPie      = "Pie"
	groupTable    = "Table"
	groupTimeline = "Timeline"
)

var groupViews = []string{groupBars, groupPie, groupTable, groupTimeline}

// showsTimeline reports whether d shows its timeline.
func (u *UI) showsTimeline(d *drill) bool { return !d.group || u.groupView.Value == groupTimeline }

// openTimeline shows the timeline of p (or, for Ungrouped, its windows)
// in place of the current view's chart or drill-down.
func (u *UI) openTimeline(p report.Point) {
	if !hasTimeline(p) {
		return
	}
	d := &drill{point: p, group: p.Item.Group != "" || p.Item.Ungrouped}
	u.mu.Lock()
	stack := u.drills[u.view]
	for _, old := range stack {
		d.hideEmpty.Value = old.hideEmpty.Value // keep the toggle's state
	}
	u.drills[u.view] = append(stack, d)
	u.mu.Unlock()
	u.ensureDrill(true)
	u.invalidate()
}

// currentDrill returns the current view's innermost drill-down, if one is
// open.
func (u *UI) currentDrill() *drill {
	u.mu.Lock()
	defer u.mu.Unlock()
	if stack := u.drills[u.view]; len(stack) > 0 {
		return stack[len(stack)-1]
	}
	return nil
}

// ensureDrill starts loading the current drill-down's data when its inputs
// changed, when it is due for its automatic refresh, or if force.
func (u *UI) ensureDrill(force bool) {
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
		var (
			s   report.Series
			w   *report.Dashboard
			err error
		)
		if d.group {
			w, err = u.svc.GroupWindows(ctx, item, rng, filter)
			if w != nil {
				for _, p := range w.Series.Points {
					u.icons.prefetch(p)
				}
			}
		}
		if err == nil {
			s, err = u.svc.Timeline(ctx, item, rng, filter)
		}
		u.mu.Lock()
		if u.onStack(v, d) && d.key == key { // not superseded
			d.series, d.windows, d.err, d.loading = s, w, err, false
			d.generated, d.rng = time.Now(), rng
		}
		u.mu.Unlock()
		u.invalidate()
	}()
}

// onStack reports whether d is one of view v's drill-downs (u.mu held).
func (u *UI) onStack(v report.View, d *drill) bool {
	for _, x := range u.drills[v] {
		if x == d {
			return true
		}
	}
	return false
}

// handleDrill processes the choice of a group's view and the drill-down's
// Back button: back to the drill-down below, or to the view.
func (u *UI) handleDrill(gtx C) {
	// Before the layout, which reads the choice: a click on a radio button
	// shows the new view in the same frame.
	u.groupView.Update(gtx)
	d := u.currentDrill()
	if d == nil || !d.back.Clicked(gtx) {
		return
	}
	u.mu.Lock()
	if stack := u.drills[u.view]; len(stack) > 0 {
		u.drills[u.view] = stack[:len(stack)-1]
	}
	u.mu.Unlock()
	u.ensureDrill(false)
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
	case report.ViewTaskGroups:
		charts = append(charts, &u.groupsPie)
	}
	for _, c := range charts {
		if p, ok := c.takeClick(); ok {
			u.openTimeline(p)
		}
	}
}

// layoutDrill draws the drill-down d: a heading with Back and the item,
// for a group the choice of view, for a timeline the toggle for empty
// segments, then the timeline or the group's windows.
func (u *UI) layoutDrill(gtx C, d *drill) D {
	u.mu.Lock()
	s, w, err, loading, generated := d.series, d.windows, d.err, d.loading, d.generated
	u.mu.Unlock()
	timeline := u.showsTimeline(d)
	seg := u.svc.SegmentFor(u.rng)
	title := d.point.Label
	if it := d.point.Item; it.Process != "" && it.Process != title {
		title += " (" + it.Process + ")"
	}
	var info string
	switch {
	case !timeline && w != nil:
		info = w.Series.Title
	case !timeline:
		info = "Windows"
	case s.Title != "":
		info = s.Title
	default:
		info = "Focus time per " + seg.String()
	}
	info += " · " + u.svc.RangeLabel(u.rng)
	if !loading && !generated.IsZero() {
		info += " · updated " + timefmt.Format(generated.In(u.svc.Location()), u.svc.Config().TimeFormat)
	}
	if !timeline && w != nil && w.Hidden.Count > 0 {
		info += fmt.Sprintf(" · %d under %ds in total hidden", w.Hidden.Count, w.Hidden.MinDuration)
	}
	if d.hideEmpty.Value {
		s = s.WithoutEmpty()
	}
	heading := func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &d.back, "← Back") }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 12, Right: 8}.Layout(gtx, func(gtx C) D {
					return layoutIcon(gtx, u.icons.get(d.point), 22)
				})
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return truncLabel(gtx, u.th, 18, cs.fg, title) }),
					layout.Rigid(func(gtx C) D { return labelThenThrobber(gtx, u.th, info, loading) }),
				)
			}),
		}
		if d.group {
			for _, v := range groupViews {
				children = append(children, layout.Rigid(func(gtx C) D {
					return material.RadioButton(u.th, &u.groupView, v, v).Layout(gtx)
				}))
			}
		}
		if timeline {
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 12}.Layout(gtx,
					material.CheckBox(u.th, &d.hideEmpty, "Hide "+seg.String()+"s without focus").Layout)
			}))
		}
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	}
	body := func(gtx C) D {
		switch {
		case err != nil:
			l := material.Body1(u.th, "Error: "+err.Error())
			l.Color = cs.err
			return l.Layout(gtx)
		case loading && generated.IsZero():
			return centredThrobber(gtx, u.th)
		case timeline:
			return d.chart.layout(gtx, u.th, s)
		case w == nil || len(w.Series.Points) == 0:
			return emptyChart(gtx, u.th)
		}
		pts := w.Series.Points
		var dims D
		switch u.groupView.Value {
		case groupPie:
			dims = d.pie.layout(gtx, u.th, u.icons, w.Series)
		case groupTable:
			d.table.rowClick = func(r int) bool { return r < len(pts) && hasTimeline(pts[r]) }
			dims = d.table.layout(gtx, u.th, w.Table, u.windowColumns(w.Table, pts))
			if r, ok := d.table.takeClick(); ok && r < len(pts) { // the table's rows are the points
				defer u.openTimeline(pts[r])
			}
		default:
			dims = d.bars.layout(gtx, u.th, u.icons, w.Series)
		}
		for _, c := range []interface {
			takeClick() (report.Point, bool)
		}{&d.pie, &d.bars} {
			if p, ok := c.takeClick(); ok {
				defer u.openTimeline(p)
			}
		}
		return dims
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(heading),
		layout.Rigid(func(gtx C) D { return u.filterBadge(gtx) }),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Flexed(1, body),
		layout.Rigid(u.layoutExport),
	)
}

// drillTable returns what the current view's drill-down shows, as a table
// (for the CSV export), if one is open.
func (u *UI) drillTable() (*report.Dashboard, report.Series, bool) {
	d := u.currentDrill()
	if d == nil {
		return nil, report.Series{}, false
	}
	timeline := u.showsTimeline(d)
	u.mu.Lock()
	defer u.mu.Unlock()
	if !timeline {
		return d.windows, report.Series{}, true
	}
	if d.hideEmpty.Value {
		return nil, d.series.WithoutEmpty(), true
	}
	return nil, d.series, true
}
