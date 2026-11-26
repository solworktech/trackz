package gio

import (
	"context"
	"encoding/json"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/groups"
	"github.com/solworktech/trackz/internal/patterns"
	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/timefmt"
)

// groupEditor edits the task groups, in place of the Task Groups view: a
// list of groups (order sets precedence) and the selected group's name,
// patterns, processes and windows assigned by hand, with a preview of what
// it takes in the range. Nothing is saved until Save.
type groupEditor struct {
	open   bool
	groups []groups.Group // the draft
	sel    int            // selected group, -1 for none
	loaded int            // the group the fields hold, -1 for none

	rows                       []widget.Clickable
	add, del, up, down         widget.Clickable
	save, cancel, edit         widget.Clickable
	name, include, exclude, ps widget.Editor
	unassign                   []widget.Clickable
	err                        error // from the last Save
	body                       widget.List
	groupList                  widget.List

	// Processes with time in the range, to tick (guarded by UI.mu).
	procs        []procChoice
	procsFor     report.Range
	procsReady   bool
	procsLoading bool
	procList     widget.List
	procSearch   widget.Editor // text, or /regular expression/

	// The preview of the selected group (guarded by UI.mu).
	prevKey     string
	prev        report.GroupPreview
	prevErr     error
	prevLoading bool
	prevDue     time.Time // when to compute it, a moment after the last edit
}

type procChoice struct {
	report.Point
	toggle widget.Clickable
}

// openGroupEditor starts editing a copy of the saved task groups.
func (u *UI) openGroupEditor() {
	e := &u.groupEdit
	// From the file as it is now, with any changes made by hand.
	if err := u.svc.ReloadConfig(); err != nil {
		u.setStatus(true, "Config file: %v", err)
	}
	u.cfgGen++
	e.groups = cloneGroups(u.svc.Config().TaskGroups)
	e.sel, e.loaded, e.err = -1, -1, nil
	if len(e.groups) > 0 {
		e.sel = 0
	}
	e.name.SingleLine = true
	e.procSearch.SingleLine = true
	e.open = true
	u.mu.Lock()
	e.prevKey, e.prev, e.prevErr = "", report.GroupPreview{}, nil
	e.procsReady = false
	u.mu.Unlock()
	u.loadProcesses()
}

func cloneGroups(gs []groups.Group) []groups.Group {
	out := make([]groups.Group, len(gs))
	for i, g := range gs {
		out[i] = groups.Group{
			Name:      g.Name,
			Include:   slices.Clone(g.Include),
			Exclude:   slices.Clone(g.Exclude),
			Processes: slices.Clone(g.Processes),
			Windows:   slices.Clone(g.Windows),
		}
	}
	return out
}

// loadProcesses lists the processes with time in the range, to tick.
func (u *UI) loadProcesses() {
	e := &u.groupEdit
	rng := u.rng
	u.mu.Lock()
	if e.procsReady && e.procsFor == rng {
		u.mu.Unlock()
		return
	}
	e.procsFor, e.procsReady, e.procsLoading = rng, true, true
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		pts, err := u.svc.Processes(ctx, rng)
		if err != nil {
			u.mu.Lock()
			e.procsLoading = false
			u.mu.Unlock()
			u.setStatusAsync(true, "Cannot list processes: %v", err)
			return
		}
		for _, p := range pts {
			u.icons.prefetch(p)
		}
		choices := make([]procChoice, len(pts))
		for i, p := range pts {
			choices[i].Point = p
		}
		u.mu.Lock()
		if e.procsFor == rng {
			e.procs, e.procsLoading = choices, false
		}
		u.mu.Unlock()
		u.invalidate()
	}()
}

// syncFields moves the fields' text into the selected group, and the
// selected group into the fields when the selection changed.
func (e *groupEditor) syncFields() {
	if e.loaded != e.sel {
		e.loaded = e.sel
		g := groups.Group{}
		if e.sel >= 0 {
			g = e.groups[e.sel]
		}
		e.name.SetText(g.Name)
		e.include.SetText(strings.Join(g.Include, "\n"))
		e.exclude.SetText(strings.Join(g.Exclude, "\n"))
		e.ps.SetText(strings.Join(g.Processes, "\n"))
		return
	}
	if e.sel < 0 {
		return
	}
	g := &e.groups[e.sel]
	g.Name = strings.TrimSpace(e.name.Text())
	g.Include = patterns.Lines(e.include.Text())
	g.Exclude = patterns.Lines(e.exclude.Text())
	g.Processes = patterns.Lines(e.ps.Text())
}

