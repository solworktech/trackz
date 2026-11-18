// Package tracker turns window focus/title events and sleep/lock events into
// trackz records. Platform-specific code lives in the backends; this file is
// the platform-independent logic, ported from the devilspie2 Lua hooks.
package tracker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/solworktech/trackz/internal/store"
)

// Window describes the currently focused window.
type Window struct {
	PID         int
	ProcessName string // executable name, e.g. "firefox-esr"
	AppName     string // application name, e.g. "Firefox"
	Title       string // window title
	Owner       string // user owning the process
}

// PowerEvent reports that the user went away (Away=true) or came back.
// Source distinguishes independent signals ("lock", "sleep", ...); the user
// counts as away while any source is active.
type PowerEvent struct {
	Source string
	Away   bool
}

// Sink receives events from a Backend. Implementations must be safe for
// concurrent use: power events may arrive on a different goroutine.
type Sink interface {
	WindowChanged(Window)
	Power(PowerEvent)
}

// Backend watches the windowing system.
type Backend interface {
	// Name identifies the backend in logs.
	Name() string
	// Run delivers events to sink until ctx is cancelled. Some platforms
	// (macOS, GTK) require it to be called on the main OS thread.
	Run(ctx context.Context, sink Sink) error
}

// Recorder is the subset of *store.Store the tracker needs.
type Recorder interface {
	InsertEvent(ctx context.Context, e store.Event, start int64) (int64, error)
	InsertSleep(ctx context.Context, start int64) (int64, error)
	SetFocusEnd(ctx context.Context, id, end int64) error
	CloseOrphan(ctx context.Context, id, end int64) (bool, error)
}

// Tracker implements Sink and writes records.
type Tracker struct {
	rec    Recorder
	idFile string
	hbFile string
	log    *log.Logger
	debug  bool
	now    func() time.Time

	mu      sync.Mutex
	lastID  int64   // open record, 0 if none
	lastWin *Window // window of the open record; nil for a sleep record
	current *Window // most recent focused window, even while away
	away    map[string]bool
	closed  bool
}

// Options configure a Tracker.
type Options struct {
	// LastEventIDFile mirrors the id of the open record, as the Lua hooks
	// did. Empty means DefaultLastEventIDFile(); "-" disables it.
	LastEventIDFile string
	// HeartbeatFile records "<open record id> <unix time>" periodically
	// (see Heartbeat), so a record left open by a crash or kill can be
	// closed at the last time the tracker was alive. Empty disables it.
	HeartbeatFile string
	Logger        *log.Logger
	Debug         bool
	Now           func() time.Time // for tests
}

// DefaultLastEventIDFile is $XDG_RUNTIME_DIR/trackz_last_event_id, falling
// back to the system temporary directory.
func DefaultLastEventIDFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "trackz_last_event_id")
}

// New creates a Tracker. Any stale last-event-id file is removed, as the
// original systemd unit did in ExecStartPre.
func New(rec Recorder, opts Options) *Tracker {
	t := &Tracker{
		rec:    rec,
		idFile: opts.LastEventIDFile,
		hbFile: opts.HeartbeatFile,
		log:    opts.Logger,
		debug:  opts.Debug,
		now:    opts.Now,
		away:   map[string]bool{},
	}
	if t.idFile == "" {
		t.idFile = DefaultLastEventIDFile()
	}
	if t.idFile == "-" {
		t.idFile = ""
	}
	if t.log == nil {
		t.log = log.New(os.Stderr, "trackz: ", log.LstdFlags)
	}
	if t.now == nil {
		t.now = time.Now
	}
	t.removeFile(t.idFile)
	t.recoverOrphan()
	return t
}

// recoverOrphan closes the record a previous run left open (it was killed
// or crashed) at that run's last heartbeat.
func (t *Tracker) recoverOrphan() {
	if t.hbFile == "" {
		return
	}
	data, err := os.ReadFile(t.hbFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.log.Printf("error: couldn't read %s: %v", t.hbFile, err)
		}
		return
	}
	t.removeFile(t.hbFile)
	var id, at int64
	if _, err := fmt.Sscan(string(data), &id, &at); err != nil || id <= 0 || at <= 0 {
		return
	}
	closed, err := t.rec.CloseOrphan(context.Background(), id, at)
	switch {
	case err != nil:
		t.log.Printf("error: %v", err)
	case closed:
		t.log.Printf("closed record %d left open by a previous run at its last heartbeat (%s)",
			id, time.Unix(at, 0).Format(time.RFC3339))
	}
}

