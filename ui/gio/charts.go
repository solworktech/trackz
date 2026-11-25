package gio

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
)

// hover tracks the pointer over a chart area, and clicks in it.
type hover struct {
	pos     image.Point
	inside  bool
	pressed bool
	// clicked is set by events when a click (press and release inside)
	// ended at pos in this frame.
	clicked bool
}

// update processes the pointer events and registers the area for them.
func (h *hover) update(gtx C, size image.Point) {
	h.events(gtx)
	h.area(gtx, size, pointer.CursorDefault)
}

// events processes the pointer events since the last frame.
func (h *hover) events(gtx C) {
	h.clicked = false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: h,
			Kinds: pointer.Move | pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Leave:
			h.inside, h.pressed = false, false
		case pointer.Cancel:
			h.pressed = false
		case pointer.Press:
			h.inside, h.pressed = true, e.Buttons == pointer.ButtonPrimary
			h.pos = e.Position.Round()
		case pointer.Release:
			h.clicked = h.pressed && h.inside
			h.pressed = false
			h.pos = e.Position.Round()
		default:
			h.inside = true
			h.pos = e.Position.Round()
		}
	}
}

// area registers size for the pointer events, with cursor over it (none
// for CursorDefault).
func (h *hover) area(gtx C, size image.Point, cursor pointer.Cursor) {
	area := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	if cursor != pointer.CursorDefault {
		cursor.Add(gtx.Ops)
	}
	event.Op(gtx.Ops, h)
	area.Pop()
}

// hasTimeline reports whether clicking p opens its timeline.
func hasTimeline(p report.Point) bool { return !p.Item.IsZero() }

// tipLines are the tooltip lines for p: label, value (with share when
// total > 0), process, detail, and when it was first and last in focus.
func tipLines(s report.Series, p report.Point, total float64) []string {
	v := s.FormatValue(p.Value)
	if total > 0 {
		v += "  (" + pct(p.Value, total) + ")"
	}
	lines := []string{p.Label, v}
	if p.Process != "" && p.Process != p.Label {
		lines = append(lines, p.Process)
	}
	if p.Detail != "" {
		lines = append(lines, p.Detail)
	}
	if p.Span != "" {
		lines = append(lines, p.Span)
	}
	return lines
}

func pct(v, total float64) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", 100*v/total)
}

// pieChart draws a pie with a legend (icon, label, value, share) beside it.
type pieChart struct {
	hover  hover
	legend widget.List
	// scale is the pie's diameter relative to the room it has (0 means 1).
	scale float32
	// compact puts each value right after the longest label instead of at
	// the right edge: for short labels (application names).
	compact bool
	rows    []widget.Clickable // legend entries
	// clicked is the point whose slice or legend entry was clicked in the
	// last layout, if it has a timeline (see takeClick).
	clicked *report.Point
}

// takeClick returns the point clicked in the last layout, if any.
func (pc *pieChart) takeClick() (report.Point, bool) {
	p := pc.clicked
	pc.clicked = nil
	if p == nil {
		return report.Point{}, false
	}
	return *p, true
}

func (pc *pieChart) layout(gtx C, th *material.Theme, ic *iconCache, s report.Series) D {
	total := s.Total()
	if len(s.Points) == 0 || total <= 0 {
		return emptyChart(gtx, th)
	}
	// The pie is centred in the left 55% of the width, as large as that
	// and the height allow (it is round). The legend starts pieGap after
	// the pie rather than at 55%, so when the height limits the pie the
	// legend gets the width beside it. It is centred vertically against
	// the pie, or starts at the top and scrolls if it is taller.
	width := gtx.Constraints.Max.X
	colW := width * 55 / 100
	side := min(colW, gtx.Constraints.Max.Y)
	if pc.scale > 0 && pc.scale < 1 {
		side = int(float32(side) * pc.scale)
	}
	pg := gtx
	pg.Constraints = layout.Constraints{Max: image.Pt(side, side)}
	pm := op.Record(gtx.Ops)
	pd := pc.pie(pg, th, ic, s, total)
	pie := pm.Stop()
	pieX := (colW - pd.Size.X) / 2
	ps := op.Offset(image.Pt(pieX, 0)).Push(gtx.Ops)
	pie.Add(gtx.Ops)
	ps.Pop()
	pieW := min(colW, pieX+pd.Size.X+gtx.Dp(pieGap)) // where the legend starts

	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(width-pieW, gtx.Constraints.Max.Y)}
	m := op.Record(gtx.Ops)
	ld := pc.legendList(lg, th, ic, s, total)
	legend := m.Stop()
	top := max(0, (pd.Size.Y-ld.Size.Y)/2)
	st := op.Offset(image.Pt(pieW, top)).Push(gtx.Ops)
	legend.Add(gtx.Ops)
	st.Pop()
	return D{Size: image.Pt(width, max(pd.Size.Y, top+ld.Size.Y))}
}

