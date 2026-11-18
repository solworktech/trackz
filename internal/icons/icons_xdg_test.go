//go:build linux || freebsd || openbsd || netbsd || dragonfly

package icons

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopLookup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "none"))
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "applications", "org.example.Term.desktop"), []byte(
		"[Desktop Entry]\nName=Example Term\nExec=env FOO=1 /usr/bin/exterm --new\nIcon=exterm-icon\nStartupWMClass=ExTerm\n"))
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	img.Set(1, 1, color.NRGBA{255, 0, 0, 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(root, "icons", "hicolor", "32x32", "apps", "exterm-icon.png"), pngData.Bytes())
	write(filepath.Join(root, "icons", "hicolor", "scalable", "apps", "vectorapp.svg"), []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="#00f"/></svg>`))

	// An SVG oksvg renders blank must not hide a usable PNG.
	write(filepath.Join(root, "icons", "hicolor", "scalable", "apps", "ghost.svg"), []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"></svg>`))
	write(filepath.Join(root, "pixmaps", "ghost.png"), pngData.Bytes())

	r := New(24)
	if got := r.Icon("ghost", ""); got == nil || got.Bounds().Dx() != 32 {
		t.Errorf("blank SVG should fall back to the PNG: %v", got)
	}
	if r.Icon("exterm", "") == nil {
		t.Error("lookup by Exec name failed")
	}
	if r.Icon("other", "ExTerm") == nil {
		t.Error("lookup by StartupWMClass failed")
	}
	if got := r.Icon("vectorapp", ""); got == nil || got.Bounds().Dx() != 24 {
		t.Errorf("SVG fallback by process name failed: %v", got)
	}
	if r.Icon("nothing", "Nothing") != nil {
		t.Error("unexpected icon")
	}
}
