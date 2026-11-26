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
	"github.com/solworktech/trackz/internal/groups"
	"github.com/solworktech/trackz/internal/lockfile"
	"github.com/solworktech/trackz/internal/queries"
	"github.com/solworktech/trackz/internal/store"
	"github.com/solworktech/trackz/internal/timefmt"
	"github.com/solworktech/trackz/internal/titles"
)

// Service is the controller used by frontends. It is safe for concurrent
// use, so a frontend can run queries on a background goroutine.
type Service struct {
	st         *store.Store // read-write: predefined reports
	ro         *store.Store // read-only: user-supplied queries
	configPath string
	now        func() time.Time

	mu     sync.RWMutex
	cfg    config.Config
	titles *titles.Normalizer // cfg.TitleRules, compiled
	groups *groups.Matcher    // cfg.TaskGroups, compiled (nil if invalid)
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
	s := &Service{st: st, ro: ro, configPath: cfgPath, now: opts.Now}
	if s.now == nil {
		s.now = time.Now
	}
	if err := s.setConfig(cfg); err != nil {
		// Invalid task groups are kept (and reported), unused. Load falls
		// back to the default title rules for invalid ones, so they do not
		// fail here; keep going with the defaults if they do.
		cfgErr = errors.Join(cfgErr, err)
		if s.titles == nil {
			cfg.TitleRules = config.Default().TitleRules
			if err := s.setConfig(cfg); err != nil && s.titles == nil {
				return nil, errors.Join(err, s.Close())
			}
		}
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
	return s.setConfig(cfg)
}

// ModifyConfig changes the settings with change, applied to the config
// file as it is now (so changes made to it by hand since it was read, such
// as task groups, are kept), and saves and applies the result.
func (s *Service) ModifyConfig(change func(config.Config) (config.Config, error)) (config.Config, error) {
	base, err := config.Load(s.configPath)
	if err != nil {
		base = s.Config() // unreadable: start from the settings in use
	}
	cfg, err := change(base)
	if err != nil {
		return cfg, err
	}
	return cfg, s.UpdateConfig(cfg)
}

// ReloadConfig reads the config file again (it may have been edited by
// hand) and applies it. A file that cannot be parsed leaves the current
// settings in place; invalid task groups are applied unused (see
// setConfig) and reported.
func (s *Service) ReloadConfig() error {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	return s.setConfig(cfg)
}

// setConfig makes cfg current, with its title rules and task groups
// compiled. Invalid title rules leave the configuration unchanged; invalid
// task groups are kept but not used (no window is in a group) and
// reported.
func (s *Service) setConfig(cfg config.Config) error {
	n, err := titles.New(cfg.TitleRules)
	if err != nil {
		return err
	}
	g, gErr := groups.New(cfg.TaskGroups)
	if gErr != nil {
		g, gErr = nil, fmt.Errorf("task groups (not used until fixed): %w", gErr)
	}
	s.mu.Lock()
	s.cfg, s.titles, s.groups = cfg, n, g
	s.mu.Unlock()
	store.SetTitleNormalizer(n)
	norm := func(t string) string { return t }
	if cfg.MergeTitles {
		norm = n.Normalize
	}
	store.SetTaskGrouper(func(process, title string) string { return g.Group(process, norm(title)) })
	return gErr
}

// TaskGroupsError reports why the configured task groups are not used,
// if they are invalid.
func (s *Service) TaskGroupsError() error {
	cfg := s.Config()
	if len(cfg.TaskGroups) == 0 {
		return nil
	}
	return groups.Validate(cfg.TaskGroups)
}

// NormalizeTitle returns title as reports show it: with the title rules
// applied when merging is on (config MergeTitles), else unchanged.
func (s *Service) NormalizeTitle(title string) string {
	s.mu.Lock()
	n, merge := s.titles, s.cfg.MergeTitles
	s.mu.Unlock()
	if !merge {
		return title
	}
	return n.Normalize(title)
}

// titleFunc returns NormalizeTitle with a cache, for one report: many
// records share a title.
func (s *Service) titleFunc() func(string) string {
	s.mu.Lock()
	n, merge := s.titles, s.cfg.MergeTitles
	s.mu.Unlock()
	if !merge {
		return func(t string) string { return t }
	}
	seen := map[string]string{}
	return func(t string) string {
		out, ok := seen[t]
		if !ok {
			out = n.Normalize(t)
			seen[t] = out
		}
		return out
	}
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

// records returns the records in r that pass f, with the time spent in a
// status taken out of the others (see Status).
func (s *Service) records(ctx context.Context, r Range, f Filter) ([]store.Record, error) {
	from, to := r.Bounds(s.now(), s.Config().Location())
	recs, err := s.st.Records(ctx, from.Unix(), to.Unix(), 0, s.now().Unix())
	if err != nil {
		return nil, err
	}
	title := s.titleFunc()
	return f.apply(clipStatuses(recs), title, s.groupFunc(title))
}

// Apps returns the applications with focus time in r (ignoring any
// filter), most used first: Label and App are the app_name, Process its
// main process (for icons), Value the seconds.
func (s *Service) Apps(ctx context.Context, r Range) ([]Point, error) {
	recs, err := s.records(ctx, r, Filter{})
	if err != nil {
		return nil, err
	}
	pts := appTotals(recs)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Value > pts[j].Value })
	return pts, nil
}

