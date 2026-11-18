package tracker

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/solworktech/trackz/internal/store"
)

type fakeRow struct {
	store.Event
	start, end int64
}

type fakeRecorder struct{ rows []fakeRow }

func (f *fakeRecorder) InsertEvent(_ context.Context, e store.Event, start int64) (int64, error) {
	f.rows = append(f.rows, fakeRow{Event: e, start: start})
	return int64(len(f.rows)), nil
}

func (f *fakeRecorder) InsertSleep(ctx context.Context, start int64) (int64, error) {
	return f.InsertEvent(ctx, store.Event{ProcessName: store.SleepProcessName, AppName: store.SleepAppName,
		WindowName: store.SleepWindowName, ProcessOwner: store.SleepOwner}, start)
}

func (f *fakeRecorder) SetFocusEnd(_ context.Context, id, end int64) error {
	f.rows[id-1].end = end
	return nil
}

func (f *fakeRecorder) CloseOrphan(_ context.Context, id, end int64) (bool, error) {
	if id < 1 || int(id) > len(f.rows) || f.rows[id-1].end != 0 || f.rows[id-1].start > end {
		return false, nil
	}
	f.rows[id-1].end = end
	return true, nil
}

type clock struct{ t int64 }

func (c *clock) now() time.Time { return time.Unix(c.t, 0) }

func newTest(t *testing.T) (*Tracker, *fakeRecorder, *clock, string) {
	rec := &fakeRecorder{}
	clk := &clock{t: 1000}
	idFile := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(idFile, []byte("999"), 0o600); err != nil { // stale file from a previous run
		t.Fatal(err)
	}
	tr := New(rec, Options{LastEventIDFile: idFile, Logger: log.New(io.Discard, "", 0), Now: clk.now})
	return tr, rec, clk, idFile
}

func win(proc, title string) Window {
	return Window{ProcessName: proc, AppName: proc, Title: title, Owner: "jesse"}
}

func TestStaleIDFileRemoved(t *testing.T) {
	_, _, _, idFile := newTest(t)
	if _, err := os.Stat(idFile); !os.IsNotExist(err) {
		t.Fatalf("stale id file should be removed, stat err=%v", err)
	}
}

func TestFocusChangesCloseAndInsert(t *testing.T) {
	tr, rec, clk, idFile := newTest(t)
	tr.WindowChanged(win("firefox-esr", "A"))
	clk.t = 1010
	tr.WindowChanged(win("firefox-esr", "A")) // duplicate: ignored
	clk.t = 1020
	tr.WindowChanged(win("lxterminal", "B"))
	clk.t = 1030
	tr.WindowChanged(Window{Title: "no process"}) // ignored, like the Lua hooks
	if len(rec.rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rec.rows), rec.rows)
	}
	if r := rec.rows[0]; r.start != 1000 || r.end != 1020 || r.WindowName != "A" {
		t.Errorf("row 1 = %+v", r)
	}
	if r := rec.rows[1]; r.start != 1020 || r.end != 0 {
		t.Errorf("row 2 = %+v", r)
	}
	if b, _ := os.ReadFile(idFile); string(b) != "2" {
		t.Errorf("id file = %q, want 2", b)
	}
	clk.t = 1040
	tr.Close()
	if rec.rows[1].end != 1040 {
		t.Errorf("Close should end open record, got %+v", rec.rows[1])
	}
	if _, err := os.Stat(idFile); !os.IsNotExist(err) {
		t.Error("Close should remove id file")
	}
	tr.WindowChanged(win("x", "after close"))
	if len(rec.rows) != 2 {
		t.Error("events after Close must be ignored")
	}
}

func TestOwnerChangeStartsNewRecord(t *testing.T) {
	tr, rec, _, _ := newTest(t)
	w := win("xterm", "T")
	tr.WindowChanged(w)
	w.Owner = "root"
	tr.WindowChanged(w)
	if len(rec.rows) != 2 {
		t.Fatalf("owner change should create a record, rows=%d", len(rec.rows))
	}
}

func TestSleepAndResume(t *testing.T) {
	tr, rec, clk, _ := newTest(t)
	tr.WindowChanged(win("code", "main.go"))
	clk.t = 1100
	tr.Power(PowerEvent{Source: "lock", Away: true})
	clk.t = 1150
	tr.Power(PowerEvent{Source: "sleep", Away: true}) // already away: no new record
	tr.WindowChanged(win("slack", "general"))         // focus changes while locked
	clk.t = 1200
	tr.Power(PowerEvent{Source: "sleep", Away: false}) // still locked
	if len(rec.rows) != 2 {
		t.Fatalf("rows=%d, want 2 while locked: %+v", len(rec.rows), rec.rows)
	}
	clk.t = 1300
	tr.Power(PowerEvent{Source: "lock", Away: false})
	if len(rec.rows) != 3 {
		t.Fatalf("rows=%d, want 3: %+v", len(rec.rows), rec.rows)
	}
	if r := rec.rows[0]; r.end != 1100 {
		t.Errorf("window record should end at lock: %+v", r)
	}
	if r := rec.rows[1]; r.AppName != store.SleepAppName || r.start != 1100 || r.end != 1300 {
		t.Errorf("sleep record = %+v", r)
	}
	if r := rec.rows[2]; r.WindowName != "general" || r.start != 1300 {
		t.Errorf("after unlock the current window should be recorded: %+v", r)
	}
	// Same window as before the lock: still recorded again after resume.
	tr.Power(PowerEvent{Source: "lock", Away: true})
	tr.Power(PowerEvent{Source: "lock", Away: false})
	if len(rec.rows) != 5 || rec.rows[4].WindowName != "general" {
		t.Errorf("resume must restart the current window: %+v", rec.rows)
	}
}

func TestHeartbeatRecoversOrphan(t *testing.T) {
	dir := t.TempDir()
	hb := filepath.Join(dir, "hb")
	rec := &fakeRecorder{}
	clk := &clock{t: 1000}
	opts := Options{LastEventIDFile: "-", HeartbeatFile: hb, Logger: log.New(io.Discard, "", 0), Now: clk.now}

	// First run: records a window, beats, then is "killed" (no Close).
	tr := New(rec, opts)
	tr.WindowChanged(win("wezterm-gui", "trackz-go-git"))
	clk.t = 1090
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // write one beat and return
	tr.Heartbeat(ctx, time.Hour)
	if b, _ := os.ReadFile(hb); string(b) != "1 1090\n" {
		t.Fatalf("heartbeat file = %q", b)
	}

	// Second run, 20 minutes later: the orphan is closed at the last beat.
	clk.t = 2300
	New(rec, opts)
	if rec.rows[0].end != 1090 {
		t.Errorf("orphan end = %d, want 1090 (last heartbeat)", rec.rows[0].end)
	}
	if _, err := os.Stat(hb); !os.IsNotExist(err) {
		t.Error("heartbeat file should be consumed")
	}
}