// hasProcess reports whether the processes field lists p (ignoring case).
func (e *groupEditor) hasProcess(p string) bool {
	for _, l := range patterns.Lines(e.ps.Text()) {
		if strings.EqualFold(l, p) {
			return true
		}
	}
	return false
}

// toggleProcess adds p to the processes field, or removes it.
func (e *groupEditor) toggleProcess(p string) {
	lines := patterns.Lines(e.ps.Text())
	kept := slices.DeleteFunc(slices.Clone(lines), func(l string) bool { return strings.EqualFold(l, p) })
	if len(kept) == len(lines) {
		kept = append(kept, p)
	}
	e.ps.SetText(strings.Join(kept, "\n"))
}

// handleGroupEditor processes the editor's buttons.
func (u *UI) handleGroupEditor(gtx C) {
	e := &u.groupEdit
	if e.edit.Clicked(gtx) {
		u.openGroupEditor()
	}
	if !e.open {
		return
	}
	if u.view == report.ViewTaskGroups {
		u.loadProcesses() // for the range shown
	}
	e.syncFields()
	if e.cancel.Clicked(gtx) {
		e.open = false
		return
	}
	if e.save.Clicked(gtx) {
		draft := cloneGroups(e.groups)
		if _, err := u.svc.ModifyConfig(func(c config.Config) (config.Config, error) {
			c.TaskGroups = draft
			return c, groups.Validate(draft)
		}); err != nil {
			e.err = err
		} else {
			e.open = false
			u.cfgGen++
			u.setStatus(false, "Task groups saved to %s", u.svc.ConfigPath())
		}
		return
	}
	for i := range e.rows {
		if e.rows[i].Clicked(gtx) && i < len(e.groups) {
			e.sel = i
		}
	}
	switch {
	case e.add.Clicked(gtx):
		// A group still without a name is selected again rather than
		// another added.
		if i := slices.IndexFunc(e.groups, func(g groups.Group) bool { return g.Name == "" }); i >= 0 {
			e.sel = i
		} else {
			e.groups = append(e.groups, groups.Group{})
			e.sel = len(e.groups) - 1
		}
		gtx.Execute(key.FocusCmd{Tag: &e.name})
	case e.del.Clicked(gtx) && e.sel >= 0:
		e.groups = slices.Delete(e.groups, e.sel, e.sel+1)
		e.sel = min(e.sel, len(e.groups)-1)
		e.loaded = -2 // reload the fields
	case e.up.Clicked(gtx) && e.sel > 0:
		e.groups[e.sel-1], e.groups[e.sel] = e.groups[e.sel], e.groups[e.sel-1]
		e.sel--
		e.loaded = e.sel
	case e.down.Clicked(gtx) && e.sel >= 0 && e.sel < len(e.groups)-1:
		e.groups[e.sel+1], e.groups[e.sel] = e.groups[e.sel], e.groups[e.sel+1]
		e.sel++
		e.loaded = e.sel
	}
	if e.sel >= 0 {
		g := &e.groups[e.sel]
		for i := range e.unassign {
			if e.unassign[i].Clicked(gtx) && i < len(g.Windows) {
				g.Windows = slices.Delete(g.Windows, i, i+1)
				break
			}
		}
	}
	u.mu.Lock()
	procs := e.procs
	u.mu.Unlock()
	for i := range procs {
		if procs[i].toggle.Clicked(gtx) {
			e.toggleProcess(procs[i].Label)
		}
	}
	e.syncFields()
	u.ensurePreview(gtx)
}

