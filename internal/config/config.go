// Package config holds user preferences shared by the tracker daemon and all
// frontends (Gio, and any future TUI): database location, display time zone
// and date/time formats, and report defaults.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones on systems without a zoneinfo database (Windows)

	"github.com/solworktech/trackz/internal/groups"
	"github.com/solworktech/trackz/internal/timefmt"
	"github.com/solworktech/trackz/internal/titles"
)

const (
	// EnvDB is the environment variable holding the database path. It is
	// kept from the original devilspie2-based implementation.
	EnvDB = "TRACKZ_DB"
	// EnvConfig overrides the location of the config file.
	EnvConfig = "TRACKZ_CONFIG"

	// DefaultMinDuration hides report entries whose total is below this
	// many seconds. (The original viewer's DURATION_THRESHOLD applied the
	// same value to each record, which dropped all short visits.)
	DefaultMinDuration = 120
	// DefaultRowLimit is the default number of rows on the dashboard and
	// of applications in Top Apps.
	DefaultRowLimit = 20
	// DefaultRefreshMinutes is how often views showing the present are
	// recomputed.
	DefaultRefreshMinutes = 1

	// Theme values.
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Config is persisted as JSON.
type Config struct {
	// DBPath is used when neither a command-line flag nor TRACKZ_DB is set.
	DBPath string `json:"db_path,omitempty"`
	// TimeZone is "Local" or an IANA zone name such as "Europe/London".
	// It controls how timestamps are displayed and where day boundaries
	// fall ("Today", daily statistics). Stored data is always Unix time.
	TimeZone string `json:"time_zone"`
	// DateFormat and TimeFormat are strftime-style patterns.
	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	// RowLimit is the number of entries shown on the dashboard and in Top
	// Apps (the Rows field).
	RowLimit int `json:"row_limit"`
	// MinDurationSeconds hides report entries (a window, an application)
	// whose total focus time in the period is below this. Individual short
	// records still count towards totals and daily statistics.
	MinDurationSeconds int `json:"min_duration_seconds"`
	// Theme is "system" (follow the desktop), "light" or "dark".
	Theme string `json:"theme"`
	// RefreshMinutes is how often views that include the present are
	// refreshed automatically; 0 disables automatic refresh.
	RefreshMinutes int `json:"refresh_minutes"`
	// MergeTitles makes reports add up window titles that differ only in
	// what TitleRules remove (notification counts such as "(3) YouTube").
	// Recorded titles are not changed.
	MergeTitles bool `json:"merge_titles"`
	// TitleRules are applied in order to normalise titles (see package
	// titles); they default to titles.DefaultRules.
	TitleRules []titles.Rule `json:"title_rules"`
	// TaskGroups are the task groups (customers, projects) reports can add
	// time up by; see package groups. Load keeps them even when invalid,
	// so a mistake in the file does not lose them; trackz-viz reports it.
	TaskGroups []groups.Group `json:"task_groups,omitempty"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		TimeZone:           "Local",
		DateFormat:         "%Y-%m-%d",
		TimeFormat:         "%H:%M:%S",
		RowLimit:           DefaultRowLimit,
		MinDurationSeconds: DefaultMinDuration,
		Theme:              ThemeSystem,
		RefreshMinutes:     DefaultRefreshMinutes,
		MergeTitles:        true,
		TitleRules:         slices.Clone(titles.DefaultRules),
	}
}

func validTheme(t string) bool {
	return t == ThemeSystem || t == ThemeLight || t == ThemeDark
}

// Validate checks every field and returns the first problem found.
func (c Config) Validate() error {
	if _, err := LoadLocation(c.TimeZone); err != nil {
		return err
	}
	if err := timefmt.Validate(c.DateFormat); err != nil {
		return fmt.Errorf("date format: %w", err)
	}
	if err := timefmt.Validate(c.TimeFormat); err != nil {
		return fmt.Errorf("time format: %w", err)
	}
	if c.RowLimit < 1 || c.RowLimit > 10000 {
		return fmt.Errorf("row limit must be between 1 and 10000")
	}
	if c.MinDurationSeconds < 0 {
		return fmt.Errorf("minimum duration must not be negative")
	}
	if !validTheme(c.Theme) {
		return fmt.Errorf("theme must be %q, %q or %q", ThemeSystem, ThemeLight, ThemeDark)
	}
	if c.RefreshMinutes < 0 || c.RefreshMinutes > 24*60 {
		return fmt.Errorf("refresh interval must be between 0 (off) and 1440 minutes")
	}
	if _, err := titles.New(c.TitleRules); err != nil {
		return err
	}
	return groups.Validate(c.TaskGroups)
}

// Location resolves the configured time zone, falling back to time.Local.
func (c Config) Location() *time.Location {
	loc, err := LoadLocation(c.TimeZone)
	if err != nil {
		return time.Local
	}
	return loc
}

// LoadLocation accepts "", "Local", "UTC" or an IANA zone name.
func LoadLocation(name string) (*time.Location, error) {
	switch name {
	case "", "Local", "local":
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q", name)
	}
	return loc, nil
}

// FormatDate renders t (converted to the configured zone) as a date.
func (c Config) FormatDate(t time.Time) string {
	return timefmt.Format(t.In(c.Location()), c.DateFormat)
}

// FormatDateTime renders t (converted to the configured zone) as date and time.
func (c Config) FormatDateTime(t time.Time) string {
	t = t.In(c.Location())
	return timefmt.Format(t, c.DateFormat) + " " + timefmt.Format(t, c.TimeFormat)
}

// Path returns the config file location: $TRACKZ_CONFIG, or
// <user config dir>/trackz/config.json.
func Path() (string, error) {
	if p := os.Getenv(EnvConfig); p != "" {
		return ExpandPath(p), nil
	}
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the config file at path. A missing file yields defaults.
// Unset or invalid fields fall back to their defaults individually.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", path, err)
	}
	def := Default()
	if _, err := LoadLocation(cfg.TimeZone); err != nil {
		cfg.TimeZone = def.TimeZone
	}
	if timefmt.Validate(cfg.DateFormat) != nil {
		cfg.DateFormat = def.DateFormat
	}
	if timefmt.Validate(cfg.TimeFormat) != nil {
		cfg.TimeFormat = def.TimeFormat
	}
	if cfg.RowLimit < 1 {
		cfg.RowLimit = def.RowLimit
	}
	if cfg.MinDurationSeconds < 0 {
		cfg.MinDurationSeconds = def.MinDurationSeconds
	}
	if !validTheme(cfg.Theme) {
		cfg.Theme = def.Theme
	}
	if cfg.RefreshMinutes < 0 || cfg.RefreshMinutes > 24*60 {
		cfg.RefreshMinutes = def.RefreshMinutes
	}
	if _, err := titles.New(cfg.TitleRules); err != nil {
		cfg.TitleRules = def.TitleRules
	}
	return cfg, nil
}

// Save validates cfg and writes it atomically to path.
func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ResolveDBPath picks the database path, identically for trackz and
// trackz-viz: the -db flag if given, then $TRACKZ_DB, then db_path from
// config.json, then DefaultDBPath().
//
// The result has "~" and $VARIABLES expanded (systemd's Environment= and
// EnvironmentFile= do not expand them, so TRACKZ_DB=~/x would otherwise be
// a relative path) and is made absolute.
func ResolveDBPath(flagValue string, cfg Config) (string, error) {
	var p string
	switch {
	case flagValue != "":
		p = flagValue
	case os.Getenv(EnvDB) != "":
		p = os.Getenv(EnvDB)
	case cfg.DBPath != "":
		p = cfg.DBPath
	default:
		var err error
		if p, err = DefaultDBPath(); err != nil {
			return "", err
		}
	}
	return ExpandPath(p), nil
}

// DisplayPath shortens a path in the home directory to ~/..., for display.
func DisplayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// ExpandPath expands a leading "~" (or "~/...") to the home directory and
// $VAR / ${VAR} references, then makes the path absolute.
func ExpandPath(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p
}

// DefaultDBPath is trackz.db in the per-user config directory, next to
// config.json:
//
//	Linux/BSD: $XDG_CONFIG_HOME/trackz/trackz.db (~/.config/trackz/trackz.db if unset)
//	macOS:     ~/Library/Application Support/trackz/trackz.db
//	Windows:   %APPDATA%\trackz\trackz.db
func DefaultDBPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "trackz.db"), nil
}

// Dir is the per-user Trackz directory holding config.json, the default
// database and saved queries: <user config dir>/trackz.
func Dir() (string, error) {
	d, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "trackz"), nil
}

// userConfigDir is os.UserConfigDir ($XDG_CONFIG_HOME or ~/.config on
// Linux/BSD, ~/Library/Application Support on macOS, %APPDATA% on Windows),
// falling back to the platform default when it fails (for instance when
// $XDG_CONFIG_HOME is not an absolute path, which the XDG spec says to
// ignore).
func userConfigDir() (string, error) {
	if d, err := os.UserConfigDir(); err == nil {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no per-user config directory: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Roaming"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		return filepath.Join(home, ".config"), nil
	}
}
