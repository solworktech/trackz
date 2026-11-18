package store

import (
	"context"
	"fmt"
	"time"
)

// LegacyFix describes one record changed by FixLegacyTimestamps.
type LegacyFix struct {
	ID                 int64
	OldStart, NewStart int64
	OldEnd, NewEnd     int64 // 0 when NULL
}

// FixLegacyTimestamps corrects rows written by the devilspie2 Lua hooks.
//
// The hooks computed time as os.time(os.date("!*t")): the current UTC wall
// clock interpreted as local time. That stores (true epoch - UTC offset), so
// rows are early by the offset in effect at the time (e.g. one hour during
// British Summer Time, zero in GMT). This adds the offset of loc back.
//
// Only records with id <= maxID are touched, so the command can be limited to
// data collected before switching to the Go tracker. Sleep records (written
// by the Rust viewer with real Unix time) are left alone, as is the end time
// of a record that was closed by the sleep listener (detected by the next
// record being a sleep record starting at exactly that end time).
//
// This is NOT idempotent: running it twice shifts the data twice.
func (s *Store) FixLegacyTimestamps(ctx context.Context, maxID int64, loc *time.Location, dryRun bool) ([]LegacyFix, error) {
	if s.readOnly {
		return nil, fmt.Errorf("store is read-only")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.focus_start_time, COALESCE(t.focus_end_time, 0),
		       EXISTS (SELECT 1 FROM trackz n
		               WHERE n.id = (SELECT MIN(id) FROM trackz WHERE id > t.id)
		                 AND n.app_name = ? AND n.focus_start_time = t.focus_end_time)
		FROM trackz t
		WHERE t.id <= ? AND t.app_name <> ?
		ORDER BY t.id`, SleepAppName, maxID, SleepAppName)
	if err != nil {
		return nil, err
	}
	var fixes []LegacyFix
	for rows.Next() {
		var f LegacyFix
		var endIsReal bool
		if err := rows.Scan(&f.ID, &f.OldStart, &f.OldEnd, &endIsReal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		f.NewStart = f.OldStart + offsetAt(f.OldStart, loc)
		f.NewEnd = f.OldEnd
		if f.OldEnd != 0 && !endIsReal {
			f.NewEnd = f.OldEnd + offsetAt(f.OldEnd, loc)
		}
		if f.NewStart != f.OldStart || f.NewEnd != f.OldEnd {
			fixes = append(fixes, f)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if dryRun || len(fixes) == 0 {
		return fixes, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE trackz SET focus_start_time = ?, focus_end_time = NULLIF(?, 0) WHERE id = ?`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stmt.Close() }() // closed with the transaction anyway
	for _, f := range fixes {
		if _, err := stmt.ExecContext(ctx, f.NewStart, f.NewEnd, f.ID); err != nil {
			return nil, fmt.Errorf("update %d: %w", f.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return fixes, nil
}

// MaxID returns the highest record id, or 0 for an empty table.
func (s *Store) MaxID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM trackz`).Scan(&id)
	return id, err
}

// offsetAt returns the UTC offset (seconds) of loc around a legacy timestamp.
// The stored value is already shifted by that offset, so it is applied to the
// shifted instant; this only differs from the exact answer within an hour of
// a DST transition.
func offsetAt(ts int64, loc *time.Location) int64 {
	_, off := time.Unix(ts, 0).In(loc).Zone()
	return int64(off)
}
