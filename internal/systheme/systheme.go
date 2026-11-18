// Package systheme reports whether the desktop prefers a dark or a light
// appearance, and notices when that changes.
//
//	Linux/BSD: the XDG desktop portal (org.freedesktop.appearance
//	           color-scheme), then GNOME's color-scheme setting, GTK_THEME,
//	           the GTK settings files and KDE's colour scheme
//	macOS:     AppleInterfaceStyle
//	Windows:   AppsUseLightTheme in the registry
package systheme

import (
	"context"
	"time"
)

// Theme is a colour preference.
type Theme int

const (
	// Unknown means the platform expressed no preference.
	Unknown Theme = iota
	Light
	Dark
)

func (t Theme) String() string {
	switch t {
	case Light:
		return "light"
	case Dark:
		return "dark"
	}
	return "unknown"
}

// Detect returns the current system preference.
func Detect() Theme { return detect() }

// Watch calls fn with the current preference and again whenever it changes,
// until ctx is cancelled. fn runs on a background goroutine.
func Watch(ctx context.Context, fn func(Theme)) {
	cur := Detect()
	fn(cur)
	changes := make(chan struct{}, 1)
	go subscribe(ctx, changes) // push notifications, where available
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-changes:
		}
		if t := Detect(); t != cur {
			cur = t
			fn(cur)
		}
	}
}

const pollInterval = 15 * time.Second
