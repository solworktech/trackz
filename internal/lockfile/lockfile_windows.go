//go:build windows

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Windows byte-range locks are mandatory, so the lock covers a byte far
// beyond the file's contents: the PID stays readable.
const lockOffset = 0x7fffffff

func region() *windows.Overlapped {
	return &windows.Overlapped{Offset: lockOffset}
}

func lockWith(f *os.File, flags uint32) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, region())
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

func lock(f *os.File) error      { return lockWith(f, windows.LOCKFILE_EXCLUSIVE_LOCK) }
func tryShared(f *os.File) error { return lockWith(f, 0) }

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, region())
}