// ensurePreview recomputes the preview of the selected group a moment
// after the draft, the range or the filter last changed.
func (u *UI) ensurePreview(gtx C) {
	e := &u.groupEdit
	if e.sel < 0 {
		return
	}
	name := e.groups[e.sel].Name
	if name == "" {
		return // nothing to preview until it has a name
	}
	// Groups not named yet take nothing (and would make the draft invalid).
	draft := slices.DeleteFunc(cloneGroups(e.groups), func(g groups.Group) bool { return g.Name == "" })
	b, err := json.Marshal(draft)
	if err != nil {
		return
	}
	key := string(b) + "\x00" + name + "\x00" + u.resultKey()
	u.mu.Lock()
	same := key == e.prevKey
	u.mu.Unlock()
	if same {
		return
	}
	e.err = nil // about an earlier draft
	if e.prevDue.IsZero() {
		e.prevDue = gtx.Now.Add(300 * time.Millisecond)
	}
	if gtx.Now.Before(e.prevDue) {
		gtx.Execute(op.InvalidateCmd{At: e.prevDue})
		return
	}
	e.prevDue = time.Time{}
	u.mu.Lock()
	e.prevKey, e.prevLoading = key, true
	u.mu.Unlock()
	rng, filter := u.rng, u.filter
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		prev, err := u.svc.PreviewGroup(ctx, draft, name, rng, filter)
		for _, p := range prev.Windows {
			u.icons.prefetch(p)
		}
		for _, p := range prev.Missed {
			u.icons.prefetch(p)
		}
		u.mu.Lock()
		if e.prevKey == key {
			e.prev, e.prevErr, e.prevLoading = prev, err, false
		}
		u.mu.Unlock()
		u.invalidate()
	}()
}

// layoutGroupEditor draws the editor.
func (u *UI) layoutGroupEditor(gtx C) D {
	e := &u.groupEdit
	top := func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H6(u.th, "Edit task groups").Layout),
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
			layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &e.cancel, "Cancel", false) }),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &e.save, "Save") }),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(top),
		layout.Rigid(func(gtx C) D {
			if e.err == nil {
				return D{}
			}
			l := material.Body2(u.th, "Not saved: "+e.err.Error())
			l.Color = cs.err
			return layout.Inset{Top: 6}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(230), gtx.Dp(230)
					return u.layoutGroupList(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Flexed(1, u.layoutGroupForm),
			)
		}),
	)
}

// layoutGroupList is the list of groups with the buttons to add, delete
// and reorder them.
func (u *UI) layoutGroupList(gtx C) D {
	e := &u.groupEdit
	if len(e.rows) != len(e.groups) {
		e.rows = make([]widget.Clickable, len(e.groups))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, "A window goes to the group it is assigned to by hand, else to the first one (in this order) that takes it.")
			l.Color = cs.muted
			return layout.Inset{Bottom: 6}.Layout(gtx, l.Layout)
		}),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return bordered(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				if len(e.groups) == 0 {
					l := material.Body2(u.th, "No groups yet: Add one.")
					l.Color = cs.muted
					return layout.UniformInset(8).Layout(gtx, l.Layout)
				}
				e.groupList.Axis = layout.Vertical
				return material.List(u.th, &e.groupList).Layout(gtx, len(e.groups), func(gtx C, i int) D {
					return e.rows[i].Layout(gtx, func(gtx C) D {
						pointer.CursorPointer.Add(gtx.Ops)
						h := gtx.Dp(30)
						if i == e.sel {
							fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), cs.active)
						} else if e.rows[i].Hovered() {
							fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), cs.header)
						}
						col := cs.fg
						if i == e.sel {
							col = cs.onActive
						}
						name := strings.TrimSpace(e.groups[i].Name)
						if name == "" {
							name = "(no name)"
						}
						gtx.Constraints.Min.Y = h
						return layout.Inset{Left: 8, Right: 8, Top: 6}.Layout(gtx, func(gtx C) D {
							return truncLabel(gtx, u.th, 14, col, strconv.Itoa(i+1)+". "+name)
						})
					})
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &e.add, "Add") }),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &e.del, "Delete", false) }),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &e.up, "↑", false) }),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &e.down, "↓", false) }),
			)
		}),
	)
}

