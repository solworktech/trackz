package gio

import (
	"strings"
	"testing"

	"gioui.org/widget"
)

func TestWordAt(t *testing.T) {
	text := []rune("SELECT SUM(focus_end_time - x),'unix' FROM t\nLIMIT 10")
	cases := map[int]string{
		0:  "SELECT",
		3:  "SELECT",
		6:  "SELECT", // just after the word
		11: "focus_end_time",
		20: "focus_end_time",
		26: "-",
		28: "x",
		29: "),'",
		33: "unix",
		47: "LIMIT",
		52: "10",
	}
	for off, want := range cases {
		s, e := wordAt(text, off)
		if got := string(text[s:e]); got != want {
			t.Errorf("wordAt(%d) = %q, want %q", off, got, want)
		}
	}
	if s, e := lineAt(text, 50); string(text[s:e]) != "LIMIT 10" {
		t.Errorf("lineAt = %q", string(text[s:e]))
	}
}

func TestExtendByWords(t *testing.T) {
	var ed widget.Editor
	text := "SELECT SUM(focus_end_time - start) AS total"
	ed.SetText(text)
	c := multiClick{ed: &ed, text: text, runes: []rune(text)}
	sel := func() string {
		a, b := ed.Selection()
		return string(c.runes[min(a, b):max(a, b)])
	}
	off := strings.Index(text, "end") // inside focus_end_time
	c.unit = 2
	c.anchorFrom, c.anchorTo = c.unitAt(2, off)
	for _, tc := range []struct {
		to   int
		want string
	}{
		{off + 1, "focus_end_time"}, // jitter within the word
		{strings.Index(text, "art"), "focus_end_time - start"},
		{strings.Index(text, "LECT"), "SELECT SUM(focus_end_time"},
		{strings.Index(text, "tal"), "focus_end_time - start) AS total"},
	} {
		c.extendTo(tc.to)
		if got := sel(); got != tc.want {
			t.Errorf("extend to %d: selected %q, want %q", tc.to, got, tc.want)
		}
	}
	// The caret follows the pointer side.
	c.extendTo(0)
	if caret, _ := ed.Selection(); caret != 0 {
		t.Errorf("caret = %d after extending backwards, want 0", caret)
	}
}
