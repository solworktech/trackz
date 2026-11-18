package highlight

import "testing"

func TestSQL(t *testing.T) {
	text := "SELECT name, 42 -- note\nFROM t WHERE x = 'a b'"
	runes := []rune(text)
	got := map[string]string{}
	covered := 0
	for _, s := range SQL(text) {
		got[string(runes[s.Start:s.End])] = s.Group
		covered += s.End - s.Start
	}
	if covered != len(runes)-1 { // all but the newline
		t.Errorf("spans cover %d runes, want %d", covered, len(runes)-1)
	}
	for tok, want := range map[string]string{
		"SELECT": "statement", "FROM": "statement", "WHERE": "statement",
		"42": "constant.number", "-- note": "comment", "'a b'": "constant.string",
	} {
		if got[tok] != want {
			t.Errorf("%q: group %q, want %q (all: %v)", tok, got[tok], want, got)
		}
	}
}
