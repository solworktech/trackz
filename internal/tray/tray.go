// Package tray shows an icon in the desktop's system tray (notification
// area / menu bar) with a click action and a context menu.
//
//	Linux/BSD: StatusNotifierItem + com.canonical.dbusmenu over D-Bus (KDE,
//	           XFCE, LXQt, Cinnamon, MATE, Budgie, GNOME with the
//	           AppIndicator extension, waybar, ...)
//	Windows:   Shell_NotifyIcon
//	macOS:     NSStatusItem
//
// It does not depend on a GUI toolkit; callbacks run on their own goroutine.
package tray

import (
	"errors"
	"image"
)

// Item is a context menu entry. An Item with an empty Label is a separator.
type Item struct {
	Label   string
	OnClick func()
}

// Options configure a tray icon.
type Options struct {
	// ID is a stable identifier, e.g. "trackz".
	ID string
	// Title is the application name, shown by some hosts.
	Title string
	// Tooltip is shown when hovering over the icon.
	Tooltip string
	// Icon returns the icon at about size x size pixels.
	Icon func(size int) image.Image
	// OnActivate runs on a (left) click on the icon.
	OnActivate func()
	// Items make up the context (right-click) menu.
	Items []Item
}

// ErrUnsupported is returned where no tray implementation exists.
var ErrUnsupported = errors.New("system tray not supported on this platform")

// Tray is a running tray icon.
type Tray struct {
	impl impl
}

type impl interface {
	setItems([]Item) error
	close()
	active() bool
}

// New shows the icon. On Linux/BSD the icon appears once a tray host
// (StatusNotifierWatcher) is running; New does not fail if there is none
// yet, and registers when one starts.
func New(opts Options) (*Tray, error) {
	i, err := newImpl(opts)
	if err != nil {
		return nil, err
	}
	return &Tray{impl: i}, nil
}

// SetItems replaces the context menu.
func (t *Tray) SetItems(items []Item) error { return t.impl.setItems(items) }

// Active reports whether the icon is currently shown, i.e. the user can
// reach its menu. On Linux/BSD this means a tray host accepted it.
func (t *Tray) Active() bool { return t != nil && t.impl.active() }

// Close removes the icon.
func (t *Tray) Close() { t.impl.close() }

func run(f func()) {
	if f != nil {
		go f()
	}
}
