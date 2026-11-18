package report

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/lockfile"
	"github.com/solworktech/trackz/internal/queries"
	"github.com/solworktech/trackz/internal/store"
	"github.com/solworktech/trackz/internal/timefmt"
)

// Service is the controller used by frontends. It is safe for concurrent
// use, so a frontend can run queries on a background goroutine.
type Service struct {
	st         *store.Store // read-write: predefined reports
	ro         *store.Store // read-only: user-supplied queries
	configPath string
	now        func() time.Time

	mu  sync.RWMutex
	cfg config.Config
}

// Options configure Open.
type Options struct {
	// DBPath overrides the database location (see config.ResolveDBPath).
	DBPath string
	// ConfigPath overrides the config file location (see config.Path).
	ConfigPath string
	// Now is used instead of time.Now (tests).
	Now func() time.Time
}

// Open loads the configuration and opens the database.
func Open(ctx context.Context, opts Options) (*Service, error) {
	cfgPath := opts.ConfigPath
	if cfgPath == "" {
		p, err := config.Path()
		if err != nil {
			return nil, err
		}
		cfgPath = p
	}
	cfg, cfgErr := config.Load(cfgPath)
	dbPath, err := config.ResolveDBPath(opts.DBPath, cfg)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	ro, err := store.OpenReadOnly(ctx, dbPath)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	s := &Service{st: st, ro: ro, configPath: cfgPath, cfg: cfg, now: opts.Now}
	if s.now == nil {
		s.now = time.Now
	}
	// A broken config file is not fatal: defaults are used and the error
	// is reported so the frontend can show it.
	if cfgErr != nil {
		return s, fmt.Errorf("config: %w", cfgErr)
	}
	return s, nil
}

// Close releases the database handles.
func (s *Service) Close() error {
	return errors.Join(s.ro.Close(), s.st.Close())
}

// DBPath returns the database in use.
func (s *Service) DBPath() string { return s.st.Path() }

// ConfigPath returns the config file location.
func (s *Service) ConfigPath() string { return s.configPath }

