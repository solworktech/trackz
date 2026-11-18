// Package lockfile provides an exclusive, advisory lock on a file, released
// automatically by the OS when the process exits (even if it is killed).
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrLocked is returned by Acquire when another process holds the lock.
var ErrLocked = errors.New("locked by another process")

// Lock is a held lock.
type Lock struct{ f *os.File }

// Acquire takes the lock on path (created if needed) without waiting and
// writes the current PID into it. If another process holds it, the error
// wraps ErrLocked and names that process's PID when known.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		_ = f.Close()
		if errors.Is(err, ErrLocked) {
			if b, rerr := os.ReadFile(path); rerr == nil {
				if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil {
					return nil, fmt.Errorf("%w (PID %d)", ErrLocked, pid)
				}
			}
		}
		return nil, err
	}
	l := &Lock{f: f}
	if err := writePID(f); err != nil {
		_ = l.Release()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return l, nil
}

func writePID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return err
}

// Held reports whether another process holds the lock on path, and that
// process's PID if known. It does not take the lock (it tests it with a
// shared lock, released at once) and does not create the file.
func Held(path string) (held bool, pid int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = f.Close() }() // read-only; the lock goes with it
	switch err := tryShared(f); {
	case errors.Is(err, ErrLocked):
		if b, rerr := os.ReadFile(path); rerr == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		return true, pid, nil
	case err != nil:
		return false, 0, err
	}
	if err := unlock(f); err != nil {
		return false, 0, err
	}
	return false, 0, nil
}

// ForDatabase is the lock file trackz holds while recording to db.
func ForDatabase(db string) string { return db + ".lock" }

// Release unlocks and removes the lock file.
func (l *Lock) Release() error {
	name := l.f.Name()
	err := errors.Join(unlock(l.f), l.f.Close())
	if rerr := os.Remove(name); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		err = errors.Join(err, rerr)
	}
	return err
}
