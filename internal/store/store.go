// Package store is the model layer: all access to the Trackz SQLite database.
//
// The schema is the original one from trackz_schema.sql, unchanged. All
// timestamps are Unix epoch seconds (UTC); conversion to a display time zone
// is the job of the presentation layers.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/solworktech/trackz"
)

// Values written for sleep/lock periods. They match what the original Rust
// viewer's sleep listener wrote, so existing queries keep working.
const (
	SleepProcessName = "sleep"
	SleepAppName     = "sleep_mode"
	SleepWindowName  = "System Sleep"
	SleepOwner       = "system"

	// Records written by trackz-viz for a status the user set ("Out to
	// lunch", ...): app_name is trackz-viz and window_name the status. The
	// process name tells them apart from the daemon's records of the
	// trackz-viz window itself.
	StatusProcessName = "trackz-status"
	StatusAppName     = "trackz-viz"
)

// Store wraps a database handle.
type Store struct {
	db       *sql.DB
	path     string
	readOnly bool
}

// Event is a single focus event as recorded by the tracker.
type Event struct {
	ProcessName  string
	AppName      string
	WindowName   string
	ProcessOwner string
}

// Record is a row of the trackz table.
type Record struct {
	ID           int64
	ProcessName  string
	AppName      string
	WindowName   string
	Start        int64
	End          int64 // 0 when the record is still open
	ProcessOwner string
}

// Duration returns End-Start in seconds, or 0 for an open record.
func (r Record) Duration() int64 {
	if r.End == 0 {
		return 0
	}
	return r.End - r.Start
}

// Open opens (creating if necessary) the database at path for reading and
// writing, and creates the trackz table if it does not exist.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	s, err := open(ctx, path, false)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureSchema(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// OpenReadOnly opens an existing database with writes disabled
// (PRAGMA query_only). It is used for user-supplied queries.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return open(ctx, path, true)
}

func open(ctx context.Context, path string, readOnly bool) (*Store, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection: SQLite serialises writers anyway, and per-connection
	// PRAGMAs below then apply to every statement.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	pragmas := []string{"PRAGMA busy_timeout = 5000"}
	if readOnly {
		pragmas = append(pragmas, "PRAGMA query_only = 1")
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("open database %s: %w", path, err)
		}
	}
	return &Store{db: db, path: path, readOnly: readOnly}, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// EnsureSchema creates the trackz table from trackz_schema.sql if missing.
func (s *Store) EnsureSchema(ctx context.Context) error {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'trackz'`).Scan(&n)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, trackz.Schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// InsertEvent inserts an open record (focus_end_time NULL) and returns its id.
func (s *Store) InsertEvent(ctx context.Context, e Event, start int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO trackz (process_name, app_name, window_name, focus_start_time, focus_end_time, process_owner)
		 VALUES (?, ?, ?, ?, NULL, ?)`,
		e.ProcessName, e.AppName, e.WindowName, start, e.ProcessOwner)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	return res.LastInsertId()
}

// InsertSleep inserts an open sleep/lock record and returns its id.
func (s *Store) InsertSleep(ctx context.Context, start int64) (int64, error) {
	return s.InsertEvent(ctx, Event{
		ProcessName:  SleepProcessName,
		AppName:      SleepAppName,
		WindowName:   SleepWindowName,
		ProcessOwner: SleepOwner,
	}, start)
}

// SetFocusEnd closes record id at end.
func (s *Store) SetFocusEnd(ctx context.Context, id, end int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE trackz SET focus_end_time = ? WHERE id = ?`, end, id)
	if err != nil {
		return fmt.Errorf("update event %d: %w", id, err)
	}
	return nil
}

// CloseOrphan closes record id at end if it is still open and started no
// later than end. It reports whether the record was changed.
func (s *Store) CloseOrphan(ctx context.Context, id, end int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE trackz SET focus_end_time = ? WHERE id = ? AND focus_end_time IS NULL AND focus_start_time <= ?`,
		end, id, end)
	if err != nil {
		return false, fmt.Errorf("close record %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("close record %d: %w", id, err)
	}
	return n == 1, nil
}

