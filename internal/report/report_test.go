package report

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/store"
)

// 2025-07-01 is in British Summer Time (UTC+1).
var testNow = time.Date(2025, 7, 1, 15, 0, 0, 0, time.UTC)

func newService(t *testing.T, tz string) (*Service, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.Default()
	cfg.TimeZone = tz
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbPath := filepath.Join(dir, "trackz.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	s, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: cfgPath, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, st
}

func add(t *testing.T, st *store.Store, proc, app, title string, start time.Time, secs int64) {
	t.Helper()
	ctx := context.Background()
	id, err := st.InsertEvent(ctx, store.Event{ProcessName: proc, AppName: app, WindowName: title, ProcessOwner: "jesse"}, start.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if secs >= 0 {
		if err := st.SetFocusEnd(ctx, id, start.Unix()+secs); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDashboardAndApps(t *testing.T) {
	s, st := newService(t, "Europe/London")
	base := time.Date(2025, 7, 1, 9, 0, 0, 0, time.UTC)
	add(t, st, "firefox-esr", "Firefox", "Docs", base, 600)
	add(t, st, "firefox-esr", "Firefox", "Docs", base.Add(time.Hour), 300)
	add(t, st, "firefox-esr", "Firefox", "Mail", base.Add(2*time.Hour), 400)
	add(t, st, "lxterminal", "Lxterminal", "vim", base.Add(3*time.Hour), 1000)
	add(t, st, "lxterminal", "Lxterminal", "short", base.Add(4*time.Hour), 60) // below threshold
	add(t, st, "kblocks", "KBlocks", "open", base.Add(5*time.Hour), -1)        // still open
	add(t, st, "old", "Old", "yesterday", base.Add(-24*time.Hour), 5000)       // out of range

	d, err := s.Dashboard(context.Background(), Preset(Today), Filter{}, 20)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range d.Series.Points {
		got = append(got, p.Label+"="+FormatValue(Seconds, p.Value))
	}
	want := "vim=00:16:40 Docs=00:15:00 Mail=00:06:40"
	if strings.Join(got, " ") != want {
		t.Errorf("dashboard = %v, want %s", got, want)
	}
	if len(d.Table.Rows) != 3 || d.Table.Rows[1][0].Text != "firefox-esr" {
		t.Errorf("table = %+v", d.Table.Rows)
	}

	apps, _, err := s.topApps(context.Background(), Preset(Today), Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Short records count towards app totals: Lxterminal = 1000 + 60.
	if len(apps.Points) != 2 || apps.Points[0].Label != "Firefox" || apps.Points[0].Value != 1300 ||
		apps.Points[0].Process != "firefox-esr" || apps.Points[1].Value != 1060 {
		t.Errorf("top apps = %+v", apps.Points)
	}
}

func TestTodayUsesDisplayTimeZone(t *testing.T) {
	// 23:30 UTC on 30 June is 00:30 on 1 July in London: it is "today"
	// there, but not in UTC.
	late := time.Date(2025, 6, 30, 23, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		tz   string
		want int
	}{{"Europe/London", 1}, {"UTC", 0}} {
		s, st := newService(t, tc.tz)
		add(t, st, "a", "A", "w", late, 500)
		d, err := s.Dashboard(context.Background(), Preset(Today), Filter{}, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Series.Points) != tc.want {
			t.Errorf("%s: %d points, want %d", tc.tz, len(d.Series.Points), tc.want)
		}
	}
}

func TestActivityHourly(t *testing.T) {
	s, st := newService(t, "Europe/London")
	add(t, st, "a", "A", "w", time.Date(2025, 7, 1, 9, 10, 0, 0, time.UTC), 600)
	add(t, st, "a", "A", "v", time.Date(2025, 7, 1, 9, 40, 0, 0, time.UTC), 300)
	series, err := s.Activity(context.Background(), Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// Today: 00:00..16:00 London = 17 hourly buckets; 09:xx UTC = 10:00 BST.
	if len(series.Points) != 17 {
		t.Fatalf("buckets = %d", len(series.Points))
	}
	p := series.Points[10]
	if p.Label != "10:00" || p.Value != 900 || p.Detail != "2 window switches" {
		t.Errorf("bucket 10 = %+v", p)
	}
}

func TestCustomQueryReadOnly(t *testing.T) {
	s, st := newService(t, "UTC")
	add(t, st, "a", "A", "w", testNow.Add(-time.Hour), 600)
	ctx := context.Background()
	tbl, err := s.CustomQuery(ctx, "-- comment\nSELECT app_name, SUM(focus_end_time - focus_start_time) AS total_time FROM trackz GROUP BY app_name")
	if err != nil {
		t.Fatal(err)
	}
	if len(tbl.Rows) != 1 || tbl.Rows[0][1].Int != 600 {
		t.Fatalf("rows = %+v", tbl.Rows)
	}
	ser, err := SeriesFromTable(tbl)
	if err != nil || ser.Unit != Seconds || ser.Points[0].Label != "A" || ser.Points[0].Value != 600 {
		t.Errorf("series = %+v, %v", ser, err)
	}
	if _, err := s.CustomQuery(ctx, "DELETE FROM trackz"); err == nil {
		t.Error("DELETE must be rejected")
	}
	if _, err := s.CustomQuery(ctx, "WITH x AS (DELETE FROM trackz RETURNING id) SELECT * FROM x"); err == nil {
		t.Error("write inside WITH must be rejected by the read-only connection")
	}
	if n, _ := st.MaxID(ctx); n != 1 {
		t.Errorf("data changed: max id %d", n)
	}
}

func TestCSV(t *testing.T) {
	tbl := &store.Table{Columns: []string{"a", "b"}, Rows: [][]store.Value{{text("x,y"), integer(3)}}}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, tbl); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "a,b\n\"x,y\",3\n" {
		t.Errorf("csv = %q", buf.String())
	}
	path, err := ExportCSV(filepath.Join(t.TempDir(), "out"), tbl)
	if err != nil || !strings.HasSuffix(path, "out.csv") {
		t.Fatal(path, err)
	}
	if b, _ := os.ReadFile(path); string(b) != buf.String() {
		t.Errorf("file = %q", b)
	}
}

func TestFixLegacyTimestamps(t *testing.T) {
	_, st := newService(t, "UTC")
	ctx := context.Background()
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	must := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	end := func(id, at int64) {
		t.Helper()
		if err := st.SetFocusEnd(ctx, id, at); err != nil {
			t.Fatal(err)
		}
	}
	// Lua hook in summer stored true-3600.
	trueStart := time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC).Unix()
	id := must(st.InsertEvent(ctx, store.Event{ProcessName: "a", AppName: "A", WindowName: "w"}, trueStart-3600))
	end(id, trueStart-3600+60)
	// Window closed by the Rust sleep listener with real time, then sleep.
	id2 := must(st.InsertEvent(ctx, store.Event{ProcessName: "b", AppName: "B", WindowName: "w"}, trueStart-3600+60))
	end(id2, trueStart+500)
	sid := must(st.InsertSleep(ctx, trueStart+500))
	end(sid, trueStart+900)
	// Winter record: offset 0, unchanged.
	winter := time.Date(2025, 1, 10, 10, 0, 0, 0, time.UTC).Unix()
	must(st.InsertEvent(ctx, store.Event{ProcessName: "c", AppName: "C", WindowName: "w"}, winter))

	fixes, err := st.FixLegacyTimestamps(ctx, 100, london, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixes) != 2 {
		t.Fatalf("dry run fixes = %+v", fixes)
	}
	if r, _ := st.Get(ctx, id); r.Start != trueStart-3600 {
		t.Error("dry run must not write")
	}
	if _, err := st.FixLegacyTimestamps(ctx, 100, london, false); err != nil {
		t.Fatal(err)
	}
	r1, _ := st.Get(ctx, id)
	r2, _ := st.Get(ctx, id2)
	rs, _ := st.Get(ctx, sid)
	if r1.Start != trueStart || r1.End != trueStart+60 {
		t.Errorf("r1 = %+v", r1)
	}
	if r2.Start != trueStart+60 || r2.End != trueStart+500 {
		t.Errorf("r2 end was real time and must not move: %+v", r2)
	}
	if rs.Start != trueStart+500 {
		t.Errorf("sleep record must not move: %+v", rs)
	}
}

func TestOpenRecordAndHidden(t *testing.T) {
	s, st := newService(t, "Europe/London")
	now := testNow
	// A crash left an old record open: must be ignored.
	add(t, st, "crashed", "Crashed", "x", now.Add(-5*time.Hour), -1)
	add(t, st, "a", "A", "short1", now.Add(-50*time.Minute), 30)
	add(t, st, "a", "A", "short2", now.Add(-45*time.Minute), 60)
	add(t, st, "b", "B", "long", now.Add(-40*time.Minute), 600)
	// The window focused right now: open for 20 minutes.
	add(t, st, "c", "C", "current", now.Add(-20*time.Minute), -1)

	res, err := s.Run(context.Background(), ViewDashboard, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, p := range res.Series.Points {
		got[p.Label] = p.Value
	}
	if len(got) != 2 || got["current"] != 1200 || got["long"] != 600 {
		t.Errorf("points = %v, want current=1200 and long=600", got)
	}
	if res.Hidden.Count != 2 || res.Hidden.Seconds != 90 || res.Hidden.MinDuration != 120 {
		t.Errorf("hidden = %+v", res.Hidden)
	}
}

// The query from the original README/viewer: "AS label" picks the labels and
// a time() string in a column named duration is the value.
func TestCustomQueryLabelAndTimeString(t *testing.T) {
	s, st := newService(t, "UTC")
	add(t, st, "code", "Code", "ui.go", testNow.Add(-3*time.Hour), 3725)
	add(t, st, "slack", "Slack", "Jim", testNow.Add(-2*time.Hour), 90)
	tbl, err := s.CustomQuery(context.Background(), `SELECT process_name, window_name as label,
		time(focus_end_time - focus_start_time,'unixepoch') as duration
		FROM trackz WHERE (focus_end_time - focus_start_time) > 1
		GROUP BY process_name, window_name ORDER BY duration DESC LIMIT 20`)
	if err != nil {
		t.Fatal(err)
	}
	ser, err := SeriesFromTable(tbl)
	if err != nil {
		t.Fatal(err)
	}
	if ser.Unit != Seconds || len(ser.Points) != 2 {
		t.Fatalf("series = %+v", ser)
	}
	if p := ser.Points[0]; p.Label != "ui.go" || p.Value != 3725 || p.Process != "code" {
		t.Errorf("point 0 = %+v", p)
	}
	if p := ser.Points[1]; p.Label != "Jim" || p.Value != 90 {
		t.Errorf("point 1 = %+v", p)
	}
}

func TestSeriesFromTableColumnChoice(t *testing.T) {
	tbl := &store.Table{
		Columns: []string{"n", "app_name", "value"},
		Rows:    [][]store.Value{{integer(5), text("A"), integer(7)}},
	}
	ser, err := SeriesFromTable(tbl)
	if err != nil || ser.Points[0].Label != "A" || ser.Points[0].Value != 7 || ser.Unit != Number {
		t.Errorf("value/app_name preference: %+v %v", ser, err)
	}
	mixed := &store.Table{Columns: []string{"x"}, Rows: [][]store.Value{{text("01:00:00")}, {text("abc")}}}
	if _, err := SeriesFromTable(mixed); err == nil {
		t.Error("text column must not be charted")
	}
}

// Frequent switching produces many short records; they must add up rather
// than be dropped one by one (the bug seen with real data).
func TestShortVisitsAddUp(t *testing.T) {
	s, st := newService(t, "Europe/London")
	start := testNow.Add(-30 * time.Minute)
	for i := 0; i < 10; i++ { // ten 20 s visits to each window, interleaved
		add(t, st, "wezterm-gui", "org.wezfurlong.wezterm", "trackz-go-git", start, 20)
		start = start.Add(20 * time.Second)
		add(t, st, "firefox-esr", "firefox-esr", "YouTube", start, 20)
		start = start.Add(20 * time.Second)
	}
	add(t, st, "xfce4-appfinder", "Xfce4-appfinder", "Application Finder", start, 8)
	res, err := s.Run(context.Background(), ViewDashboard, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 2 || res.Series.Points[0].Value != 200 || res.Series.Points[1].Value != 200 {
		t.Errorf("points = %+v", res.Series.Points)
	}
	if res.Hidden.Count != 1 || res.Hidden.Seconds != 8 {
		t.Errorf("hidden = %+v", res.Hidden)
	}
	apps, hidden, err := s.timePerApp(context.Background(), Preset(Today), Filter{})
	if err != nil || apps.Total()+float64(hidden.Seconds) != 408 {
		t.Errorf("app totals should include every record: %+v %+v %v", apps.Points, hidden, err)
	}
}
