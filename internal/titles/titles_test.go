package titles

import "testing"

func TestDefaultRules(t *testing.T) {
	n := Default()
	for in, want := range map[string]string{
		"(3) YouTube":                           "YouTube",
		"(12+) Feed | LinkedIn":                 "Feed | LinkedIn",
		"[2] Discord | #general":                "Discord | #general",
		"* general | Slack":                     "general | Slack",
		"• Chat - Google":                       "Chat - Google",
		"Inbox (12) - you@gmail.com - Gmail":    "Inbox - you@gmail.com - Gmail",
		"(1) Inbox (4) - you@gmail.com - Gmail": "Inbox - you@gmail.com - Gmail",
		"Chat (2) | Microsoft Teams":            "Chat | Microsoft Teams",
		"report (1).pdf - Document Viewer":      "report (1).pdf - Document Viewer", // a real name
		"Chapter (2) - Book":                    "Chapter (2) - Book",
		"(3)":                                   "(3)", // never emptied
		"vim notes.md":                          "vim notes.md",
		"Watch (2024) trailer - YouTube":        "Watch (2024) trailer - YouTube",
		"  (5)   Messenger  ":                   "Messenger",
		"Messages (7) - Signal":                 "Messages - Signal",
	} {
		if got := n.Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFormat(t *testing.T) {
	text := "# comment\n^\\[\\d+\\]\\s*\n\n - Mozilla Firefox$ =>  \n(foo) => [$1]\n"
	rules, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{{Pattern: `^\[\d+\]\s*`}, {Pattern: `- Mozilla Firefox$`}, {Pattern: `(foo)`, Replace: `[$1]`}}
	if len(rules) != len(want) {
		t.Fatalf("rules = %+v", rules)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Errorf("rule %d = %+v, want %+v", i, rules[i], want[i])
		}
	}
	again, err := Parse(Format(rules))
	if err != nil || len(again) != len(rules) || again[2] != rules[2] {
		t.Errorf("round trip: %+v %v", again, err)
	}
	if _, err := Parse("ok\n(unclosed\n"); err == nil || err.Error()[:6] != "line 2" {
		t.Errorf("bad pattern: %v", err)
	}
	if _, err := New([]Rule{{Pattern: "("}}); err == nil {
		t.Error("New must reject a bad pattern")
	}
}
