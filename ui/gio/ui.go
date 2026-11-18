package gio

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/store"
	"github.com/solworktech/trackz/internal/systheme"
	"github.com/solworktech/trackz/internal/timefmt"
	"github.com/solworktech/trackz/internal/titles"
	"github.com/solworktech/trackz/internal/tray"
)

// UI is the Gio frontend state.
type UI struct {
	svc   *report.Service
	opts  []app.Option               // for each new window
	geom  windowGeometry             // of the last window, for the next
	win   atomic.Pointer[app.Window] // nil while closed to the tray
	th    *material.Theme
	icons *iconCache
	logo  *paint.ImageOp

	view    report.View
	tabs    []widget.Clickable
	presets []widget.Clickable // report.Presets, then Custom
	rng     report.Range

	rangeBtn             widget.Clickable // reopens the custom range dialog
	modal                rangeModal
	customFrom, customTo report.Date // last custom range chosen

	tray     *tray.Tray
	daemon   daemonCheck   // guarded by mu
	quitting atomic.Bool   // Quit was chosen
	wake     chan struct{} // reopen the window, or quit, while it is closed

	sysTheme atomic.Int32 // systheme.Theme, updated by a watcher

	rowLimit widget.Editor
	refresh  widget.Clickable

	mu      sync.Mutex
	results map[report.View]*viewData
	cfgGen  int
	dataGen int // guarded by mu; bumped when the data changed (a status)

	filter      report.Filter // applied to the predefined views
	filterDlg   filterDialog
	filterBtn   widget.Clickable
	clearFilter widget.Clickable

	statusBtn widget.Clickable // "Set Status"
	statusDlg statusDialog
	away      awayDialog

	dashPie, spentPie, customPie pieChart
	dashTable, custTbl           tableView
	topBars, customBars          barChart
	activity, customLine         lineChart
	customCols                   columnChart
	exportPath                   widget.Editor
	exportBtn                    widget.Clickable
	exportFor                    report.View
	queryEd                      sqlEditor
	picker                       queryPicker
	saveDlg                      saveDialog
	saveQuery                    widget.Clickable
	runQuery                     widget.Clickable
	chartKind                    widget.Enum
	custom                       customData
	settings                     settingsForm
	statusMu                     sync.Mutex // guards the status bar message
	status                       string
	statusErr                    bool
	statusAt                     time.Time
}

type viewData struct {
	key     string
	res     *report.Result
	err     error
	loading bool
}

type customData struct {
	table     *store.Table
	series    report.Series
	seriesErr error
	err       error
	running   bool
	ran       string
}

type settingsForm struct {
	tz, dateFmt, timeFmt, rows, minDur widget.Editor
	refresh                            widget.Editor
	theme                              widget.Enum
	mergeTitles                        widget.Bool
	titleRules, titleTest              widget.Editor
	defaultRules                       widget.Clickable
	save, reset                        widget.Clickable
	list                               widget.List
	loaded                             bool
}

const defaultQuery = `SELECT process_name, norm_title(window_name) AS label,
       SUM(focus_end_time - focus_start_time) AS duration
FROM trackz
WHERE focus_end_time IS NOT NULL
  AND focus_start_time >= unixepoch('now') - 86400
  AND focus_start_time <= unixepoch('now')
GROUP BY process_name, label
ORDER BY duration DESC
LIMIT 20
`

// New creates the UI for svc. Its windows are created with opts.
func New(svc *report.Service, opts ...app.Option) *UI {
	u := &UI{
		svc:     svc,
		opts:    opts,
		wake:    make(chan struct{}, 1),
		th:      material.NewTheme(),
		icons:   newIconCache(),
		logo:    logoOp(),
		tabs:    make([]widget.Clickable, len(report.Views)),
		presets: make([]widget.Clickable, len(report.Presets)+1),
		rng:     report.Preset(report.Today),
		results: map[report.View]*viewData{},
	}
	// System fonts as before, plus Go Mono (bundled) for the SQL editor.
	u.th.Shaper = text.NewShaper(text.WithCollection(monoFaces()))
	u.th.Face = "sans-serif" // the UI keeps using the system font
	u.rowLimit.SingleLine, u.rowLimit.Submit = true, true
	u.rowLimit.SetText(strconv.Itoa(svc.Config().RowLimit))
	u.exportPath.SingleLine = true
	u.queryEd.ed.SetText(defaultQuery)
	u.chartKind.Value = report.BarChart.String()
	today := svc.Today()
	u.customFrom, u.customTo = today.AddDays(-6), today
	u.settings.theme.Value = svc.Config().Theme
	for _, e := range []*widget.Editor{&u.settings.tz, &u.settings.dateFmt, &u.settings.timeFmt, &u.settings.rows, &u.settings.minDur, &u.settings.refresh, &u.settings.titleTest} {
		e.SingleLine = true
	}
	u.setExportDefault()
	return u
}

