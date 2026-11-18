//go:build (linux || freebsd || openbsd || netbsd || dragonfly) && !android

package gio

import (
	"image"
	"image/draw"
	"io"
	"log"
	"sync"
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

// The window state is read and set through a connection of our own: Gio's
// view of it goes stale on X11 once the window manager changes it (e.g. a
// window un-maximised from its title bar is still Maximized for Gio).
var x11State struct {
	sync.Mutex
	conn  *xgb.Conn
	atoms map[string]xproto.Atom
}

// withX11 runs f with the shared connection, reconnecting after an error.
func withX11(f func(conn *xgb.Conn, atom func(string) (xproto.Atom, error)) error) error {
	x11State.Lock()
	defer x11State.Unlock()
	if x11State.conn == nil {
		xgb.Logger = log.New(io.Discard, "", 0)
		conn, err := xgb.NewConn()
		if err != nil {
			return err
		}
		x11State.conn, x11State.atoms = conn, map[string]xproto.Atom{}
	}
	conn := x11State.conn
	atom := func(name string) (xproto.Atom, error) {
		if a, ok := x11State.atoms[name]; ok {
			return a, nil
		}
		r, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			return 0, err
		}
		x11State.atoms[name] = r.Atom
		return r.Atom, nil
	}
	err := f(conn, atom)
	if err != nil {
		conn.Close()
		x11State.conn = nil
	}
	return err
}

// platformWindowMode returns the window's mode from _NET_WM_STATE, or
// fallback (Gio's idea of it) when not on X11 or it cannot be read.
func platformWindowMode(fallback app.WindowMode) app.WindowMode {
	win := xproto.Window(x11Window.Load())
	if win == 0 {
		return fallback
	}
	mode := fallback
	err := withX11(func(conn *xgb.Conn, atom func(string) (xproto.Atom, error)) error {
		state, err := atom("_NET_WM_STATE")
		if err != nil {
			return err
		}
		r, err := xproto.GetProperty(conn, false, win, state, xproto.AtomAtom, 0, 32).Reply()
		if err != nil {
			return err
		}
		has := map[xproto.Atom]bool{}
		for i := 0; i+4 <= len(r.Value); i += 4 {
			has[xproto.Atom(xgb.Get32(r.Value[i:]))] = true
		}
		is := func(name string) bool {
			a, err := atom(name)
			return err == nil && has[a]
		}
		switch {
		case is("_NET_WM_STATE_HIDDEN"):
			mode = app.Minimized
		case is("_NET_WM_STATE_FULLSCREEN"):
			mode = app.Fullscreen
		case is("_NET_WM_STATE_MAXIMIZED_VERT") && is("_NET_WM_STATE_MAXIMIZED_HORZ"):
			mode = app.Maximized
		default:
			mode = app.Windowed
		}
		return nil
	})
	if err != nil {
		return fallback
	}
	return mode
}

// platformSetMode asks the window manager to maximise the window or make
// it full screen, as a user would; the window manager then keeps its
// normal size for un-maximising. It reports false when not on X11.
func platformSetMode(mode app.WindowMode) bool {
	win := xproto.Window(x11Window.Load())
	if win == 0 {
		return false
	}
	err := withX11(func(conn *xgb.Conn, atom func(string) (xproto.Atom, error)) error {
		var names []string
		switch mode {
		case app.Maximized:
			names = []string{"_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ"}
		case app.Fullscreen:
			names = []string{"_NET_WM_STATE_FULLSCREEN"}
		default:
			return nil
		}
		state, err := atom("_NET_WM_STATE")
		if err != nil {
			return err
		}
		data := []uint32{1, 0, 0, 1, 0} // _NET_WM_STATE_ADD, first, second, source: application
		for i, n := range names {
			a, err := atom(n)
			if err != nil {
				return err
			}
			data[1+i] = uint32(a)
		}
		ev := xproto.ClientMessageEvent{Format: 32, Window: win, Type: state,
			Data: xproto.ClientMessageDataUnionData32New(data)}
		root := xproto.Setup(conn).DefaultScreen(conn).Root
		return xproto.SendEventChecked(conn, false, root,
			xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(ev.Bytes())).Check()
	})
	return err == nil
}
