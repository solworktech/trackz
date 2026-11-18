//go:build !windows

package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// runtimeDir returns a directory only the current user can write to.
func runtimeDir(name string) (string, error) {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d, nil
	}
	if runtime.GOOS == "darwin" {
		return os.TempDir(), nil // $TMPDIR is per user
	}
	// /tmp is shared: use a directory of our own, and refuse one that
	// someone else created in its place.
	uid := os.Getuid()
	d := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", name, uid))
	if err := os.Mkdir(d, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	fi, err := os.Lstat(d)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != uid || fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is not a private directory of this user", d)
	}
	return d, nil
}

// allowForeground is only needed on Windows.
func allowForeground() {}