// legendValueGap is the space before the values in a compact legend.
const legendValueGap unit.Dp = 24

// pieGap is the space between a pie (whose drawing keeps a 12 dp margin
// of its own) and its legend.
const pieGap unit.Dp = 24

func (pc *pieChart) pie(gtx C, th *material.Theme, ic *iconCache, s report.Series, total float64) D {
	size := gtx.Constraints.Max
	if size.Y > size.X {
		size.Y = size.X
	}
	pc.hover.events(gtx)
	r := float32(min(size.X, size.Y))/2 - float32(gtx.Dp(12))
	if r < 10 {
		pc.hover.area(gtx, size, pointer.CursorDefault)
		return D{Size: size}
	}
	c := f32.Pt(float32(size.X)/2, float32(size.Y)/2)

	hovered := -1
	if pc.hover.inside {
		d := f32.Pt(float32(pc.hover.pos.X), float32(pc.hover.pos.Y)).Sub(c)
		if float32(math.Hypot(float64(d.X), float64(d.Y))) <= r {
			a := math.Atan2(float64(d.Y), float64(d.X)) + math.Pi/2 // 0 at 12 o'clock
			if a < 0 {
				a += 2 * math.Pi
			}
			acc := 0.0
			for i, p := range s.Points {
				acc += p.Value / total * 2 * math.Pi
				if a <= acc {
					hovered = i
					break
				}
			}
		}
	}
	cursor := pointer.CursorDefault
	if hovered >= 0 && hasTimeline(s.Points[hovered]) {
		cursor = pointer.CursorPointer
		if pc.hover.clicked {
			p := s.Points[hovered]
			pc.clicked = &p
		}
	}
	pc.hover.area(gtx, size, cursor)

	start := -math.Pi / 2
	for i, p := range s.Points {
		sweep := p.Value / total * 2 * math.Pi
		rr := r
		if i == hovered {
			rr = r + float32(gtx.Dp(6))
		}
		drawSlice(gtx, c, rr, start, sweep, seriesColor(i))
		start += sweep
	}
	// Icons on the slices wide enough to hold one; the tooltip shows it for
	// the others.
	start = -math.Pi / 2
	for _, p := range s.Points {
		sweep := p.Value / total * 2 * math.Pi
		mid := start + sweep/2
		start += sweep
		icon := ic.get(p)
		if icon == nil {
			continue
		}
		if at, ok := sliceIconSpot(c, r, mid, sweep, float32(gtx.Dp(sliceIconSize))); ok {
			drawSliceIcon(gtx, icon, at)
		}
	}
	if hovered >= 0 {
		p := s.Points[hovered]
		tooltip(gtx, th, pc.hover.pos, size, ic.get(p), tipLines(s, p, total)...)
	}
	return D{Size: size}
}

const sliceIconSize = 22 // dp

// sliceIconSpot returns where to centre an icon of size px on a slice of
// a pie of radius r centred at c (mid is the slice's middle angle), and
// whether the slice has room for it: the icon, with a margin, must fit
// across the slice at that distance from the centre and inside the rim.
func sliceIconSpot(c f32.Point, r float32, mid, sweep float64, px float32) (f32.Point, bool) {
	need := float64(px) * 1.4
	dist := float64(r) * 0.65
	if float64(r)-dist < need/2 {
		dist = float64(r) - need/2
	}
	across := 2 * dist * math.Sin(math.Min(sweep, math.Pi)/2)
	if dist <= need/2 || across < need {
		return f32.Point{}, false
	}
	return c.Add(f32.Pt(float32(dist*math.Cos(mid)), float32(dist*math.Sin(mid)))), true
}