// layoutGroupForm is the selected group's fields and preview, scrolling.
func (u *UI) layoutGroupForm(gtx C) D {
	e := &u.groupEdit
	if e.sel < 0 {
		l := material.Body1(u.th, "Add a group, or select one to edit.")
		l.Color = cs.muted
		return l.Layout(gtx)
	}
	g := e.groups[e.sel]
	caption := func(txt string) layout.Widget {
		return func(gtx C) D {
			l := material.Body2(u.th, txt)
			l.Color = cs.muted
			return layout.Inset{Bottom: 4}.Layout(gtx, l.Layout)
		}
	}
	items := []layout.Widget{
		func(gtx C) D {
			return field(gtx, u.th, "Name", &e.name, "e.g. GenOrg", 320)
		},
		func(gtx C) D {
			return layout.Inset{Top: 12}.Layout(gtx, caption("Window titles, one pattern per line: whole words (ignoring case), or /a regular expression/."))
		},
		func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return u.patternField(gtx, "Titles with any of", &e.include, "e.g. GenOrg\nJim Smith\n/widget-(api|ui)/")
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return u.patternField(gtx, "But not with any of (also for processes)", &e.exclude, "e.g. newsletter")
				}),
			)
		},
		func(gtx C) D {
			return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return u.patternFieldH(gtx, "Processes whose windows all belong to it", &e.ps, "e.g. genorg-vpn", procBoxHeight)
					}),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, u.layoutProcChoices),
				)
			})
		},
		func(gtx C) D { return layout.Inset{Top: 12}.Layout(gtx, caption("Windows assigned by hand")) },
		func(gtx C) D { return u.layoutAssigned(gtx, g) },
		func(gtx C) D { return layout.Inset{Top: 12}.Layout(gtx, u.layoutGroupPreview) },
	}
	e.body.Axis = layout.Vertical
	return material.List(u.th, &e.body).Layout(gtx, len(items), func(gtx C, i int) D {
		return layout.Inset{Right: 12}.Layout(gtx, items[i])
	})
}

// Heights in the processes section: the list shows 10 processes, and the
// processes field is as high as the search box and the list together.
const (
	procRowHeight  unit.Dp = 24
	procListHeight         = 10*procRowHeight + 4
	procSearchH    unit.Dp = 36
	procBoxHeight          = procListHeight + procSearchH + 6
)

// layoutProcChoices lists the processes with time in the range that match
// the search box, ticked when the processes field lists them; a click adds
// or removes one.
func (u *UI) layoutProcChoices(gtx C) D {
	e := &u.groupEdit
	u.mu.Lock()
	all, loading := e.procs, e.procsLoading
	u.mu.Unlock()
	match, searchErr := patterns.Search(e.procSearch.Text())
	var shown []int // indexes in all
	if searchErr == nil {
		for i := range all {
			if match(all[i].Label) {
				shown = append(shown, i)
			}
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, "Processes in this range (click to add or remove)")
			l.Color = cs.muted
			return layout.Inset{Bottom: 4}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(procSearchH), gtx.Dp(procSearchH)
			return bordered(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				return layout.Inset{Left: 8, Right: 8, Top: 8}.Layout(gtx, func(gtx C) D {
					ed := material.Editor(u.th, &e.procSearch, "Search: text, or /regular expression/")
					ed.TextSize = 14
					return editorField(gtx, ed)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx C) D {
			h := gtx.Dp(procListHeight)
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = h, h
			return bordered(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				if loading && len(all) == 0 {
					return centredThrobber(gtx, u.th)
				}
				note := ""
				switch {
				case searchErr != nil:
					note = searchErr.Error()
				case len(all) == 0:
					note = "None."
				case len(shown) == 0:
					note = "No process matches."
				}
				if note != "" {
					l := material.Body2(u.th, note)
					l.Color = cs.muted
					if searchErr != nil {
						l.Color = cs.err
					}
					return layout.UniformInset(8).Layout(gtx, l.Layout)
				}
				e.procList.Axis = layout.Vertical
				return material.List(u.th, &e.procList).Layout(gtx, len(shown), func(gtx C, i int) D {
					p := &all[shown[i]]
					return p.toggle.Layout(gtx, func(gtx C) D {
						pointer.CursorPointer.Add(gtx.Ops)
						h := gtx.Dp(procRowHeight)
						gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = h, h
						return layout.W.Layout(gtx, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.Inset{Left: 6, Right: 6}.Layout(gtx, func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D { return checkBox(gtx, e.hasProcess(p.Label)) }),
									layout.Rigid(layout.Spacer{Width: 6}.Layout),
									layout.Rigid(func(gtx C) D { return layoutIcon(gtx, u.icons.get(p.Point), 16) }),
									layout.Rigid(layout.Spacer{Width: 6}.Layout),
									layout.Flexed(1, func(gtx C) D { return truncLabel(gtx, u.th, 13, cs.fg, p.Label) }),
									layout.Rigid(func(gtx C) D {
										l := material.Label(u.th, 12, timefmt.Duration(int64(p.Value)))
										l.Color = cs.muted
										return l.Layout(gtx)
									}),
								)
							})
						})
					})
				})
			})
		}),
	)
}

