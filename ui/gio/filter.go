package gio

import (
	"context"
	"image"
	"runtime"
	"slices"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/patterns"
	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/timefmt"
)

// filterDialog edits the filter applied to the predefined views.
type filterDialog struct {
	open                 bool
	include, exclude     widget.Editor // title patterns, one per line
	err                  error         // an invalid pattern, from Apply
	apps                 []filterApp   // guarded by UI.mu while loading
	loading              bool          // guarded by UI.mu
	loadErr              error         // guarded by UI.mu
	list                 widget.List
	all, none            widget.Clickable
	apply, cancel, clear widget.Clickable
	scrim, panel         int
	excludedWhenOpened   []string
	appsFor              report.Range
	focus                bool // focus the include editor on the next frame
}

type filterApp struct {
	report.Point
	shown widget.Bool // checked: included
}

func (u *UI) openFilterDialog() {
	d := &u.filterDlg
	for _, e := range []struct {
		ed *widget.Editor
		ps []string
	}{{&d.include, u.filter.Include}, {&d.exclude, u.filter.Exclude}} {
		e.ed.SetText(strings.Join(e.ps, "\n"))
		e.ed.SetCaret(e.ed.Len(), e.ed.Len())
	}
	d.err = nil
	excluded := slices.Clone(u.filter.ExcludeApps)
	d.excludedWhenOpened = excluded
	d.open, d.focus = true, true
	rng := u.rng
	d.appsFor = rng
	u.mu.Lock()
	d.apps, d.loading, d.loadErr = nil, true, nil
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		pts, err := u.svc.Apps(ctx, rng)
		for _, p := range pts {
			u.icons.prefetch(p)
		}
		apps := make([]filterApp, 0, len(pts))
		for _, p := range pts {
			apps = append(apps, filterApp{Point: p})
		}
		// Apps excluded earlier stay listed (and can be re-included) even
		// when they have no time in this range.
		for _, name := range excluded {
			if !slices.ContainsFunc(apps, func(a filterApp) bool { return a.App == name }) {
				apps = append(apps, filterApp{Point: report.Point{Label: name, App: name}})
			}
		}
		for i := range apps {
			apps[i].shown.Value = !slices.Contains(excluded, apps[i].App)
		}
		u.mu.Lock()
		if d.open && d.appsFor == rng {
			d.apps, d.loading, d.loadErr = apps, false, err
		}
		u.mu.Unlock()
		u.invalidate()
	}()
}

func (u *UI) applyFilter(f report.Filter) {
	u.filter = f
	u.filterDlg.open = false
	u.ensure(true)
}

func (u *UI) applyFilterDialog() {
	d := &u.filterDlg
	f := report.Filter{
		Include: patterns.Lines(d.include.Text()),
		Exclude: patterns.Lines(d.exclude.Text()),
	}
	if d.err = f.Validate(); d.err != nil {
		return // shown in the dialog
	}
	u.mu.Lock()
	if d.loading || d.loadErr != nil {
		// The list is not there to edit: keep the exclusions as they were.
		f.ExcludeApps = slices.Clone(d.excludedWhenOpened)
	}
	for i := range d.apps {
		if !d.apps[i].shown.Value {
			f.ExcludeApps = append(f.ExcludeApps, d.apps[i].App)
		}
	}
	u.mu.Unlock()
	u.applyFilter(f)
}

// layoutFilterDialog handles and draws the filter dialog.
func (u *UI) layoutFilterDialog(gtx C) {
	d := &u.filterDlg
	if d.cancel.Clicked(gtx) || dialogClickedOutside(gtx, &d.scrim) {
		d.open = false
	}
	if d.clear.Clicked(gtx) {
		u.applyFilter(report.Filter{})
	}
	if d.apply.Clicked(gtx) {
		u.applyFilterDialog()
	}
	setAll := func(v bool) {
		u.mu.Lock()
		for i := range d.apps {
			d.apps[i].shown.Value = v
		}
		u.mu.Unlock()
	}
	if d.all.Clicked(gtx) {
		setAll(true)
	}
	if d.none.Clicked(gtx) {
		setAll(false)
	}
	for _, ed := range []*widget.Editor{&d.include, &d.exclude} {
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.ChangeEvent); ok {
				d.err = nil
			}
		}
	}
	// Enter starts a new line in the pattern fields: Ctrl+Enter (Cmd on
	// macOS) applies, as does Enter elsewhere.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape},
			key.Filter{Name: key.NameReturn, Optional: key.ModShortcut},
			key.Filter{Name: key.NameEnter, Optional: key.ModShortcut})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			if e.Name == key.NameEscape {
				d.open = false
			} else {
				u.applyFilterDialog()
			}
		}
	}
	if !d.open {
		return
	}
	if d.focus {
		gtx.Execute(key.FocusCmd{Tag: &d.include})
		d.focus = false
	}
	drawDialog(gtx, &d.scrim, &d.panel, 560, u.filterDialogBody)
}

