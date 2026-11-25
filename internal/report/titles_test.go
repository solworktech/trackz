package report

import (
	"context"
	"testing"
	"time"
)

func TestMergedTitles(t *testing.T) {
	s, st := newService(t, "UTC")
	base := testNow.Add(-3 * time.Hour)
	add(t, st, "firefox-esr", "Firefox", "(3) YouTube - Mozilla Firefox", base, 300)
	add(t, st, "firefox-esr", "Firefox", "(12) YouTube - Mozilla Firefox", base.Add(10*time.Minute), 300)
	add(t, st, "firefox-esr", "Firefox", "YouTube - Mozilla Firefox", base.Add(20*time.Minute), 300)
	add(t, st, "firefox-esr", "Firefox", "Inbox (4) - me@gmail.com - Gmail - Mozilla Firefox", base.Add(30*time.Minute), 300)
	ctx := context.Background()

	res, err := s.Run(ctx, ViewDashboard, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 2 {
		t.Fatalf("points = %+v", res.Series.Points)
	}
	p := res.Series.Points[0]
	if p.Label != "YouTube - Mozilla Firefox" || p.Value != 900 || p.Detail != "merged from 3 titles" {
		t.Errorf("merged = %+v", p)
	}
	if got := res.Series.Points[1].Label; got != "Inbox - me@gmail.com - Gmail - Mozilla Firefox" {
		t.Errorf("gmail = %q", got)
	}

	// The title filter sees the titles as shown: "(3)" is gone.
	res, err = s.Run(ctx, ViewDashboard, Preset(Today), Filter{Include: []string{"(3)"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 0 {
		t.Errorf("filter on a removed count: %+v", res.Series.Points)
	}

	// norm_title() gives custom queries the same grouping.
	tbl, err := s.CustomQuery(ctx, `SELECT norm_title(window_name) AS label, SUM(focus_end_time - focus_start_time) AS total
		FROM trackz GROUP BY label ORDER BY total DESC`)
	if err != nil {
		t.Fatal(err)
	}
	if len(tbl.Rows) != 2 || tbl.Rows[0][0].Text != "YouTube - Mozilla Firefox" || tbl.Rows[0][1].Int != 900 {
		t.Errorf("norm_title rows = %+v", tbl.Rows)
	}

	// With merging off, titles are reported as recorded.
	cfg := s.Config()
	cfg.MergeTitles = false
	if err := s.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	res, err = s.Run(ctx, ViewDashboard, Preset(Today), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Series.Points) != 4 {
		t.Errorf("unmerged points = %+v", res.Series.Points)
	}
}
