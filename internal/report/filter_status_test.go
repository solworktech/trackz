package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/solworktech/trackz/internal/store"
)

func TestFilter(t *testing.T) {
	s, st := newService(t, "UTC")
	base := testNow.Add(-3 * time.Hour)
	add(t, st, "firefox-esr", "Firefox", "Invoice 42 - Mail", base, 600)
	add(t, st, "firefox-esr", "Firefox", "News", base.Add(time.Hour), 600)
	add(t, st, "lxterminal", "Lxterminal", "vim invoice.go", base.Add(2*time.Hour), 600)
	ctx := context.Background()

	f := Filter{Include: []string{"INVOICE"}}
	res, err := s.Run(ctx, ViewTimeSpent, Preset(Today), f)
	if err != nil {
		t.Fatal(err)
	}
	if res.Series.Total() != 1200 || len(res.Series.Points) != 2 {
		t.Errorf("title filter: %+v", res.Series.Points)
	}
	f.ExcludeApps = []string{"Lxterminal"}
	res, err = s.Run(ctx, ViewDashboard, Preset(Today), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 1 || res.Series.Points[0].Label != "Invoice 42 - Mail" {
		t.Errorf("title + app filter: %+v", res.Series.Points)
	}
	if got := f.Describe(); got != `titles with "INVOICE" · excluding Lxterminal` {
		t.Errorf("describe = %q", got)
	}
	// Several include patterns (any matches), exclusions, regexps; whole
	// words for plain patterns.
	for _, c := range []struct {
		f    Filter
		want float64
	}{
		{Filter{Include: []string{"news", "/invoice\\.go/"}}, 1200},
		{Filter{Include: []string{"invoice"}, Exclude: []string{"mail"}}, 600},
		{Filter{Exclude: []string{"invoice"}}, 600},
		{Filter{Include: []string{"invoic"}}, 0}, // not a whole word
		{Filter{Include: []string{"/invoic/"}}, 1200},
	} {
		res, err := s.Run(ctx, ViewTimeSpent, Preset(Today), c.f)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Series.Total(); got != c.want {
			t.Errorf("%+v: %v seconds, want %v", c.f, got, c.want)
		}
	}
	bad := Filter{Include: []string{"/a(/"}}
	if bad.Validate() == nil {
		t.Error("invalid regexp must not validate")
	}
	if _, err := s.Run(ctx, ViewTimeSpent, Preset(Today), bad); err == nil {
		t.Error("a report with an invalid pattern must fail")
	}
	if got := (Filter{Include: []string{"a", "/b/"}, Exclude: []string{"c"}}).Describe(); got != `titles with "a" or "/b/" · titles without "c"` {
		t.Errorf("describe = %q", got)
	}
	if (Filter{}).Active() || (Filter{Include: []string{"  "}, Exclude: []string{""}}).Active() {
		t.Error("blank filter must be inactive")
	}
	if a, b := (Filter{ExcludeApps: []string{"b", "a"}}).Key(), (Filter{ExcludeApps: []string{"a", "b"}}).Key(); a != b {
		t.Error("key must not depend on order")
	}
	if a, b := (Filter{Include: []string{"x"}}).Key(), (Filter{Exclude: []string{"x"}}).Key(); a == b {
		t.Error("include and exclude must not share a key")
	}
	// The app list ignores the filter, so excluded apps can be re-included.
	apps, err := s.Apps(ctx, Preset(Today))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].App != "Firefox" || apps[0].Value != 1200 {
		t.Errorf("apps = %+v", apps)
	}
	long := Filter{ExcludeApps: []string{"e", "d", "c", "b", "a"}}
	if got := long.Describe(); got != "excluding a, b, c and 2 more apps" {
		t.Errorf("describe = %q", got)
	}
}

func TestStatusLifecycle(t *testing.T) {
	s, _ := newService(t, "UTC")
	ctx := context.Background()
	if _, err := s.StartStatus(ctx, "  "); !errors.Is(err, ErrEmptyStatus) {
		t.Errorf("blank status: %v", err)
	}
	st, err := s.StartStatus(ctx, " Out to lunch ")
	if err != nil {
		t.Fatal(err)
	}
	if st.Text != "Out to lunch" || !st.Start.Equal(testNow.Truncate(time.Second)) {
		t.Errorf("status = %+v", st)
	}
	if _, err := s.StartStatus(ctx, "Meeting"); !errors.Is(err, ErrStatusActive) {
		t.Errorf("second status: %v", err)
	}
	cur, err := s.ActiveStatus(ctx)
	if err != nil || cur == nil || cur.ID != st.ID || cur.Text != "Out to lunch" {
		t.Fatalf("active = %+v, %v", cur, err)
	}
	if err := s.EndStatus(ctx, st); err != nil {
		t.Fatal(err)
	}
	if cur, err := s.ActiveStatus(ctx); err != nil || cur != nil {
		t.Errorf("after end: %+v, %v", cur, err)
	}
}

// Time within a status counts for the status only, not for the window that
// had focus meanwhile (trackz-viz showing "I'm back") or a screen lock.
func TestStatusClipsOverlappingRecords(t *testing.T) {
	s, st := newService(t, "UTC")
	ctx := context.Background()
	base := testNow.Add(-2 * time.Hour)
	// trackz-viz focused for an hour; a 30 minute lunch in the middle.
	add(t, st, "trackz-viz", "trackz-viz", "Trackz Visualization", base, 3600)
	id, err := st.InsertEvent(ctx, store.Event{ProcessName: store.StatusProcessName, AppName: store.StatusAppName,
		WindowName: "Out to lunch"}, base.Add(15*time.Minute).Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetFocusEnd(ctx, id, base.Add(45*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	res, err := s.Run(ctx, ViewDashboard, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, p := range res.Series.Points {
		got[p.Label] = p.Value
	}
	if got["Trackz Visualization"] != 1800 || got["Out to lunch"] != 1800 || res.Series.Total() != 3600 {
		t.Errorf("points = %+v", res.Series.Points)
	}
}

// An open status counts up to now, even after the tracker wrote newer
// records (e.g. the screen locked during lunch).
func TestOpenStatusCountsLive(t *testing.T) {
	s, st := newService(t, "UTC")
	ctx := context.Background()
	if _, err := s.StartStatus(ctx, "Out to lunch"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertSleep(ctx, testNow.Unix()); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return testNow.Add(20 * time.Minute) }
	res, err := s.Run(ctx, ViewTimeSpent, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 1 || res.Series.Points[0].App != store.StatusAppName || res.Series.Points[0].Value != 1200 {
		t.Errorf("points = %+v", res.Series.Points)
	}
}

func TestClipStatuses(t *testing.T) {
	rec := func(proc string, a, b int64) store.Record { return store.Record{ProcessName: proc, Start: a, End: b} }
	got := clipStatuses([]store.Record{
		rec("x", 0, 100),
		rec(store.StatusProcessName, 10, 20),
		rec(store.StatusProcessName, 50, 60),
		rec("y", 15, 55), // entirely between and within the statuses
		rec("z", 55, 58), // entirely within one
	})
	var spans [][2]int64
	for _, r := range got {
		if r.ProcessName != store.StatusProcessName {
			spans = append(spans, [2]int64{r.Start, r.End})
		}
	}
	want := [][2]int64{{0, 10}, {20, 50}, {60, 100}, {20, 50}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %v, want %v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("spans = %v, want %v", spans, want)
			break
		}
	}
}