// Run shows the window and returns when the program should exit.
//
// With a tray icon, closing the window destroys it but the program keeps
// running in the tray, whose icon opens a new window; Quit in the tray menu
// exits. Without a tray icon, closing the window exits. The UI state (view,
// range, query, ...) lives here rather than in the window, so a reopened
// window shows what the closed one did.
func (u *UI) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.watchDaemon(ctx)
	go systheme.Watch(ctx, func(t systheme.Theme) {
		u.sysTheme.Store(int32(t))
		u.invalidate()
	})
	// Wake up periodically so reports covering the present are refreshed.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				u.invalidate()
			case <-ctx.Done():
				return
			}
		}
	}()
	u.tray = u.startTray()
	if u.tray != nil {
		defer u.tray.Close()
	}
	go u.resumeStatus()
	defer u.endStatusOnExit()
	for {
		err := u.runWindow()
		if err != nil || u.quitting.Load() || !u.tray.Active() {
			return err
		}
		// Drop requests to show the window made while it was open.
		select {
		case <-u.wake:
		default:
		}
		<-u.wake // the tray asks for the window again, or to quit
		if u.quitting.Load() {
			return nil
		}
	}
}

// runWindow creates a window and processes its events until it is closed.
func (u *UI) runWindow() error {
	w := new(app.Window)
	w.Option(append(u.opts, u.geom.sizeOption()...)...)
	u.win.Store(w)
	// The mode (maximised, ...) is set once the window is shown: on X11
	// the window manager then also knows its normal size.
	pendingMode := u.geom.mode
	defer u.win.Store(nil)
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.ConfigEvent:
			u.geom.update(e.Config.Size, platformWindowMode(e.Config.Mode))
		case app.FrameEvent:
			u.geom.metric = e.Metric
			if pendingMode == app.Maximized || pendingMode == app.Fullscreen {
				if !platformSetMode(pendingMode) {
					w.Option(pendingMode.Option())
				}
			}
			pendingMode = app.Windowed
			gtx := app.NewContext(&ops, e)
			u.layout(gtx)
			e.Frame(gtx.Ops)
		default:
			handlePlatformEvent(e) // e.g. set the X11 window icon
		}
	}
}

// invalidate redraws the window, if it is open. Safe from any goroutine.
func (u *UI) invalidate() {
	if w := u.win.Load(); w != nil {
		w.Invalidate()
	}
}

// signal wakes Run while the window is closed.
func (u *UI) signal() {
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

func (u *UI) setStatus(err bool, format string, args ...any) {
	u.statusMu.Lock()
	u.status, u.statusErr, u.statusAt = fmt.Sprintf(format, args...), err, time.Now()
	u.statusMu.Unlock()
}

// setStatusAsync is setStatus for other goroutines: it also redraws.
func (u *UI) setStatusAsync(err bool, format string, args ...any) {
	u.setStatus(err, format, args...)
	u.invalidate()
}

func (u *UI) setExportDefault() {
	name := strings.ToLower(strings.ReplaceAll(u.view.String(), " ", "-"))
	day := u.svc.Today().String()
	file := fmt.Sprintf("trackz-%s-%s.csv", name, day)
	if home, err := os.UserHomeDir(); err == nil {
		file = filepath.Join(home, file)
	} // else: just the name, which exports to the working directory
	u.exportPath.SetText(file)
	u.exportFor = u.view
}

// resultKey identifies the inputs of a report, so results are recomputed
// when the range or the settings change (and never shown for other inputs).
func (u *UI) resultKey() string {
	u.mu.Lock()
	gen := u.dataGen
	u.mu.Unlock()
	return fmt.Sprintf("%d|%v|%v|%v|%d|%d|%q", u.rng.Kind, u.rng.FromDay, u.rng.ToDay, u.view, u.cfgGen, gen, u.filter.Key())
}

// ensure starts loading the current view's report if needed.
func (u *UI) ensure(force bool) {
	v := u.view
	if v == report.ViewCustomQuery || v == report.ViewSettings {
		return
	}
	key := u.resultKey()
	u.mu.Lock()
	d := u.results[v]
	fresh := d != nil && d.key == key && (d.loading || d.err != nil ||
		(d.res != nil && !u.live(d.res)))
	if fresh && !force {
		u.mu.Unlock()
		return
	}
	nd := &viewData{key: key, loading: true}
	if d != nil && d.key == key {
		nd.res = d.res // keep showing the previous data while reloading
	}
	d = nd
	u.results[v] = d
	u.mu.Unlock()
	rng, filter := u.rng, u.filter
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		res, err := u.svc.Run(ctx, v, rng, filter)
		if res != nil {
			for _, p := range res.Series.Points {
				u.icons.prefetch(p)
			}
		}
		u.mu.Lock()
		if u.results[v] == d { // not superseded
			d.res, d.err, d.loading = res, err, false
		}
		u.mu.Unlock()
		u.invalidate()
	}()
}

