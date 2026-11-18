//go:build (linux || freebsd || openbsd || netbsd || dragonfly) && !android

package gio

import (
	"image"
	"image/draw"
	"io"
	"log"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/event"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/solworktech/trackz/internal/assets"
)

// handlePlatformEvent sets the window icon (_NET_WM_ICON) when Gio reports
// its X11 window; Gio has no API for window icons. On Wayland the icon comes
// from the trackz-viz.desktop file matching the application ID instead.
func handlePlatformEvent(e event.Event) {
	if v, ok := e.(app.X11ViewEvent); ok {
		x11Window.Store(uint32(v.Window)) // 0 when the window is destroyed
		if v.Window != 0 {
			go setX11Icon(xproto.Window(v.Window))
		}
	}
}

// x11Window is the window's X11 id (0 on Wayland or before it exists).
var x11Window atomic.Uint32

// platformRaise asks the window manager to activate the window, as a pager
// or taskbar does. Gio's own raise sends _NET_ACTIVE_WINDOW without a source,
// which focus-stealing prevention may turn into a mere attention hint; a
// click on the tray icon is an explicit user request.
func platformRaise() {
	win := xproto.Window(x11Window.Load())
	if win == 0 {
		return
	}
	xgb.Logger = log.New(io.Discard, "", 0)
	conn, err := xgb.NewConn()
	if err != nil {
		return
	}
	defer conn.Close()
	r, err := xproto.InternAtom(conn, false, uint16(len("_NET_ACTIVE_WINDOW")), "_NET_ACTIVE_WINDOW").Reply()
	if err != nil {
		return
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	xproto.MapWindow(conn, win) // un-minimise
	ev := xproto.ClientMessageEvent{
		Format: 32,
		Window: win,
		Type:   r.Atom,
		// source 2 = pager/taskbar, timestamp 0 = CurrentTime
		Data: xproto.ClientMessageDataUnionData32New([]uint32{2, 0, 0, 0, 0}),
	}
	xproto.SendEvent(conn, false, root,
		xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(ev.Bytes()))
	conn.Sync()
}

func setX11Icon(win xproto.Window) {
	xgb.Logger = log.New(io.Discard, "", 0) // it logs harmless auth fallbacks
	conn, err := xgb.NewConn()
	if err != nil {
		return
	}
	defer conn.Close()
	atom := func(name string) xproto.Atom {
		r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			return 0
		}
		return r.Atom
	}
	netWMIcon, cardinal := atom("_NET_WM_ICON"), atom("CARDINAL")
	if netWMIcon == 0 || cardinal == 0 {
		return
	}
	// _NET_WM_ICON: for each size, width, height, then ARGB pixels.
	var data []uint32
	for _, size := range []int{16, 32, 48, 64, 128} {
		src := assets.Icon(size)
		if src == nil {
			continue
		}
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
		data = append(data, uint32(size), uint32(size))
		for i := 0; i < len(img.Pix); i += 4 {
			r, g, b, a := uint32(img.Pix[i]), uint32(img.Pix[i+1]), uint32(img.Pix[i+2]), uint32(img.Pix[i+3])
			data = append(data, a<<24|r<<16|g<<8|b)
		}
	}
	buf := make([]byte, 4*len(data))
	for i, v := range data {
		xgb.Put32(buf[4*i:], v)
	}
	xproto.ChangeProperty(conn, xproto.PropModeReplace, win, netWMIcon, cardinal, 32, uint32(len(data)), buf)
	conn.Sync()
}
