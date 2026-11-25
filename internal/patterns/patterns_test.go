package patterns

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, text string
		want          bool
	}{
		{"trackz", "~/src/trackz - VIM", true},
		{"trackz", "trackz-viz", true},
		{"trackz", "TRACKZ issues", true},
		{"trackz", "trackzilla", false},
		{"trackz", "mytrackz", false},
		{"trackz", "my_trackz_repo", true}, // _ is not part of a word
		{"Jim Smith", "Re: Jim Smith's PR", true},
		{"Jim Smith", "Re: Jim  Smith", true},
		{"Jim Smith", "Jim Smithson", false},
		{"#general", "#general | Slack", true},
		{"#general", "x#general", true}, // no boundary before '#'
		{"C++", "C++ reference", true},
		{"C++", "C++x", true},
		{"genorg.com", "genorg.com - Mail", true},
		{"genorg.com", "genorgXcom", false}, // '.' is literal
		{"éclair", "Un éclair", true},
		{"éclair", "Unéclair", false},
		{"/gen(org|co)/", "GenCo portal", true},
		{"/^vim/", "notes - vim", false},
		{"/", "a / b", true}, // a single slash is plain text
	} {
		s, err := Parse(c.pattern)
		if err != nil {
			t.Fatalf("%q: %v", c.pattern, err)
		}
		if got := s.Match(c.text); got != c.want {
			t.Errorf("%q in %q: %v, want %v", c.pattern, c.text, got, c.want)
		}
	}
}

func TestParseLines(t *testing.T) {
	s, err := Parse("  trackz \n\n/gen.*org/\n  ")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Patterns(), "|"); got != "trackz|/gen.*org/" {
		t.Errorf("patterns %q", got)
	}
	if !s.Match("GenXOrg") || !s.Match("trackz") || s.Match("other") {
		t.Error("any pattern must match")
	}
	if e, _ := Parse(" \n "); !e.Empty() || e.Match("x") {
		t.Error("blank text must give an empty set that matches nothing")
	}
	if _, err := Parse("ok\n/a(b/"); err == nil || !strings.Contains(err.Error(), "/a(b/") {
		t.Errorf("invalid regexp: %v", err)
	}
}