// beat records the open record and the current time. Called with t.mu held,
// whenever a record is opened and periodically from Heartbeat.
func (t *Tracker) beat() {
	if t.hbFile == "" || t.lastID == 0 {
		return
	}
	line := fmt.Sprintf("%d %d\n", t.lastID, t.now().Unix())
	if err := os.WriteFile(t.hbFile, []byte(line), 0o600); err != nil {
		t.log.Printf("error: heartbeat: %v", err)
	}
}

// Heartbeat refreshes the heartbeat file every interval until ctx is done.
func (t *Tracker) Heartbeat(ctx context.Context, interval time.Duration) {
	if t.hbFile == "" {
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		t.mu.Lock()
		if !t.closed {
			t.beat()
		}
		t.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func sameRecord(a, b *Window) bool {
	// The Lua hooks compared window title, process name and owner.
	return a != nil && b != nil &&
		a.Title == b.Title && a.ProcessName == b.ProcessName && a.Owner == b.Owner
}

// WindowChanged records a focus or title change of the active window.
func (t *Tracker) WindowChanged(w Window) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if w.ProcessName == "" {
		t.debugf("ignoring window %q: no process name", w.Title)
		return
	}
	cp := w
	t.current = &cp
	if t.isAway() {
		t.debugf("away; not recording window %q", w.Title)
		return
	}
	t.record(&cp)
}

func (t *Tracker) record(w *Window) {
	if t.lastID != 0 && sameRecord(t.lastWin, w) {
		return
	}
	ctx := context.Background()
	now := t.now().Unix()
	t.closeOpen(ctx, now)
	id, err := t.rec.InsertEvent(ctx, store.Event{
		ProcessName:  w.ProcessName,
		AppName:      w.AppName,
		WindowName:   w.Title,
		ProcessOwner: w.Owner,
	}, now)
	if err != nil {
		t.log.Printf("error: %v", err)
		return
	}
	t.lastID, t.lastWin = id, w
	t.writeID()
	t.debugf("insert window '%s (%s)' Owner: '%s'", w.Title, w.ProcessName, w.Owner)
}

// Power records the user going away (lock/sleep) or returning.
func (t *Tracker) Power(e PowerEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	wasAway := t.isAway()
	if e.Away {
		t.away[e.Source] = true
	} else {
		delete(t.away, e.Source)
	}
	isAway := t.isAway()
	t.debugf("power event %s away=%v (away: %v -> %v)", e.Source, e.Away, wasAway, isAway)
	ctx := context.Background()
	now := t.now().Unix()
	switch {
	case !wasAway && isAway:
		t.closeOpen(ctx, now)
		id, err := t.rec.InsertSleep(ctx, now)
		if err != nil {
			t.log.Printf("error: %v", err)
			return
		}
		t.lastID, t.lastWin = id, nil
		t.writeID()
		t.debugf("insert sleep record %d", id)
	case wasAway && !isAway:
		t.closeOpen(ctx, now)
		// Resume tracking the window that is focused now. Without this the
		// time after waking would only be recorded once focus changed.
		if t.current != nil {
			t.record(t.current)
		}
	}
}

func (t *Tracker) isAway() bool { return len(t.away) > 0 }

// closeOpen sets focus_end_time on the open record, if any.
func (t *Tracker) closeOpen(ctx context.Context, now int64) {
	if t.lastID == 0 {
		return
	}
	if err := t.rec.SetFocusEnd(ctx, t.lastID, now); err != nil {
		t.log.Printf("error: %v", err)
	} else {
		t.debugf("update record %d: focus_end_time=%d", t.lastID, now)
	}
	t.lastID, t.lastWin = 0, nil
}

// Close ends the open record and removes the last-event-id file. Further
// events are ignored.
func (t *Tracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closeOpen(context.Background(), t.now().Unix())
	t.closed = true
	t.removeFile(t.idFile)
	t.removeFile(t.hbFile)
}

// removeFile removes path, if set, logging failures other than its absence.
func (t *Tracker) removeFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.log.Printf("error: couldn't remove %s: %v", path, err)
	}
}

func (t *Tracker) writeID() {
	t.beat()
	if t.idFile == "" {
		return
	}
	if err := os.WriteFile(t.idFile, []byte(strconv.FormatInt(t.lastID, 10)), 0o600); err != nil {
		t.log.Printf("error: couldn't write to %s: %v", t.idFile, err)
	}
}

func (t *Tracker) debugf(format string, args ...any) {
	if t.debug {
		_ = t.log.Output(2, fmt.Sprintf(format, args...)) // a failing log has nowhere to report to
	}
}
