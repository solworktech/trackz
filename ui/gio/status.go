package gio

import (
	"context"
	"errors"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/timefmt"
	"github.com/solworktech/trackz/internal/tray"
)

const otherStatus = "\x00other"

// statusDialog chooses a status to start ("Set Status").
type statusDialog struct {
	open          bool
	choice        widget.Enum // a preset, or otherStatus
	other         widget.Editor
	start, cancel widget.Clickable
	scrim, panel  int
}

// awayDialog is the "I'm back" message box shown while a status is set.
type awayDialog struct {
	active *report.Status // guarded by UI.mu
	busy   bool           // guarded by UI.mu: a database write is running
	back   widget.Clickable
	scrim  int
	panel  int
}

func (u *UI) openStatusDialog() {
	d := &u.statusDlg
	d.other.SingleLine, d.other.Submit = true, true
	if d.choice.Value == "" {
		d.choice.Value = report.StatusPresets[0]
	}
	d.open = true
}

// statusText is the chosen status, or "" if Other is chosen but empty.
func (d *statusDialog) statusText() string {
	if d.choice.Value == otherStatus {
		return d.other.Text()
	}
	return d.choice.Value
}

// startStatus records the chosen status and shows the "I'm back" box.
func (u *UI) startStatus() {
	d := &u.statusDlg
	text := d.statusText()
	if text == "" {
		return
	}
	d.open = false
	u.mu.Lock()
	if u.away.busy || u.away.active != nil {
		u.mu.Unlock()
		return
	}
	u.away.busy = true
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		st, err := u.svc.StartStatus(ctx, text)
		if errors.Is(err, report.ErrStatusActive) {
			// Set from another session (or left open): show that one.
			var cur *report.Status
			if cur, err = u.svc.ActiveStatus(ctx); err == nil && cur != nil {
				st = *cur
			}
		}
		u.mu.Lock()
		u.away.busy = false
		if err == nil {
			u.away.active = &st
		}
		u.mu.Unlock()
		if err != nil {
			u.setStatusAsync(true, "Cannot set the status: %v", err)
		}
		u.statusChanged()
	}()
}

// endStatus records the end of the status that is set.
func (u *UI) endStatus() {
	u.mu.Lock()
	st := u.away.active
	if st == nil || u.away.busy {
		u.mu.Unlock()
		return
	}
	u.away.busy = true
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := u.svc.EndStatus(ctx, *st)
		u.mu.Lock()
		u.away.busy = false
		if err == nil {
			u.away.active = nil
		}
		u.mu.Unlock()
		if err != nil {
			u.setStatusAsync(true, "Cannot end the status: %v", err)
		} else {
			u.setStatusAsync(false, "Welcome back: %s lasted %s", st.Text, timefmt.Duration(int64(time.Since(st.Start).Seconds())))
		}
		u.statusChanged()
	}()
}

// endStatusOnExit ends a status still set when trackz-viz quits, so it does
// not go on counting until trackz-viz is next started.
func (u *UI) endStatusOnExit() {
	u.mu.Lock()
	st := u.away.active
	u.mu.Unlock()
	if st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := u.svc.EndStatus(ctx, *st); err != nil {
		u.setStatusAsync(true, "Cannot end the status: %v", err)
	}
}

// resumeStatus shows the "I'm back" box for a status left set by a
// trackz-viz that was killed.
func (u *UI) resumeStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := u.svc.ActiveStatus(ctx)
	if err != nil {
		u.setStatusAsync(true, "Cannot read the status: %v", err)
		return
	}
	if st == nil {
		return
	}
	u.mu.Lock()
	if u.away.active == nil {
		u.away.active = st
	}
	u.mu.Unlock()
	u.statusChanged()
}

// statusChanged reloads the reports (the status counts in them) and
// updates the tray menu.
func (u *UI) statusChanged() {
	u.mu.Lock()
	u.dataGen++
	st := u.away.active
	u.mu.Unlock()
	if u.tray != nil {
		items := u.trayItems()
		if st != nil {
			items = append([]tray.Item{{Label: "I'm back (" + st.Text + ")", OnClick: u.endStatus}, {}}, items...)
		}
		if err := u.tray.SetItems(items); err != nil {
			u.setStatusAsync(true, "Cannot update the tray menu: %v", err)
		}
	}
	u.invalidate()
}

