package timefmt

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	tm := time.Date(2025, 2, 8, 17, 53, 6, 0, time.UTC)
	cases := map[string]string{
		"%d/%m/%Y %I:%M:%S%p": "08/02/2025 05:53:06PM",
		"%Y-%m-%d %H:%M":      "2025-02-08 17:53",
		"%a %e %b %y %P":      "Sat  8 Feb 25 pm",
		"%F %T %%":            "2025-02-08 17:53:06 %",
		"%s":                  "1739037186",
	}
	for f, want := range cases {
		if got := Format(tm, f); got != want {
			t.Errorf("Format(%q) = %q, want %q", f, got, want)
		}
	}
	if Validate("%Y-%Q") == nil || Validate("%Y-%") == nil || Validate("") == nil {
		t.Error("Validate should reject bad formats")
	}
	if err := Validate("%d.%m.%Y"); err != nil {
		t.Error(err)
	}
}

func TestDuration(t *testing.T) {
	if got := Duration(102); got != "00:01:42" {
		t.Error(got)
	}
	if got := Duration(53*3600 + 602); got != "53:10:02" {
		t.Error(got)
	}
	if got := Hours(5400); got != "1.5h" {
		t.Error(got)
	}
}