// drawSliceIcon draws icon centred at at, on a disc so it stands out from
// the slice colour.
func drawSliceIcon(gtx C, icon *paint.ImageOp, at f32.Point) {
	px := gtx.Dp(sliceIconSize)
	pad := gtx.Dp(3)
	disc := image.Rectangle{
		Min: image.Pt(int(at.X)-px/2-pad, int(at.Y)-px/2-pad),
		Max: image.Pt(int(at.X)+px/2+pad, int(at.Y)+px/2+pad),
	}
	bg := cs.panel
	bg.A = 0xe0
	paint.FillShape(gtx.Ops, bg, clip.Ellipse(disc).Op(gtx.Ops))
	defer op.Offset(image.Pt(int(at.X)-px/2, int(at.Y)-px/2)).Push(gtx.Ops).Pop()
	layoutIcon(gtx, icon, sliceIconSize)
}

func drawSlice(gtx C, c f32.Point, r float32, start, sweep float64, col color.NRGBA) {
	if sweep <= 0 {
		return
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(c)
	p.LineTo(c.Add(f32.Pt(r*float32(math.Cos(start)), r*float32(math.Sin(start)))))
	// Arc's foci are relative to the pen; for a circle both are the centre.
	for sweep > 0 {
		step := math.Min(sweep, math.Pi/2)
		pen := p.Pos()
		p.Arc(c.Sub(pen), c.Sub(pen), float32(step))
		sweep -= step
	}
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
}

func (pc *pieChart) legendList(gtx C, th *material.Theme, ic *iconCache, s report.Series, total float64) D {
	pc.legend.Axis = layout.Vertical
	// The scrollbar shows (and takes room) only when the legend does not
	// fit: when it all fitted in the last frame, the bar is not reserved.
	pos := pc.legend.Position
	fits := pos.First == 0 && pos.Offset == 0 && pos.Count == len(s.Points) && pos.OffsetLast >= 0
	list := material.List(th, &pc.legend)
	// Space between the values and the scrollbar: the same as in tables,
	// whose right-aligned values leave room for the sort icon.
	var barGap unit.Dp
	if fits {
		list.AnchorStrategy = material.Overlay
	} else {
		barGap = 6 + sortIconW + sortIconGap
	}
	// In a compact legend, labels and values are columns as wide as their
	// widest entry (labels at most what the row leaves them).
	values := make([]string, len(s.Points))
	labelW, valueW := 0, 0
	for i, p := range s.Points {
		values[i] = s.FormatValue(p.Value) + "  " + pct(p.Value, total)
		if pc.compact {
			labelW = max(labelW, measure(gtx, th, 13, p.Label).X)
			valueW = max(valueW, measure(gtx, th, 13, values[i]).X)
		}
	}
	if pc.compact {
		fixed := gtx.Dp(12+6) + gtx.Dp(18+6) + gtx.Dp(legendValueGap) + valueW + gtx.Dp(barGap)
		labelW = min(labelW, max(0, gtx.Constraints.Max.X-fixed))
	}
	if len(pc.rows) != len(s.Points) {
		pc.rows = make([]widget.Clickable, len(s.Points))
	}
	for i := range pc.rows {
		if pc.rows[i].Clicked(gtx) && hasTimeline(s.Points[i]) {
			p := s.Points[i]
			pc.clicked = &p
		}
	}
	return list.Layout(gtx, len(s.Points), func(gtx C, i int) D {
		p := s.Points[i]
		if !hasTimeline(p) {
			return pc.legendRow(gtx, th, ic, p, i, values[i], labelW, valueW, barGap)
		}
		return pc.rows[i].Layout(gtx, func(gtx C) D {
			pointer.CursorPointer.Add(gtx.Ops)
			return pc.legendRow(gtx, th, ic, p, i, values[i], labelW, valueW, barGap)
		})
	})
}

// legendRow is one legend entry: colour, icon, label, value (value text
// valTxt; labelW and valueW are the column widths of a compact legend).
func (pc *pieChart) legendRow(gtx C, th *material.Theme, ic *iconCache, p report.Point, i int, valTxt string, labelW, valueW int, barGap unit.Dp) D {
	label := layout.Flexed(1, func(gtx C) D { return truncLabel(gtx, th, 13, cs.fg, p.Label) })
	value := layout.Rigid(func(gtx C) D {
		return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
			l := material.Label(th, 13, valTxt)
			l.Color = cs.muted
			return l.Layout(gtx)
		})
	})
	if pc.compact {
		label = layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = labelW, labelW
			return truncLabel(gtx, th, 13, cs.fg, p.Label)
		})
		value = layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: legendValueGap}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = valueW, valueW
				l := material.Label(th, 13, valTxt)
				l.Color = cs.muted
				l.Alignment = text.End
				return l.Layout(gtx)
			})
		})
	}
	return layout.Inset{Top: 2, Bottom: 2, Right: barGap}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				sz := gtx.Dp(12)
				fillRect(gtx, image.Rect(0, 0, sz, sz), seriesColor(i))
				return D{Size: image.Pt(sz+gtx.Dp(6), sz)}
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
					return layoutIcon(gtx, ic.get(p), 18)
				})
			}),
			label,
			value,
		)
	})
}

