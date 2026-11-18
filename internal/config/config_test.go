package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/solworktech/trackz/internal/titles"
)

func TestLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	cfg, err := Load(path)
	if err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("missing file should give defaults: %+v %v", cfg, err)
	}
	cfg.TimeZone = "Asia/Tokyo"
	cfg.DateFormat = "%d/%m/%Y"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if got.FormatDate(time.Date(2025, 1, 1, 20, 0, 0, 0, time.UTC)) != "02/01/2025" {
		t.Error("date should be rendered in Tokyo time")
	}
	cfg.TimeZone = "Mars/Olympus"
	if Save(path, cfg) == nil {
		t.Error("invalid zone must be rejected")
	}
	if err := os.WriteFile(path, []byte(`{"time_zone":"Nope/Nope","row_limit":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatalf("invalid fields fall back rather than fail: %v", err)
	}
	if got.TimeZone != "Local" || got.RowLimit != DefaultRowLimit {
		t.Errorf("invalid fields should fall back: %+v", got)
	}
}

func mustResolve(t *testing.T, flagValue string, cfg Config) string {
	t.Helper()
	p, err := ResolveDBPath(flagValue, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveDBPath(t *testing.T) {
	t.Setenv(EnvDB, "")
	if p := mustResolve(t, "", Config{DBPath: "/x.db"}); p != "/x.db" {
		t.Error(p)
	}
	t.Setenv(EnvDB, "/env.db")
	if p := mustResolve(t, "", Config{DBPath: "/x.db"}); p != "/env.db" {
		t.Error(p)
	}
	if p := mustResolve(t, "/flag.db", Config{}); p != "/flag.db" {
		t.Error(p)
	}
}

func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("TZDIR_TEST", "/srv/data")
	for in, want := range map[string]string{
		"~/.config/trackz/trackz.db": filepath.Join(home, ".config/trackz/trackz.db"),
		"$HOME/x.db":                 filepath.Join(home, "x.db"),
		"${TZDIR_TEST}/t.db":         "/srv/data/t.db",
		"/abs/t.db":                  "/abs/t.db",
	} {
		if got := ExpandPath(in); got != want {
			t.Errorf("ExpandPath(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ExpandPath("rel.db"); !filepath.IsAbs(got) {
		t.Errorf("relative path not made absolute: %q", got)
	}
	t.Setenv(EnvDB, "~/from-env.db")
	if got := mustResolve(t, "", Config{}); got != filepath.Join(home, "from-env.db") {
		t.Errorf("TRACKZ_DB with ~ not expanded: %q", got)
	}
}

func TestDefaultDBPathFollowsConfigDir(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" && runtime.GOOS != "openbsd" && runtime.GOOS != "netbsd" {
		t.Skip("XDG layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvDB, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	want := filepath.Join(home, ".config", "trackz", "trackz.db")
	if got := mustResolve(t, "", Config{}); got != want {
		t.Errorf("default = %q, want %q", got, want)
	}
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got := mustResolve(t, "", Config{}); got != filepath.Join(xdg, "trackz", "trackz.db") {
		t.Errorf("with XDG_CONFIG_HOME: %q", got)
	}
	if p, _ := Path(); p != filepath.Join(xdg, "trackz", "config.json") {
		t.Errorf("config path %q should be next to the database", p)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative/dir") // invalid per spec: ignored
	if got := mustResolve(t, "", Config{}); got != want {
		t.Errorf("relative XDG_CONFIG_HOME should be ignored: %q", got)
	}
	// Order: TRACKZ_DB, then db_path, then the default.
	if got := mustResolve(t, "", Config{DBPath: "/cfg.db"}); got != "/cfg.db" {
		t.Errorf("db_path: %q", got)
	}
	t.Setenv(EnvDB, "/env.db")
	if got := mustResolve(t, "", Config{DBPath: "/cfg.db"}); got != "/env.db" {
		t.Errorf("TRACKZ_DB must win over db_path: %q", got)
	}
}

func TestTitleRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"merge_titles":false,"title_rules":[{"pattern":"(bad"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MergeTitles || !reflect.DeepEqual(cfg.TitleRules, Default().TitleRules) {
		t.Errorf("an invalid rule should fall back to the defaults: %+v", cfg)
	}
	if !Default().MergeTitles {
		t.Error("merging is on by default")
	}
	cfg.TitleRules = []titles.Rule{{Pattern: "("}}
	if cfg.Validate() == nil {
		t.Error("Validate must reject an invalid rule")
	}
}