// Config returns the current settings.
func (s *Service) Config() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// UpdateConfig validates, applies and saves new settings. The database path
// cannot be changed at runtime; a changed db_path takes effect on restart.
func (s *Service) UpdateConfig(cfg config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(s.configPath, cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return nil
}

// Location returns the display time zone.
func (s *Service) Location() *time.Location { return s.Config().Location() }

// Today returns the current date in the display time zone.
func (s *Service) Today() Date { return DateOf(s.now().In(s.Location())) }

// RangeLabel describes r for headings: "Today", or "<from> to <to>" in the
// configured date format.
func (s *Service) RangeLabel(r Range) string {
	if r.Kind == Today {
		return "Today"
	}
	cfg := s.Config()
	from, to := r.Bounds(s.now(), cfg.Location())
	return cfg.FormatDate(from) + " to " + cfg.FormatDate(to)
}

// FormatTime renders a Unix timestamp in the display zone and format.
func (s *Service) FormatTime(unix int64) string {
	return s.Config().FormatDateTime(time.Unix(unix, 0))
}

func (s *Service) records(ctx context.Context, r Range) ([]store.Record, error) {
	from, to := r.Bounds(s.now(), s.Config().Location())
	return s.st.Records(ctx, from.Unix(), to.Unix(), 0, s.now().Unix())
}

// Run produces the result for one of the predefined views.
func (s *Service) Run(ctx context.Context, v View, r Range) (*Result, error) {
	res := &Result{View: v, Range: r, Generated: s.now()}
	var err error
	switch v {
	case ViewDashboard:
		var d *Dashboard
		d, err = s.Dashboard(ctx, r, s.Config().RowLimit)
		if d != nil {
			res.Kind, res.Series, res.Table, res.Hidden = PieChart, d.Series, d.Table, d.Hidden
		}
	case ViewTopApps:
		res.Kind = BarChart
		res.Series, res.Hidden, err = s.topApps(ctx, r, 10)
	case ViewTimeSpent:
		res.Kind = PieChart
		res.Series, res.Hidden, err = s.timePerApp(ctx, r)
	case ViewWindowActivity:
		res.Kind = LineChart
		res.Series, err = s.Activity(ctx, r)
	case ViewDailyStats:
		res.Kind = TimeSeriesChart
		res.Series, res.Table, err = s.Daily(ctx, r)
	default:
		return nil, fmt.Errorf("view %s has no predefined report", v)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Dashboard returns the top `limit` (process, window) pairs by focus time.
func (s *Service) Dashboard(ctx context.Context, r Range, limit int) (*Dashboard, error) {
	recs, err := s.records(ctx, r)
	if err != nil {
		return nil, err
	}
	type key struct{ proc, window string }
	totals := map[key]*Point{}
	var order []key
	for _, rec := range recs {
		k := key{rec.ProcessName, rec.WindowName}
		p := totals[k]
		if p == nil {
			p = &Point{Label: rec.WindowName, Process: rec.ProcessName, App: rec.AppName}
			totals[k] = p
			order = append(order, k)
		}
		p.Value += float64(rec.Duration())
	}
	pts := make([]Point, 0, len(order))
	for _, k := range order {
		pts = append(pts, *totals[k])
	}
	pts, hidden := s.dropSmall(pts)
	pts = topN(pts, limit)
	series := Series{Title: "Top activities", Unit: Seconds, Points: pts}

	t := &store.Table{Columns: []string{"process_name", "window_name", "duration", "seconds"}}
	for _, p := range pts {
		t.Rows = append(t.Rows, []store.Value{
			text(p.Process), text(p.Label),
			text(timefmt.Duration(int64(p.Value))), integer(int64(p.Value)),
		})
	}
	return &Dashboard{Series: series, Table: t, Hidden: hidden}, nil
}

// dropSmall removes entries whose total is below the configured minimum
// (config.MinDurationSeconds) and reports what was removed. Individual
// records are never dropped: many short visits to a window add up.
func (s *Service) dropSmall(pts []Point) ([]Point, Hidden) {
	min := s.Config().MinDurationSeconds
	h := Hidden{MinDuration: min}
	kept := pts[:0]
	for _, p := range pts {
		if p.Value < float64(min) {
			h.Count++
			h.Seconds += int64(p.Value)
			continue
		}
		kept = append(kept, p)
	}
	return kept, h
}

// appTotals groups records by app_name. For each app, Process is the process
// name with the most time (used for icons).
func appTotals(recs []store.Record) []Point {
	type agg struct {
		total float64
		procs map[string]float64
	}
	byApp := map[string]*agg{}
	var order []string
	for _, rec := range recs {
		a := byApp[rec.AppName]
		if a == nil {
			a = &agg{procs: map[string]float64{}}
			byApp[rec.AppName] = a
			order = append(order, rec.AppName)
		}
		d := float64(rec.Duration())
		a.total += d
		a.procs[rec.ProcessName] += d
	}
	pts := make([]Point, 0, len(order))
	for _, app := range order {
		a := byApp[app]
		best, bestT := "", -1.0
		for p, t := range a.procs {
			if t > bestT || (t == bestT && p < best) {
				best, bestT = p, t
			}
		}
		pts = append(pts, Point{Label: app, App: app, Process: best, Value: a.total})
	}
	return pts
}

// TopApps returns the `limit` applications with the most focus time.
func (s *Service) TopApps(ctx context.Context, r Range, limit int) (Series, error) {
	ser, _, err := s.topApps(ctx, r, limit)
	return ser, err
}

func (s *Service) topApps(ctx context.Context, r Range, limit int) (Series, Hidden, error) {
	recs, err := s.records(ctx, r)
	if err != nil {
		return Series{}, Hidden{}, err
	}
	pts, hidden := s.dropSmall(appTotals(recs))
	return Series{
		Title:  fmt.Sprintf("Top %d apps by total focus time", limit),
		Unit:   Seconds,
		Points: topN(pts, limit),
	}, hidden, nil
}

// TimePerApp returns focus time for every application.
func (s *Service) TimePerApp(ctx context.Context, r Range) (Series, error) {
	ser, _, err := s.timePerApp(ctx, r)
	return ser, err
}

func (s *Service) timePerApp(ctx context.Context, r Range) (Series, Hidden, error) {
	recs, err := s.records(ctx, r)
	if err != nil {
		return Series{}, Hidden{}, err
	}
	pts, hidden := s.dropSmall(appTotals(recs))
	return Series{Title: "Total time spent per app", Unit: Seconds, Points: topN(pts, 0)}, hidden, nil
}

// Activity returns focus time over time: hourly buckets for ranges of up to
// two days, daily buckets otherwise. Each record counts in the bucket where
// its focus started. Detail holds the number of window switches.
func (s *Service) Activity(ctx context.Context, r Range) (Series, error) {
	recs, err := s.records(ctx, r)
	if err != nil {
		return Series{}, err
	}
	cfg := s.Config()
	loc := cfg.Location()
	from, to := r.Bounds(s.now(), loc)
	hourly := to.Sub(from) <= 48*time.Hour

	bucketStart := func(t time.Time) time.Time {
		t = t.In(loc)
		if hourly {
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		}
		return DateOf(t).In(loc)
	}
	next := func(t time.Time) time.Time {
		if hourly {
			return t.Add(time.Hour)
		}
		return DateOf(t).AddDays(1).In(loc)
	}
	label := func(t time.Time) string {
		if hourly {
			if DateOf(from.In(loc)) != DateOf(to.In(loc)) {
				return timefmt.Format(t, "%a %H:00")
			}
			return timefmt.Format(t, "%H:00")
		}
		return cfg.FormatDate(t)
	}

	idx := map[int64]int{}
	var pts []Point
	var counts []int
	for b := bucketStart(from); !b.After(to); b = next(b) {
		idx[b.Unix()] = len(pts)
		pts = append(pts, Point{Label: label(b)})
		counts = append(counts, 0)
	}
	for _, rec := range recs {
		if i, ok := idx[bucketStart(time.Unix(rec.Start, 0)).Unix()]; ok {
			pts[i].Value += float64(rec.Duration())
			counts[i]++
		}
	}
	for i := range pts {
		pts[i].Detail = strconv.Itoa(counts[i]) + " window switches"
	}
	title := "Window activity per day"
	if hourly {
		title = "Window activity per hour"
	}
	return Series{Title: title, Unit: Seconds, Points: pts}, nil
}

// Daily returns total focus time and number of distinct apps per day in the
// display time zone.
func (s *Service) Daily(ctx context.Context, r Range) (Series, *store.Table, error) {
	recs, err := s.records(ctx, r)
	if err != nil {
		return Series{}, nil, err
	}
	cfg := s.Config()
	loc := cfg.Location()
	type day struct {
		total float64
		apps  map[string]bool
	}
	days := map[Date]*day{}
	var order []Date
	for _, rec := range recs {
		d := DateOf(time.Unix(rec.Start, 0).In(loc))
		x := days[d]
		if x == nil {
			x = &day{apps: map[string]bool{}}
			days[d] = x
			order = append(order, d)
		}
		x.total += float64(rec.Duration())
		x.apps[rec.AppName] = true
	}
	sort.Slice(order, func(i, j int) bool { return order[i].Before(order[j]) })
	series := Series{Title: "Daily usage", Unit: Seconds}
	t := &store.Table{Columns: []string{"day", "total_time", "seconds", "app_count"}}
	for _, d := range order {
		x := days[d]
		label := cfg.FormatDate(d.In(loc))
		series.Points = append(series.Points, Point{
			Label:  label,
			Value:  x.total,
			Detail: fmt.Sprintf("%d apps", len(x.apps)),
		})
		t.Rows = append(t.Rows, []store.Value{
			text(label), text(timefmt.Duration(int64(x.total))),
			integer(int64(x.total)), integer(int64(len(x.apps))),
		})
	}
	return series, t, nil
}

// Recorder describes the trackz daemon recording to this service's
// database, as far as trackz-viz can tell.
type Recorder struct {
	Running bool
	PID     int // 0 if unknown
}

// RecorderStatus checks whether a trackz daemon is recording to the
// database this service reads: trackz holds a lock on <database>.lock while
// it runs. A daemon writing to a different database counts as not running.
func (s *Service) RecorderStatus() (Recorder, error) {
	held, pid, err := lockfile.Held(lockfile.ForDatabase(s.DBPath()))
	return Recorder{Running: held, PID: pid}, err
}

// SavedQueries gives access to the saved custom queries, kept in
// "saved_queries" next to the config file (e.g. ~/.config/trackz/saved_queries).
func (s *Service) SavedQueries() queries.Store {
	return queries.Store{Dir: filepath.Join(filepath.Dir(s.configPath), "saved_queries")}
}

// CustomQuery runs a user-supplied SELECT on a read-only connection.
func (s *Service) CustomQuery(ctx context.Context, query string) (*store.Table, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is empty")
	}
	return s.ro.Query(ctx, query)
}

// topN sorts by value (descending, then label) and keeps the first n
// (all when n <= 0).
func topN(pts []Point, n int) []Point {
	sort.SliceStable(pts, func(i, j int) bool {
		if pts[i].Value != pts[j].Value {
			return pts[i].Value > pts[j].Value
		}
		return pts[i].Label < pts[j].Label
	})
	if n > 0 && len(pts) > n {
		pts = pts[:n]
	}
	return pts
}

func text(s string) store.Value   { return store.Value{Kind: store.Text, Text: s} }
func integer(i int64) store.Value { return store.Value{Kind: store.Integer, Int: i} }
func formatNumber(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}