// barChart draws horizontal bars: icon, label, bar, value. Hovering a bar
// shows its details; clicking one with a timeline reports it (takeClick).
type barChart struct {
	list    layout.List
	hover   hover
	rows    []widget.Clickable
	clicked *report.Point
}

// takeClick returns the point clicked in the last layout, if any.
func (bc *barChart) takeClick() (report.Point, bool) {
	p := bc.clicked
	bc.clicked = nil
	if p == nil {
		return report.Point{}, false
	}
	return *p, true
}

func (bc *barChart) layout(gtx C, th *material.Theme, ic *iconCache, s report.Series) D {
	if len(s.Points) == 0 {
		return emptyChart(gtx, th)
	}
	max := s.Max()
	if max <= 0 {
		max = 1
	}
	size := gtx.Constraints.Max
	bc.hover.events(gtx)
	if len(bc.rows) != len(s.Points) {
		bc.rows = make([]widget.Clickable, len(s.Points))
	}
	for i := range bc.rows {
		if bc.rows[i].Clicked(gtx) && hasTimeline(s.Points[i]) {
			p := s.Points[i]
			bc.clicked = &p
		}
	}
	labelW := gtx.Constraints.Max.X / 3
	bc.list.Axis = layout.Vertical
	row := func(gtx C, i int) D {
		p := s.Points[i]
		return layout.Inset{Top: 3, Bottom: 3}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
						return layoutIcon(gtx, ic.get(p), 20)
					})
				}),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X, gtx.Constraints.Max.X = labelW, labelW
					return truncLabel(gtx, th, 14, cs.fg, p.Label)
				}),
				layout.Flexed(1, func(gtx C) D {
					valTxt := s.FormatShort(p.Value) + "  " + s.FormatValue(p.Value)
					tw := measure(gtx, th, 13, valTxt).X + gtx.Dp(8)
					avail := gtx.Constraints.Max.X - tw
					h := gtx.Dp(20)
					w := int(float64(avail) * p.Value / max)
					if w < 1 && p.Value > 0 {
						w = 1
					}
					col := seriesColor(i)
					if bc.rows[i].Hovered() {
						col = lighten(col)
					}
					fillRect(gtx, image.Rect(0, 0, w, h), col)
					labelAt(gtx, th, image.Pt(w+gtx.Dp(6), (h-measure(gtx, th, 13, valTxt).Y)/2), 13, cs.muted, valTxt)
					return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
				}),
			)
		})
	}
	d := bc.list.Layout(gtx, len(s.Points), func(gtx C, i int) D {
		return bc.rows[i].Layout(gtx, func(gtx C) D {
			if hasTimeline(s.Points[i]) {
				pointer.CursorPointer.Add(gtx.Ops)
			}
			return row(gtx, i)
		})
	})
	// The pointer position for the tooltip, without taking the rows'
	// events (and clicks).
	pass := pointer.PassOp{}.Push(gtx.Ops)
	bc.hover.area(gtx, size, pointer.CursorDefault)
	pass.Pop()
	if bc.hover.inside {
		for i := range bc.rows {
			if bc.rows[i].Hovered() {
				p := s.Points[i]
				tooltip(gtx, th, bc.hover.pos, size, ic.get(p), tipLines(s, p, 0)...)
				break
			}
		}
	}
	return d
}

