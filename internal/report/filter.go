package report

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/solworktech/trackz/internal/store"
)

// Filter narrows the records the predefined views are computed from.
// The zero Filter keeps everything. Custom queries are not filtered.
type Filter struct {
	// TitleContains keeps records whose window title contains this text,
	// ignoring case. Empty keeps all titles.
	TitleContains string
	// ExcludeApps drops records of these applications (app_name values).
	ExcludeApps []string
}

// Active reports whether the filter drops anything.
func (f Filter) Active() bool {
	return strings.TrimSpace(f.TitleContains) != "" || len(f.ExcludeApps) > 0
}

// Excludes reports whether app is excluded.
func (f Filter) Excludes(app string) bool { return slices.Contains(f.ExcludeApps, app) }

// Key identifies the filter, for caching results computed with it.
func (f Filter) Key() string {
	apps := slices.Clone(f.ExcludeApps)
	sort.Strings(apps)
	return strings.ToLower(strings.TrimSpace(f.TitleContains)) + "\x00" + strings.Join(apps, "\x00")
}

// match reports whether r passes the filter, matching the title against
// title(r.WindowName) (the title as reports show it).
func (f Filter) match(r store.Record, title func(string) string) bool {
	if f.Excludes(r.AppName) {
		return false
	}
	t := strings.TrimSpace(f.TitleContains)
	return t == "" || strings.Contains(strings.ToLower(title(r.WindowName)), strings.ToLower(t))
}

// Describe summarises the filter for display, e.g.
// `window titles containing "invoice" · excluding Firefox, Slack`.
// It is empty for an inactive filter.
func (f Filter) Describe() string {
	var parts []string
	if t := strings.TrimSpace(f.TitleContains); t != "" {
		parts = append(parts, fmt.Sprintf("window titles containing %q", t))
	}
	if n := len(f.ExcludeApps); n > 0 {
		apps := slices.Clone(f.ExcludeApps)
		sort.Strings(apps)
		const show = 3
		if n <= show {
			parts = append(parts, "excluding "+strings.Join(apps, ", "))
		} else {
			parts = append(parts, fmt.Sprintf("excluding %s and %d more apps", strings.Join(apps[:show], ", "), n-show))
		}
	}
	return strings.Join(parts, " · ")
}

func (f Filter) apply(recs []store.Record, title func(string) string) []store.Record {
	if !f.Active() {
		return recs
	}
	kept := recs[:0]
	for _, r := range recs {
		if f.match(r, title) {
			kept = append(kept, r)
		}
	}
	return kept
}
