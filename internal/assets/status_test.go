package assets

import (
	"io/fs"
	"strings"
	"testing"
)

func TestStatusIcons(t *testing.T) {
	for status, want := range map[string]string{
		"Out to lunch":   "lunch",
		"Meeting":        "meeting",
		"Coffee break":   "coffee",
		"Phone call":     "phone",
		"Away from desk": "away",
		"Errand":         "errand",
		"Dentist":        "status",
	} {
		if got := StatusIconName(status); got != want {
			t.Errorf("StatusIconName(%q) = %q, want %q", status, got, want)
		}
	}
	// Every icon parses strictly and draws something.
	names, err := fs.Glob(statusFiles, "status/*.svg")
	if err != nil || len(names) == 0 {
		t.Fatalf("no status icons: %v", err)
	}
	for _, n := range names {
		name := strings.TrimSuffix(strings.TrimPrefix(n, "status/"), ".svg")
		img := statusIconByName(name, 24)
		if img == nil {
			t.Errorf("%s: not drawn", name)
		}
	}
}