// live reports whether res covers the present and is due for its
// automatic refresh (every Config.RefreshMinutes; 0 disables it).
func (u *UI) live(res *report.Result) bool {
	every := time.Duration(u.svc.Config().RefreshMinutes) * time.Minute
	if every <= 0 || time.Since(res.Generated) < every {
		return false
	}
	if res.Range.Kind != report.Custom {
		return true
	}
	return !res.Range.ToDay.Before(u.svc.Today())
}

func (u *UI) current() (*report.Result, error, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	d := u.results[u.view]
	if d == nil {
		return nil, nil, true
	}
	return d.res, d.err, d.loading
}

func (u *UI) handleInput(gtx C) {
	for i := range u.tabs {
		if u.tabs[i].Clicked(gtx) && u.view != report.Views[i] {
			u.view = report.Views[i]
			u.setExportDefault()
			if u.view == report.ViewSettings {
				u.settings.loaded = false
			}
		}
	}
	for i := range u.presets {
		if !u.presets[i].Clicked(gtx) {
			continue
		}
		if i < len(report.Presets) {
			u.rng = report.Preset(report.Presets[i])
			u.ensure(true) // clicking a range always shows current data
		} else {
			u.openRangeModal()
		}
	}
	if u.rangeBtn.Clicked(gtx) {
		u.openRangeModal()
	}
	if u.filterBtn.Clicked(gtx) {
		u.openFilterDialog()
	}
	if u.clearFilter.Clicked(gtx) {
		u.applyFilter(report.Filter{})
	}
	if u.statusBtn.Clicked(gtx) {
		u.openStatusDialog()
	}
	for {
		ev, ok := u.rowLimit.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			u.applyRowLimit()
		}
	}
	if u.refresh.Clicked(gtx) {
		u.applyRowLimit()
		u.ensure(true)
	}
	if u.exportBtn.Clicked(gtx) {
		u.export()
	}
	if u.runQuery.Clicked(gtx) {
		u.runCustom()
	}
	if u.view == report.ViewCustomQuery {
		u.handlePicker(gtx)
		if u.saveQuery.Clicked(gtx) {
			u.openSaveDialog()
		}
	}
	if u.chartKind.Update(gtx) {
		u.updateCustomSeries()
	}
}

func (u *UI) applyRowLimit() {
	n, err := strconv.Atoi(strings.TrimSpace(u.rowLimit.Text()))
	cfg := u.svc.Config()
	if err != nil || n < 1 || n > 10000 {
		u.setStatus(true, "Rows must be a number between 1 and 10000")
		u.rowLimit.SetText(strconv.Itoa(cfg.RowLimit))
		return
	}
	if n == cfg.RowLimit {
		return
	}
	cfg.RowLimit = n
	if err := u.svc.UpdateConfig(cfg); err != nil {
		u.setStatus(true, "Could not save settings: %v", err)
		return
	}
	u.cfgGen++
}

func (u *UI) exportTable() *store.Table {
	if u.view == report.ViewCustomQuery {
		return u.custom.table
	}
	res, _, _ := u.current()
	if res == nil {
		return nil
	}
	if res.Table != nil {
		return res.Table
	}
	return report.TableFromSeries(res.Series)
}

func (u *UI) export() {
	t := u.exportTable()
	if t == nil {
		u.setStatus(true, "Nothing to export yet")
		return
	}
	path := strings.TrimSpace(u.exportPath.Text())
	if path == "" {
		u.setStatus(true, "Enter a file name to export to")
		return
	}
	written, err := report.ExportCSV(config.ExpandPath(path), t)
	if err != nil {
		u.setStatus(true, "Export failed: %v", err)
		return
	}
	u.setStatus(false, "Exported %d rows to %s", len(t.Rows), written)
}

