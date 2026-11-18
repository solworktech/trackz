package gio

import (
	"fmt"
	"image"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
)

// rangeModal is the dialog for choosing a custom date range.
type rangeModal struct {
	open          bool
	start, end    datePicker
	apply, cancel widget.Clickable
	quick         [5]widget.Clickable
	scrim, panel  int // event tags (their addresses are used)
}

var quickNames = [5]string{"This week", "Last week", "This month", "Last month", "This year"}

// quickRange returns the dates for quick pick i, relative to today.
// Weeks start on Monday.
func quickRange(i int, today report.Date) (report.Date, report.Date) {
	t := today.In(time.UTC)
	monday := today.AddDays(-((int(t.Weekday()) + 6) % 7))
	firstOfMonth := report.Date{Year: today.Year, Month: today.Month, Day: 1}
	switch i {
	case 0:
		return monday, today
	case 1:
		return monday.AddDays(-7), monday.AddDays(-1)
	case 2:
		return firstOfMonth, today
	case 3:
		prev := report.DateOf(firstOfMonth.In(time.UTC).AddDate(0, -1, 0))
		return prev, firstOfMonth.AddDays(-1)
	default:
		return report.Date{Year: today.Year, Month: time.January, Day: 1}, today
	}
}

// openRangeModal shows the dialog, starting from the last custom range.
func (u *UI) openRangeModal() {
	u.modal.start.set(u.customFrom)
	u.modal.end.set(u.customTo)
	u.modal.open = true
}

func (u *UI) closeRangeModal() { u.modal.open = false }

func (u *UI) applyRangeModal() {
	m := &u.modal
	r, err := report.CustomRange(m.start.Value, m.end.Value)
	if err != nil {
		return // the dialog shows the problem; Apply is disabled
	}
	u.customFrom, u.customTo = m.start.Value, m.end.Value
	u.rng = r
	m.open = false
	u.ensure(true)
}

// layoutRangeModal draws the dialog over the whole window. It must be called
// after the rest of the UI so it is on top and receives input first.
func (u *UI) layoutRangeModal(gtx C) {
	m := &u.modal
	today := u.svc.Today()

	if m.cancel.Clicked(gtx) {
		u.closeRangeModal()
	}
	if m.apply.Clicked(gtx) {
		u.applyRangeModal()
	}
	for i := range m.quick {
		if m.quick[i].Clicked(gtx) {
			from, to := quickRange(i, today)
			m.start.set(from)
			m.end.set(to)
		}
	}
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameEscape},
			key.Filter{Name: key.NameReturn},
			key.Filter{Name: key.NameEnter},
		)
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			if e.Name == key.NameEscape {
				u.closeRangeModal()
			} else {
				u.applyRangeModal()
			}
		}
	}
	if dialogClickedOutside(gtx, &m.scrim) {
		u.closeRangeModal()
	}
	if !m.open {
		return
	}

	drawDialog(gtx, &m.scrim, &m.panel, 600, func(gtx C) D { return u.rangeModalBody(gtx, today) })
}

// dialogClickedOutside reports (and consumes) presses on a dialog's
// backdrop.
func dialogClickedOutside(gtx C, scrim *int) bool {
	outside := false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: scrim, Kinds: pointer.Press})
		if !ok {
			return outside
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			outside = true
		}
	}
}

// drawDialog dims the window, swallowing its input, and draws body in a
// centred panel at most maxWidth wide. scrim and panel are event tags.
func drawDialog(gtx C, scrim, panel *int, maxWidth unit.Dp, body layout.Widget) {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, cs.scrim, clip.Rect{Max: size}.Op())
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, scrim)
	area.Pop()

	rec := op.Record(gtx.Ops)
	pgtx := gtx
	pgtx.Constraints.Min = image.Point{}
	if w := gtx.Dp(maxWidth); pgtx.Constraints.Max.X > w {
		pgtx.Constraints.Max.X = w
	}
	dims := layout.UniformInset(18).Layout(pgtx, body)
	call := rec.Stop()

	off := image.Pt((size.X-dims.Size.X)/2, (size.Y-dims.Size.Y)/2)
	if off.Y < 0 {
		off.Y = 0
	}
	defer op.Offset(off).Push(gtx.Ops).Pop()
	rr := clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(10))
	paint.FillShape(gtx.Ops, cs.panel, rr.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cs.border, clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1}.Op())
	// The panel absorbs clicks so they do not reach the backdrop.
	pa := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, panel)
	pa.Pop()
	call.Add(gtx.Ops)
}

func (u *UI) rangeModalBody(gtx C, today report.Date) D {
	m := &u.modal
	cfg := u.svc.Config()
	loc := cfg.Location()
	valid := !m.end.Value.Before(m.start.Value)
	m.start.rangeFrom, m.start.rangeTo = m.start.Value, m.end.Value
	m.end.rangeFrom, m.end.rangeTo = m.start.Value, m.end.Value

	calendar := func(title string, dp *datePicker) layout.Widget {
		return func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					l := material.Body2(u.th, title+": "+cfg.FormatDate(dp.Value.In(loc)))
					l.Color = cs.muted
					return layout.Inset{Bottom: 4}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx C) D {
					d, _ := dp.layout(gtx, u.th, today)
					return d
				}),
			)
		}
	}
	var quick []layout.FlexChild
	for i := range m.quick {
		i := i
		quick = append(quick, layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
				from, to := quickRange(i, today)
				return segButton(gtx, u.th, &m.quick[i], quickNames[i], m.start.Value == from && m.end.Value == to)
			})
		}))
	}
	summary := ""
	if valid {
		days := int(m.end.Value.In(time.UTC).Sub(m.start.Value.In(time.UTC)).Hours()/24) + 1
		unit := "days"
		if days == 1 {
			unit = "day"
		}
		summary = fmt.Sprintf("%s to %s (%d %s)", cfg.FormatDate(m.start.Value.In(loc)), cfg.FormatDate(m.end.Value.In(loc)), days, unit)
	} else {
		summary = "The start date is after the end date."
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.H6(u.th, "Custom range").Layout),
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		layout.Rigid(func(gtx C) D { return layout.Flex{}.Layout(gtx, quick...) }),
		layout.Rigid(layout.Spacer{Height: 12}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(calendar("Start", &m.start)),
				layout.Rigid(layout.Spacer{Width: 16}.Layout),
				layout.Rigid(calendar("End", &m.end)),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 12}.Layout),
		layout.Rigid(func(gtx C) D {
			l := material.Body1(u.th, summary)
			if !valid {
				l.Color = cs.err
			}
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: 12}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					l := material.Caption(u.th, "Enter applies · Esc cancels")
					l.Color = cs.muted
					return layout.Inset{Top: 10}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &m.cancel, "Cancel", false) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					if !valid {
						gtx = gtx.Disabled()
					}
					return smallButton(gtx, u.th, &m.apply, "Apply")
				}),
			)
		}),
	)
}
