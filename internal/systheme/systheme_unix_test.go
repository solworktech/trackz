//go:build linux || freebsd || openbsd || netbsd || dragonfly

package systheme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestFromScheme(t *testing.T) {
	// ReadOne returns v(u); the older Read returns v(v(u)).
	if got := fromScheme(dbus.MakeVariant(uint32(1))); got != Dark {
		t.Errorf("1 -> %v", got)
	}
	if got := fromScheme(dbus.MakeVariant(dbus.MakeVariant(uint32(2)))); got != Light {
		t.Errorf("2 -> %v", got)
	}
	if got := fromScheme(dbus.MakeVariant(uint32(0))); got != Unknown {
		t.Errorf("0 -> %v", got)
	}
}

func TestGTKAndKDESettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if gtkSettings() != Unknown || kde() != Unknown {
		t.Fatal("no settings files should mean Unknown")
	}
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("gtk-3.0", "settings.ini"), "[Settings]\ngtk-application-prefer-dark-theme=1\n")
	if gtkSettings() != Dark {
		t.Error("prefer-dark-theme=1 should be Dark")
	}
	write("kdeglobals", "[General]\nColorScheme=BreezeDark\n")
	if kde() != Dark {
		t.Error("BreezeDark should be Dark")
	}
	write("kdeglobals", "[General]\nColorScheme=BreezeLight\n")
	if kde() != Light {
		t.Error("BreezeLight should be Light")
	}
}
