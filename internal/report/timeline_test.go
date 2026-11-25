package report

import (
	"context"
	"testing"
	"time"
)

func TestSegmentFor(t *testing.T) {
	s, _ := newService(t, "Europe/London")
	day := func(m time.Month, d int) Date { return Date{2025, m, d} }
	custom := func(from, to Date) Range {
		r, err := CustomRange(from, to)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, c := range []struct {
		name string
		r    Range
		want Segment
	}{
		{"today", Preset(Today), Hourly},
		{"one custom day", custom(day(6, 3), day(6, 3)), Hourly},
		{"two custom days", custom(day(6, 3), day(6, 4)), Daily},
		{"last 7 days", Preset(Last7Days), Daily},
		{"last 30 days", Preset(Last30Days), Daily},
		{"31 days", custom(day(5, 1), day(5, 31)), Daily},
		{"32 days", custom(day(5, 1), day(6, 1)), Weekly},
		{"last 90 days", Preset(Last90Days), Weekly},
	} {
		if got := s.SegmentFor(c.r); got != c.want {
			t.Errorf("%s: segment %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTimelineHourlySplitsRecords(t *testing.T) {
	s, st := newService(t, "Europe/London")
	// 09:50 to 10:20 London time (BST): 10 minutes at 09:00, 20 at 10:00.
	at := time.Date(2025, 7, 1, 8, 50, 0, 0, time.UTC)
	add(t, st, "firefox-esr", "Firefox", "Docs", at, 1800)
	add(t, st, "firefox-esr", "Firefox", "Docs", at.Add(2*time.Hour), 60) // 11:50
	add(t, st, "firefox-esr", "Firefox", "Mail", at.Add(3*time.Hour), 600)
	add(t, st, "lxterminal", "Lxterminal", "Docs", at.Add(4*time.Hour), 600)

	ctx := context.Background()
	ser, err := s.Timeline(ctx, Item{Process: "firefox-esr", Title: "Docs"}, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// Today until now, 16:00 London time: an hour each from 00:00 to 15:00.
	if len(ser.Points) != 16 || ser.Points[0].Label != "00:00" || ser.Points[15].Label != "15:00" {
		t.Fatalf("points %d, first %q", len(ser.Points), ser.Points[0].Label)
	}
	want := map[string]float64{"09:00": 600, "10:00": 1200, "11:00": 60}
	for _, p := range ser.Points {
		if p.Value != want[p.Label] {
			t.Errorf("%s: %v seconds, want %v", p.Label, p.Value, want[p.Label])
		}
	}
	if d := ser.Points[10].Detail; d != "1 focus period" {
		t.Errorf("10:00 detail %q", d)
	}
	if ser.Title != "Focus time per hour" {
		t.Errorf("title %q", ser.Title)
	}
	if got := len(ser.WithoutEmpty().Points); got != 3 {
		t.Errorf("WithoutEmpty: %d points, want 3", got)
	}

	app, err := s.Timeline(ctx, Item{App: "Firefox"}, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if v := app.Total(); v != 1800+60+600 {
		t.Errorf("app total %v", v)
	}
	// The filter applies too.
	none, err := s.Timeline(ctx, Item{App: "Firefox"}, Preset(Today), Filter{ExcludeApps: []string{"Firefox"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := none.Total(); v != 0 {
		t.Errorf("filtered total %v", v)
	}
}

func TestTimelineDailyAndWeekly(t *testing.T) {
	s, st := newService(t, "UTC")
	// Across midnight: 23:30 on 3 June to 00:45 on 4 June.
	add(t, st, "vim", "Vim", "notes", time.Date(2025, 6, 3, 23, 30, 0, 0, time.UTC), 75*60)
	add(t, st, "vim", "Vim", "notes", time.Date(2025, 6, 10, 9, 0, 0, 0, time.UTC), 600)
	ctx := context.Background()

	r, err := CustomRange(Date{2025, 6, 1}, Date{2025, 6, 30})
	if err != nil {
		t.Fatal(err)
	}
	daily, err := s.Timeline(ctx, Item{App: "Vim"}, r, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(daily.Points) != 30 {
		t.Fatalf("%d days", len(daily.Points))
	}
	if a, b := daily.Points[2].Value, daily.Points[3].Value; a != 30*60 || b != 45*60 {
		t.Errorf("3 and 4 June: %v and %v seconds", a, b)
	}

	// 8 May (a Thursday) to 30 June: weeks from Monday 5 May.
	r, err = CustomRange(Date{2025, 5, 8}, Date{2025, 6, 30})
	if err != nil {
		t.Fatal(err)
	}
	weekly, err := s.Timeline(ctx, Item{App: "Vim"}, r, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	first := weekly.Points[0].Label
	if first != s.Config().FormatDate(time.Date(2025, 5, 5, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first week %q", first)
	}
	// Week of 2 June: 75 minutes; of 9 June: 10.
	got := map[string]float64{}
	for _, p := range weekly.Points {
		got[p.Label] = p.Value
	}
	w := func(d int) string { return s.Config().FormatDate(time.Date(2025, 6, d, 0, 0, 0, 0, time.UTC)) }
	if got[w(2)] != 75*60 || got[w(9)] != 600 {
		t.Errorf("weeks: %v", got)
	}
	if len(weekly.Points) != 9 { // 5 May .. 30 June (a Monday)
		t.Errorf("%d weeks", len(weekly.Points))
	}
}

func TestPointSpans(t *testing.T) {
	s, st := newService(t, "UTC")
	add(t, st, "vim", "Vim", "notes", time.Date(2025, 7, 1, 8, 0, 0, 0, time.UTC), 600)
	add(t, st, "vim", "Vim", "notes", time.Date(2025, 7, 1, 13, 0, 0, 0, time.UTC), 900)
	ctx := context.Background()

	d, err := s.Dashboard(ctx, Preset(Today), Filter{}, 20)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Series.Points[0]
	if p.Item != (Item{Process: "vim", Title: "notes"}) {
		t.Errorf("item %+v", p.Item)
	}
	if p.Span != "First 08:00:00 · last 13:15:00" {
		t.Errorf("today span %q", p.Span)
	}
	res, err := s.Run(ctx, ViewTimeSpent, Preset(Last7Days), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	p = res.Series.Points[0]
	if p.Item != (Item{App: "Vim"}) {
		t.Errorf("item %+v", p.Item)
	}
	cfg := s.Config()
	want := "First " + cfg.FormatDateTime(time.Unix(p.First, 0)) + " · last " + cfg.FormatDateTime(time.Unix(p.Last, 0))
	if p.Span != want || p.First == 0 {
		t.Errorf("span %q, want %q", p.Span, want)
	}
}
