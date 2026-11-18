//go:build linux || freebsd || openbsd || netbsd || dragonfly

package systheme

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

func detect() Theme {
	if t := portal(); t != Unknown {
		return t
	}
	if t := gsettings(); t != Unknown {
		return t
	}
	if t := fromName(os.Getenv("GTK_THEME")); t != Unknown {
		return t
	}
	if t := gtkSettings(); t != Unknown {
		return t
	}
	return kde()
}

// portal reads org.freedesktop.appearance color-scheme: 0 no preference,
// 1 prefer dark, 2 prefer light. Most desktops implement it.
func portal() Theme {
	conn, err := dbus.SessionBus()
	if err != nil {
		return Unknown
	}
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var v dbus.Variant
	if err := obj.CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0,
		"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
		// Older portals only have Read, which wraps the value twice.
		if err := obj.CallWithContext(ctx, "org.freedesktop.portal.Settings.Read", 0,
			"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
			return Unknown
		}
	}
	return fromScheme(v)
}

func fromScheme(v dbus.Variant) Theme {
	for {
		inner, ok := v.Value().(dbus.Variant)
		if !ok {
			break
		}
		v = inner
	}
	switch n, _ := v.Value().(uint32); n {
	case 1:
		return Dark
	case 2:
		return Light
	}
	return Unknown
}

func gsettings() Theme {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return Unknown
	}
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err == nil {
		switch s := strings.Trim(strings.TrimSpace(string(out)), "'"); s {
		case "prefer-dark":
			return Dark
		case "prefer-light":
			return Light
		}
	}
	out, err = exec.Command("gsettings", "get", "org.gnome.desktop.interface", "gtk-theme").Output()
	if err == nil {
		return fromName(string(out))
	}
	return Unknown
}

func fromName(name string) Theme {
	if strings.Contains(strings.ToLower(name), "dark") {
		return Dark
	}
	return Unknown
}

func gtkSettings() Theme {
	dir, err := os.UserConfigDir()
	if err != nil {
		return Unknown
	}
	for _, f := range []string{"gtk-4.0/settings.ini", "gtk-3.0/settings.ini"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "gtk-application-prefer-dark-theme":
				if v == "1" || v == "true" {
					return Dark
				}
			case "gtk-theme-name":
				if t := fromName(v); t != Unknown {
					return t
				}
			}
		}
	}
	return Unknown
}

func kde() Theme {
	dir, err := os.UserConfigDir()
	if err != nil {
		return Unknown
	}
	data, err := os.ReadFile(filepath.Join(dir, "kdeglobals"))
	if err != nil {
		return Unknown
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "ColorScheme" {
			if t := fromName(v); t != Unknown {
				return t
			}
			return Light
		}
	}
	return Unknown
}

// subscribe signals changes announced by the portal.
func subscribe(ctx context.Context, changes chan<- struct{}) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }() // watcher is stopping; nothing to report
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.portal.Settings"),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		return
	}
	sigs := make(chan *dbus.Signal, 8)
	conn.Signal(sigs)
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-sigs:
			if len(s.Body) >= 2 && s.Body[0] == "org.freedesktop.appearance" && s.Body[1] == "color-scheme" {
				select {
				case changes <- struct{}{}:
				default:
				}
			}
		}
	}
}