// Run produces the result for one of the predefined views, from the records
// that pass f.
func (s *Service) Run(ctx context.Context, v View, r Range, f Filter) (*Result, error) {
	res := &Result{View: v, Range: r, Filter: f, Generated: s.now()}
	var err error
	switch v {
	case ViewDashboard:
		var d *Dashboard
		d, err = s.Dashboard(ctx, r, f, s.Config().RowLimit)
		if d != nil {
			res.Kind, res.Series, res.Table, res.Hidden = PieChart, d.Series, d.Table, d.Hidden
		}
	case ViewTopApps:
		res.Kind = BarChart
		res.Series, res.Hidden, err = s.topApps(ctx, r, f, s.Config().RowLimit)
	case ViewTimeSpent:
		res.Kind = PieChart
		res.Series, res.Hidden, err = s.timePerApp(ctx, r, f)
	case ViewTaskGroups:
		res.Kind = PieChart
		res.Series, err = s.timePerGroup(ctx, r, f)
	case ViewWindowActivity:
		res.Kind = LineChart
		res.Series, err = s.Activity(ctx, r, f)
	default:
		return nil, fmt.Errorf("view %s has no predefined report", v)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Dashboard returns the top `limit` (process, window) pairs by focus time.
func (s *Service) Dashboard(ctx context.Context, r Range, f Filter, limit int) (*Dashboard, error) {
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return nil, err
	}
	return s.dashboard(r, recs, limit), nil
}

// dashboard adds up recs (in r) per (process, window), keeping the top
// `limit` (all when limit <= 0).
func (s *Service) dashboard(r Range, recs []store.Record, limit int) *Dashboard {
	pts, hidden := s.dropSmall(s.windowTotals(recs))
	pts = topN(pts, limit)
	s.setSpans(r, pts)
	series := Series{Title: "Top activities", Unit: Seconds, Points: pts}

	t := &store.Table{Columns: []string{"process_name", "window_name", "duration", "seconds"}}
	for _, p := range pts {
		t.Rows = append(t.Rows, []store.Value{
			text(p.Process), text(p.Label),
			text(timefmt.Duration(int64(p.Value))), integer(int64(p.Value)),
		})
	}
	return &Dashboard{Series: series, Table: t, Hidden: hidden}
}

// windowTotals adds up recs per (process, title as shown), in the order
// the windows first appear.
func (s *Service) windowTotals(recs []store.Record) []Point {
	title := s.titleFunc()
	type key struct{ proc, window string }
	totals := map[key]*Point{}
	variants := map[key]map[string]bool{} // recorded titles merged into each
	var order []key
	for _, rec := range recs {
		k := key{rec.ProcessName, title(rec.WindowName)}
		p := totals[k]
		if p == nil {
			p = &Point{Label: k.window, Process: rec.ProcessName, App: rec.AppName,
				Item: Item{Process: rec.ProcessName, Title: k.window}}
			totals[k] = p
			variants[k] = map[string]bool{}
			order = append(order, k)
		}
		p.Value += float64(rec.Duration())
		p.span(rec)
		variants[k][rec.WindowName] = true
	}
	pts := make([]Point, 0, len(order))
	for _, k := range order {
		p := *totals[k]
		if n := len(variants[k]); n > 1 {
			p.Detail = fmt.Sprintf("merged from %d titles", n)
		}
		pts = append(pts, p)
	}
	return pts
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
		span  Point // First and Last
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
		a.span.span(rec)
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
		pts = append(pts, Point{Label: app, App: app, Process: best, Value: a.total,
			First: a.span.First, Last: a.span.Last, Item: Item{App: app}})
	}
	return pts
}

func (s *Service) topApps(ctx context.Context, r Range, f Filter, limit int) (Series, Hidden, error) {
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return Series{}, Hidden{}, err
	}
	pts, hidden := s.dropSmall(appTotals(recs))
	pts = topN(pts, limit)
	s.setSpans(r, pts)
	// The number shown: fewer than limit when fewer apps were used.
	return Series{
		Title:  fmt.Sprintf("Top %d apps by total focus time", len(pts)),
		Unit:   Seconds,
		Points: pts,
	}, hidden, nil
}

func (s *Service) timePerApp(ctx context.Context, r Range, f Filter) (Series, Hidden, error) {
	recs, err := s.records(ctx, r, f)
	if err != nil {
		return Series{}, Hidden{}, err
	}
	pts, hidden := s.dropSmall(appTotals(recs))
	pts = topN(pts, 0)
	s.setSpans(r, pts)
	return Series{Title: "Total time spent per app", Unit: Seconds, Points: pts}, hidden, nil
}

// Activity returns focus time over time: hourly buckets for ranges of up to
// two days, daily buckets otherwise. Each record counts in the bucket where
// its focus started. Detail holds the number of window switches.
func (s *Service) Activity(ctx context.Context, r Range, f Filter) (Series, error) {
	recs, err := s.records(ctx, r, f)
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
