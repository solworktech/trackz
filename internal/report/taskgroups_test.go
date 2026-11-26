package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/groups"
	"github.com/solworktech/trackz/internal/store"
)

func TestTaskGroups(t *testing.T) {
	s, st := newService(t, "UTC")
	cfg := s.Config()
	cfg.TaskGroups = []groups.Group{
		{Name: "trackz", Include: []string{"trackz"}},
		{Name: "GenOrg", Include: []string{"GenOrg", "Jim Smith"}, Exclude: []string{"newsletter"},
			Windows: []groups.Window{{Process: "xterm", Title: "bash"}}},
		{Name: "Breaks", Include: []string{"lunch"}},
	}
	if err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	base := testNow.Add(-6 * time.Hour)
	add(t, st, "firefox", "Firefox", "trackz issues", base, 600)
	add(t, st, "vim", "Vim", "GenOrg: trackz integration", base.Add(10*time.Minute), 300) // trackz: first group
	add(t, st, "mail", "Mail", "(2) Re: Jim Smith", base.Add(20*time.Minute), 400)        // merged title
	add(t, st, "mail", "Mail", "GenOrg newsletter", base.Add(30*time.Minute), 500)        // excluded: ungrouped
	add(t, st, "xterm", "XTerm", "bash", base.Add(40*time.Minute), 700)                   // by hand
	add(t, st, "xterm", "XTerm", "zsh", base.Add(55*time.Minute), 200)                    // ungrouped
	add(t, st, store.SleepProcessName, store.SleepAppName, "locked", base.Add(time.Hour), 900)
	add(t, st, store.StatusProcessName, StatusApp, "Coffee break", base.Add(2*time.Hour), 300)
	add(t, st, store.StatusProcessName, StatusApp, "Out to lunch", base.Add(3*time.Hour), 1200)
	ctx := context.Background()

	res, err := s.Run(ctx, ViewTaskGroups, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range res.Series.Points {
		got = append(got, p.Label+"="+FormatValue(Seconds, p.Value))
	}
	// Ungrouped comes last; screen lock and the coffee break (a status in
	// no group) are not counted.
	want := "Breaks=00:20:00 GenOrg=00:18:20 trackz=00:15:00 Ungrouped=00:11:40"
	if strings.Join(got, " ") != want {
		t.Errorf("groups %v, want %s", got, want)
	}
	last := res.Series.Points[len(res.Series.Points)-1]
	if last.Item != (Item{Ungrouped: true}) || res.Series.Points[1].Item != (Item{Group: "GenOrg"}) {
		t.Errorf("items %+v, %+v", res.Series.Points[1].Item, last.Item)
	}

	for _, c := range []struct {
		it   Item
		want string
	}{
		{Item{Ungrouped: true}, "GenOrg newsletter|zsh"},
		{Item{Group: "GenOrg"}, "bash|Re: Jim Smith"}, // merged title
		{Item{Group: "trackz"}, "trackz issues|GenOrg: trackz integration"},
	} {
		w, err := s.GroupWindows(ctx, c.it, Preset(Today), Filter{})
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, p := range w.Series.Points {
			titles = append(titles, p.Label)
		}
		if strings.Join(titles, "|") != c.want {
			t.Errorf("%+v: windows %v, want %s", c.it, titles, c.want)
		}
	}

	tl, err := s.Timeline(ctx, Item{Group: "GenOrg"}, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if tl.Total() != 1100 {
		t.Errorf("GenOrg timeline total %v", tl.Total())
	}
	tl, err = s.Timeline(ctx, Item{Ungrouped: true}, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if tl.Total() != 700 {
		t.Errorf("ungrouped timeline total %v", tl.Total())
	}

	// Custom queries group the same way (task_group is NULL for none, and
	// leaving out screen lock and breaks is up to the query).
	tbl, err := s.CustomQuery(ctx, `SELECT COALESCE(task_group(process_name, window_name), '-') AS g,
		SUM(focus_end_time - focus_start_time) AS secs FROM trackz GROUP BY g ORDER BY g`)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, r := range tbl.Rows {
		rows = append(rows, r[0].String()+"="+r[1].String())
	}
	if strings.Join(rows, " ") != "-=1900 Breaks=1200 GenOrg=1100 trackz=900" {
		t.Errorf("task_group(): %v", rows)
	}
}

func TestInvalidTaskGroupsAreKept(t *testing.T) {
	s, _ := newService(t, "UTC")
	cfg := s.Config()
	cfg.TaskGroups = []groups.Group{{Name: "A", Include: []string{"/(/"}}}
	if err := s.UpdateConfig(cfg); err == nil {
		t.Fatal("invalid groups must not be saved")
	}
	// A config file edited by hand: kept, reported, unused.
	if err := s.setConfig(cfg); err == nil || !strings.Contains(err.Error(), "not used until fixed") {
		t.Errorf("setConfig: %v", err)
	}
	if len(s.Config().TaskGroups) != 1 || s.TaskGroupsError() == nil {
		t.Error("invalid groups must be kept and reported")
	}
}

func TestReloadAndModifyKeepHandEdits(t *testing.T) {
	s, st := newService(t, "UTC")
	add(t, st, "firefox", "Firefox", "Inbox - Gmail", testNow.Add(-time.Hour), 600)
	ctx := context.Background()
	// Edited by hand while trackz-viz runs.
	cfg, err := config.Load(s.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	cfg.TaskGroups = []groups.Group{{Name: "Google", Include: []string{"/gmail|youtube/i"}}}
	if err := config.Save(s.ConfigPath(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Run(ctx, ViewTaskGroups, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 1 || res.Series.Points[0].Label != "Google" {
		t.Errorf("after reload: %+v", res.Series.Points)
	}
	// Another hand edit, then a change from the app: both are kept.
	cfg.TaskGroups = append(cfg.TaskGroups, groups.Group{Name: "trackz", Include: []string{"trackz"}})
	if err := config.Save(s.ConfigPath(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ModifyConfig(func(c config.Config) (config.Config, error) {
		c.RowLimit = 7
		return c, nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(s.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if saved.RowLimit != 7 || len(saved.TaskGroups) != 2 || len(s.Config().TaskGroups) != 2 {
		t.Errorf("saved %d rows, %d groups", saved.RowLimit, len(saved.TaskGroups))
	}
}

func TestPreviewGroup(t *testing.T) {
	s, st := newService(t, "UTC")
	base := testNow.Add(-5 * time.Hour)
	add(t, st, "firefox", "Firefox", "GenOrg portal", base, 600)
	add(t, st, "mail", "Mail", "GenOrg newsletter", base.Add(15*time.Minute), 300)
	add(t, st, "vim", "Vim", "GenOrg: trackz integration", base.Add(30*time.Minute), 200)
	add(t, st, "genorgvpn", "VPN", "Connected", base.Add(time.Hour), 100)
	add(t, st, "xterm", "XTerm", "zsh", base.Add(2*time.Hour), 50)
	draft := []groups.Group{
		{Name: "trackz", Include: []string{"trackz"}},
		{Name: "GenOrg", Include: []string{"GenOrg"}, Exclude: []string{"newsletter"}, Processes: []string{"GenOrgVPN"}},
	}
	ctx := context.Background()
	prev, err := s.PreviewGroup(ctx, draft, "GenOrg", Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var in, missed []string
	for _, p := range prev.Windows {
		in = append(in, p.Label)
	}
	for _, p := range prev.Missed {
		missed = append(missed, p.Label+" ("+p.Detail+")")
	}
	if strings.Join(in, "|") != "GenOrg portal|Connected" {
		t.Errorf("windows %v", in)
	}
	if strings.Join(missed, "|") != "GenOrg newsletter (excluded)|GenOrg: trackz integration (in trackz)" {
		t.Errorf("missed %v", missed)
	}
	if _, err := s.PreviewGroup(ctx, []groups.Group{{Name: ""}}, "x", Preset(Today), Filter{}); err == nil {
		t.Error("an invalid draft must fail")
	}
	procs, err := s.Processes(ctx, Preset(Today))
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 5 || procs[0].Label != "firefox" {
		t.Errorf("processes %+v", procs)
	}
}