func (u *UI) runCustom() {
	if u.custom.running {
		return
	}
	q := u.queryEd.ed.Text()
	u.custom.running = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		t, err := u.svc.CustomQuery(ctx, q)
		if t != nil {
			if s, err := report.SeriesFromTable(t); err == nil {
				for _, p := range s.Points {
					u.icons.prefetch(p)
				}
			}
		}
		u.mu.Lock()
		u.custom.table, u.custom.err, u.custom.running, u.custom.ran = t, err, false, q
		u.mu.Unlock()
		u.updateCustomSeries()
		u.invalidate()
	}()
}

func (u *UI) updateCustomSeries() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.custom.table == nil {
		return
	}
	u.custom.series, u.custom.seriesErr = report.SeriesFromTable(u.custom.table)
}

// currentScheme picks light or dark from the settings and, for "system",
// from the desktop preference.
func (u *UI) currentScheme() *scheme {
	switch u.svc.Config().Theme {
	case config.ThemeDark:
		return &darkScheme
	case config.ThemeLight:
		return &lightScheme
	}
	if systheme.Theme(u.sysTheme.Load()) == systheme.Dark {
		return &darkScheme
	}
	return &lightScheme
}

func (u *UI) layout(gtx C) D {
	if sc := u.currentScheme(); sc != cs {
		applyScheme(u.th, sc)
	}
	paint.Fill(gtx.Ops, cs.bg)
	u.handleInput(gtx)
	u.ensure(false)
	d := u.layoutMain(gtx)
	if u.modal.open {
		u.layoutRangeModal(gtx)
	}
	if u.saveDlg.open {
		u.layoutSaveDialog(gtx)
	}
	if u.filterDlg.open {
		u.layoutFilterDialog(gtx)
	}
	if u.statusDlg.open {
		u.layoutStatusDialog(gtx)
	}
	if u.daemonState().dialog {
		u.layoutDaemonDialog(gtx)
	}
	u.layoutAwayDialog(gtx) // on top of everything while a status is set
	return d
}

func (u *UI) layoutMain(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.layoutTabs),
		layout.Rigid(func(gtx C) D {
			if u.view == report.ViewCustomQuery || u.view == report.ViewSettings {
				return D{}
			}
			return u.layoutRangeBar(gtx)
		}),
		layout.Flexed(1, func(gtx C) D {
			return layout.Inset{Left: 12, Right: 12, Top: 8, Bottom: 4}.Layout(gtx, u.layoutContent)
		}),
		layout.Rigid(u.layoutStatus),
	)
}

func (u *UI) layoutTabs(gtx C) D {
	return layout.Inset{Left: 8, Right: 8, Top: 14, Bottom: 4}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
					return layoutIcon(gtx, u.logo, 30)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 16}.Layout(gtx, func(gtx C) D {
					l := material.H6(u.th, "Trackz")
					l.Font.Weight = 700
					return l.Layout(gtx)
				})
			}),
		}
		for i, v := range report.Views {
			i, v := i, v
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
					return segButton(gtx, u.th, &u.tabs[i], v.String(), u.view == v)
				})
			}))
		}
		children = append(children,
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
			layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &u.statusBtn, "Set Away Status") }),
		)
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

func (u *UI) layoutRangeBar(gtx C) D {
	return layout.Inset{Left: 8, Right: 8, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
		var children []layout.FlexChild
		for i, k := range append(append([]report.RangeKind{}, report.Presets...), report.Custom) {
			i, k := i, k
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
					label := k.String()
					if k == report.Custom {
						label += "…"
					}
					return segButton(gtx, u.th, &u.presets[i], label, u.rng.Kind == k)
				})
			}))
		}
		if u.rng.Kind == report.Custom {
			cfg := u.svc.Config()
			loc := cfg.Location()
			label := cfg.FormatDate(u.rng.FromDay.In(loc)) + " – " + cfg.FormatDate(u.rng.ToDay.In(loc)) + " ▾"
			children = append(children, layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8, Right: 4}.Layout(gtx, func(gtx C) D {
					return segButton(gtx, u.th, &u.rangeBtn, label, false)
				})
			}))
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				label := "Filter…"
				if u.filter.Active() {
					label = "Filter (on)…"
				}
				return segButton(gtx, u.th, &u.filterBtn, label, u.filter.Active())
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 6, Top: 8}.Layout(gtx, material.Body2(u.th, "Rows").Layout)
			}),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = gtx.Dp(56)
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return bordered(gtx, func(gtx C) D {
					return layout.UniformInset(7).Layout(gtx, material.Editor(u.th, &u.rowLimit, "20").Layout)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return smallButton(gtx, u.th, &u.refresh, "Refresh") })
			}),
		)
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

