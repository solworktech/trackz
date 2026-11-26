package gio

import (
	"image"

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

	"github.com/solworktech/trackz/internal/groups"
	"github.com/solworktech/trackz/internal/report"
)

// groupPicker is the "Group: …" drop-down in a window's timeline: it shows
// the window's task group and why it is in it, and assigns it by hand to
// another (or undoes that).
type groupPicker struct {
	btn   widget.Clickable
	open  bool
	items []widget.Clickable // the groups, then "not assigned"
	scrim int                // presses outside the menu close it
	panel int                // the menu takes its presses
}

// pickerMenuWidth is the menu's width.
const pickerMenuWidth unit.Dp = 280

// layoutGroupPicker draws the drop-down for the window of it (process and
// title) and handles its choices.
func (u *UI) layoutGroupPicker(gtx C, p *groupPicker, it report.Item) D {
	names := u.svc.GroupNames()
	if len(names) == 0 {
		return D{}
	}
	cur, how := u.svc.WindowGroup(it.Process, it.Title)
	if len(p.items) != len(names)+1 {
		p.items = make([]widget.Clickable, len(names)+1)
	}
	if p.btn.Clicked(gtx) {
		p.open = !p.open
	}
	for i := range p.items {
		if !p.items[i].Clicked(gtx) {
			continue
		}
		p.open = false
		group := "" // the last item: not assigned
		if i < len(names) {
			group = names[i]
		}
		if group == cur && how == groups.ByHand {
			break
		}
		if err := u.svc.AssignWindow(it.Process, it.Title, group); err != nil {
			u.setStatus(true, "Not assigned: %v", err)
			break
		}
		u.cfgGen++ // the views change
		if group == "" {
			u.setStatus(false, "%s is no longer assigned to a task group", it.Title)
		} else {
			u.setStatus(false, "%s assigned to %s", it.Title, group)
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.scrim, Kinds: pointer.Press}, key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		switch e := ev.(type) {
		case pointer.Event:
			p.open = false
		case key.Event:
			if e.State == key.Press && p.open {
				p.open = false
			}
		}
	}

	label := "Group: none"
	if cur != "" {
		label = "Group: " + cur + " (" + how.String() + ")"
	}
	d := segButton(gtx, u.th, &p.btn, label+"  ▾", p.open)
	if !p.open {
		return d
	}
	// The menu, below the button, over everything else drawn this frame.
	m := op.Record(gtx.Ops)
	// Presses anywhere else close it.
	far := gtx.Dp(100000)
	area := clip.Rect{Min: image.Pt(-far, -far), Max: image.Pt(far, far)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.scrim)
	area.Pop()
	at := op.Offset(image.Pt(d.Size.X-gtx.Dp(pickerMenuWidth), d.Size.Y+gtx.Dp(4))).Push(gtx.Ops)
	mg := gtx
	mg.Constraints = layout.Exact(image.Pt(gtx.Dp(pickerMenuWidth), 0))
	mg.Constraints.Max.Y = gtx.Dp(400)
	mrec := op.Record(gtx.Ops)
	md := u.pickerMenu(mg, p, names, cur, how)
	menu := mrec.Stop()
	box := image.Rectangle{Max: md.Size}
	rr := clip.UniformRRect(box, gtx.Dp(6))
	paint.FillShape(gtx.Ops, cs.panel, rr.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cs.border, clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1}.Op())
	pa := clip.Rect(box).Push(gtx.Ops)
	event.Op(gtx.Ops, &p.panel)
	pa.Pop()
	menu.Add(gtx.Ops)
	at.Pop()
	op.Defer(gtx.Ops, m.Stop())
	return d
}

// pickerMenu lists the groups (the window's marked) and, when it is
// assigned by hand, a way back to its patterns deciding.
func (u *UI) pickerMenu(gtx C, p *groupPicker, names []string, cur string, how groups.How) D {
	gtx.Constraints.Min.Y = 0
	item := func(i int, txt string, on bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return p.items[i].Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				h := gtx.Dp(30)
				if p.items[i].Hovered() {
					fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), cs.header)
				}
				gtx.Constraints.Min.Y = h
				return layout.Inset{Left: 10, Right: 10, Top: 6}.Layout(gtx, func(gtx C) D {
					mark := "    "
					if on {
						mark = "✓  "
					}
					return truncLabel(gtx, u.th, 14, cs.fg, mark+txt)
				})
			})
		})
	}
	children := []layout.FlexChild{layout.Rigid(func(gtx C) D {
		l := material.Caption(u.th, "Assign this window to")
		l.Color = cs.muted
		return layout.Inset{Left: 10, Right: 10, Top: 8, Bottom: 4}.Layout(gtx, l.Layout)
	})}
	for i, n := range names {
		children = append(children, item(i, n, n == cur))
	}
	if how == groups.ByHand {
		children = append(children, item(len(names), "Not assigned (patterns decide)", false))
	}
	children = append(children, layout.Rigid(layout.Spacer{Height: 6}.Layout))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