// columnChart draws vertical bars (time series) with hover details.
type columnChart struct{ hover hover }

func (cc *columnChart) layout(gtx C, th *material.Theme, s report.Series) D {
	if len(s.Points) == 0 {
		return emptyChart(gtx, th)
	}
	size := gtx.Constraints.Max
	cc.hover.update(gtx, size)
	plot := plotArea(gtx, th, s, size)
	drawAxes(gtx, th, s, plot)
	n := len(s.Points)
	slot := float32(plot.Dx()) / float32(n)
	barW := slot * 0.7
	max := seriesAxisMax(s)
	hovered := -1
	if cc.hover.inside && cc.hover.pos.In(plot) {
		hovered = int(float32(cc.hover.pos.X-plot.Min.X) / slot)
	}
	for i, p := range s.Points {
		h := float32(plot.Dy()) * float32(p.Value/max)
		x0 := float32(plot.Min.X) + slot*float32(i) + (slot-barW)/2
		r := image.Rect(int(x0), plot.Max.Y-int(h), int(x0+barW), plot.Max.Y)
		col := seriesColor(3)
		if i == hovered {
			col = seriesColor(0)
		}
		fillRect(gtx, r, col)
	}
	xLabels(gtx, th, s, plot, slot)
	if hovered >= 0 && hovered < n {
		p := s.Points[hovered]
		lines := []string{p.Label, s.FormatValue(p.Value)}
		if p.Detail != "" {
			lines = append(lines, p.Detail)
		}
		tooltip(gtx, th, cc.hover.pos, size, nil, lines...)
	}
	return D{Size: size}
}

// lineChart draws a polyline with markers and hover details.
type lineChart struct {
	hover hover
	// hideValue leaves the value out of the hover box (Window Activity:
	// its time per bucket is misleading; the window switches are kept).
	hideValue bool
}

func (lc *lineChart) layout(gtx C, th *material.Theme, s report.Series) D {
	if len(s.Points) == 0 {
		return emptyChart(gtx, th)
	}
	size := gtx.Constraints.Max
	lc.hover.update(gtx, size)
	plot := plotArea(gtx, th, s, size)
	drawAxes(gtx, th, s, plot)
	n := len(s.Points)
	slot := float32(plot.Dx()) / float32(n)
	max := seriesAxisMax(s)
	pt := func(i int) f32.Point {
		x := float32(plot.Min.X) + slot*float32(i) + slot/2
		y := float32(plot.Max.Y) - float32(plot.Dy())*float32(s.Points[i].Value/max)
		return f32.Pt(x, y)
	}
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(pt(0))
	for i := 1; i < n; i++ {
		path.LineTo(pt(i))
	}
	col := seriesColor(0)
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: path.End(), Width: float32(gtx.Dp(2))}.Op())
	hovered := -1
	if lc.hover.inside && lc.hover.pos.In(plot) {
		hovered = int(float32(lc.hover.pos.X-plot.Min.X) / slot)
		if hovered >= n {
			hovered = n - 1
		}
	}
	rad := gtx.Dp(3)
	for i := 0; i < n; i++ {
		p := pt(i).Round()
		rr := rad
		if i == hovered {
			rr = rad * 2
			line(gtx, f32.Pt(pt(i).X, float32(plot.Min.Y)), f32.Pt(pt(i).X, float32(plot.Max.Y)), cs.grid, 1)
		}
		e := clip.Ellipse(image.Rect(p.X-rr, p.Y-rr, p.X+rr, p.Y+rr))
		paint.FillShape(gtx.Ops, col, e.Op(gtx.Ops))
	}
	xLabels(gtx, th, s, plot, slot)
	if hovered >= 0 {
		p := s.Points[hovered]
		lines := []string{p.Label}
		if !lc.hideValue {
			lines = append(lines, s.FormatValue(p.Value))
		}
		if p.Detail != "" {
			lines = append(lines, p.Detail)
		}
		tooltip(gtx, th, lc.hover.pos, size, nil, lines...)
	}
	return D{Size: size}
}