func (u *UI) heading(gtx C, title string) D {
	res, _, loading := u.current()
	return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
		layout.Rigid(material.H6(u.th, title).Layout),
		layout.Rigid(func(gtx C) D {
			txt := u.svc.RangeLabel(u.rng)
			if loading {
				txt += " · loading…"
			} else if res != nil {
				txt += " · updated " + timefmt.Format(res.Generated.In(u.svc.Location()), u.svc.Config().TimeFormat)
			}
			if res != nil && res.Hidden.Count > 0 {
				noun := "entries"
				if res.Hidden.Count == 1 {
					noun = "entry"
				}
				txt += fmt.Sprintf(" · %d %s under %ds in total hidden (%s; see Settings)",
					res.Hidden.Count, noun, res.Hidden.MinDuration, timefmt.Duration(res.Hidden.Seconds))
			}
			l := material.Body2(u.th, "   "+txt)
			l.Color = cs.muted
			return l.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx C) D {
			if !u.filter.Active() {
				return D{Size: image.Pt(gtx.Constraints.Min.X, 0)}
			}
			// What the data leaves out is easy to forget: say so in the
			// accent colour, with a way back.
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D {
						return truncLabel(gtx, u.th, 14, cs.active, "Filtered: "+u.filter.Describe())
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
						return segButton(gtx, u.th, &u.clearFilter, "Clear filter", false)
					})
				}),
			)
		}),
	)
}

func (u *UI) layoutContent(gtx C) D {
	switch u.view {
	case report.ViewCustomQuery:
		return u.layoutCustom(gtx)
	case report.ViewSettings:
		return u.layoutSettings(gtx)
	}
	res, err, _ := u.current()
	body := func(gtx C) D {
		switch {
		case err != nil:
			l := material.Body1(u.th, "Error: "+err.Error())
			l.Color = cs.err
			return l.Layout(gtx)
		case res == nil:
			return D{Size: gtx.Constraints.Max}
		}
		switch u.view {
		case report.ViewDashboard:
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Flexed(0.55, func(gtx C) D { return u.dashPie.layout(gtx, u.th, u.icons, res.Series) }),
				layout.Rigid(layout.Spacer{Height: 8}.Layout),
				layout.Flexed(0.45, func(gtx C) D { return u.dashTable.layout(gtx, u.th, res.Table, u.dashboardColumns(res)) }),
				layout.Rigid(u.layoutExport),
			)
		case report.ViewTopApps:
			return u.topBars.layout(gtx, u.th, u.icons, res.Series)
		case report.ViewTimeSpent:
			return u.spentPie.layout(gtx, u.th, u.icons, res.Series)
		case report.ViewWindowActivity:
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return u.activity.layout(gtx, u.th, res.Series) }),
				layout.Rigid(u.layoutExport),
			)
		}
		return D{}
	}
	title := u.view.String()
	if res != nil && res.Series.Title != "" {
		title = res.Series.Title
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.heading(gtx, title) }),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Flexed(1, body),
	)
}

func (u *UI) layoutExport(gtx C) D {
	if u.exportFor != u.view {
		u.setExportDefault()
	}
	return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, material.Body2(u.th, "Export to").Layout)
			}),
			layout.Flexed(1, func(gtx C) D {
				return bordered(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.UniformInset(7).Layout(gtx, material.Editor(u.th, &u.exportPath, "file.csv").Layout)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return smallButton(gtx, u.th, &u.exportBtn, "Export CSV") })
			}),
		)
	})
}

