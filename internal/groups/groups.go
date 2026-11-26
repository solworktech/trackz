// Package groups assigns window records to task groups (customers,
// projects): named lists of title patterns and process names, plus windows
// assigned by hand. A record belongs to at most one group:
//
//  1. a group a window (process and title) was assigned to by hand;
//  2. otherwise the first group, in order, whose include patterns match the
//     title or whose processes contain the process, and whose exclude
//     patterns do not match the title.
//
// Titles are the titles as reports show them (merged, see package titles);
// the patterns use the syntax of package patterns.
package groups

import (
	"fmt"
	"strings"

	"github.com/solworktech/trackz/internal/patterns"
)

// Group is a task group as stored in the config file.
type Group struct {
	Name string `json:"name"`
	// Include and Exclude are title patterns, one per entry.
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
	// Processes are process names (exact, ignoring case) whose windows
	// belong to the group whatever their title (still subject to Exclude).
	Processes []string `json:"processes,omitempty"`
	// Windows were assigned to the group by hand.
	Windows []Window `json:"windows,omitempty"`
}

// Window identifies a window as the Dashboard shows it: a process and a
// (merged) title.
type Window struct {
	Process string `json:"process"`
	Title   string `json:"title"`
}

// Matcher assigns records to groups. The zero Matcher (and nil) has none.
type Matcher struct {
	groups []compiled
	manual map[Window]string // window -> group name
}

type compiled struct {
	name             string
	include, exclude patterns.Set
	processes        map[string]bool // lower case
}

// New compiles groups, checking that names are set and unique and that
// the patterns are valid.
func New(groups []Group) (*Matcher, error) {
	m := &Matcher{manual: map[Window]string{}}
	seen := map[string]bool{}
	for i, g := range groups {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			return nil, fmt.Errorf("task group %d has no name", i+1)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("task group %q is defined twice", name)
		}
		seen[key] = true
		c := compiled{name: name, processes: map[string]bool{}}
		var err error
		if c.include, err = patterns.Compile(g.Include); err != nil {
			return nil, fmt.Errorf("task group %q: %w", name, err)
		}
		if c.exclude, err = patterns.Compile(g.Exclude); err != nil {
			return nil, fmt.Errorf("task group %q: %w", name, err)
		}
		for _, p := range g.Processes {
			if p = strings.TrimSpace(p); p != "" {
				c.processes[strings.ToLower(p)] = true
			}
		}
		for _, w := range g.Windows {
			if _, dup := m.manual[w]; !dup { // the first assignment wins
				m.manual[w] = name
			}
		}
		m.groups = append(m.groups, c)
	}
	return m, nil
}

// Validate checks groups as New does.
func Validate(groups []Group) error {
	_, err := New(groups)
	return err
}

// Group returns the group of a window of process with title (as reports
// show it); "" when it belongs to none.
func (m *Matcher) Group(process, title string) string {
	if m == nil {
		return ""
	}
	if g, ok := m.manual[Window{process, title}]; ok {
		return g
	}
	proc := strings.ToLower(process)
	for _, g := range m.groups {
		if (g.include.Match(title) || g.processes[proc]) && !g.exclude.Match(title) {
			return g.name
		}
	}
	return ""
}

// Candidate reports whether the group named name would take a window of
// process with title on its own: one of its include patterns matches the
// title or its processes contain the process, ignoring its exclusions,
// assignments by hand and the groups before it (for showing near misses).
func (m *Matcher) Candidate(name, process, title string) bool {
	if m == nil {
		return false
	}
	for _, g := range m.groups {
		if g.name == name {
			return g.include.Match(title) || g.processes[strings.ToLower(process)]
		}
	}
	return false
}
