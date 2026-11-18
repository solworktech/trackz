//go:build linux || freebsd || openbsd || netbsd || dragonfly

package icons

import (
	"bufio"
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

type desktopEntry struct {
	id        string // file name without .desktop
	name      string
	icon      string
	exec      string // base name of the executable
	wmClass   string
	noDisplay bool
}

var (
	indexOnce sync.Once
	entries   []desktopEntry
	dataDirs  []string
)

func xdgDataDirs() []string {
	dirs := []string{}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // no per-user directories; the system ones still apply
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		dirs = append(dirs, d)
	} else if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share"))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "flatpak", "exports", "share"))
	}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	dirs = append(dirs, filepath.SplitList(sys)...)
	dirs = append(dirs, "/var/lib/flatpak/exports/share", "/var/lib/snapd/desktop")
	seen := map[string]bool{}
	out := dirs[:0]
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func buildIndex() {
	dataDirs = xdgDataDirs()
	for _, d := range dataDirs {
		// The callback never fails: unreadable entries are skipped, so the
		// walk's own error is always nil.
		_ = filepath.WalkDir(filepath.Join(d, "applications"), func(path string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			if e, ok := parseDesktop(path); ok {
				entries = append(entries, e)
			}
			return nil
		})
	}
}

func parseDesktop(path string) (desktopEntry, bool) {
	f, err := os.Open(path)
	if err != nil {
		return desktopEntry{}, false
	}
	defer func() { _ = f.Close() }() // read-only
	e := desktopEntry{id: strings.TrimSuffix(filepath.Base(path), ".desktop")}
	inMain := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inMain = line == "[Desktop Entry]"
			continue
		}
		if !inMain {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Name":
			e.name = v
		case "Icon":
			e.icon = v
		case "StartupWMClass":
			e.wmClass = v
		case "NoDisplay":
			e.noDisplay = v == "true"
		case "Exec":
			e.exec = execName(v)
		}
	}
	return e, e.icon != ""
}

// execName extracts the program name from an Exec= line, skipping "env
// VAR=x" prefixes.
func execName(exec string) string {
	fields := strings.Fields(exec)
	for i := 0; i < len(fields); i++ {
		f := strings.Trim(fields[i], `"'`)
		if f == "env" || strings.Contains(f, "=") {
			continue
		}
		return filepath.Base(f)
	}
	return ""
}

func find(process, app string, size int) image.Image {
	indexOnce.Do(buildIndex)
	for _, name := range iconNames(process, app) {
		if img := loadIcon(name, size); img != nil {
			return img
		}
	}
	return nil
}

// iconNames returns candidate icon names, best first.
func iconNames(process, app string) []string {
	var names []string
	add := func(n string) {
		if n == "" {
			return
		}
		for _, x := range names {
			if x == n {
				return
			}
		}
		names = append(names, n)
	}
	matchers := []func(e desktopEntry) bool{
		func(e desktopEntry) bool { return process != "" && e.id == process },
		func(e desktopEntry) bool { return app != "" && strings.EqualFold(e.wmClass, app) },
		func(e desktopEntry) bool { return process != "" && strings.EqualFold(e.wmClass, process) },
		func(e desktopEntry) bool { return process != "" && e.exec == process },
		// Linux truncates process names (comm) to 15 characters.
		func(e desktopEntry) bool { return len(process) == 15 && strings.HasPrefix(e.exec, process) },
		func(e desktopEntry) bool { return app != "" && strings.EqualFold(e.id, app) },
		func(e desktopEntry) bool { return app != "" && strings.EqualFold(e.name, app) },
		func(e desktopEntry) bool {
			return app != "" && strings.HasSuffix(strings.ToLower(e.id), "."+strings.ToLower(app))
		},
	}
	for _, m := range matchers {
		for _, e := range entries {
			if m(e) {
				add(e.icon)
			}
		}
	}
	// What the original viewer did: use the process name as icon name.
	add(process)
	add(strings.ToLower(app))
	return names
}

var sizeDirs = []string{"48x48", "32x32", "64x64", "24x24", "128x128", "256x256", "96x96", "22x22", "16x16", "512x512", "scalable"}

func loadIcon(name string, size int) image.Image {
	if filepath.IsAbs(name) {
		return loadFile(name, size)
	}
	themes := []string{}
	if t := currentTheme(); t != "" {
		themes = append(themes, t)
	}
	themes = append(themes, "hicolor", "Adwaita", "HighContrast")
	roots := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".icons"))
	}
	for _, d := range dataDirs {
		roots = append(roots, filepath.Join(d, "icons"))
	}
	for _, theme := range themes {
		for _, root := range roots {
			for _, sz := range sizeDirs {
				for _, ext := range []string{".png", ".svg"} {
					p := filepath.Join(root, theme, sz, "apps", name+ext)
					if _, err := os.Stat(p); err == nil {
						if img := loadFile(p, size); img != nil {
							return img
						}
					}
				}
			}
		}
	}
	for _, d := range append([]string{"/usr/share"}, dataDirs...) {
		for _, ext := range []string{".png", ".svg"} {
			p := filepath.Join(d, "pixmaps", name+ext)
			if _, err := os.Stat(p); err == nil {
				if img := loadFile(p, size); img != nil {
					return img
				}
			}
		}
	}
	return nil
}

var (
	themeOnce sync.Once
	theme     string
)

// currentTheme reads gtk-icon-theme-name from the GTK settings, if any.
func currentTheme() string {
	themeOnce.Do(func() {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return
		}
		for _, f := range []string{"gtk-4.0/settings.ini", "gtk-3.0/settings.ini"} {
			data, err := os.ReadFile(filepath.Join(cfg, f))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "gtk-icon-theme-name" {
					theme = strings.Trim(strings.TrimSpace(v), `"`)
					return
				}
			}
		}
	})
	return theme
}

func loadFile(path string, size int) image.Image {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(path), ".svg") || strings.HasSuffix(strings.ToLower(path), ".svgz") {
		return rasterizeSVG(data, size)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

func rasterizeSVG(data []byte, size int) image.Image {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1)
	return img
}
