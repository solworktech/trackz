package gio

import (
	"log"

	"gioui.org/io/system"

	"github.com/solworktech/trackz/internal/assets"
	"github.com/solworktech/trackz/internal/tray"
)

// startTray shows the Trackz icon in the system tray: a click brings the
// window to the front, or opens it again after it was closed; the context
// menu offers Quit. Further entries can be
// added to the Items list.
func (u *UI) startTray() *tray.Tray {
	t, err := tray.New(tray.Options{
		ID:         "trackz",
		Title:      "Trackz",
		Tooltip:    "Trackz: time spent in windows and applications",
		Icon:       assets.Icon,
		OnActivate: u.Raise,
		Items: []tray.Item{
			{Label: "Show Trackz", OnClick: u.Raise},
			{}, // separator
			{Label: "Quit", OnClick: u.quit},
		},
	})
	if err != nil {
		log.Printf("trackz-viz: no tray icon: %v", err)
		return nil
	}
	return t
}

// Raise brings the window to the front and gives it focus, or opens a new
// one if it was closed to the tray. Safe to call from any goroutine; used
// by the tray icon and when trackz-viz is started again.
func (u *UI) Raise() {
	w := u.win.Load()
	if w == nil {
		u.signal()
		return
	}
	w.Perform(system.ActionRaise)
	platformRaise()
	w.Invalidate()
}

// quit ends the program, closing the window if it is open. Safe to call
// from any goroutine.
func (u *UI) quit() {
	u.quitting.Store(true)
	if w := u.win.Load(); w != nil {
		w.Perform(system.ActionClose)
		// On X11 the close request is only flushed to the server when the
		// event loop next runs; wake it, or an idle window never closes.
		w.Invalidate()
	}
	u.signal()
}