func (u *UI) filterDialogBody(gtx C) D {
	d := &u.filterDlg
	u.mu.Lock()
	loading, loadErr, n := d.loading, d.loadErr, len(d.apps)
	u.mu.Unlock()
	caption := func(txt string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, txt)
			l.Color = cs.muted
			return layout.Inset{Bottom: 4}.Layout(gtx, l.Layout)
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.H6(u.th, "Filter").Layout),
		layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, "Applies to every view except Custom Query, for "+u.svc.RangeLabel(d.appsFor)+".")
			l.Color = cs.muted
			return layout.Inset{Top: 2, Bottom: 12}.Layout(gtx, l.Layout)
		}),
		caption("Window titles, one pattern per line: whole words (ignoring case), or /a regular expression/."),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return u.patternField(gtx, "With any of (empty: all titles)", &d.include, "e.g. trackz\ninvoice")
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return u.patternField(gtx, "Without any of", &d.exclude, "e.g. /draft|newsletter/")
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			if d.err == nil {
				return D{}
			}
			l := material.Body2(u.th, d.err.Error())
			l.Color = cs.err
			return layout.Inset{Top: 6}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(layout.Spacer{Height: 14}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					l := material.Body2(u.th, "Applications (untick to exclude)")
					l.Color = cs.muted
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &d.all, "All", false) }),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &d.none, "None", false) }),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(300))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return bordered(gtx, func(gtx C) D {
				switch {
				case loadErr != nil:
					l := material.Body2(u.th, "Cannot list applications: "+loadErr.Error())
					l.Color = cs.err
					return layout.UniformInset(8).Layout(gtx, l.Layout)
				case loading:
					return layout.UniformInset(8).Layout(gtx, func(gtx C) D { return throbber(gtx, u.th, 24) })
				case n == 0:
					return layout.UniformInset(8).Layout(gtx, material.Body2(u.th, "No applications in this range.").Layout)
				}
				d.list.Axis = layout.Vertical
				return material.List(u.th, &d.list).Layout(gtx, n, func(gtx C, i int) D {
					u.mu.Lock()
					a := &d.apps[i]
					u.mu.Unlock()
					return u.filterAppRow(gtx, a)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: 14}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &d.clear, "Clear filter", false) }),
				layout.Flexed(1, func(gtx C) D {
					l := material.Caption(u.th, shortcutName+"+Enter applies · Esc cancels")
					l.Color = cs.muted
					return layout.Inset{Left: 12}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &d.cancel, "Cancel", false) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &d.apply, "Apply") }),
			)
		}),
	)
}

// shortcutName is the key of key.ModShortcut.
var shortcutName = map[bool]string{false: "Ctrl", true: "Cmd"}[runtime.GOOS == "darwin"]

// patternField is a labelled editor for a list of patterns, a few lines
// high (it scrolls beyond).
func (u *UI) patternField(gtx C, label string, ed *widget.Editor, hint string) D {
	return u.patternFieldH(gtx, label, ed, hint, 88)
}

// patternFieldH is patternField with the editor h high.
func (u *UI) patternFieldH(gtx C, label string, ed *widget.Editor, hint string, h unit.Dp) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, label)
			l.Color = cs.muted
			return layout.Inset{Bottom: 4}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(h), gtx.Dp(h)
			return bordered(gtx, func(gtx C) D {
				return layout.UniformInset(8).Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min = gtx.Constraints.Max
					e := material.Editor(u.th, ed, hint)
					e.TextSize = 14
					return editorField(gtx, e)
				})
			})
		}),
	)
}

// filterAppRow is one application: the whole row toggles its checkbox.
func (u *UI) filterAppRow(gtx C, a *filterApp) D {
	return a.shown.Layout(gtx, func(gtx C) D {
		return layout.Inset{Left: 8, Right: 8, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return checkBox(gtx, a.shown.Value) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return layoutIcon(gtx, u.icons.get(a.Point), 18) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Flexed(1, func(gtx C) D {
					col := cs.fg
					if !a.shown.Value {
						col = cs.muted
					}
					return truncLabel(gtx, u.th, 14, col, a.Label)
				}),
				layout.Rigid(func(gtx C) D {
					txt := "–"
					if a.Value > 0 {
						txt = timefmt.Duration(int64(a.Value))
					}
					l := material.Label(u.th, 13, txt)
					l.Color = cs.muted
					return layout.Inset{Left: 8}.Layout(gtx, l.Layout)
				}),
			)
		})
	})
}

// checkBox draws a check box (ticked or not) without handling input.
func checkBox(gtx C, checked bool) D {
	sz := gtx.Dp(16)
	r := image.Rectangle{Max: image.Pt(sz, sz)}
	rr := clip.UniformRRect(r, gtx.Dp(3))
	if checked {
		paint.FillShape(gtx.Ops, cs.active, rr.Op(gtx.Ops))
		var p clip.Path
		p.Begin(gtx.Ops)
		s := float32(sz)
		p.MoveTo(f32.Pt(0.22*s, 0.52*s))
		p.LineTo(f32.Pt(0.42*s, 0.72*s))
		p.LineTo(f32.Pt(0.78*s, 0.30*s))
		paint.FillShape(gtx.Ops, cs.onActive, clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(2))}.Op())
	} else {
		paint.FillShape(gtx.Ops, cs.border, clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(gtx.Dp(unit.Dp(1.5)))}.Op())
	}
	return D{Size: r.Max}
}
