package groups

import (
	"strings"
	"testing"
)

func TestGroup(t *testing.T) {
	m, err := New([]Group{
		{Name: "trackz", Include: []string{"trackz"}},
		{
			Name:      "GenOrg",
			Include:   []string{"GenOrg", "Jim Smith", "/widget-(api|ui)/"},
			Exclude:   []string{"newsletter"},
			Processes: []string{"GenOrgVPN"},
			Windows:   []Window{{"xterm", "bash"}},
		},
		{Name: "Other", Windows: []Window{{"xterm", "bash"}}}, // GenOrg's wins
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ process, title, want string }{
		{"firefox", "trackz issues - GitHub", "trackz"},
		{"firefox", "GenOrg: trackz integration", "trackz"}, // order decides
		{"thunderbird", "Re: Jim Smith's PR", "GenOrg"},
		{"vim", "~/src/widget-api/main.go", "GenOrg"},
		{"thunderbird", "GenOrg newsletter", ""}, // excluded
		{"genorgvpn", "Connected", "GenOrg"},     // process, ignoring case
		{"genorgvpn", "newsletter", ""},          // exclusions apply to processes too
		{"xterm", "bash", "GenOrg"},              // by hand
		{"xterm", "zsh", ""},
		{"firefox", "trackzilla", ""}, // whole words
	} {
		if got := m.Group(c.process, c.title); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.process, c.title, got, c.want)
		}
	}
	if !m.Candidate("GenOrg", "thunderbird", "GenOrg newsletter") || !m.Candidate("GenOrg", "GENORGVPN", "x") ||
		m.Candidate("GenOrg", "xterm", "bash") || m.Candidate("Nope", "x", "trackz") {
		t.Error("Candidate: include patterns and processes, ignoring exclusions and assignments")
	}
	var none *Matcher
	if none.Group("x", "trackz") != "" {
		t.Error("a nil matcher has no groups")
	}
}

func TestNewChecks(t *testing.T) {
	for _, c := range []struct {
		groups []Group
		err    string
	}{
		{[]Group{{Name: " "}}, "has no name"},
		{[]Group{{Name: "A"}, {Name: "a"}}, "defined twice"},
		{[]Group{{Name: "A", Include: []string{"/(/"}}}, `task group "A": pattern /(/`},
		{[]Group{{Name: "A", Exclude: []string{"/[/"}}}, `task group "A": pattern /[/`},
	} {
		if err := Validate(c.groups); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%+v: error %v, want %q", c.groups, err, c.err)
		}
	}
	if err := Validate(nil); err != nil {
		t.Error(err)
	}
}