func (u *UI) layoutCustom(gtx C) D {
	u.mu.Lock()
	cd := u.custom
	u.mu.Unlock()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.H6(u.th, "Custom query").Layout),
		layout.Rigid(func(gtx C) D {
			l := material.Caption(u.th, "Any SELECT on the trackz table; it runs on a read-only connection. Times are Unix epoch seconds.")
			l.Color = cs.muted
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: 8}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Right: 8}.Layout(gtx, material.Body2(u.th, "Saved query").Layout)
				}),
				layout.Rigid(u.layoutPicker),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.Y = gtx.Dp(150)
			gtx.Constraints.Max.Y = gtx.Dp(150)
			return bordered(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				return layout.UniformInset(8).Layout(gtx, func(gtx C) D {
					return u.queryEd.layout(gtx, u.th, "SELECT …")
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
				children := []layout.FlexChild{
					layout.Rigid(func(gtx C) D {
						label := "Run query"
						if cd.running {
							label = "Running…"
						}
						return smallButton(gtx, u.th, &u.runQuery, label)
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &u.saveQuery, "Save query", false) }),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
				}
				for _, k := range report.ChartKinds {
					k := k
					children = append(children, layout.Rigid(func(gtx C) D {
						return material.RadioButton(u.th, &u.chartKind, k.String(), k.String()).Layout(gtx)
					}))
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			switch {
			case cd.err != nil:
				l := material.Body1(u.th, "Error: "+cd.err.Error())
				l.Color = cs.err
				return l.Layout(gtx)
			case cd.table == nil:
				l := material.Body2(u.th, "Run a query to see results.")
				l.Color = cs.muted
				return l.Layout(gtx)
			}
			kind := report.TableView
			for _, k := range report.ChartKinds {
				if k.String() == u.chartKind.Value {
					kind = k
				}
			}
			if kind != report.TableView && cd.seriesErr != nil {
				l := material.Body1(u.th, cd.seriesErr.Error())
				l.Color = cs.err
				return l.Layout(gtx)
			}
			switch kind {
			case report.BarChart:
				return u.customBars.layout(gtx, u.th, u.icons, cd.series)
			case report.LineChart:
				return u.customLine.layout(gtx, u.th, cd.series)
			case report.PieChart:
				return u.customPie.layout(gtx, u.th, u.icons, cd.series)
			case report.TimeSeriesChart:
				return u.customCols.layout(gtx, u.th, cd.series)
			}
			return u.custTbl.layout(gtx, u.th, cd.table, nil)
		}),
		layout.Rigid(func(gtx C) D {
			if cd.table == nil {
				return D{}
			}
			return u.layoutExport(gtx)
		}),
	)
}

func (u *UI) loadSettings() {
	cfg := u.svc.Config()
	u.settings.tz.SetText(cfg.TimeZone)
	u.settings.dateFmt.SetText(cfg.DateFormat)
	u.settings.timeFmt.SetText(cfg.TimeFormat)
	u.settings.rows.SetText(strconv.Itoa(cfg.RowLimit))
	u.settings.minDur.SetText(strconv.Itoa(cfg.MinDurationSeconds))
	u.settings.refresh.SetText(strconv.Itoa(cfg.RefreshMinutes))
	u.settings.theme.Value = cfg.Theme
	u.settings.mergeTitles.Value = cfg.MergeTitles
	u.settings.titleRules.SetText(titles.Format(cfg.TitleRules))
	u.settings.loaded = true
}

func (u *UI) formSettings() (config.Config, error) {
	cfg := u.svc.Config()
	cfg.TimeZone = strings.TrimSpace(u.settings.tz.Text())
	cfg.DateFormat = u.settings.dateFmt.Text()
	cfg.TimeFormat = u.settings.timeFmt.Text()
	var err error
	if cfg.RowLimit, err = strconv.Atoi(strings.TrimSpace(u.settings.rows.Text())); err != nil {
		return cfg, fmt.Errorf("dashboard rows must be a number")
	}
	if cfg.MinDurationSeconds, err = strconv.Atoi(strings.TrimSpace(u.settings.minDur.Text())); err != nil {
		return cfg, fmt.Errorf("minimum duration must be a number of seconds")
	}
	if cfg.RefreshMinutes, err = strconv.Atoi(strings.TrimSpace(u.settings.refresh.Text())); err != nil {
		return cfg, fmt.Errorf("refresh interval must be a number of minutes")
	}
	cfg.Theme = u.settings.theme.Value
	cfg.MergeTitles = u.settings.mergeTitles.Value
	if cfg.TitleRules, err = titles.Parse(u.settings.titleRules.Text()); err != nil {
		return cfg, fmt.Errorf("title rules, %w", err)
	}
	return cfg, cfg.Validate()
}

