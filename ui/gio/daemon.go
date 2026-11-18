package gio

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/config"
)

// daemonCheck tracks whether trackz is recording to the viewer's database.
type daemonCheck struct {
	known   bool // a check has completed
	running bool
	err     error
	dialog  bool // the message box is open
	ok      widget.Clickable
	scrim   int
	panel   int
}

const daemonCheckEvery = 30 * time.Second

// watchDaemon checks at start-up and every daemonCheckEvery. The message
// box opens on the first failed check and whenever trackz stops; the status
// bar shows the problem for as long as it lasts.
func (u *UI) watchDaemon(ctx context.Context) {
	t := time.NewTicker(daemonCheckEvery)
	defer t.Stop()
	for {
		st, err := u.svc.RecorderStatus()
		u.mu.Lock()
		d := &u.daemon
		wasRunning := d.running || !d.known
		d.known, d.running, d.err = true, st.Running, err
		if !st.Running && wasRunning {
			d.dialog = true
		}
		u.mu.Unlock()
		u.invalidate()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (u *UI) daemonState() daemonCheck {
	u.mu.Lock()
	defer u.mu.Unlock()
	return daemonCheck{known: u.daemon.known, running: u.daemon.running, err: u.daemon.err, dialog: u.daemon.dialog}
}

func startHint() string {
	switch runtime.GOOS {
	case "linux":
		return "Start it with \"systemctl --user start trackz\" (or run \"trackz run\")."
	case "windows":
		return "Start trackz.exe (see packaging\\windows\\install-autostart.ps1 to start it at logon)."
	case "darwin":
		return "Start it with \"launchctl load ~/Library/LaunchAgents/org.solworktech.trackz.plist\" (or run \"trackz run\")."
	}
	return "Start it with \"trackz run\"."
}

// daemonMessage is the status bar text while trackz is not recording.
func (u *UI) daemonMessage(d daemonCheck) string {
	if d.err != nil {
		return fmt.Sprintf("Cannot tell whether trackz is running: %v", d.err)
	}
	return "trackz is not recording to " + config.DisplayPath(u.svc.DBPath()) + ": no new activity will appear. " + startHint()
}

func (u *UI) closeDaemonDialog() {
	u.mu.Lock()
	u.daemon.dialog = false
	u.mu.Unlock()
}

// layoutDaemonDialog handles and draws the "not running" message box.
func (u *UI) layoutDaemonDialog(gtx C) {
	d := &u.daemon
	if d.ok.Clicked(gtx) || dialogClickedOutside(gtx, &d.scrim) {
		u.closeDaemonDialog()
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape}, key.Filter{Name: key.NameReturn}, key.Filter{Name: key.NameEnter})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			u.closeDaemonDialog()
		}
	}
	state := u.daemonState()
	if !state.dialog {
		return
	}
	db := config.DisplayPath(u.svc.DBPath())
	drawDialog(gtx, &d.scrim, &d.panel, 520, func(gtx C) D {
		para := func(txt string, muted bool) layout.FlexChild {
			return layout.Rigid(func(gtx C) D {
				l := material.Body1(u.th, txt)
				if muted {
					l = material.Body2(u.th, txt)
					l.Color = cs.muted
				}
				return layout.Inset{Bottom: 10}.Layout(gtx, l.Layout)
			})
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				l := material.H6(u.th, "Trackz is not recording")
				l.Color = cs.err
				return layout.Inset{Bottom: 12}.Layout(gtx, l.Layout)
			}),
		}
		if state.err != nil {
			children = append(children, para(u.daemonMessage(state), false))
		} else {
			children = append(children,
				para("No trackz daemon is recording to "+db+", the database shown here, so no new activity will appear.", false),
				para(startHint(), false),
				para("If trackz is running, it is recording to a different database: TRACKZ_DB (or db_path in config.json) must point to the same file for both programs.", true),
			)
		}
		children = append(children, layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return D{Size: gtx.Constraints.Min} }),
				layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &d.ok, "OK") }),
			)
		}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}
