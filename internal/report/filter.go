package report

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/solworktech/trackz/internal/patterns"
	"github.com/solworktech/trackz/internal/store"
)

// Filter narrows the records the predefined views are computed from.
// The zero Filter keeps everything. Custom queries are not filtered.
//
// Include and Exclude are patterns for the window title (see package
// patterns: whole words or /regular expressions/, ignoring case). A record
// is kept when its title matches one of Include (or Include is empty),
// none of Exclude, and its application is not in ExcludeApps.
type Filter struct {
	Include, Exclude []string
	// ExcludeApps drops records of these applications (app_name values).
	ExcludeApps []string
}

// Active reports whether the filter drops anything.
func (f Filter) Active() bool {
	return len(nonBlank(f.Include)) > 0 || len(nonBlank(f.Exclude)) > 0 || len(f.ExcludeApps) > 0
}

// nonBlank returns the patterns in ps that are not blank, trimmed.
func nonBlank(ps []string) []string { return patterns.Lines(strings.Join(ps, "\n")) }

// Excludes reports whether app is excluded.
func (f Filter) Excludes(app string) bool { return slices.Contains(f.ExcludeApps, app) }

// Key identifies the filter, for caching results computed with it.
func (f Filter) Key() string {
	apps := slices.Clone(f.ExcludeApps)
	sort.Strings(apps)
	return strings.Join([]string{
		strings.Join(f.Include, "\n"), strings.Join(f.Exclude, "\n"), strings.Join(apps, "\n"),
	}, "\x00")
}

// Validate reports the first invalid pattern.
func (f Filter) Validate() error {
	_, err := f.compile()
	return err
}

// titleFilter is a Filter's patterns, compiled.
type titleFilter struct{ include, exclude patterns.Set }

func (f Filter) compile() (titleFilter, error) {
	in, err := patterns.Compile(f.Include)
	if err != nil {
		return titleFilter{}, err
	}
	ex, err := patterns.Compile(f.Exclude)
	if err != nil {
		return titleFilter{}, err
	}
	return titleFilter{in, ex}, nil
}

// Describe summarises the filter for display, e.g.
// `titles with "invoice" or "/inv-\d+/" · without "draft" · excluding Firefox, Slack`.
// It is empty for an inactive filter.
func (f Filter) Describe() string {
	var parts []string
	quoted := func(ps []string) string {
		ps = nonBlank(ps)
		for i, p := range ps {
			ps[i] = fmt.Sprintf("%q", p)
		}
		return strings.Join(ps, " or ")
	}
	if q := quoted(f.Include); q != "" {
		parts = append(parts, "titles with "+q)
	}
	if q := quoted(f.Exclude); q != "" {
		parts = append(parts, "titles without "+q)
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

// apply keeps the records that pass f, matching titles against
// title(r.WindowName) (the title as reports show it).
func (f Filter) apply(recs []store.Record, title func(string) string) ([]store.Record, error) {
	if !f.Active() {
		return recs, nil
	}
	tf, err := f.compile()
	if err != nil {
		return nil, err
	}
	kept := recs[:0]
	for _, r := range recs {
		if f.Excludes(r.AppName) {
			continue
		}
		t := title(r.WindowName)
		if (tf.include.Empty() || tf.include.Match(t)) && !tf.exclude.Match(t) {
			kept = append(kept, r)
		}
	}
	return kept, nil
}