func (u *UI) layoutSettings(gtx C) D {
	s := &u.settings
	if !s.loaded {
		u.loadSettings()
	}
	if s.reset.Clicked(gtx) {
		u.loadSettings()
	}
	if s.defaultRules.Clicked(gtx) {
		s.titleRules.SetText(titles.Format(titles.DefaultRules))
	}
	if s.save.Clicked(gtx) {
		cfg, err := u.formSettings()
		if err == nil {
			err = u.svc.UpdateConfig(cfg)
		}
		if err != nil {
			u.setStatus(true, "Settings not saved: %v", err)
		} else {
			u.cfgGen++
			u.rowLimit.SetText(strconv.Itoa(cfg.RowLimit))
			u.setStatus(false, "Settings saved to %s", u.svc.ConfigPath())
		}
	}
	preview := ""
	if cfg, err := u.formSettings(); err == nil {
		preview = "Now: " + cfg.FormatDateTime(time.Now())
	} else {
		preview = err.Error()
	}
	rows := []layout.Widget{
		material.H6(u.th, "Settings").Layout,
		func(gtx C) D {
			return field(gtx, u.th, "Time zone (IANA name such as Europe/London, or Local)", &s.tz, "Local", 320)
		},
		func(gtx C) D {
			return field(gtx, u.th, "Date format (strftime, e.g. %Y-%m-%d or %d/%m/%Y)", &s.dateFmt, "%Y-%m-%d", 320)
		},
		func(gtx C) D {
			return field(gtx, u.th, "Time format (strftime, e.g. %H:%M:%S or %I:%M %p)", &s.timeFmt, "%H:%M:%S", 320)
		},
		func(gtx C) D {
			l := material.Body2(u.th, preview)
			l.Color = cs.muted
			return l.Layout(gtx)
		},
		func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					l := material.Caption(u.th, "Appearance")
					l.Color = cs.muted
					return l.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(material.RadioButton(u.th, &s.theme, config.ThemeSystem, "Follow system").Layout),
						layout.Rigid(material.RadioButton(u.th, &s.theme, config.ThemeLight, "Light").Layout),
						layout.Rigid(material.RadioButton(u.th, &s.theme, config.ThemeDark, "Dark").Layout),
					)
				}),
			)
		},
		func(gtx C) D { return field(gtx, u.th, "Rows (Dashboard and Top Apps)", &s.rows, "20", 120) },
		func(gtx C) D {
			return field(gtx, u.th, "Refresh views that include today every (minutes; 0 = off)", &s.refresh, "5", 120)
		},
		func(gtx C) D {
			return field(gtx, u.th, "Hide entries totalling less than (seconds)", &s.minDur, "120", 120)
		},
		func(gtx C) D { return u.layoutTitleSettings(gtx) },
		func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &s.save, "Save") }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return smallButton(gtx, u.th, &s.reset, "Revert") }),
			)
		},
		func(gtx C) D {
			l := material.Caption(u.th, "Database: "+u.svc.DBPath()+"\nConfig file: "+u.svc.ConfigPath())
			l.Color = cs.muted
			return l.Layout(gtx)
		},
	}
	s.list.Axis = layout.Vertical
	return material.List(u.th, &s.list).Layout(gtx, len(rows), func(gtx C, i int) D {
		return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, rows[i])
	})
}

func (u *UI) layoutStatus(gtx C) D {
	var lines []layout.FlexChild
	u.statusMu.Lock()
	msg, isErr, at := u.status, u.statusErr, u.statusAt
	u.statusMu.Unlock()
	if msg != "" && time.Since(at) <= 15*time.Second {
		lines = append(lines, layout.Rigid(func(gtx C) D {
			l := material.Body2(u.th, msg)
			l.Color = cs.ok
			if isErr {
				l.Color = cs.err
			}
			return l.Layout(gtx)
		}))
	}
	// Stays until trackz is found recording again.
	if d := u.daemonState(); d.known && !d.running {
		msg, col := u.daemonMessage(d), cs.err
		if d.waiting {
			msg, col = "Waiting for the trackz recorder to start…", cs.muted
		}
		lines = append(lines, layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					sz := gtx.Dp(10)
					fillRect(gtx, image.Rect(0, 0, sz, sz), col)
					return D{Size: image.Pt(sz+gtx.Dp(8), sz)}
				}),
				layout.Flexed(1, func(gtx C) D {
					l := material.Body2(u.th, msg)
					l.Color = col
					l.MaxLines = 2
					return l.Layout(gtx)
				}),
			)
		}))
	}
	if len(lines) == 0 {
		return D{}
	}
	return layout.Inset{Left: 12, Right: 12, Bottom: 6, Top: 2}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, lines...)
	})
}

