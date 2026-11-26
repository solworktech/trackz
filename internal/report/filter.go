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
// none of Exclude, its application is not in ExcludeApps and its task
// group is not excluded.
type Filter struct {
	Include, Exclude []string
	// ExcludeApps drops records of these applications (app_name values).
	ExcludeApps []string
	// ExcludeGroups drops records of these task groups, ExcludeUngrouped
	// those in none (screen lock and breaks, in neither, stay).
	ExcludeGroups    []string
	ExcludeUngrouped bool
}

// Active reports whether the filter drops anything.
func (f Filter) Active() bool {
	return len(nonBlank(f.Include)) > 0 || len(nonBlank(f.Exclude)) > 0 || len(f.ExcludeApps) > 0 ||
		f.groupsActive()
}

func (f Filter) groupsActive() bool { return len(f.ExcludeGroups) > 0 || f.ExcludeUngrouped }

// ExcludesGroup reports whether the task group named name ("" for
// Ungrouped) is excluded.
func (f Filter) ExcludesGroup(name string) bool {
	if name == "" {
		return f.ExcludeUngrouped
	}
	return slices.Contains(f.ExcludeGroups, name)
}

// nonBlank returns the patterns in ps that are not blank, trimmed.
func nonBlank(ps []string) []string { return patterns.Lines(strings.Join(ps, "\n")) }

// Excludes reports whether app is excluded.
func (f Filter) Excludes(app string) bool { return slices.Contains(f.ExcludeApps, app) }

// Key identifies the filter, for caching results computed with it.
func (f Filter) Key() string {
	apps := slices.Clone(f.ExcludeApps)
	sort.Strings(apps)
	gs := slices.Clone(f.ExcludeGroups)
	sort.Strings(gs)
	if f.ExcludeUngrouped {
		gs = append(gs, "\x01ungrouped")
	}
	return strings.Join([]string{
		strings.Join(f.Include, "\n"), strings.Join(f.Exclude, "\n"), strings.Join(apps, "\n"), strings.Join(gs, "\n"),
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
	excluding := func(names []string, noun string) {
		const show = 3
		switch n := len(names); {
		case n == 0:
		case n <= show:
			parts = append(parts, "excluding "+strings.Join(names, ", "))
		default:
			parts = append(parts, fmt.Sprintf("excluding %s and %d more %s", strings.Join(names[:show], ", "), n-show, noun))
		}
	}
	apps := slices.Clone(f.ExcludeApps)
	sort.Strings(apps)
	excluding(apps, "apps")
	gs := slices.Clone(f.ExcludeGroups)
	sort.Strings(gs)
	if f.ExcludeUngrouped {
		gs = append(gs, UngroupedLabel)
	}
	if len(gs) > 0 {
		gs[0] = "groups " + gs[0]
	}
	excluding(gs, "groups")
	return strings.Join(parts, " · ")
}

// apply keeps the records that pass f, matching titles against
// title(r.WindowName) (the title as reports show it); group gives a
// record's task group (see Service.groupFunc).
func (f Filter) apply(recs []store.Record, title func(string) string, group func(store.Record) (string, bool)) ([]store.Record, error) {
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
		if f.groupsActive() {
			if g, counted := group(r); counted && f.ExcludesGroup(g) {
				continue
			}
		}
		t := title(r.WindowName)
		if (tf.include.Empty() || tf.include.Match(t)) && !tf.exclude.Match(t) {
			kept = append(kept, r)
		}
	}
	return kept, nil
}