// OpenStatus returns the newest status record that has not ended, if any
// (ok is false when there is none).
func (s *Store) OpenStatus(ctx context.Context) (r Record, ok bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM trackz
		WHERE process_name = ? AND focus_end_time IS NULL ORDER BY id DESC LIMIT 1`, StatusProcessName)
	r, err = scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return r, true, nil
}

// Get returns the record with the given id.
func (s *Store) Get(ctx context.Context, id int64) (Record, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM trackz WHERE id = ?`, id)
	return scanRecord(row)
}

// Records returns the records whose focus started within [from, to] (Unix
// seconds, inclusive) and lasted more than minDuration seconds, ordered by
// start time.
//
// The tracker's newest record, if still open (the window focused right
// now, or the current sleep/lock period), is included with End = now, so
// reports cover the present; so is an open status record. Older open
// records (left behind when the tracker was killed) are ignored, as their
// real end is unknown.
func (s *Store) Records(ctx context.Context, from, to, minDuration, now int64) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, process_name, app_name, window_name, focus_start_time,
		        COALESCE(focus_end_time, ?), COALESCE(process_owner, '')
		 FROM trackz
		 WHERE (focus_end_time IS NOT NULL
		        OR id = (SELECT MAX(id) FROM trackz WHERE process_name <> ?)
		        OR process_name = ?)
		   AND (COALESCE(focus_end_time, ?) - focus_start_time) > ?
		   AND focus_start_time >= ? AND focus_start_time <= ?
		 ORDER BY focus_start_time, id`,
		now, StatusProcessName, StatusProcessName, now, minDuration, from, to)
	if err != nil {
		return nil, fmt.Errorf("query records: %w", err)
	}
	defer func() { _ = rows.Close() }() // errors surface through rows.Err
	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const recordColumns = `id, process_name, app_name, window_name, focus_start_time,
	COALESCE(focus_end_time, 0), COALESCE(process_owner, '')`

type scanner interface{ Scan(dest ...any) error }

func scanRecord(sc scanner) (Record, error) {
	var r Record
	err := sc.Scan(&r.ID, &r.ProcessName, &r.AppName, &r.WindowName, &r.Start, &r.End, &r.ProcessOwner)
	if err != nil {
		return Record{}, fmt.Errorf("read record: %w", err)
	}
	return r, nil
}

// ErrNotSelect is returned by Query for statements that are not queries.
var ErrNotSelect = errors.New("only SELECT (or WITH ... SELECT) queries are allowed")

// Query runs a user-supplied read-only query and returns all rows. The
// statement must start with SELECT or WITH; when the store was opened with
// OpenReadOnly, SQLite itself also rejects any write.
func (s *Store) Query(ctx context.Context, query string, args ...any) (*Table, error) {
	q := strings.ToUpper(strings.TrimSpace(stripLeadingComments(query)))
	if !strings.HasPrefix(q, "SELECT") && !strings.HasPrefix(q, "WITH") {
		return nil, ErrNotSelect
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // errors surface through rows.Err
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, errors.New("query must return at least one column")
	}
	t := &Table{Columns: cols}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]Value, len(cols))
		for i, v := range raw {
			row[i] = toValue(v)
		}
		t.Rows = append(t.Rows, row)
	}
	return t, rows.Err()
}

func stripLeadingComments(q string) string {
	for {
		q = strings.TrimSpace(q)
		switch {
		case strings.HasPrefix(q, "--"):
			if i := strings.IndexByte(q, '\n'); i >= 0 {
				q = q[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(q, "/*"):
			if i := strings.Index(q, "*/"); i >= 0 {
				q = q[i+2:]
				continue
			}
			return ""
		}
		return q
	}
}
