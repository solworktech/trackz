package gio

import (
	"errors"
	"image"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/queries"
)

// queryPicker is a select box listing the saved queries, with a filter.
type queryPicker struct {
	open     bool
	btn      widget.Clickable
	filter   widget.Editor
	list     widget.List
	items    []queries.Query // all saved queries, loaded when opened
	shown    []queries.Query // items matching the filter
	clicks   []widget.Clickable
	selected int    // highlighted row in shown
	current  string // name of the query last loaded into the editor
	outside  int    // event tags
	panelTag int
}

// saveDialog asks for the name to save the query under.
type saveDialog struct {
	open         bool
	name         widget.Editor
	save, cancel widget.Clickable
	confirm      bool // name exists: the next Save overwrites
	err          string
	scrim, panel int
	focusPending bool
}

func (u *UI) openPicker() {
	p := &u.picker
	list, err := u.svc.SavedQueries().List()
	if err != nil {
		u.setStatus(true, "Cannot read saved queries: %v", err)
	}
	p.items = list
	p.filter.SingleLine, p.filter.Submit = true, true
	p.filter.SetText("")
	p.selected = 0
	p.open = true
	p.applyFilter()
}

func (p *queryPicker) applyFilter() {
	p.shown = queries.Filter(p.items, p.filter.Text())
	if len(p.clicks) < len(p.shown) {
		p.clicks = make([]widget.Clickable, len(p.shown))
	}
	if p.selected >= len(p.shown) {
		p.selected = max(len(p.shown)-1, 0)
	}
}

func (u *UI) pickQuery(q queries.Query) {
	sql, err := u.svc.SavedQueries().Load(q.Name)
	if err != nil {
		u.setStatus(true, "Cannot load %q: %v", q.Name, err)
		return
	}
	u.queryEd.ed.SetText(strings.TrimRight(sql, "\n"))
	u.picker.current = q.Name
	u.picker.open = false
	u.setStatus(false, "Loaded %q; edit it if needed, then Run query", q.Name)
}

// handlePicker processes the picker's input; call before laying it out.
func (u *UI) handlePicker(gtx C) {
	p := &u.picker
	if p.btn.Clicked(gtx) {
		if p.open {
			p.open = false
		} else {
			u.openPicker()
			gtx.Execute(key.FocusCmd{Tag: &p.filter})
		}
	}
	if !p.open {
		return
	}
	for {
		ev, ok := p.filter.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			p.selected = 0
			p.applyFilter()
		case widget.SubmitEvent:
			if p.selected < len(p.shown) {
				u.pickQuery(p.shown[p.selected])
				return
			}
		}
	}
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &p.filter, Name: key.NameUpArrow},
			key.Filter{Focus: &p.filter, Name: key.NameDownArrow},
			key.Filter{Name: key.NameEscape},
		)
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case key.NameUpArrow:
			p.selected = max(p.selected-1, 0)
		case key.NameDownArrow:
			p.selected = min(p.selected+1, max(len(p.shown)-1, 0))
		case key.NameEscape:
			p.open = false
			return
		}
	}
	for i := range p.shown {
		if p.clicks[i].Clicked(gtx) {
			u.pickQuery(p.shown[i])
			return
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.outside, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			p.open = false
		}
	}
}

