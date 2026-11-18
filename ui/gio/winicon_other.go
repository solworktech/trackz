//go:build !(linux || freebsd || openbsd || netbsd || dragonfly) || android

package gio

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

// Windows takes the window icon from the executable's resources
// (cmd/trackz-viz/rsrc_windows_*.syso); macOS from the Dock icon set in
// dockicon_darwin.go.
func handlePlatformEvent(e event.Event) {}

// platformRaise: Gio's ActionRaise is sufficient on these platforms.
func platformRaise() {}

// platformWindowMode: Gio's own view of the window mode is used.
func platformWindowMode(fallback app.WindowMode) app.WindowMode { return fallback }

// platformSetMode: Gio's window options are used instead.
func platformSetMode(app.WindowMode) bool { return false }