func (u *UI) activeStatus() (*report.Status, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.away.active, u.away.busy
}

// layoutStatusDialog handles and draws the "Set Status" dialog.
func (u *UI) layoutStatusDialog(gtx C) {
	d := &u.statusDlg
	if d.cancel.Clicked(gtx) || dialogClickedOutside(gtx, &d.scrim) {
		d.open = false
	}
	if d.start.Clicked(gtx) {
		u.startStatus()
	}
	for {
		ev, ok := d.other.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			d.choice.Value = otherStatus // typing chooses Other
		case widget.SubmitEvent:
			u.startStatus()
		}
	}
	if d.choice.Update(gtx) && d.choice.Value == otherStatus {
		gtx.Execute(key.FocusCmd{Tag: &d.other})
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape}, key.Filter{Name: key.NameReturn}, key.Filter{Name: key.NameEnter})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			if e.Name == key.NameEscape {
				d.open = false
			} else {
				u.startStatus()
			}
		}
	}
	if !d.open {
		return
	}
	drawDialog(gtx, &d.scrim, &d.panel, 420, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(material.H6(u.th, "Set status").Layout),
			layout.Rigid(func(gtx C) D {
				l := material.Body2(u.th, "Time from now until you are back is recorded under this status.")
				l.Color = cs.muted
				return layout.Inset{Top: 2, Bottom: 10}.Layout(gtx, l.Layout)
			}),
		}
		for _, p := range report.StatusPresets {
			children = append(children, layout.Rigid(material.RadioButton(u.th, &d.choice, p, p).Layout))
		}
		children = append(children,
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.RadioButton(u.th, &d.choice, otherStatus, "Other:").Layout),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return bordered(gtx, func(gtx C) D {
							return layout.UniformInset(6).Layout(gtx, material.Editor(u.th, &d.other, "e.g. Dentist").Layout)
						})
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						l := material.Caption(u.th, "Enter starts · Esc cancels")
						l.Color = cs.muted
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &d.cancel, "Cancel", false) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						if d.statusText() == "" {
							gtx = gtx.Disabled()
						}
						return smallButton(gtx, u.th, &d.start, "Start Now")
					}),
				)
			}),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutAwayDialog handles and draws the "I'm back" box. It cannot be
// dismissed otherwise: the status lasts until the user is back.
func (u *UI) layoutAwayDialog(gtx C) {
	d := &u.away
	st, busy := u.activeStatus()
	if st == nil {
		return
	}
	if d.back.Clicked(gtx) {
		u.endStatus()
	}
	dialogClickedOutside(gtx, &d.scrim) // swallow
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameReturn}, key.Filter{Name: key.NameEnter})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			u.endStatus()
		}
	}
	// The dialog takes the keyboard, so Enter reaches it.
	gtx.Execute(key.FocusCmd{Tag: &d.back})
	elapsed := time.Since(st.Start).Round(time.Second)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(time.Second)})
	cfg := u.svc.Config()
	drawDialog(gtx, &d.scrim, &d.panel, 420, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.H5(u.th, st.Text)
				return layout.Inset{Bottom: 6}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				l := material.Body1(u.th, "since "+timefmt.Format(st.Start.In(cfg.Location()), cfg.TimeFormat)+
					" · "+timefmt.Duration(int64(elapsed.Seconds())))
				l.Color = cs.muted
				return layout.Inset{Bottom: 18}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				if busy {
					gtx = gtx.Disabled()
				}
				b := material.Button(u.th, &d.back, "I'm back")
				b.Background, b.Color = cs.active, cs.onActive
				b.TextSize = 18
				b.Inset = layout.Inset{Left: 28, Right: 28, Top: 12, Bottom: 12}
				return b.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				l := material.Caption(u.th, "or press Enter")
				l.Color = cs.muted
				return layout.Inset{Top: 8}.Layout(gtx, l.Layout)
			}),
		)
	})
}