// layoutPicker draws the select box; when open, its drop-down list is
// deferred so it floats above the rest of the window.
func (u *UI) layoutPicker(gtx C) D {
	p := &u.picker
	label := "Saved queries…"
	if p.current != "" {
		label = p.current
	}
	gtx.Constraints.Min.X = gtx.Dp(280)
	gtx.Constraints.Max.X = gtx.Constraints.Min.X
	dims := segButton(gtx, u.th, &p.btn, label+"  ▾", p.open)
	if !p.open {
		return dims
	}

	rec := op.Record(gtx.Ops)
	// Clicks anywhere outside the list close it.
	huge := image.Rect(-1<<15, -1<<15, 1<<15, 1<<15)
	area := clip.Rect(huge).Push(gtx.Ops)
	event.Op(gtx.Ops, &p.outside)
	area.Pop()

	op.Offset(image.Pt(0, dims.Size.Y+gtx.Dp(2))).Add(gtx.Ops)
	pgtx := gtx
	pgtx.Constraints = layout.Exact(image.Pt(gtx.Dp(360), gtx.Dp(260)))
	size := pgtx.Constraints.Max
	rr := clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(6))
	paint.FillShape(gtx.Ops, cs.panel, rr.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, cs.border, clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1}.Op())
	pa := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.panelTag)
	pa.Pop()
	layout.UniformInset(8).Layout(pgtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return bordered(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
						e := material.Editor(u.th, &p.filter, "Filter by name…")
						e.TextSize = 14
						return editorField(gtx, e)
					})
				})
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Flexed(1, func(gtx C) D {
				if len(p.shown) == 0 {
					msg := "No saved queries yet. Use Save query to add one."
					if len(p.items) > 0 {
						msg = "No saved query matches the filter."
					}
					l := material.Body2(u.th, msg)
					l.Color = cs.muted
					return l.Layout(gtx)
				}
				p.list.Axis = layout.Vertical
				return material.List(u.th, &p.list).Layout(gtx, len(p.shown), func(gtx C, i int) D {
					return material.Clickable(gtx, &p.clicks[i], func(gtx C) D {
						h := gtx.Dp(28)
						if i == p.selected || p.clicks[i].Hovered() {
							fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), cs.editorSel)
						}
						gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
						return layout.Inset{Left: 8, Top: 5}.Layout(gtx, func(gtx C) D {
							return truncLabel(gtx, u.th, 14, cs.fg, p.shown[i].Name)
						})
					})
				})
			}),
		)
	})
	op.Defer(gtx.Ops, rec.Stop())
	return dims
}

func (u *UI) openSaveDialog() {
	d := &u.saveDlg
	if strings.TrimSpace(u.queryEd.ed.Text()) == "" {
		u.setStatus(true, "Nothing to save: the query is empty")
		return
	}
	d.name.SingleLine, d.name.Submit = true, true
	d.name.SetText(u.picker.current)
	d.name.SetCaret(d.name.Len(), 0)
	d.confirm, d.err = false, ""
	d.open, d.focusPending = true, true
}

func (u *UI) doSave() {
	d := &u.saveDlg
	name := strings.TrimSpace(d.name.Text())
	if err := queries.ValidateName(name); err != nil {
		d.err = err.Error()
		return
	}
	store := u.svc.SavedQueries()
	err := store.Save(name, u.queryEd.ed.Text(), d.confirm)
	if errors.Is(err, queries.ErrExists) {
		d.confirm = true
		d.err = "A query named \"" + name + "\" already exists. Save again to overwrite it."
		return
	}
	if err != nil {
		d.err = err.Error()
		return
	}
	d.open = false
	u.picker.current = name
	u.setStatus(false, "Saved query %q in %s", name, store.Dir)
}

// layoutSaveDialog handles and draws the "Save query" dialog.
func (u *UI) layoutSaveDialog(gtx C) {
	d := &u.saveDlg
	if d.cancel.Clicked(gtx) || dialogClickedOutside(gtx, &d.scrim) {
		d.open = false
	}
	if d.save.Clicked(gtx) {
		u.doSave()
	}
	for {
		ev, ok := d.name.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			u.doSave()
		case widget.ChangeEvent:
			d.confirm, d.err = false, ""
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			d.open = false
		}
	}
	if !d.open {
		return
	}
	if d.focusPending {
		gtx.Execute(key.FocusCmd{Tag: &d.name})
		d.focusPending = false
	}
	dir := u.svc.SavedQueries().Dir
	drawDialog(gtx, &d.scrim, &d.panel, 480, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(material.H6(u.th, "Save query").Layout),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx C) D {
				return field(gtx, u.th, "Name", &d.name, "e.g. Billing - Project X", 440)
			}),
			layout.Rigid(func(gtx C) D {
				l := material.Caption(u.th, "Saved as "+dir+"/<name>.sql")
				l.Color = cs.muted
				return layout.Inset{Top: 4}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				if d.err == "" {
					return D{}
				}
				l := material.Body2(u.th, d.err)
				l.Color = cs.err
				return layout.Inset{Top: 8}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				label := "Save"
				if d.confirm {
					label = "Overwrite"
				}
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						l := material.Caption(u.th, "Enter saves · Esc cancels")
						l.Color = cs.muted
						return layout.Inset{Top: 10}.Layout(gtx, l.Layout)
					}),
					layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &d.cancel, "Cancel", false) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &d.save, label) }),
				)
			}),
		)
	})
}
