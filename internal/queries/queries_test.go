package queries

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSaveListLoad(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "saved_queries")}
	if l, err := s.List(); err != nil || len(l) != 0 {
		t.Fatalf("missing dir: %v %v", l, err)
	}
	if err := s.Save("Top windows", "SELECT 1", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("billing (Project X)", "SELECT 2\n", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("Top windows", "SELECT 3", false); !errors.Is(err, ErrExists) {
		t.Errorf("want ErrExists, got %v", err)
	}
	if err := s.Save("Top windows", "SELECT 3", true); err != nil {
		t.Fatal(err)
	}
	l, _ := s.List()
	if len(l) != 2 || l[0].Name != "billing (Project X)" || l[1].Name != "Top windows" {
		t.Errorf("list = %+v", l)
	}
	if q, err := s.Load("Top windows"); err != nil || q != "SELECT 3\n" {
		t.Errorf("load = %q, %v", q, err)
	}
	if f := Filter(l, "PROJ"); len(f) != 1 || f[0].Name != "billing (Project X)" {
		t.Errorf("filter = %+v", f)
	}
}

func TestValidateName(t *testing.T) {
	for _, bad := range []string{"", " x", "x ", ".hidden", "a/b", `a\b`, "a:b", "trailing.", "a\nb"} {
		if ValidateName(bad) == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	for _, good := range []string{"Daily totals", "billing-2026_Q1", "Über (alt)"} {
		if err := ValidateName(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
}