// seriesAxisMax rounds the series maximum up so that the gridlines fall on
// readable values: durations in steps of whole minutes (1, 2, 5, 10, 15)
// or hours (1, 2, 2.5, 5 x 10^n), numbers to 1, 2, 2.5, 5 x 10^n.
func seriesAxisMax(s report.Series) float64 {
	v := s.Max()
	if s.Unit == report.Seconds {
		switch {
		case v >= 3600:
			return niceStep(v/3600/gridLines, 1, 2, 2.5, 5, 10) * gridLines * 3600
		case v >= 60:
			return niceStep(v/60/gridLines, 1, 2, 5, 10, 15) * gridLines * 60
		}
	}
	return axisMax(v)
}

// niceStep returns the first of steps (times a power of ten, for values of
// 1 and more) that is at least v.
func niceStep(v float64, steps ...float64) float64 {
	exp := 1.0
	if v > steps[len(steps)-1] {
		exp = math.Pow(10, math.Floor(math.Log10(v)))
	}
	for _, m := range steps {
		if m*exp >= v {
			return m * exp
		}
	}
	return steps[len(steps)-1] * exp
}

// axisMax rounds the maximum up to a "nice" value (1, 2, 2.5, 5 x 10^n).
func axisMax(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*exp >= v {
			return m * exp
		}
	}
	return 10 * exp
}

const gridLines = 4

// plotArea leaves room for y-axis labels on the left and x labels below.
func plotArea(gtx C, th *material.Theme, s report.Series, size image.Point) image.Rectangle {
	max := seriesAxisMax(s)
	w := measure(gtx, th, 12, s.FormatShort(max)).X
	lh := measure(gtx, th, 12, "0").Y
	return image.Rect(w+gtx.Dp(10), gtx.Dp(8), size.X-gtx.Dp(8), size.Y-lh-gtx.Dp(10))
}

func drawAxes(gtx C, th *material.Theme, s report.Series, plot image.Rectangle) {
	max := seriesAxisMax(s)
	lh := measure(gtx, th, 12, "0").Y
	for i := 0; i <= gridLines; i++ {
		v := max * float64(i) / gridLines
		y := float32(plot.Max.Y) - float32(plot.Dy())*float32(i)/gridLines
		line(gtx, f32.Pt(float32(plot.Min.X), y), f32.Pt(float32(plot.Max.X), y), cs.grid, 1)
		txt := s.FormatShort(v)
		tw := measure(gtx, th, 12, txt).X
		labelAt(gtx, th, image.Pt(plot.Min.X-tw-gtx.Dp(6), int(y)-lh/2), 12, cs.muted, txt)
	}
	line(gtx, f32.Pt(float32(plot.Min.X), float32(plot.Max.Y)), f32.Pt(float32(plot.Max.X), float32(plot.Max.Y)), cs.border, 1)
}

// xLabels draws as many x labels as fit without overlapping.
func xLabels(gtx C, th *material.Theme, s report.Series, plot image.Rectangle, slot float32) {
	maxW := 0
	for _, p := range s.Points {
		if w := measure(gtx, th, 12, p.Label).X; w > maxW {
			maxW = w
		}
	}
	every := 1
	if slot > 0 {
		every = int(math.Ceil(float64(maxW+gtx.Dp(10)) / float64(slot)))
	}
	if every < 1 {
		every = 1
	}
	for i := 0; i < len(s.Points); i += every {
		txt := s.Points[i].Label
		tw := measure(gtx, th, 12, txt).X
		x := plot.Min.X + int(slot*float32(i)+slot/2) - tw/2
		labelAt(gtx, th, image.Pt(x, plot.Max.Y+gtx.Dp(4)), 12, cs.muted, txt)
	}
}

func emptyChart(gtx C, th *material.Theme) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		l := material.Body1(th, "No data for this period.")
		l.Color = cs.muted
		return l.Layout(gtx)
	})
}

// lighten mixes c a quarter of the way to white (a hovered bar).
func lighten(c color.NRGBA) color.NRGBA {
	mix := func(v uint8) uint8 { return v + (255-v)/4 }
	return color.NRGBA{R: mix(c.R), G: mix(c.G), B: mix(c.B), A: c.A}
}