// layoutAssigned lists the windows assigned to g by hand, each with a
// button to remove it, noting those with no time in the range (perhaps
// stale: assignments use the title as shown, so changing the title rules
// can leave them matching nothing).
func (u *UI) layoutAssigned(gtx C, g groups.Group) D {
	e := &u.groupEdit
	u.mu.Lock()
	taken := e.prev.Windows
	u.mu.Unlock()
	if len(g.Windows) == 0 {
		l := material.Body2(u.th, "None.")
		l.Color = cs.muted
		return l.Layout(gtx)
	}
	if len(e.unassign) < len(g.Windows) {
		e.unassign = make([]widget.Clickable, len(g.Windows))
	}
	children := make([]layout.FlexChild, len(g.Windows))
	for i, w := range g.Windows {
		children[i] = layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &e.unassign[i], "Remove", false) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						return layoutIcon(gtx, u.icons.get(report.Point{Process: w.Process}), 16)
					}),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return truncLabel(gtx, u.th, 14, cs.fg, w.Title+"  ("+w.Process+")")
					}),
					layout.Rigid(func(gtx C) D {
						if slices.ContainsFunc(taken, func(p report.Point) bool { return p.Process == w.Process && p.Label == w.Title }) {
							return D{}
						}
						l := material.Label(u.th, 13, "no time in this range")
						l.Color = cs.muted
						return layout.Inset{Left: 8}.Layout(gtx, l.Layout)
					}),
				)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutGroupPreview shows the windows the selected group takes in the
// range, and those it matches but does not take, and why.
func (u *UI) layoutGroupPreview(gtx C) D {
	e := &u.groupEdit
	if e.sel >= 0 && e.groups[e.sel].Name == "" {
		l := material.Body2(u.th, "Name the group to see what it takes.")
		l.Color = cs.muted
		return layout.Inset{Top: 8}.Layout(gtx, l.Layout)
	}
	u.mu.Lock()
	prev, err, loading := e.prev, e.prevErr, e.prevLoading
	u.mu.Unlock()
	title := func(txt string, busy bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						l := material.Body2(u.th, txt)
						l.Color = cs.muted
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						if !busy {
							return D{}
						}
						return inlineThrobber(gtx, u.th)
					}),
				)
			})
		})
	}
	row := func(p report.Point, note string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 3}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return layoutIcon(gtx, u.icons.get(p), 16) }),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return truncLabel(gtx, u.th, 13, cs.fg, p.Label+"  ("+p.Process+")")
					}),
					layout.Rigid(func(gtx C) D {
						l := material.Label(u.th, 13, note)
						l.Color = cs.muted
						return layout.Inset{Left: 8}.Layout(gtx, l.Layout)
					}),
				)
			})
		})
	}
	var children []layout.FlexChild
	children = append(children, title("Windows this group takes, "+u.svc.RangeLabel(u.rng), loading))
	switch {
	case err != nil:
		children = append(children, layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, err.Error())
			l.Color = cs.err
			return l.Layout(gtx)
		}))
	case len(prev.Windows) == 0:
		children = append(children, layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, "None in this range.")
			l.Color = cs.muted
			return l.Layout(gtx)
		}))
	}
	if err == nil {
		for _, p := range prev.Windows {
			children = append(children, row(p, timefmt.Duration(int64(p.Value))))
		}
		if len(prev.Missed) > 0 {
			children = append(children, title("Matched, but not in this group", false))
			for _, p := range prev.Missed {
				children = append(children, row(p, p.Detail+" · "+timefmt.Duration(int64(p.Value))))
			}
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