// layoutTitleSettings is the part of Settings about merging window titles
// that differ only in notification counts.
func (u *UI) layoutTitleSettings(gtx C) D {
	s := &u.settings
	caption := func(txt string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			l := material.Caption(u.th, txt)
			l.Color = cs.muted
			return layout.Inset{Bottom: 5}.Layout(gtx, l.Layout)
		})
	}
	// Live preview of the rules as typed.
	preview := ""
	if rules, err := titles.Parse(s.titleRules.Text()); err != nil {
		preview = "Title rules, " + err.Error()
	} else if n, err := titles.New(rules); err != nil {
		preview = err.Error()
	} else if in := s.titleTest.Text(); in != "" {
		preview = "→ " + n.Normalize(in)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.CheckBox(u.th, &s.mergeTitles,
			"Merge window titles that differ only in notification counts, e.g. “(3) YouTube” and “YouTube”").Layout),
		caption("Title rules, applied in order: one per line, “regular expression => replacement” (leave the replacement out to delete the match; # starts a comment). Recorded titles are not changed; custom queries can use norm_title(window_name)."),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(720))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return bordered(gtx, func(gtx C) D {
				return layout.UniformInset(8).Layout(gtx, func(gtx C) D {
					e := material.Editor(u.th, &s.titleRules, "")
					e.Font = monoFont
					e.TextSize = 13
					return e.Layout(gtx)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return segButton(gtx, u.th, &s.defaultRules, "Default rules", false) }),
				layout.Rigid(layout.Spacer{Width: 12}.Layout),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.X = gtx.Dp(320)
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return bordered(gtx, func(gtx C) D {
						return layout.UniformInset(7).Layout(gtx, material.Editor(u.th, &s.titleTest, "Try a title, e.g. (3) YouTube").Layout)
					})
				}),
				layout.Flexed(1, func(gtx C) D {
					l := material.Body2(u.th, preview)
					if strings.HasPrefix(preview, "→") {
						l.Color = cs.fg
					} else {
						l.Color = cs.err
					}
					l.MaxLines = 2
					return layout.Inset{Left: 10}.Layout(gtx, l.Layout)
				}),
			)
		}),
	)
}

// windowGeometry remembers the size and mode (maximised, full screen) of
// the window, so the window reopened from the tray looks as it was closed.
type windowGeometry struct {
	size   image.Point    // in the windowed mode, in pixels
	mode   app.WindowMode // Windowed, Maximized or Fullscreen
	metric unit.Metric    // of the last frame, to convert size to dp
	known  bool
}

func (g *windowGeometry) update(size image.Point, mode app.WindowMode) {
	switch mode {
	case app.Windowed:
		if size.X > 0 && size.Y > 0 {
			g.size, g.known = size, true
		}
		g.mode = app.Windowed
	case app.Maximized, app.Fullscreen:
		g.mode = mode
	} // Minimized: keep what it was before
}

// sizeOption returns the option restoring the windowed size (none before
// the first window has reported it). It applies to a maximised window too,
// so un-maximising it goes back to that size.
func (g *windowGeometry) sizeOption() []app.Option {
	if !g.known || g.metric.PxPerDp <= 0 {
		return nil
	}
	return []app.Option{app.Size(
		unit.Dp(float32(g.size.X)/g.metric.PxPerDp),
		unit.Dp(float32(g.size.Y)/g.metric.PxPerDp),
	)}
}

// dashboardColumns shows the dashboard table as Process Name (with the
// application's icon), Window Title and Duration, sorting the duration by
// its seconds. The table's other columns (seconds) are for the CSV export.
func (u *UI) dashboardColumns(res *report.Result) []tableColumn {
	t := res.Table
	proc, win := t.ColumnIndex("process_name"), t.ColumnIndex("window_name")
	dur, secs := t.ColumnIndex("duration"), t.ColumnIndex("seconds")
	if proc < 0 || win < 0 || dur < 0 || secs < 0 {
		return nil
	}
	pts := res.Series.Points
	return []tableColumn{
		{title: "Process Name", src: proc, sortBy: proc, weight: 2.2, icon: func(row int) *paint.ImageOp {
			if row < len(pts) { // the table's rows are the series' points
				return u.icons.get(pts[row])
			}
			return nil
		}},
		{title: "Window Title", src: win, sortBy: win, weight: 6},
		{title: "Duration", src: dur, sortBy: secs, weight: 1.3, right: true},
	}
}
